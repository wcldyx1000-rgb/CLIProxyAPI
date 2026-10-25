package misc

import (
	"context"
	"testing"
	"time"
)

func overrideAntigravityFirstRefreshForTest(t *testing.T, started bool) chan struct{} {
	t.Helper()

	oldDone := antigravityFirstRefreshDone
	oldStarted := antigravityUpdaterStarted.Load()
	done := make(chan struct{})
	antigravityFirstRefreshDone = done
	antigravityUpdaterStarted.Store(started)
	t.Cleanup(func() {
		antigravityFirstRefreshDone = oldDone
		antigravityUpdaterStarted.Store(oldStarted)
	})
	return done
}

func TestWaitForAntigravityVersionReturnsImmediatelyWithoutUpdater(t *testing.T) {
	overrideAntigravityFirstRefreshForTest(t, false)

	start := time.Now()
	WaitForAntigravityVersion(context.Background(), time.Minute)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("WaitForAntigravityVersion waited %s without a running updater", elapsed)
	}
}

func TestWaitForAntigravityVersionWaitsForFirstRefresh(t *testing.T) {
	done := overrideAntigravityFirstRefreshForTest(t, true)

	returned := make(chan struct{})
	go func() {
		WaitForAntigravityVersion(context.Background(), time.Minute)
		close(returned)
	}()

	select {
	case <-returned:
		t.Fatal("WaitForAntigravityVersion returned before the first refresh finished")
	case <-time.After(50 * time.Millisecond):
	}
	close(done)
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("WaitForAntigravityVersion did not return after the first refresh finished")
	}
}

func TestWaitForAntigravityVersionIsBoundedByMaxWait(t *testing.T) {
	overrideAntigravityFirstRefreshForTest(t, true)

	start := time.Now()
	WaitForAntigravityVersion(context.Background(), 50*time.Millisecond)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("WaitForAntigravityVersion waited %s, want about 50ms", elapsed)
	}
}
