package domain

import (
	"errors"
	"time"
)

var ErrNotificationNotFound = errors.New("notification not found")

type NotificationPreference struct {
	ActorSubject string    `json:"actor_subject"`
	EmailAddress string    `json:"email_address"`
	EmailEnabled bool      `json:"email_enabled"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Notification struct {
	ID         string     `json:"id"`
	CustomerID string     `json:"customer_id"`
	MatchID    string     `json:"match_id"`
	Type       string     `json:"type"`
	Title      string     `json:"title"`
	Message    string     `json:"message"`
	Read       bool       `json:"read"`
	CreatedAt  time.Time  `json:"created_at"`
	ReadBy     string     `json:"read_by,omitempty"`
	ReadAt     *time.Time `json:"read_at,omitempty"`
}
