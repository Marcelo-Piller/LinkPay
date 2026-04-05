const USED_KEYS_STORAGE = "ledgerpay.used-operation-keys";
const LAST_PAYMENT_STORAGE = "linkpay.last-selected-payment";
const OPERATION_KEY_PATTERN = /^[A-Za-z0-9_-]{8,64}$/;

const state = {
    selectedPaymentId: "",
    usedOperationKeys: loadUsedOperationKeys(),
};

const els = {
    introSplash: document.querySelector("#intro-splash"),
    createForm: document.querySelector("#create-payment-form"),
    idempotencyKey: document.querySelector("#idempotency-key"),
    keyFeedback: document.querySelector("#key-feedback"),
    amount: document.querySelector("#amount"),
    currency: document.querySelector("#currency"),
    payerId: document.querySelector("#payer-id"),
    merchantId: document.querySelector("#merchant-id"),
    lookupForm: document.querySelector("#lookup-payment-form"),
    lookupPaymentId: document.querySelector("#lookup-payment-id"),
    reverseForm: document.querySelector("#reverse-payment-form"),
    reversePaymentId: document.querySelector("#reverse-payment-id"),
    reverseReason: document.querySelector("#reverse-reason"),
    paymentSummary: document.querySelector("#payment-summary"),
    receiptCard: document.querySelector("#receipt-card"),
    ledgerRows: document.querySelector("#ledger-rows"),
    refreshLedger: document.querySelector("#refresh-ledger"),
    generateIdem: document.querySelector("#generate-idem"),
    submitPayment: document.querySelector("#submit-payment"),
    submitReversal: document.querySelector("#submit-reversal"),
};

function boot() {
    startIntroSplash();
    setGeneratedOperationKey();
    renderKeyFeedback("A chave sera validada antes do envio.", false);
    wireEvents();
    bootStatementPage();
}

function startIntroSplash() {
    if (!els.introSplash) {
        return;
    }

    document.body.classList.add("is-intro-active");

    const prefersReducedMotion = window.matchMedia?.("(prefers-reduced-motion: reduce)")?.matches;
    const splashDuration = prefersReducedMotion ? 140 : 2500;

    const dismissSplash = () => {
        if (!els.introSplash || els.introSplash.classList.contains("is-exit")) {
            return;
        }

        els.introSplash.classList.add("is-exit");
        window.setTimeout(() => {
            if (!els.introSplash) {
                return;
            }

            els.introSplash.hidden = true;
            document.body.classList.remove("is-intro-active");
        }, prefersReducedMotion ? 0 : 540);
    };

    window.setTimeout(dismissSplash, splashDuration);
    els.introSplash.addEventListener("click", dismissSplash, { once: true });
    window.addEventListener("keydown", (event) => {
        if (event.key === "Escape") {
            dismissSplash();
        }
    }, { once: true });
}

function wireEvents() {
    if (els.generateIdem) {
        els.generateIdem.addEventListener("click", () => {
            setGeneratedOperationKey();
            renderKeyFeedback("Nova chave unica gerada para esta operacao.", false, true);
        });
    }

    if (els.idempotencyKey) {
        els.idempotencyKey.addEventListener("input", () => {
            const validationMessage = validateOperationKeyInput(els.idempotencyKey.value);
            if (validationMessage) {
                renderKeyFeedback(validationMessage, true);
                return;
            }

            if (els.idempotencyKey.value.trim()) {
                renderKeyFeedback("Chave valida para uma nova operacao.", false, true);
            }
        });
    }

    if (els.refreshLedger) {
        els.refreshLedger.addEventListener("click", async () => {
            toggleButton(els.refreshLedger, true, "Atualizando...");

            try {
                if (!state.selectedPaymentId) {
                    renderLedger([]);
                    renderReceipt({
                        tone: "error",
                        kicker: "Atenção",
                        title: "Selecione uma transacao antes de atualizar o extrato",
                        message: "Consulte um pagamento para carregar as movimentacoes desta operacao.",
                        badge: "Pendente",
                    });
                    return;
                }

                await loadLedger(state.selectedPaymentId);
                renderReceipt({
                    tone: "success",
                    kicker: "Extrato atualizado",
                    title: "Movimentacoes atualizadas com sucesso",
                    message: "As movimentacoes mais recentes desta transacao ja estao visiveis na tela.",
                    badge: "Atualizado",
                    items: [
                        { label: "Codigo da transacao", value: state.selectedPaymentId },
                        { label: "Momento da consulta", value: formatDate(new Date().toISOString()) },
                    ],
                });
            } catch (error) {
                renderError("Falha ao atualizar o extrato", error);
            } finally {
                toggleButton(els.refreshLedger, false, "Atualizar extrato");
            }
        });
    }

    if (els.createForm) {
        els.createForm.addEventListener("submit", handleCreatePayment);
    }

    if (els.lookupForm) {
        els.lookupForm.addEventListener("submit", handleLookupPayment);
    }

    if (els.reverseForm) {
        els.reverseForm.addEventListener("submit", handleReversePayment);
    }
}

