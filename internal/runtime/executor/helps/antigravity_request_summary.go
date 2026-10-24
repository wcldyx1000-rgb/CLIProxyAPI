package helps

import (
	"context"
	"math"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

// AntigravityRequestSummary describes the size and shape of an upstream generate
// request without carrying any of its content.
type AntigravityRequestSummary struct {
	Fields log.Fields
}

// NewAntigravityRequestSummary extracts counts and sizes from a generate payload.
// It only reads structure (array lengths, text lengths, thinking settings), never
// the text itself, so the result is safe to log.
func NewAntigravityRequestSummary(payload []byte) AntigravityRequestSummary {
	root := gjson.ParseBytes(payload)
	req := root.Get("request")
	if !req.Exists() {
		req = root
	}

	toolDecls := int64(0)
	req.Get("tools").ForEach(func(_, tool gjson.Result) bool {
		toolDecls += tool.Get("functionDeclarations.#").Int()
		return true
	})

	fields := log.Fields{
		"req_bytes":       len(payload),
		"req_contents":    req.Get("contents.#").Int(),
		"req_tool_groups": req.Get("tools.#").Int(),
		"req_tool_decls":  toolDecls,
	}
	systemChars := 0
	req.Get("systemInstruction.parts").ForEach(func(_, part gjson.Result) bool {
		systemChars += len(part.Get("text").String())
		return true
	})
	fields["req_system_chars"] = systemChars

	thinking := req.Get("generationConfig.thinkingConfig")
	if level := thinking.Get("thinkingLevel"); level.Exists() {
		fields["req_thinking"] = "level:" + level.String()
	} else if budget := thinking.Get("thinkingBudget"); budget.Exists() {
		fields["req_thinking"] = "budget:" + budget.String()
	}
	return AntigravityRequestSummary{Fields: fields}
}

// LogFirstByte reports how long the upstream took to return response headers,
// which for an SSE stream is the wait before the first chunk can arrive.
func (s AntigravityRequestSummary) LogFirstByte(ctx context.Context, operation, requestedModel, upstreamModel string, status int, since time.Time) {
	if !log.IsLevelEnabled(log.DebugLevel) {
		return
	}
	s.entry(ctx, operation, requestedModel, upstreamModel).
		WithFields(log.Fields{"upstream_status": status, "ttfb_ms": time.Since(since).Milliseconds()}).
		Debug("antigravity upstream response headers")
}

// LogDone reports the total time from sending the request until the response body ended.
// When the upstream returned usage, it adds token counts and the end-to-end output rate,
// which counts reasoning tokens as output because they are generated before the answer.
func (s AntigravityRequestSummary) LogDone(ctx context.Context, operation, requestedModel, upstreamModel string, since time.Time, detail usage.Detail, hasUsage bool) {
	if !log.IsLevelEnabled(log.DebugLevel) {
		return
	}
	elapsed := time.Since(since)
	fields := log.Fields{"total_ms": elapsed.Milliseconds()}
	if hasUsage {
		fields["in_tokens"] = detail.InputTokens
		fields["out_tokens"] = detail.OutputTokens
		fields["think_tokens"] = detail.ReasoningTokens
		if generated := detail.OutputTokens + detail.ReasoningTokens; generated > 0 && elapsed > 0 {
			fields["e2e_tok_s"] = math.Round(float64(generated)/elapsed.Seconds()*10) / 10
		}
	}
	s.entry(ctx, operation, requestedModel, upstreamModel).
		WithFields(fields).
		Debug("antigravity upstream response finished")
}

func (s AntigravityRequestSummary) entry(ctx context.Context, operation, requestedModel, upstreamModel string) *log.Entry {
	return log.WithFields(s.Fields).WithFields(log.Fields{
		"provider":       "antigravity",
		"operation":      operation,
		"request_id":     logging.GetRequestID(ctx),
		"model":          requestedModel,
		"upstream_model": upstreamModel,
	})
}
