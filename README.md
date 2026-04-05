# Ledgerpay

`Ledgerpay` e uma plataforma de transacoes de exemplo, desenhada para simular um pipeline de pagamentos no estilo bancario com lancamentos contabeis, padroes de resiliencia e observabilidade completa.

## Problema e Contexto

Sistemas transacionais financeiros exigem garantias fortes de consistencia, trilha de auditoria, seguranca em reprocessamento e visibilidade operacional. Este projeto demonstra um fluxo realista de pagamentos com APIs sincronas, coreografia assicrona por eventos e razao contabil com debito/credito.

Fluxo principal:

1. O cliente cria uma `Payment Intent`.
2. O `risk-worker` avalia e emite a decisao.
3. O `ledger-api` faz os lancamentos contabeis quando aprovado.
4. O `notifications-worker` envia notificacoes de confirmacao/reprovacao.
5. O `payments-api` mantem as transicoes de estado no estilo saga.

## Arquitetura

- `frontend` (Golang Web UI + BFF): entrega a interface verde e branca, gera JWT localmente para o ambiente de desenvolvimento e orquestra chamadas para `payments-api` e `ledger-api`.
- `payments-api` (.NET 8 Web API): cria/consulta/estorna intents de pagamento, idempotencia, outbox e estado da saga.
- `risk-worker` (.NET 8 Worker): consome `payment.created.v1`, calcula score e publica aprovacao/reprovacao.
- `ledger-api` (.NET 8 Web API + consumidor): faz lancamentos debito/credito e expoe conciliacao.
- `notifications-worker` (.NET 8 Worker): consome eventos e gera logs/webhook fake de notificacao.

Modelo de comunicacao:

- REST APIs para comandos/consultas.
- RabbitMQ (topic exchange) para eventos de dominio:
  - `payment.created.v1`
  - `payment.approved.v1`
  - `payment.rejected.v1`
  - `payment.reversed.v1`
  - `ledger.posted.v1`
- Ownership de dados no PostgreSQL: banco por servico (`paymentsdb`, `riskdb`, `ledgerdb`, `notificationsdb`) com schema especifico por servico.

Diagramas:

- `docs/diagrams/c4.md`
- `docs/architecture.md`

## Roadmap de entrega

1. Sprint 1 (1-2 dias): API de pagamentos + worker de risco + primeiro fluxo assincrono.
2. Sprint 2 (2-4 dias): idempotencia, outbox/inbox, posting do ledger.
3. Sprint 3 (2-4 dias): observabilidade, retry/DLQ, estorno, conciliacao, CI/testes.
4. Sprint 4 (opcional): deploy em AWS (ECS Fargate, RDS, broker gerenciado).

## Padroes de confiabilidade

- Idempotency: `Idempotency-Key` + registro de idempotencia no banco + cache Redis.
- Outbox Pattern: eventos sao persistidos em `outbox_messages` na mesma transacao da escrita de dominio.
- Inbox Pattern: consumidores deduplicam por chave unica `(EventId, Consumer)`.
- Retry + DLQ: cada fila consumidora possui retry queue com TTL e dead-letter queue.
- Saga: `payments-api` atualiza estado com base em eventos aprovados/reprovados/ledger-posted.
- Reversal: `POST /api/payments/{id}/reverse` emite evento de estorno e o ledger escreve os lancamentos inversos.

## Modelo de Ledger

Para pagamento aprovado:

- Debito `CustomerCashAccount`
- Credito `MerchantSettlementAccount`

Para estorno:

- Debito `MerchantSettlementAccount`
- Credito `CustomerCashAccount`

Isso demonstra entendimento contabil e auditabilidade.

## Observabilidade

Stack incluido no Docker Compose:

- OpenTelemetry Collector
- Jaeger (tracing distribuido)
- Prometheus (metricas)
- Grafana (dashboards)
- Serilog (logs estruturados)

URLs uteis apos subir o ambiente:

- Frontend Ledgerpay: `http://localhost:8080`
- Swagger Payments: `http://localhost:8081/swagger`
- Swagger Ledger: `http://localhost:8082/swagger`
- RabbitMQ UI: `http://localhost:15672` (`guest/guest`)
- Jaeger: `http://localhost:16686`
- Prometheus: `http://localhost:9090`
- Grafana: `http://localhost:3000` (`admin/admin`)