function setGeneratedOperationKey() {
    if (!els.idempotencyKey) {
        return;
    }

    let nextKey = "";

    do {
        nextKey = `pag_${randomToken().replaceAll("-", "").slice(0, 18)}`;
    } while (hasStoredOperationKey(nextKey));

    els.idempotencyKey.value = nextKey;
}

async function handleCreatePayment(event) {
    event.preventDefault();

    const payload = {
        amount: Number(els.amount.value),
        currency: els.currency.value.trim().toUpperCase(),
        payerId: els.payerId.value.trim(),
        merchantId: els.merchantId.value.trim(),
    };

    const operationKey = els.idempotencyKey.value.trim();
    const keyValidationMessage = validateOperationKeyInput(operationKey);
    if (keyValidationMessage) {
        renderKeyFeedback(keyValidationMessage, true);
        renderReceipt({
            tone: "error",
            kicker: "Chave invalida",
            title: "Revise a chave unica antes de continuar",
            message: keyValidationMessage,
            badge: "Erro",
        });
        return;
    }

    toggleButton(els.submitPayment, true, "Confirmando...");

    try {
        const response = await apiRequest("/api/payments", {
            method: "POST",
            headers: {
                "Content-Type": "application/json",
                "Idempotency-Key": operationKey,
            },
            body: JSON.stringify(payload),
        });

        registerOperationKey(operationKey, response.id);
        renderKeyFeedback("Chave registrada com sucesso para esta operacao.", false, true);
        syncSelectedPayment(response);
        renderReceipt({
            tone: "success",
            kicker: "Pagamento enviado",
            title: "Operacao recebida com sucesso",
            message: "O pagamento foi enviado para processamento. Agora voce pode acompanhar o andamento da transacao.",
            badge: formatStatusLabel(response.status),
            items: [
                { label: "Codigo da transacao", value: response.id },
                { label: "Chave unica", value: operationKey },
                { label: "Valor", value: formatMoney(response.amount, response.currency) },
                { label: "Horário", value: formatDate(response.createdAtUtc) },
            ],
        });

        await loadLedger(response.id);
        setGeneratedOperationKey();
    } catch (error) {
        if (isOperationKeyError(error)) {
            renderKeyFeedback(normalizeErrorMessage(error), true);
        }

        renderError("Falha ao criar pagamento", error);
    } finally {
        toggleButton(els.submitPayment, false, "Confirmar pagamento");
    }
}

async function handleLookupPayment(event) {
    event.preventDefault();

    const paymentId = els.lookupPaymentId.value.trim();
    if (!paymentId) {
        return;
    }

    const lookupButton = els.lookupForm.querySelector("button");
    toggleButton(lookupButton, true, "Consultando...");

    try {
        const response = await apiRequest(`/api/payments/${paymentId}`);
        syncSelectedPayment(response);
        renderReceipt({
            tone: "success",
            kicker: "Consulta realizada",
            title: "Transacao localizada",
            message: "Os dados mais recentes desta transacao foram carregados para acompanhamento.",
            badge: formatStatusLabel(response.status),
            items: [
                { label: "Codigo da transacao", value: response.id },
                { label: "Pagador", value: response.payerId },
                { label: "Recebedor", value: response.merchantId },
                { label: "Atualizado em", value: formatDate(response.updatedAtUtc) },
            ],
        });

        await loadLedger(paymentId);
    } catch (error) {
        renderError("Falha ao consultar transacao", error);
    } finally {
        toggleButton(lookupButton, false, "Consultar");
    }
}

