package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	//go:embed web/templates/*.html
	templateFS embed.FS

	//go:embed web/static/*
	staticFS embed.FS

	operationKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
)

type config struct {
	ListenAddr        string
	PaymentsAPIURL    string
	LedgerAPIURL      string
	PaymentsPublicURL string
	LedgerPublicURL   string
	GrafanaURL        string
	RabbitMQURL       string
	JaegerURL         string
	JWTIssuer         string
	JWTAudience       string
	JWTSigningKey     string
	JWTSubject        string
	TokenTTL          time.Duration
	RequestTimeout    time.Duration
}

type pageData struct {
	PaymentsSwaggerURL string
	LedgerSwaggerURL   string
	GrafanaURL         string
	RabbitMQURL        string
	JaegerURL          string
	CurrentYear        int
	PageTitle          string
}

type server struct {
	cfg      config
	client   *http.Client
	logger   *slog.Logger
	template *template.Template
	static   http.Handler
}

type serviceHealth struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Endpoint  string `json:"endpoint"`
	Message   string `json:"message"`
	CheckedAt string `json:"checkedAt"`
}

type healthOverview struct {
	GeneratedAt string          `json:"generatedAt"`
	Services    []serviceHealth `json:"services"`
}

func loadConfig() config {
	listenAddr := strings.TrimSpace(os.Getenv("LISTEN_ADDR"))
	if listenAddr == "" {
		port := strings.TrimSpace(os.Getenv("PORT"))
		if port == "" {
			port = "8080"
		}

		listenAddr = ":" + port
	}

	paymentsAPIURL := normalizeBaseURL(envOrDefault("PAYMENTS_API_URL", "http://localhost:8081"))
	ledgerAPIURL := normalizeBaseURL(envOrDefault("LEDGER_API_URL", "http://localhost:8082"))

	timeoutMs, err := strconv.Atoi(envOrDefault("REQUEST_TIMEOUT_MS", "10000"))
	if err != nil || timeoutMs < 1000 {
		timeoutMs = 10000
	}

	tokenHours, err := strconv.Atoi(envOrDefault("JWT_TTL_HOURS", "24"))
	if err != nil || tokenHours < 1 {
		tokenHours = 24
	}

	return config{
		ListenAddr:        listenAddr,
		PaymentsAPIURL:    paymentsAPIURL,
		LedgerAPIURL:      ledgerAPIURL,
		PaymentsPublicURL: normalizeBaseURL(envOrDefault("PAYMENTS_PUBLIC_URL", paymentsAPIURL)),
		LedgerPublicURL:   normalizeBaseURL(envOrDefault("LEDGER_PUBLIC_URL", ledgerAPIURL)),
		GrafanaURL:        normalizeBaseURL(envOrDefault("GRAFANA_URL", "http://localhost:3000")),
		RabbitMQURL:       normalizeBaseURL(envOrDefault("RABBITMQ_URL", "http://localhost:15672")),
		JaegerURL:         normalizeBaseURL(envOrDefault("JAEGER_URL", "http://localhost:16686")),
		JWTIssuer:         envOrDefault("JWT_ISSUER", "ledgerpay.local"),
		JWTAudience:       envOrDefault("JWT_AUDIENCE", "ledgerpay.api"),
		JWTSigningKey:     envOrDefault("JWT_SIGNING_KEY", "ledgerpay-super-secret-signing-key-change-me"),
		JWTSubject:        envOrDefault("JWT_SUBJECT", "ledgerpay-web-ui"),
		TokenTTL:          time.Duration(tokenHours) * time.Hour,
		RequestTimeout:    time.Duration(timeoutMs) * time.Millisecond,
	}
}

