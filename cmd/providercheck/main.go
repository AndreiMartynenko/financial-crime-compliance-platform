package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/infrastructure/screening"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	endpoint, subject := os.Getenv("SCREENING_PROVIDER_URL"), os.Getenv("SCREENING_PROVIDER_CHECK_NAME")
	if endpoint == "" || subject == "" {
		return fmt.Errorf("SCREENING_PROVIDER_URL and SCREENING_PROVIDER_CHECK_NAME are required")
	}
	timeout, err := time.ParseDuration(valueOr("SCREENING_PROVIDER_TIMEOUT", "5s"))
	if err != nil || timeout <= 0 {
		return fmt.Errorf("SCREENING_PROVIDER_TIMEOUT must be a positive duration")
	}
	retries, err := strconv.Atoi(valueOr("SCREENING_PROVIDER_RETRIES", "0"))
	if err != nil || retries < 0 || retries > 10 {
		return fmt.Errorf("SCREENING_PROVIDER_RETRIES must be between 0 and 10")
	}
	allowHTTP, err := strconv.ParseBool(valueOr("SCREENING_PROVIDER_ALLOW_HTTP", "false"))
	if err != nil {
		return fmt.Errorf("SCREENING_PROVIDER_ALLOW_HTTP must be true or false")
	}
	provider, err := screening.NewConfiguredHTTPProvider(screening.HTTPProviderConfig{
		Endpoint: endpoint, APIKey: os.Getenv("SCREENING_PROVIDER_API_KEY"), Name: valueOr("SCREENING_PROVIDER_NAME", "external-http-screening-v1"), Timeout: timeout, Retries: retries, OpenFor: 30 * time.Second,
		CAFile: os.Getenv("SCREENING_PROVIDER_CA_FILE"), ClientCertFile: os.Getenv("SCREENING_PROVIDER_CLIENT_CERT_FILE"), ClientKeyFile: os.Getenv("SCREENING_PROVIDER_CLIENT_KEY_FILE"), AllowInsecureHTTP: allowHTTP,
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout*time.Duration(retries+1)+time.Second)
	defer cancel()
	candidates, err := provider.Screen(ctx, subject)
	if err != nil {
		return fmt.Errorf("provider conformance check failed: %w", err)
	}
	fmt.Printf("provider=%s contract=valid candidates=%d\n", provider.Name(), len(candidates))
	return nil
}

func valueOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
