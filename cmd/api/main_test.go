package main

import (
	"testing"
	"time"
)

func TestEnvDuration(t *testing.T) {
	t.Setenv("TEST_DURATION", "250ms")
	got, err := envDuration("TEST_DURATION", time.Second)
	if err != nil || got != 250*time.Millisecond {
		t.Fatalf("duration=%v err=%v", got, err)
	}
	t.Setenv("TEST_DURATION", "invalid")
	if _, err := envDuration("TEST_DURATION", time.Second); err == nil {
		t.Fatal("expected invalid duration error")
	}
}
