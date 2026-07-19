package domain

import "time"

type OutboxMessage struct {
	ID             string         `json:"id"`
	NotificationID string         `json:"notification_id"`
	Channel        string         `json:"channel"`
	Destination    string         `json:"-"`
	Payload        map[string]any `json:"payload"`
	Status         string         `json:"status"`
	Attempts       int            `json:"attempts"`
	NextAttemptAt  time.Time      `json:"next_attempt_at"`
	LastError      string         `json:"last_error,omitempty"`
	LeaseOwner     string         `json:"-"`
	LeaseUntil     *time.Time     `json:"-"`
	CreatedAt      time.Time      `json:"created_at"`
	DeliveredAt    *time.Time     `json:"delivered_at,omitempty"`
}
