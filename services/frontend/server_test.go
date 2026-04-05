package main

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSignedTokenIncludesExpectedClaims(t *testing.T) {
	app, err := newServer(config{
		ListenAddr:        ":8080",
		PaymentsAPIURL:    "http://localhost:8081",
		LedgerAPIURL:      "http://localhost:8082",
		PaymentsPublicURL: "http://localhost:8081",
		LedgerPublicURL:   "http://localhost:8082",
		GrafanaURL:        "http://localhost:3000",
		RabbitMQURL:       "http://localhost:15672",
		JaegerURL:         "http://localhost:16686",
		JWTIssuer:         "ledgerpay.local",
		JWTAudience:       "ledgerpay.api",
		JWTSigningKey:     "test-secret",
		JWTSubject:        "frontend-user",
		TokenTTL:          time.Hour,
		RequestTimeout:    time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	token, err := app.signedToken()
	if err != nil {
		t.Fatalf("signed token: %v", err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 token parts, got %d", len(parts))
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	var claims map[string]any
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if claims["iss"] != "ledgerpay.local" {
		t.Fatalf("unexpected issuer: %v", claims["iss"])
	}

	if claims["aud"] != "ledgerpay.api" {
		t.Fatalf("unexpected audience: %v", claims["aud"])
	}

	if claims["sub"] != "frontend-user" {
		t.Fatalf("unexpected subject: %v", claims["sub"])
	}

	if claims["scope"] != "payments.write payments.read ledger.read" {
		t.Fatalf("unexpected scope: %v", claims["scope"])
	}
}

func TestHomePageRenders(t *testing.T) {
	app, err := newServer(config{
		ListenAddr:        ":8080",
		PaymentsAPIURL:    "http://localhost:8081",
		LedgerAPIURL:      "http://localhost:8082",
		PaymentsPublicURL: "http://localhost:8081",
		LedgerPublicURL:   "http://localhost:8082",
		GrafanaURL:        "http://localhost:3000",
		RabbitMQURL:       "http://localhost:15672",
		JaegerURL:         "http://localhost:16686",
		JWTIssuer:         "ledgerpay.local",
		JWTAudience:       "ledgerpay.api",
		JWTSigningKey:     "test-secret",
		JWTSubject:        "frontend-user",
		TokenTTL:          time.Hour,
		RequestTimeout:    time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()

	app.routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status code: %d", recorder.Code)
	}

	if !strings.Contains(recorder.Body.String(), "LinkPay Banco Digital") {
		t.Fatalf("page did not render expected title")
	}
}

func TestStatementPageRenders(t *testing.T) {
	app, err := newServer(config{
		ListenAddr:        ":8080",
		PaymentsAPIURL:    "http://localhost:8081",
		LedgerAPIURL:      "http://localhost:8082",
		PaymentsPublicURL: "http://localhost:8081",
		LedgerPublicURL:   "http://localhost:8082",
		GrafanaURL:        "http://localhost:3000",
		RabbitMQURL:       "http://localhost:15672",
		JaegerURL:         "http://localhost:16686",
		JWTIssuer:         "ledgerpay.local",
		JWTAudience:       "ledgerpay.api",
		JWTSigningKey:     "test-secret",
		JWTSubject:        "frontend-user",
		TokenTTL:          time.Hour,
		RequestTimeout:    time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/extrato", nil)
	recorder := httptest.NewRecorder()

	app.routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status code: %d", recorder.Code)
	}

	if !strings.Contains(recorder.Body.String(), "Extrato LinkPay") {
		t.Fatalf("statement page did not render expected title")
	}
}

func TestValidateOperationKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{name: "valid key", key: "pix_2026-abril", wantErr: false},
		{name: "too short", key: "abc", wantErr: true},
		{name: "invalid chars", key: "pix chave!", wantErr: true},
		{name: "blank", key: "   ", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := validateOperationKey(test.key)
			if test.wantErr && got == "" {
				t.Fatalf("expected validation error for key %q", test.key)
			}

			if !test.wantErr && got != "" {
				t.Fatalf("expected valid key, got %q", got)
			}
		})
	}
}
