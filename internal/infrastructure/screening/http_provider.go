package screening

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/application"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
)

var ErrCircuitOpen = errors.New("screening provider circuit is open")

type HTTPProvider struct {
	endpoint, apiKey string
	client           *http.Client
	retries          int
	openFor          time.Duration
	mu               sync.Mutex
	failures         int
	openUntil        time.Time
}

func NewHTTPProvider(endpoint, apiKey string, timeout time.Duration, retries int, openFor time.Duration) *HTTPProvider {
	return &HTTPProvider{endpoint: strings.TrimRight(endpoint, "/"), apiKey: apiKey, client: &http.Client{Timeout: timeout}, retries: retries, openFor: openFor}
}
func (p *HTTPProvider) Name() string { return "external-http-screening-v1" }
func (p *HTTPProvider) Screen(ctx context.Context, name string) ([]application.ScreeningCandidate, error) {
	p.mu.Lock()
	if time.Now().Before(p.openUntil) {
		p.mu.Unlock()
		return nil, ErrCircuitOpen
	}
	p.mu.Unlock()
	var last error
	for attempt := 0; attempt <= p.retries; attempt++ {
		candidates, retry, err := p.request(ctx, name)
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
func (p *HTTPProvider) request(ctx context.Context, name string) ([]application.ScreeningCandidate, bool, error) {
	body, _ := json.Marshal(map[string]string{"name": name})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint+"/screen", bytes.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	request.Header.Set("Content-Type", "application/json")
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
	if response.StatusCode >= 400 {
		return nil, false, fmt.Errorf("screening provider status %d", response.StatusCode)
	}
	var payload struct {
		Candidates []struct {
			ListType domain.ScreeningListType `json:"list_type"`
			Name     string                   `json:"name"`
			Score    int                      `json:"score"`
			Reason   string                   `json:"reason"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, false, fmt.Errorf("decode screening response: %w", err)
	}
	result := make([]application.ScreeningCandidate, 0, len(payload.Candidates))
	for _, item := range payload.Candidates {
		if item.Score < 0 || item.Score > 100 {
			return nil, false, errors.New("screening provider returned invalid score")
		}
		result = append(result, application.ScreeningCandidate{ListType: item.ListType, Name: item.Name, Score: item.Score, Reason: item.Reason})
	}
	return result, false, nil
}
