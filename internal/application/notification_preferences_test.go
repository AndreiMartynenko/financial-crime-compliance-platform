package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/application"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/infrastructure/memory"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/infrastructure/screening"
)

func TestNotificationPreferenceValidationAndPersistence(t *testing.T) {
	service := application.NewScreeningService(memory.NewRepository(), screening.DemoProvider{})
	ctx := context.Background()
	defaultPreference, err := service.GetNotificationPreference(ctx, "operator-1")
	if err != nil || defaultPreference.EmailEnabled || defaultPreference.ActorSubject != "operator-1" {
		t.Fatalf("default=%+v err=%v", defaultPreference, err)
	}
	if _, err := service.ConfigureNotificationPreference(ctx, "operator-1", "invalid", true); !errors.Is(err, application.ErrInvalidNotificationPreference) {
		t.Fatalf("invalid email error=%v", err)
	}
	saved, err := service.ConfigureNotificationPreference(ctx, "operator-1", "operator@example.com", true)
	if err != nil || !saved.EmailEnabled {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	loaded, err := service.GetNotificationPreference(ctx, "operator-1")
	if err != nil || loaded.EmailAddress != "operator@example.com" {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
}