func newServer(cfg config, logger *slog.Logger) (*server, error) {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	tmpl, err := template.ParseFS(templateFS, "web/templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	staticContent, err := fs.Sub(staticFS, "web/static")
	if err != nil {
		return nil, fmt.Errorf("load static assets: %w", err)
	}

	return &server{
		cfg:      cfg,
		client:   &http.Client{Timeout: cfg.RequestTimeout},
		logger:   logger,
		template: tmpl,
		static:   http.StripPrefix("/static/", http.FileServer(http.FS(staticContent))),
	}, nil
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/static/", s.static)
	mux.HandleFunc("/", s.handleHome)
	mux.HandleFunc("/extrato", s.handleStatementPage)
	mux.HandleFunc("/health", s.handleFrontendHealth)
	mux.HandleFunc("/api/overview/health", s.handleOverviewHealth)
	mux.HandleFunc("/api/reconciliation", s.handleReconciliation)
	mux.HandleFunc("/api/ledger/payment/", s.handleLedgerEntries)
	mux.HandleFunc("/api/payments", s.handlePaymentsCollection)
	mux.HandleFunc("/api/payments/", s.handlePaymentsItem)
	return s.logRequests(mux)
}

func (s *server) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	if r.Method != http.MethodGet {
		s.methodNotAllowed(w, http.MethodGet)
		return
	}

	data := pageData{
		PaymentsSwaggerURL: joinURL(s.cfg.PaymentsPublicURL, "/swagger"),
		LedgerSwaggerURL:   joinURL(s.cfg.LedgerPublicURL, "/swagger"),
		GrafanaURL:         s.cfg.GrafanaURL,
		RabbitMQURL:        s.cfg.RabbitMQURL,
		JaegerURL:          s.cfg.JaegerURL,
		CurrentYear:        time.Now().Year(),
		PageTitle:          "LinkPay Banco Digital",
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.template.ExecuteTemplate(w, "index.html", data); err != nil {
		s.logger.Error("render home page", "error", err)
		http.Error(w, "Nao foi possivel renderizar a interface.", http.StatusInternalServerError)
	}
}

func (s *server) handleStatementPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/extrato" {
		http.NotFound(w, r)
		return
	}

	if r.Method != http.MethodGet {
		s.methodNotAllowed(w, http.MethodGet)
		return
	}

	data := pageData{
		PaymentsSwaggerURL: joinURL(s.cfg.PaymentsPublicURL, "/swagger"),
		LedgerSwaggerURL:   joinURL(s.cfg.LedgerPublicURL, "/swagger"),
		GrafanaURL:         s.cfg.GrafanaURL,
		RabbitMQURL:        s.cfg.RabbitMQURL,
		JaegerURL:          s.cfg.JaegerURL,
		CurrentYear:        time.Now().Year(),
		PageTitle:          "LinkPay Extrato",
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.template.ExecuteTemplate(w, "extrato.html", data); err != nil {
		s.logger.Error("render statement page", "error", err)
		http.Error(w, "Nao foi possivel renderizar a interface.", http.StatusInternalServerError)
	}
}

func (s *server) handleFrontendHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.methodNotAllowed(w, http.MethodGet)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "ledgerpay-frontend",
	})
}

func (s *server) handleOverviewHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.methodNotAllowed(w, http.MethodGet)
		return
	}

	type serviceDependency struct {
		name     string
		endpoint string
	}

	dependencies := []serviceDependency{
		{name: "Payments API", endpoint: joinURL(s.cfg.PaymentsAPIURL, "/health")},
		{name: "Ledger API", endpoint: joinURL(s.cfg.LedgerAPIURL, "/health")},
	}

	results := make([]serviceHealth, len(dependencies))
	var wg sync.WaitGroup

	for index, dep := range dependencies {
		wg.Add(1)
		go func(i int, dependency serviceDependency) {
			defer wg.Done()
			results[i] = s.checkDependency(r.Context(), dependency.name, dependency.endpoint)
		}(index, dep)
	}

	wg.Wait()

	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})

	writeJSON(w, http.StatusOK, healthOverview{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Services:    results,
	})
}

func (s *server) checkDependency(ctx context.Context, name, endpoint string) serviceHealth {
	checkedAt := time.Now().UTC().Format(time.RFC3339)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return serviceHealth{
			Name:      name,
			Status:    "offline",
			Endpoint:  endpoint,
			Message:   "falha ao preparar requisicao",
			CheckedAt: checkedAt,
		}
	}

	response, err := s.client.Do(request)
	if err != nil {
		return serviceHealth{
			Name:      name,
			Status:    "offline",
			Endpoint:  endpoint,
			Message:   "servico indisponivel",
			CheckedAt: checkedAt,
		}
	}
	defer response.Body.Close()

	payload, _ := io.ReadAll(io.LimitReader(response.Body, 4096))

	message := http.StatusText(response.StatusCode)
	if len(payload) > 0 {
		var body map[string]any
		if err := json.Unmarshal(payload, &body); err == nil {
			if status, ok := body["status"].(string); ok && status != "" {
				message = status
			}
		}
	}

	status := "online"
	if response.StatusCode >= http.StatusBadRequest {
		status = "degraded"
	}

	return serviceHealth{
		Name:      name,
		Status:    status,
		Endpoint:  endpoint,
		Message:   message,
		CheckedAt: checkedAt,
	}
}

func (s *server) handlePaymentsCollection(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/payments" {
		http.NotFound(w, r)
		return
	}

	if r.Method != http.MethodPost {
		s.methodNotAllowed(w, http.MethodPost)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"message": "Nao foi possivel ler o pagamento enviado.",
		})
		return
	}

	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		idempotencyKey = fmt.Sprintf("ui-%d", time.Now().UnixNano())
	}

	if validationMessage := validateOperationKey(idempotencyKey); validationMessage != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"message": validationMessage,
		})
		return
	}

	w.Header().Set("X-Ledgerpay-Idempotency-Key", idempotencyKey)
	s.forwardJSON(w, r, http.MethodPost, joinURL(s.cfg.PaymentsAPIURL, "/api/payments"), body, map[string]string{
		"Idempotency-Key": idempotencyKey,
	})
}

