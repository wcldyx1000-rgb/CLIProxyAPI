package auth

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
)

func TestAntigravityTransientRetryWaitDoublesWithEqualJitter(t *testing.T) {
	low := func(int64) int64 { return 0 }
	high := func(n int64) int64 { return n - 1 }
	cases := []struct {
		attempt  int
		min, max time.Duration
	}{
		{attempt: 0, min: 500 * time.Millisecond, max: time.Second},
		{attempt: 1, min: time.Second, max: 2 * time.Second},
		{attempt: 2, min: 2 * time.Second, max: 4 * time.Second},
		{attempt: 9, min: 2 * time.Second, max: 4 * time.Second},
	}
	for _, tc := range cases {
		if got := antigravityTransientRetryWait(tc.attempt, 30*time.Second, low); got != tc.min {
			t.Fatalf("attempt %d lowest wait = %v, want %v", tc.attempt, got, tc.min)
		}
		if got := antigravityTransientRetryWait(tc.attempt, 30*time.Second, high); got != tc.max {
			t.Fatalf("attempt %d highest wait = %v, want %v", tc.attempt, got, tc.max)
		}
	}
	if got := antigravityTransientRetryWait(2, 3*time.Second, high); got != 3*time.Second {
		t.Fatalf("wait above maxWait = %v, want it capped at 3s", got)
	}
	if got := antigravityTransientRetryWait(0, 0, high); got != 0 {
		t.Fatalf("wait with maxWait=0 = %v, want an immediate retry", got)
	}
}

// Before this change an Antigravity request that lost its connection was retried at once,
// so one broken stream produced four upstream requests within about 0.3s.
func TestShouldRetryAfterErrorBacksOffTransientErrorsOnlyForAntigravity(t *testing.T) {
	m := NewManager(nil, nil, nil)
	m.SetRetryConfig(3, 30*time.Second, 0)
	model := "test-model"
	for _, provider := range []string{"antigravity", "claude"} {
		auth := &Auth{ID: "auth-" + provider, Provider: provider}
		registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: model}})
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
		if _, errRegister := m.Register(context.Background(), auth); errRegister != nil {
			t.Fatalf("register %s auth: %v", provider, errRegister)
		}
	}
	errEOF := fmt.Errorf("read stream: %w", io.ErrUnexpectedEOF)
	_, _, maxWait := m.retrySettings()

	wait, shouldRetry := m.shouldRetryAfterError(errEOF, 0, []string{"antigravity"}, model, maxWait)
	if !shouldRetry || wait < 500*time.Millisecond || wait > time.Second {
		t.Fatalf("antigravity first retry: shouldRetry=%v wait=%v, want a retry after 0.5s to 1s", shouldRetry, wait)
	}
	wait, shouldRetry = m.shouldRetryAfterError(errEOF, 2, []string{"antigravity"}, model, maxWait)
	if !shouldRetry || wait < 2*time.Second || wait > 4*time.Second {
		t.Fatalf("antigravity third retry: shouldRetry=%v wait=%v, want a retry after 2s to 4s", shouldRetry, wait)
	}
	wait, shouldRetry = m.shouldRetryAfterError(errEOF, 0, []string{"claude"}, model, maxWait)
	if !shouldRetry || wait != 0 {
		t.Fatalf("claude retry: shouldRetry=%v wait=%v, want the unchanged immediate retry", shouldRetry, wait)
	}
}