async function handleReversePayment(event) {
    event.preventDefault();

    const paymentId = els.reversePaymentId.value.trim();
    const reason = els.reverseReason.value.trim();

    if (!paymentId || !reason) {
        renderReceipt({
            tone: "error",
            kicker: "Dados incompletos",
            title: "Informe os dados do estorno",
            message: "Para solicitar o estorno, preencha o codigo da transacao e o motivo do pedido.",
            badge: "Erro",
        });
        return;
    }

    toggleButton(els.submitReversal, true, "Solicitando...");

    try {
        const response = await apiRequest(`/api/payments/${paymentId}/reverse`, {
            method: "POST",
            headers: {
                "Content-Type": "application/json",
            },
            body: JSON.stringify({ reason }),
        });

        renderReceipt({
            tone: "success",
            kicker: "Estorno solicitado",
            title: "Pedido de estorno recebido",
            message: "A reversao foi encaminhada. O status da transacao sera atualizado assim que o processamento terminar.",
            badge: "Em analise",
            items: [
                { label: "Codigo da transacao", value: paymentId },
                { label: "Motivo informado", value: reason },
                { label: "Momento da solicitacao", value: formatDate(new Date().toISOString()) },
                { label: "Resposta", value: response ? "Retorno recebido" : "Solicitacao aceita" },
            ],
        });

        if (paymentId) {
            setTimeout(() => {
                lookupPaymentById(paymentId).catch((error) => {
                    renderError("Falha ao atualizar o status apos o estorno", error);
                });
            }, 1500);
        }
    } catch (error) {
        renderError("Falha ao solicitar estorno", error);
    } finally {
        toggleButton(els.submitReversal, false, "Solicitar estorno");
    }
}

async function lookupPaymentById(paymentId) {
    const response = await apiRequest(`/api/payments/${paymentId}`);
    syncSelectedPayment(response);
    await loadLedger(paymentId);
}

async function loadLedger(paymentId) {
    const rows = await apiRequest(`/api/ledger/payment/${paymentId}`);
    renderLedger(rows);
    return rows;
}

function syncSelectedPayment(payment) {
    state.selectedPaymentId = payment.id;
    persistLastSelectedPayment(payment.id);

    if (els.lookupPaymentId) {
        els.lookupPaymentId.value = payment.id;
    }

    if (els.reversePaymentId) {
        els.reversePaymentId.value = payment.id;
    }

    renderPaymentSummary(payment);
}

function renderPaymentSummary(payment) {
    if (!els.paymentSummary) {
        return;
    }

    const lastReason = payment.lastReason?.trim()
        ? payment.lastReason
        : "Sem observacoes adicionais para esta transacao.";

    els.paymentSummary.innerHTML = `
        <div class="summary-head">
            <div>
                <span class="panel-kicker">Resumo da transacao</span>
                <h3 class="summary-value">${formatMoney(payment.amount, payment.currency)}</h3>
            </div>
            <span class="summary-status ${statusClassName(payment.status)}">${escapeHtml(formatStatusLabel(payment.status))}</span>
        </div>
        <div class="summary-grid">
            <div class="summary-stat">
                <span>Codigo da transacao</span>
                <strong>${escapeHtml(payment.id)}</strong>
            </div>
            <div class="summary-stat">
                <span>Moeda</span>
                <strong>${escapeHtml(payment.currency)}</strong>
            </div>
            <div class="summary-stat">
                <span>Pagador</span>
                <strong>${escapeHtml(payment.payerId)}</strong>
            </div>
            <div class="summary-stat">
                <span>Recebedor</span>
                <strong>${escapeHtml(payment.merchantId)}</strong>
            </div>
            <div class="summary-stat">
                <span>Criado em</span>
                <strong>${formatDate(payment.createdAtUtc)}</strong>
            </div>
            <div class="summary-stat">
                <span>Ultima atualizacao</span>
                <strong>${formatDate(payment.updatedAtUtc)}</strong>
            </div>
        </div>
        <p class="summary-note">${escapeHtml(lastReason)}</p>
    `;
}

