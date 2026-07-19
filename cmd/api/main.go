package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/application"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/infrastructure/memory"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/transport/httpapi"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	repo := memory.NewRepository()
	service := application.NewOnboardingService(repo)
	handler := httpapi.NewHandler(service, logger)

	server := &http.Server{Addr: ":8080", Handler: handler, ReadHeaderTimeout: 5_000_000_000}
	logger.Info("api listening", "address", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("api stopped", "error", err)
		os.Exit(1)
	}
}
