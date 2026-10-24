package helps

import (
	"context"
	"time"

	"fmt"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"strings"
	"testing"
)

func TestNewAntigravityRequestSummaryCountsStructureOnly(t *testing.T) {
	payload := []byte(`{"model":"gemini-3.8-flash","request":{
		"contents":[{"role":"user","parts":[{"text":"secret user prompt"}]},{"role":"model","parts":[{"text":"answer"}]}],
		"systemInstruction":{"parts":[{"text":"abcde"},{"text":"fg"}]},
		"tools":[{"functionDeclarations":[{"name":"a"},{"name":"b"}]},{"googleSearch":{}}],
		"generationConfig":{"thinkingConfig":{"thinkingLevel":"medium"}}}}`)

	fields := NewAntigravityRequestSummary(payload).Fields

	want := map[string]any{
		"req_contents":     int64(2),
		"req_tool_groups":  int64(2),
		"req_tool_decls":   int64(2),
		"req_system_chars": 7,
		"req_thinking":     "level:medium",
		"req_bytes":        len(payload),
	}
	for key, value := range want {
		if fields[key] != value {
			t.Errorf("%s = %v, want %v", key, fields[key], value)
		}
	}
	if dump := fmt.Sprint(fields); strings.Contains(dump, "secret user prompt") {
		t.Fatalf("summary leaked request content: %s", dump)
	}
}

func TestNewAntigravityRequestSummaryAcceptsUnwrappedPayloadAndBudget(t *testing.T) {
	payload := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":8192}}}`)

	fields := NewAntigravityRequestSummary(payload).Fields

	if fields["req_contents"] != int64(1) {
		t.Errorf("contents = %v, want 1", fields["req_contents"])
	}
	if fields["req_thinking"] != "budget:8192" {
		t.Errorf("thinking = %v, want budget:8192", fields["req_thinking"])
	}
}

func TestAntigravityRequestSummaryLogDoneReportsTokensAndRate(t *testing.T) {
	logger, hook := logtest.NewNullLogger()
	logger.SetLevel(log.DebugLevel)
	previousOut, previousLevel := log.StandardLogger().Out, log.GetLevel()
	log.SetOutput(logger.Out)
	log.SetLevel(log.DebugLevel)
	log.AddHook(hook)
	defer func() {
		log.SetOutput(previousOut)
		log.SetLevel(previousLevel)
		log.StandardLogger().ReplaceHooks(make(log.LevelHooks))
	}()

	summary := NewAntigravityRequestSummary([]byte(`{"request":{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}}`))
	detail := usage.Detail{InputTokens: 100, OutputTokens: 30, ReasoningTokens: 20}
	summary.LogDone(context.Background(), "generate_stream", "gemini-3.8-flash", "gemini-3.8-flash-medium", time.Now().Add(-2*time.Second), detail, true)

	entry := hook.LastEntry()
	if entry == nil || entry.Message != "antigravity upstream response finished" {
		t.Fatalf("missing finished log entry: %+v", entry)
	}
	for key, want := range map[string]any{"in_tokens": int64(100), "out_tokens": int64(30), "think_tokens": int64(20)} {
		if entry.Data[key] != want {
			t.Errorf("%s = %v, want %v", key, entry.Data[key], want)
		}
	}
	// (30 + 20) tokens over about 2 s.
	if rate, ok := entry.Data["e2e_tok_s"].(float64); !ok || rate < 20 || rate > 26 {
		t.Errorf("e2e_tok_s = %v, want about 25", entry.Data["e2e_tok_s"])
	}

	hook.Reset()
	summary.LogDone(context.Background(), "generate_stream", "m", "m", time.Now(), usage.Detail{}, false)
	if _, present := hook.LastEntry().Data["in_tokens"]; present {
		t.Error("token fields must be omitted when upstream returned no usage")
	}
}