## Como rodar

Pre-requisitos:

- Docker + Docker Compose

Comando unico:

```bash
docker compose -f infra/docker-compose.yml up --build
```

Se ja tiver subido uma versao anterior, recrie o volume do Postgres para reaplicar scripts de bootstrap:

```bash
docker compose -f infra/docker-compose.yml down -v
```

## Autenticacao (JWT)

As APIs exigem Bearer JWT com scopes.

Scopes:

- `payments.write`
- `payments.read`
- `ledger.read`

Valores locais padrao (appsettings):

- issuer: `ledgerpay.local`
- audience: `ledgerpay.api`
- signing key: `ledgerpay-super-secret-signing-key-change-me`

Exemplo de payload do token: `docs/api-contracts/jwt-example.md`

## Frontend Web

O projeto agora inclui um frontend em `Go` que serve a UI e funciona como BFF para as APIs existentes.

- Interface operacional unica para criar pagamentos, consultar status, pedir estorno, visualizar ledger e reconciliacao.
- JWT gerado no servidor Go com os scopes `payments.write payments.read ledger.read`, evitando configuracao manual de token no ambiente local.
- Variaveis de ambiente principais do frontend:
  - `PAYMENTS_API_URL`
  - `LEDGER_API_URL`
  - `PAYMENTS_PUBLIC_URL`
  - `LEDGER_PUBLIC_URL`
  - `JWT_ISSUER`
  - `JWT_AUDIENCE`
  - `JWT_SIGNING_KEY`

## Demo de idempotencia (cURL)

Use a mesma `Idempotency-Key` duas vezes:

```bash
curl -X POST http://localhost:8081/api/payments \
  -H "Authorization: Bearer <TOKEN_WITH_payments.write>" \
  -H "Idempotency-Key: idem-001" \
  -H "Content-Type: application/json" \
  -d '{"amount":100.50,"currency":"BRL","payerId":"customer-001","merchantId":"merchant-abc"}'
```

Repita exatamente a mesma requisicao: a API devolve o mesmo pagamento e nao cria duplicidade.

## Demo de conciliacao

```bash
curl http://localhost:8082/api/reconciliation \
  -H "Authorization: Bearer <TOKEN_WITH_ledger.read>"
```

## Estrategia de testes

- Testes unitarios:
  - transicoes de estado do dominio de pagamentos
  - regras de decisao de risco
  - fundamentos de dominio do ledger
  - fundamentos de modelo de notificacao
- Base de testes de integracao:
  - placeholder de smoke test API + DB + broker em `services/payments/tests/Payments.IntegrationTests`
- Comportamento de consumidores de eventos:
  - dedupe de inbox e logica de retry/DLQ implementados na base compartilhada de consumidores

Para rodar os testes:

```bash
dotnet test Ledgerpay.sln
cd services/frontend && go test ./...
```

## CI/CD

Pipeline do GitHub Actions em `.github/workflows/ci.yml`:

1. restore
2. validacao de formato (`dotnet format --verify-no-changes`)
3. build
4. test
5. test do frontend Go
6. docker compose build

## Estrutura do repositorio

```text
ledgerpay/
  docs/
    architecture.md
    diagrams/
    api-contracts/
  services/
    frontend/
    payments/
    risk/
    ledger/
    notifications/
  infra/
    docker-compose.yml
    k8s/
    terraform/
  shared/
    messaging/
    observability/
    common-kernel/
  .github/workflows/ci.yml
  README.md
```

## Evolucao AWS (MVP real)

Caminho-alvo de deploy:

- ECS Fargate por servico
- RDS PostgreSQL
- Amazon MQ (RabbitMQ) ou MSK (Kafka)
- CloudWatch logs/alarms
- AWS X-Ray ou pipeline OpenTelemetry
- S3 para relatorios de conciliacao


## ATS keywords

.NET Core, Microservices, REST APIs, Event-Driven Architecture, AWS, PostgreSQL, Docker, Observability, OpenTelemetry, CI/CD, Idempotency, Outbox Pattern, Saga, Resilience, Structured Logging
