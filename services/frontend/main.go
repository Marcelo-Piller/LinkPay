package main

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg := loadConfig()

	app, err := newServer(cfg, logger)
	if err != nil {
		logger.Error("failed to initialize frontend", "error", err)
		os.Exit(1)
	}

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           app.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	logger.Info("ledgerpay frontend listening",
		"listen_addr", cfg.ListenAddr,
		"payments_api", cfg.PaymentsAPIURL,
		"ledger_api", cfg.LedgerAPIURL,
	)

	err = httpServer.ListenAndServe()
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return
	}

	logger.Error("frontend stopped unexpectedly", "error", err)
	_, _ = io.WriteString(os.Stderr, err.Error())
	os.Exit(1)
}
