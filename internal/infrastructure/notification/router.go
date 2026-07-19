package notification

import (
	"context"
	"fmt"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/application"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
)

type Router struct {
	senders map[string]application.DeliverySender
}

func NewRouter(senders map[string]application.DeliverySender) *Router {
	return &Router{senders: senders}
}

func (r *Router) Send(ctx context.Context, message domain.OutboxMessage) error {
	sender, ok := r.senders[message.Channel]
	if !ok {
		return fmt.Errorf("notification channel %q is not configured", message.Channel)
	}
	return sender.Send(ctx, message)
}
