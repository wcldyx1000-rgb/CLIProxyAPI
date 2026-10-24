package executor

import (
	"context"
	"sync/atomic"
	"time"
)

// antigravityStreamDrainGrace bounds how long an upstream stream may keep running after
// its consumer has gone away, once the terminal chunk has already been delivered. Google
// closes the SSE body within milliseconds of the terminal chunk, so this only matters
// when the upstream stalls.
const antigravityStreamDrainGrace = 2 * time.Second

// newAntigravityStreamContext returns the context for one upstream streaming request and
// a release function that must run once the stream is finished with it.
//
// API handlers return, and cancel the request context, as soon as they have the terminal
// chunk. The upstream body usually reaches EOF a few milliseconds later, and net/http's
// read loop picks at random between "body finished, pool the connection" and "request
// cancelled, close it". Passing the handler's context straight through therefore threw
// away most connections, and each lost connection cost the next request a new TCP + TLS
// handshake.
//
// The returned context keeps the parent's values but follows its cancellation only when
// that is still safe: immediately before the terminal chunk, so an abandoned stream stops
// at once, and after a short grace period once terminal is set, so EOF can be read and the
// connection pooled.
func newAntigravityStreamContext(parent context.Context, terminal *atomic.Bool) (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	stop := context.AfterFunc(parent, func() {
		if !terminal.Load() {
			cancel()
			return
		}
		time.AfterFunc(antigravityStreamDrainGrace, cancel)
	})
	return ctx, func() {
		stop()
		cancel()
	}
}
