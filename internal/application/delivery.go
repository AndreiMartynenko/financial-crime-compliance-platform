package application

import (
	"context"
	"fmt"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"time"
)

type DeliveryRepository interface {
	ClaimOutbox(context.Context, time.Time, int, string, time.Time) ([]domain.OutboxMessage, error)
	CompleteOutbox(context.Context, string, string, time.Time, time.Time, string) error
	CountPendingOutbox(context.Context) (int, error)
}

func (s *DeliveryService) Pending(ctx context.Context) (int, error) {
	return s.repo.CountPendingOutbox(ctx)
}

type DeliverySender interface {
	Send(context.Context, domain.OutboxMessage) error
}
type DeliveryService struct {
	repo     DeliveryRepository
	sender   DeliverySender
	workerID string
	now      func() time.Time
}

func NewDeliveryService(repo DeliveryRepository, sender DeliverySender) *DeliveryService {
	return &DeliveryService{repo: repo, sender: sender, workerID: newID(), now: time.Now}
}
func (s *DeliveryService) RunDue(ctx context.Context, limit int) (int, error) {
	now := s.now().UTC()
	items, err := s.repo.ClaimOutbox(ctx, now, limit, s.workerID, now.Add(2*time.Minute))
	if err != nil {
		return 0, err
	}
	delivered, failed := 0, 0
	for _, item := range items {
		sendCtx, sendSpan := otel.Tracer("fccp/notifications").Start(ctx, "notification.delivery", trace.WithAttributes(attribute.String("notification.channel", item.Channel)))
		sendErr := s.sender.Send(sendCtx, item)
		if sendErr != nil {
			sendSpan.RecordError(sendErr)
			sendSpan.SetStatus(codes.Error, sendErr.Error())
		}
		sendSpan.End()
		lastError := ""
		next := now
		if sendErr != nil {
			lastError = sendErr.Error()
			next = now.Add(deliveryBackoff(item.Attempts + 1))
			failed++
		} else {
			delivered++
		}
		if err := s.repo.CompleteOutbox(ctx, item.ID, s.workerID, now, next, lastError); err != nil {
			return delivered, err
		}
	}
	if failed > 0 {
		return delivered, fmt.Errorf("%d notification deliveries failed", failed)
	}
	return delivered, nil
}
func deliveryBackoff(attempt int) time.Duration {
	delay := time.Minute
	for i := 1; i < attempt && delay < time.Hour; i++ {
		delay *= 2
	}
	if delay > time.Hour {
		return time.Hour
	}
	return delay
}
