package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/application"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/auth"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/infrastructure/postgres"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/transport/httpapi"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	authenticator, err := auth.NewJWKSAuthenticator(os.Getenv("JWT_JWKS_URL"), os.Getenv("JWT_ISSUER"), envString("JWT_AUTHORIZED_PARTY", "fccp-web"))
	if err != nil {
		return fmt.Errorf("configure authentication: %w", err)
	}
	readHeaderTimeout, err := envDuration("HTTP_READ_HEADER_TIMEOUT", 5*time.Second)
	if err != nil {
		return err
	}
	shutdownTimeout, err := envDuration("HTTP_SHUTDOWN_TIMEOUT", 10*time.Second)
	if err != nil {
		return err
	}
	address := envString("HTTP_ADDR", ":8080")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("configure database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	if err := migrations.Up(ctx, pool); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}

	repo := postgres.NewRepository(pool)
	service := application.NewOnboardingService(repo)
	transactionService := application.NewTransactionService(repo)
	queryService := application.NewQueryService(repo)
	caseService := application.NewCaseService(repo)
	handler := httpapi.NewHandler(service, transactionService, queryService, caseService, logger, authenticator, pool)

	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: readHeaderTimeout}
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()
	logger.Info("api listening", "address", server.Addr)

	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serverErrors:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-signalCtx.Done():
		logger.Info("shutting down api")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}
	return nil
}

func envString(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envDuration(name string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return duration, nil
}
