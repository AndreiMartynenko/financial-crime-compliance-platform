package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/application"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/auth"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/infrastructure/notification"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/infrastructure/postgres"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/infrastructure/screening"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/observability"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/security"
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
	traceShutdown, err := observability.InitTracing(context.Background(), os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"), envString("OTEL_SERVICE_NAME", "fccp-api"))
	if err != nil {
		return fmt.Errorf("configure tracing: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := traceShutdown(shutdownCtx); err != nil {
			logger.Error("flush traces", "error", err)
		}
	}()
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
	workerInterval, err := envDuration("SCREENING_WORKER_INTERVAL", time.Minute)
	if err != nil {
		return err
	}
	workerLease, err := envDuration("SCREENING_JOB_LEASE", 5*time.Minute)
	if err != nil {
		return err
	}
	providerTimeout, err := envDuration("SCREENING_PROVIDER_TIMEOUT", 5*time.Second)
	if err != nil {
		return err
	}
	providerRetries, err := envInt("SCREENING_PROVIDER_RETRIES", 2)
	if err != nil {
		return err
	}
	if providerRetries > 10 {
		return fmt.Errorf("SCREENING_PROVIDER_RETRIES must be between 0 and 10")
	}
	rateLimit, err := envFloat("HTTP_RATE_LIMIT_RPS", 20)
	if err != nil {
		return err
	}
	rateBurst, err := envInt("HTTP_RATE_LIMIT_BURST", 40)
	if err != nil {
		return err
	}
	if rateBurst < 1 {
		return fmt.Errorf("HTTP_RATE_LIMIT_BURST must be positive")
	}
	address := envString("HTTP_ADDR", ":8080")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse database config: %w", err)
	}
	poolConfig.ConnConfig.Tracer = observability.PGXTracer{}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
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
	dueDiligenceService := application.NewDueDiligenceService(repo)
	var screeningProvider application.ScreeningProvider = screening.DemoProvider{}
	if endpoint := os.Getenv("SCREENING_PROVIDER_URL"); endpoint != "" {
		screeningProvider = screening.NewHTTPProvider(endpoint, os.Getenv("SCREENING_PROVIDER_API_KEY"), providerTimeout, providerRetries, 30*time.Second)
	}
	screeningService := application.NewScreeningService(repo, screeningProvider)
	screeningService.SetLeaseDuration(workerLease)
	webhookURL := os.Getenv("NOTIFICATION_WEBHOOK_URL")
	screeningService.SetNotificationWebhook(webhookURL)
	metrics := observability.NewRegistry()
	metrics.SetMetricsToken(os.Getenv("METRICS_TOKEN"))
	workerCtx, stopWorker := context.WithCancel(context.Background())
	defer stopWorker()
	go runScreeningWorker(workerCtx, logger, screeningService, metrics, workerInterval)
	if webhookURL != "" {
		deliveryService := application.NewDeliveryService(repo, notification.NewWebhookSender(providerTimeout))
		go runDeliveryWorker(workerCtx, logger, deliveryService, metrics, workerInterval)
	}
	handler := httpapi.NewHandler(service, transactionService, queryService, caseService, dueDiligenceService, screeningService, logger, authenticator, pool, metrics)
	handler = security.Headers(security.NewRateLimiter(rateLimit, rateBurst).Middleware(handler))

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
func runDeliveryWorker(ctx context.Context, logger *slog.Logger, service *application.DeliveryService, metrics *observability.Registry, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			workerCtx, span := observability.StartWorkerSpan(ctx, "notification.delivery")
			count, err := service.RunDue(workerCtx, 25)
			span.End()
			pending, pendingErr := service.Pending(ctx)
			if err == nil && pendingErr != nil {
				err = pendingErr
			}
			metrics.ObserveDelivery(count, pending, err)
			if err != nil {
				logger.Error("notification delivery failed", "error", err)
			} else if count > 0 {
				logger.Info("notifications delivered", "count", count)
			}
		}
	}
}

func runScreeningWorker(ctx context.Context, logger *slog.Logger, service *application.ScreeningService, metrics *observability.Registry, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("screening worker stopped")
			return
		case <-ticker.C:
			workerCtx, span := observability.StartWorkerSpan(ctx, "screening.recurring")
			count, err := service.RunDue(workerCtx, 25)
			span.End()
			metrics.ObserveWorker(count, err)
			if err != nil {
				logger.Error("ongoing screening failed", "error", err)
				continue
			}
			if count > 0 {
				logger.Info("ongoing screening completed", "customers", count)
			}
		}
	}
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
func envInt(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 || parsed > 10000 {
		return 0, fmt.Errorf("%s must be between 0 and 10000", name)
	}
	return parsed, nil
}
func envFloat(name string, fallback float64) (float64, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed <= 0 || parsed > 10000 {
		return 0, fmt.Errorf("%s must be a positive number up to 10000", name)
	}
	return parsed, nil
}
