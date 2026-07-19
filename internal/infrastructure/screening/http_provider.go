package screening

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/application"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
)

var ErrCircuitOpen = errors.New("screening provider circuit is open")

type HTTPProvider struct {
	endpoint, apiKey, name string
	client                 *http.Client
	retries                int
	openFor                time.Duration
	mu                     sync.Mutex
	failures               int
	openUntil              time.Time
}

type HTTPProviderConfig struct {
	Endpoint, APIKey, Name string
	Timeout, OpenFor       time.Duration
	Retries                int
	CAFile, ClientCertFile string
	ClientKeyFile          string
	AllowInsecureHTTP      bool
}

const maxProviderResponseBytes = 1 << 20

func NewHTTPProvider(endpoint, apiKey string, timeout time.Duration, retries int, openFor time.Duration) *HTTPProvider {
	return &HTTPProvider{endpoint: strings.TrimRight(endpoint, "/"), apiKey: apiKey, name: "external-http-screening-v1", client: &http.Client{Timeout: timeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, retries: retries, openFor: openFor}
}
func NewConfiguredHTTPProvider(config HTTPProviderConfig) (*HTTPProvider, error) {
	endpoint, err := url.Parse(strings.TrimSpace(config.Endpoint))
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Scheme != "https" && !(config.AllowInsecureHTTP && endpoint.Scheme == "http")) {
		return nil, errors.New("screening provider URL must use HTTPS")
	}
	if (config.ClientCertFile == "") != (config.ClientKeyFile == "") {
		return nil, errors.New("screening provider client certificate and key must be configured together")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if config.CAFile != "" {
		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("load system CA pool: %w", err)
		}
		pem, err := os.ReadFile(config.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read screening provider CA: %w", err)
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, errors.New("screening provider CA file contains no certificates")
		}
		tlsConfig.RootCAs = roots
	}
	if config.ClientCertFile != "" {
		certificate, err := tls.LoadX509KeyPair(config.ClientCertFile, config.ClientKeyFile)
		if err != nil {
			return nil, fmt.Errorf("load screening provider client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	provider := NewHTTPProvider(config.Endpoint, config.APIKey, config.Timeout, config.Retries, config.OpenFor)
	provider.client.Transport = transport
	if name := strings.TrimSpace(config.Name); name != "" {
		provider.name = name
	}
	return provider, nil
}
func (p *HTTPProvider) Name() string { return p.name }
func (p *HTTPProvider) Screen(ctx context.Context, name string) ([]application.ScreeningCandidate, error) {
	p.mu.Lock()
	if time.Now().Before(p.openUntil) {
		p.mu.Unlock()
		return nil, ErrCircuitOpen
	}
	p.mu.Unlock()
	idempotencyKey := newProviderRequestID()
	var last error
	for attempt := 0; attempt <= p.retries; attempt++ {
		candidates, retry, err := p.request(ctx, name, idempotencyKey)
		if err == nil {
			p.mu.Lock()
			p.failures = 0
			p.openUntil = time.Time{}
			p.mu.Unlock()
			return candidates, nil
		}
		last = err
		if !retry {
			break
		}
	}
	p.mu.Lock()
	p.failures++
	if p.failures >= 3 {
		p.openUntil = time.Now().Add(p.openFor)
		p.failures = 0
	}
	p.mu.Unlock()
	return nil, last
}
func (p *HTTPProvider) request(ctx context.Context, name, idempotencyKey string) ([]application.ScreeningCandidate, bool, error) {
	body, _ := json.Marshal(map[string]string{"name": name})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint+"/screen", bytes.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "fccp-screening-adapter/1.0")
	request.Header.Set("X-Correlation-ID", idempotencyKey)
	request.Header.Set("Idempotency-Key", idempotencyKey)
	if p.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	response, err := p.client.Do(request)
	if err != nil {
		return nil, true, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 500 {
		return nil, true, fmt.Errorf("screening provider status %d", response.StatusCode)
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return nil, true, fmt.Errorf("screening provider status %d", response.StatusCode)
	}
	if response.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("screening provider status %d", response.StatusCode)
	}
	contentType := response.Header.Get("Content-Type")
	if !strings.Contains(strings.ToLower(contentType), "json") {
		return nil, false, errors.New("screening provider returned a non-JSON response")
	}
	var payload struct {
		Candidates []struct {
			ListType domain.ScreeningListType `json:"list_type"`
			Name     string                   `json:"name"`
			Score    int                      `json:"score"`
			Reason   string                   `json:"reason"`
		} `json:"candidates"`
	}
	encoded, err := io.ReadAll(io.LimitReader(response.Body, maxProviderResponseBytes+1))
	if err != nil {
		return nil, false, fmt.Errorf("read screening response: %w", err)
	}
	if len(encoded) > maxProviderResponseBytes {
		return nil, false, errors.New("screening provider response exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return nil, false, fmt.Errorf("decode screening response: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, false, errors.New("screening provider returned trailing JSON data")
	}
	if len(payload.Candidates) > 100 {
		return nil, false, errors.New("screening provider returned too many candidates")
	}
	result := make([]application.ScreeningCandidate, 0, len(payload.Candidates))
	for _, item := range payload.Candidates {
		if item.Score < 0 || item.Score > 100 || strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.Reason) == "" || (item.ListType != domain.ScreeningSanctions && item.ListType != domain.ScreeningPEP && item.ListType != domain.ScreeningAdverseMedia) {
			return nil, false, errors.New("screening provider returned an invalid candidate")
		}
		result = append(result, application.ScreeningCandidate{ListType: item.ListType, Name: item.Name, Score: item.Score, Reason: item.Reason})
	}
	return result, false, nil
}

func newProviderRequestID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("fccp-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(value)
}