function renderLedger(entries) {
    if (!els.ledgerRows) {
        return;
    }

    if (!Array.isArray(entries) || entries.length === 0) {
        els.ledgerRows.innerHTML = `
            <tr>
                <td colspan="5" class="table-empty">Ainda nao existem movimentacoes disponiveis para esta transacao.</td>
            </tr>
        `;
        return;
    }

    els.ledgerRows.innerHTML = entries.map((entry) => {
        const operationLabel = entry.operation === "Reversal" ? "Estorno" : "Pagamento";
        const movementClass = entry.operation === "Reversal" ? "movement-pill movement-pill-reversal" : "movement-pill";

        return `
            <tr>
                <td>${formatDate(entry.createdAtUtc)}</td>
                <td>${escapeHtml(operationLabel)} • <span class="movement-type">${escapeHtml(formatEntryType(entry.entryType))}</span></td>
                <td>${escapeHtml(formatAccountLabel(entry.account))}</td>
                <td><span class="${movementClass}">${escapeHtml(operationLabel)}</span></td>
                <td>${formatMoney(entry.amount, entry.currency)}</td>
            </tr>
        `;
    }).join("");
}

function renderReceipt(model) {
    if (!els.receiptCard) {
        return;
    }

    const items = Array.isArray(model.items) ? model.items : [];
    const receiptTone = model.tone === "error" ? "receipt-card is-error" : "receipt-card";
    const receiptStatusClass = model.tone === "error" ? "receipt-status" : `receipt-status ${statusClassName(model.badge)}`;

    els.receiptCard.className = receiptTone;
    els.receiptCard.innerHTML = `
        <div class="receipt-header">
            <div>
                <span class="card-kicker">${escapeHtml(model.kicker || "Comprovante")}</span>
                <h3 class="receipt-title">${escapeHtml(model.title || "Operacao registrada")}</h3>
                <p class="receipt-message">${escapeHtml(model.message || "Acompanhe os detalhes abaixo.")}</p>
            </div>
            <span class="${receiptStatusClass}">${escapeHtml(model.badge || "Informativo")}</span>
        </div>
        <div class="receipt-grid">
            ${items.map((item) => `
                <div class="receipt-item">
                    <span>${escapeHtml(item.label)}</span>
                    <strong>${escapeHtml(item.value)}</strong>
                </div>
            `).join("")}
        </div>
    `;
}

function renderError(title, error) {
    const friendlyMessage = normalizeErrorMessage(error);
    renderReceipt({
        tone: "error",
        kicker: "Nao foi possivel concluir",
        title,
        message: friendlyMessage,
        badge: "Erro",
        items: [
            { label: "Momento", value: formatDate(new Date().toISOString()) },
        ],
    });
}

async function apiRequest(url, options = {}) {
    const response = await fetch(url, options);
    const raw = await response.text();

    let data = null;
    if (raw) {
        try {
            data = JSON.parse(raw);
        } catch {
            data = raw;
        }
    }

    if (!response.ok) {
        const message = typeof data === "object" && data !== null
            ? data.message || JSON.stringify(data)
            : raw || `HTTP ${response.status}`;

        const error = new Error(message);
        error.details = data;
        error.status = response.status;
        throw error;
    }

    return data;
}

function toggleButton(button, loading, label) {
    if (!button) {
        return;
    }

    button.disabled = loading;
    button.textContent = label;
}

function renderKeyFeedback(message, isError, isValid = false) {
    if (!els.keyFeedback) {
        return;
    }

    els.keyFeedback.textContent = message;
    els.keyFeedback.className = "field-feedback";

    if (isError) {
        els.keyFeedback.classList.add("is-error");
        return;
    }

    if (isValid) {
        els.keyFeedback.classList.add("is-valid");
    }
}

function validateOperationKeyInput(value) {
    const key = value.trim();

    if (!key) {
        return "Informe a chave unica da operacao.";
    }

    if (!OPERATION_KEY_PATTERN.test(key)) {
        return "A chave unica deve ter entre 8 e 64 caracteres e usar apenas letras, numeros, hifen ou underscore.";
    }

    if (hasStoredOperationKey(key)) {
        return "Essa chave ja foi utilizada neste navegador. Gere outra para uma nova operacao.";
    }

    return "";
}

function loadUsedOperationKeys() {
    try {
        const raw = localStorage.getItem(USED_KEYS_STORAGE);
        if (!raw) {
            return {};
        }

        const parsed = JSON.parse(raw);
        return parsed && typeof parsed === "object" ? parsed : {};
    } catch {
        return {};
    }
}

function persistUsedOperationKeys() {
    try {
        localStorage.setItem(USED_KEYS_STORAGE, JSON.stringify(state.usedOperationKeys));
    } catch {
        // Ignore storage failures and keep the validation fallback on the server side.
    }
}

