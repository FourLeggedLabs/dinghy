package util

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestRateLimitRetryDelay(t *testing.T) {
	now := time.Date(2026, 10, 27, 19, 56, 25, 0, time.UTC)
	limited := func(reset time.Time) error { return &GithubRateLimitErr{RateLimit: 5000, Reset: reset} }

	tests := []struct {
		name     string
		err      error
		ok       bool
		min, max time.Duration
	}{
		{"not a rate limit", errors.New("boom"), false, 0, 0},
		{"nil", nil, false, 0, 0},
		{"waits for the reset", limited(now.Add(16*time.Minute + 30*time.Second)), true, 16*time.Minute + 30*time.Second, 17 * time.Minute},
		{"wrapped", fmt.Errorf("parse: %w", limited(now.Add(time.Minute))), true, time.Minute, time.Minute + 30*time.Second},
		{"reset already passed", limited(now.Add(-time.Minute)), true, 0, 30 * time.Second},
		{"reset past an hour is capped", limited(now.Add(3 * time.Hour)), true, time.Hour, time.Hour + 30*time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := RateLimitRetryDelay(tt.err, now)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && (got < tt.min || got >= tt.max) {
				t.Fatalf("delay = %s, want in [%s, %s)", got, tt.min, tt.max)
			}
		})
	}
}
