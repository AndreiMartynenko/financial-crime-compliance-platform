package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
)

type WebhookSender struct{ client *http.Client }

func NewWebhookSender(timeout time.Duration) *WebhookSender {
	return &WebhookSender{client: &http.Client{Timeout: timeout}}
}
func (s *WebhookSender) Send(ctx context.Context, message domain.OutboxMessage) error {
	body, err := json.Marshal(message.Payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, message.Destination, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("webhook status %d", response.StatusCode)
	}
	return nil
}
