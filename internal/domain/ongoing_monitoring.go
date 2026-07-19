package domain

import (
	"errors"
	"time"
)

var ErrScreeningScheduleNotFound = errors.New("screening schedule not found")

type ScreeningSchedule struct {
	CustomerID    string     `json:"customer_id"`
	Enabled       bool       `json:"enabled"`
	IntervalHours int        `json:"interval_hours"`
	NextRunAt     time.Time  `json:"next_run_at"`
	LastRunAt     *time.Time `json:"last_run_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
	UpdatedBy     string     `json:"updated_by"`
	UpdatedAt     time.Time  `json:"updated_at"`
}