func (s *server) handlePaymentsItem(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/payments/")
	path = strings.Trim(path, "/")
	if path == "" {
		http.NotFound(w, r)
		return
	}

	parts := strings.Split(path, "/")

	if len(parts) == 1 && r.Method == http.MethodGet {
		s.forwardJSON(w, r, http.MethodGet, joinURL(s.cfg.PaymentsAPIURL, "/api/payments/"+parts[0]), nil, nil)
		return
	}

	if len(parts) == 2 && parts[1] == "reverse" && r.Method == http.MethodPost {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"message": "Nao foi possivel ler o pedido de estorno.",
			})
			return
		}

		s.forwardJSON(w, r, http.MethodPost, joinURL(s.cfg.PaymentsAPIURL, "/api/payments/"+parts[0]+"/reverse"), body, nil)
		return
	}

	if len(parts) == 1 {
		s.methodNotAllowed(w, http.MethodGet)
		return
	}

	if len(parts) == 2 && parts[1] == "reverse" {
		s.methodNotAllowed(w, http.MethodPost)
		return
	}

	http.NotFound(w, r)
}

func (s *server) handleReconciliation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.methodNotAllowed(w, http.MethodGet)
		return
	}

	s.forwardJSON(w, r, http.MethodGet, joinURL(s.cfg.LedgerAPIURL, "/api/reconciliation"), nil, nil)
}

func (s *server) handleLedgerEntries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.methodNotAllowed(w, http.MethodGet)
		return
	}

	paymentID := strings.TrimPrefix(r.URL.Path, "/api/ledger/payment/")
	paymentID = strings.Trim(paymentID, "/")
	if paymentID == "" {
		http.NotFound(w, r)
		return
	}

	s.forwardJSON(w, r, http.MethodGet, joinURL(s.cfg.LedgerAPIURL, "/api/ledger/payment/"+paymentID), nil, nil)
}

func (s *server) forwardJSON(
	w http.ResponseWriter,
	r *http.Request,
	method string,
	targetURL string,
	body []byte,
	extraHeaders map[string]string,
) {
	token, err := s.signedToken()
	if err != nil {
		s.logger.Error("generate jwt token", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"message": "Nao foi possivel autenticar a chamada interna.",
		})
		return
	}

	request, err := http.NewRequestWithContext(r.Context(), method, targetURL, bytes.NewReader(body))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"message": "Nao foi possivel preparar a chamada para a API.",
		})
		return
	}

	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}

	for key, value := range extraHeaders {
		request.Header.Set(key, value)
	}

	response, err := s.client.Do(request)
	if err != nil {
		s.logger.Warn("upstream request failed", "target", targetURL, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"message": "Nao foi possivel falar com a API agora.",
		})
		return
	}
	defer response.Body.Close()

	payload, err := io.ReadAll(response.Body)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"message": "A API respondeu, mas o retorno nao pode ser lido.",
		})
		return
	}

	contentType := response.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json; charset=utf-8"
	}

	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(response.StatusCode)
	if len(payload) == 0 {
		return
	}

	_, _ = w.Write(payload)
}

func (s *server) signedToken() (string, error) {
	headerBytes, err := json.Marshal(map[string]string{
		"alg": "HS256",
		"typ": "JWT",
	})
	if err != nil {
		return "", err
	}

	now := time.Now().UTC()
	payloadBytes, err := json.Marshal(map[string]any{
		"sub":   s.cfg.JWTSubject,
		"scope": "payments.write payments.read ledger.read",
		"iss":   s.cfg.JWTIssuer,
		"aud":   s.cfg.JWTAudience,
		"iat":   now.Unix(),
		"exp":   now.Add(s.cfg.TokenTTL).Unix(),
	})
	if err != nil {
		return "", err
	}

	encodedHeader := base64.RawURLEncoding.EncodeToString(headerBytes)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signingInput := encodedHeader + "." + encodedPayload

	mac := hmac.New(sha256.New, []byte(s.cfg.JWTSigningKey))
	if _, err := mac.Write([]byte(signingInput)); err != nil {
		return "", err
	}

	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return signingInput + "." + signature, nil
}

func (s *server) methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
		"message": "Metodo nao permitido.",
	})
}

func (s *server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		next.ServeHTTP(w, r)
		s.logger.Info("request served",
			"method", r.Method,
			"path", r.URL.Path,
			"duration", time.Since(startedAt).String(),
		)
	})
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)

	if payload == nil {
		return
	}

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, "{}", http.StatusInternalServerError)
	}
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	return value
}

func normalizeBaseURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

func joinURL(base, path string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}

func validateOperationKey(key string) string {
	trimmedKey := strings.TrimSpace(key)
	if trimmedKey == "" {
		return "Informe a chave unica da operacao."
	}

	if !operationKeyPattern.MatchString(trimmedKey) {
		return "A chave unica deve ter entre 8 e 64 caracteres e usar apenas letras, numeros, hifen ou underscore."
	}

	return ""
}
