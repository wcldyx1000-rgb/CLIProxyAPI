package auth

import (
	"math/rand/v2"
	"time"
)

// Antigravity retries after transient transport errors (a dropped connection, EOF, a timeout)
// back off exponentially instead of retrying at once. The shape follows the official client's
// mid-stream retry: the window doubles each round, the wait is half fixed and half random,
// and the longest wait is 5s. The client reads its base from runtime configuration that could
// not be recovered, so the 1s base here is this project's own choice.
const (
	antigravityTransientRetryBase = time.Second
	// antigravityTransientRetryMax caps the window before waitForCooldown adds up to 25% more
	// jitter, so the longest real wait matches the client's 5s cap.
	antigravityTransientRetryMax = 4 * time.Second
)

// antigravityTransientRetryWait returns the wait before retry round attempt+1. attempt is the
// zero-based round that just failed. randomInt64N is injected so tests are deterministic.
func antigravityTransientRetryWait(attempt int, maxWait time.Duration, randomInt64N func(int64) int64) time.Duration {
	if maxWait <= 0 {
		// A non-positive maxWait means "never wait"; keep the immediate retry.
		return 0
	}
	// 1s, 2s, then 4s for every later round.
	window := antigravityTransientRetryMax
	if attempt < 2 {
		window = antigravityTransientRetryBase << max(attempt, 0)
	}
	half := window / 2
	wait := half + time.Duration(randomInt64N(int64(half)+1))
	return min(wait, maxWait)
}

func antigravityTransientRetryRandom(n int64) int64 { return rand.Int64N(n) }
