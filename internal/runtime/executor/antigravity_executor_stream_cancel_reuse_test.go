package executor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

// TestAntigravityExecuteStreamReusesConnectionWhenConsumerCancelsAtTerminal covers the
// race between the consumer finishing and the upstream body reaching EOF. API handlers
// return, and cancel the request context, as soon as they have seen the terminal chunk.
// Google ends the SSE body a few milliseconds later. If that cancellation reaches the
// upstream request first, net/http closes the connection instead of pooling it and the
// next request pays a new TCP + TLS handshake (about 0.5 s).
func TestAntigravityExecuteStreamReusesConnectionWhenConsumerCancelsAtTerminal(t *testing.T) {
	var mu sync.Mutex
	remotes := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		remotes[r.RemoteAddr]++
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":1,\"candidatesTokenCount\":1,\"totalTokenCount\":2}}}\n\n"))
		w.(http.Flusher).Flush()
		// The body stays open briefly after the terminal chunk, as it does upstream.
		time.Sleep(50 * time.Millisecond)
	}))
	defer server.Close()

	enabledPool := true
	exec := NewAntigravityExecutor(&config.Config{
		RequestRetry: 1,
		Antigravity: config.AntigravityConfig{
			ConnectionPool: config.AntigravityConnectionPoolConfig{Enabled: &enabledPool},
		},
	})
	auth := &cliproxyauth.Auth{
		ID:         "antigravity-cancel-at-terminal-auth",
		Provider:   "antigravity",
		Attributes: map[string]string{"base_url": server.URL},
		Metadata: map[string]any{
			"access_token": "token",
			"project_id":   "project-1",
			"expired":      time.Now().Add(time.Hour).Format(time.RFC3339),
		},
	}

	const requests = 4
	payload := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	for i := 0; i < requests; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		result, errExecute := exec.ExecuteStream(ctx, auth, cliproxyexecutor.Request{
			Model:   "gemini-3.6-flash-high",
			Payload: payload,
		}, cliproxyexecutor.Options{
			SourceFormat:    sdktranslator.FormatGemini,
			ResponseFormat:  sdktranslator.FormatGemini,
			Stream:          true,
			OriginalRequest: payload,
		})
		if errExecute != nil {
			cancel()
			t.Fatalf("request %d: ExecuteStream() error = %v", i, errExecute)
		}
		for chunk := range result.Chunks {
			if chunk.Err != nil {
				cancel()
				t.Fatalf("request %d: stream chunk error: %v", i, chunk.Err)
			}
			cancel() // the handler returns once it has the terminal chunk
		}
		cancel()
	}

	mu.Lock()
	distinct, total := len(remotes), 0
	for _, c := range remotes {
		total += c
	}
	mu.Unlock()
	if total != requests {
		t.Fatalf("expected %d upstream requests, got %d", requests, total)
	}
	if distinct != 1 {
		t.Fatalf("expected %d requests to reuse one upstream connection, got %d connections", requests, distinct)
	}
}