function bootStatementPage() {
    if (document.body.dataset.page !== "statement") {
        return;
    }

    const lastSelectedPayment = loadLastSelectedPayment();
    if (!lastSelectedPayment) {
        return;
    }

    state.selectedPaymentId = lastSelectedPayment;
    if (els.lookupPaymentId) {
        els.lookupPaymentId.value = lastSelectedPayment;
    }

    lookupPaymentById(lastSelectedPayment).then(() => {
        renderReceipt({
            tone: "success",
            kicker: "Extrato pronto",
            title: "Ultima transacao carregada automaticamente",
            message: "A pagina abriu com a ultima transacao consultada para agilizar sua consulta.",
            badge: "Atualizado",
            items: [
                { label: "Codigo da transacao", value: lastSelectedPayment },
                { label: "Momento", value: formatDate(new Date().toISOString()) },
            ],
        });
    }).catch(() => {
        renderReceipt({
            tone: "error",
            kicker: "Extrato indisponivel",
            title: "Nao foi possivel carregar a ultima transacao automaticamente",
            message: "Informe o codigo da transacao para consultar o extrato manualmente.",
            badge: "Erro",
        });
    });
}

function persistLastSelectedPayment(paymentId) {
    try {
        localStorage.setItem(LAST_PAYMENT_STORAGE, paymentId);
    } catch {
        // Ignore storage failures; the page still works with manual lookup.
    }
}

function loadLastSelectedPayment() {
    try {
        return localStorage.getItem(LAST_PAYMENT_STORAGE) || "";
    } catch {
        return "";
    }
}

function registerOperationKey(key, paymentId) {
    state.usedOperationKeys[key.toLowerCase()] = {
        paymentId,
        registeredAt: new Date().toISOString(),
    };
    persistUsedOperationKeys();
}

function hasStoredOperationKey(key) {
    return Boolean(state.usedOperationKeys[key.trim().toLowerCase()]);
}

function normalizeErrorMessage(error) {
    const rawMessage = String(error?.message || "Erro inesperado").trim();

    if (rawMessage.includes("already used with a different payload")) {
        return "Essa chave unica ja foi usada com outros dados. Gere uma nova chave para continuar.";
    }

    if (rawMessage.includes("Payment not found")) {
        return "Nao localizamos a transacao informada. Revise o codigo e tente novamente.";
    }

    if (rawMessage.includes("must be in Posted status")) {
        return "Somente transacoes liquidadas podem seguir para estorno.";
    }

    return rawMessage;
}

function isOperationKeyError(error) {
    const message = String(error?.message || "");
    return message.includes("Idempotency-Key") || message.toLowerCase().includes("chave unica");
}

function randomToken() {
    if (window.crypto?.randomUUID) {
        return window.crypto.randomUUID();
    }

    return `${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

function formatMoney(value, currency = "BRL") {
    const numericValue = Number(value ?? 0);
    return new Intl.NumberFormat("pt-BR", {
        style: "currency",
        currency,
        minimumFractionDigits: 2,
    }).format(Number.isFinite(numericValue) ? numericValue : 0);
}

function formatDate(value) {
    if (!value) {
        return "Sem horario";
    }

    return new Intl.DateTimeFormat("pt-BR", {
        dateStyle: "short",
        timeStyle: "short",
    }).format(new Date(value));
}

function formatStatusLabel(status) {
    const labels = {
        PendingRisk: "Em analise",
        Approved: "Aprovado",
        Rejected: "Recusado",
        Posted: "Liquidado",
        ReversalRequested: "Estorno solicitado",
        Reversed: "Estornado",
        Atualizado: "Atualizado",
        Informativo: "Informativo",
        Erro: "Erro",
    };

    return labels[status] || status || "Informativo";
}

function statusClassName(status) {
    const normalized = String(status || "")
        .replaceAll(" ", "")
        .toLowerCase();

    return normalized ? `status-${normalized}` : "";
}

function formatAccountLabel(account) {
    const labels = {
        CustomerCashAccount: "Conta do cliente",
        MerchantSettlementAccount: "Conta do recebedor",
    };

    return labels[account] || account;
}

function formatEntryType(entryType) {
    const labels = {
        Debit: "Debito",
        Credit: "Credito",
    };

    return labels[entryType] || entryType;
}

function escapeHtml(value) {
    return String(value ?? "")
        .replaceAll("&", "&amp;")
        .replaceAll("<", "&lt;")
        .replaceAll(">", "&gt;")
        .replaceAll('"', "&quot;")
        .replaceAll("'", "&#39;");
}

boot();
