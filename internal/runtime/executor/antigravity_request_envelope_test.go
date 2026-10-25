package executor

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/tidwall/gjson"
)

var antigravityAgentRequestIDPattern = regexp.MustCompile(`^agent/([0-9a-f-]{36})/(\d+)/([0-9a-f-]{36})/(\d+)$`)

func TestGeminiToAntigravityAgentEnvelopeMatchesDesktopClient(t *testing.T) {
	RecordAntigravityCatalogModels(map[string]AntigravityCatalogModel{"Gemini-Envelope-Test": {Enum: "MODEL_PLACEHOLDER_M20"}})
	now := time.UnixMilli(1792890907568)

	turn1 := []byte(`{"request":{"contents":[{"role":"user","parts":[{"text":"first"}]}]}}`)
	out1 := applyAntigravityAgentEnvelope(turn1, "gemini-envelope-test", "proj-1", "-123", now)

	match := antigravityAgentRequestIDPattern.FindStringSubmatch(gjson.GetBytes(out1, "requestId").String())
	if match == nil {
		t.Fatalf("requestId = %q, want agent/<cascade>/<ms>/<trajectory>/<step>", gjson.GetBytes(out1, "requestId").String())
	}
	cascadeID, trajectoryID := match[1], match[3]
	if match[2] != "1792890907568" || match[4] != "1" {
		t.Fatalf("requestId timestamp/step = %s/%s, want 1792890907568/1", match[2], match[4])
	}
	if cascadeID[14] != '4' || trajectoryID[14] != '4' || cascadeID == trajectoryID {
		t.Fatalf("cascade/trajectory IDs = %s/%s, want distinct version-4 UUIDs", cascadeID, trajectoryID)
	}

	wantLabels := `{"cascade_id":"` + cascadeID + `","last_step_index":"0","model_enum":"MODEL_PLACEHOLDER_M20","request_id":"` + trajectoryID + `-0","root_cascade_id":"` + cascadeID + `","trajectory_id":"` + trajectoryID + `","used_claude":"false","used_claude_conservative":"false","used_non_gemini_model":"false"}`
	if got := gjson.GetBytes(out1, "request.labels").Raw; got != wantLabels {
		t.Fatalf("labels =\n%s\nwant\n%s", got, wantLabels)
	}

	// Third turn: five contents, two earlier user turns. IDs stay with the conversation.
	turn3 := []byte(`{"request":{"contents":[
		{"role":"user","parts":[{"text":"first"}]},{"role":"model","parts":[{"text":"a"}]},
		{"role":"user","parts":[{"text":"second"}]},{"role":"model","parts":[{"text":"b"}]},
		{"role":"user","parts":[{"text":"third"}]}]}}`)
	out3 := applyAntigravityAgentEnvelope(turn3, "gemini-envelope-test", "proj-1", "-123", now)
	match3 := antigravityAgentRequestIDPattern.FindStringSubmatch(gjson.GetBytes(out3, "requestId").String())
	if match3 == nil || match3[1] != cascadeID || match3[3] != trajectoryID || match3[4] != "5" {
		t.Fatalf("turn 3 requestId = %q, want same conversation IDs and step 5", gjson.GetBytes(out3, "requestId").String())
	}
	labels3 := gjson.GetBytes(out3, "request.labels")
	if labels3.Get("last_step_index").String() != "4" || labels3.Get("request_id").String() != trajectoryID+"-2" {
		t.Fatalf("turn 3 labels = %s, want last_step_index 4 and request_id suffix -2", labels3.Raw)
	}
	if id := labels3.Get("last_execution_id").String(); len(id) != 36 {
		t.Fatalf("turn 3 last_execution_id = %q, want a UUID", id)
	}
	if gjson.GetBytes(out1, "request.labels.last_execution_id").Exists() {
		t.Fatal("first turn must not carry last_execution_id")
	}

	// Another project gets different conversation IDs for the same session.
	other := applyAntigravityAgentEnvelope(turn1, "gemini-envelope-test", "proj-2", "-123", now)
	if strings.Contains(gjson.GetBytes(other, "requestId").String(), cascadeID) {
		t.Fatal("conversation IDs must differ between projects")
	}
}

func TestAntigravityAgentEnvelopeClaudeLabelsAndToolResults(t *testing.T) {
	payload := []byte(`{"request":{"contents":[
		{"role":"user","parts":[{"text":"run it"}]},
		{"role":"model","parts":[{"functionCall":{"name":"f","args":{}}}]},
		{"role":"user","parts":[{"functionResponse":{"name":"f","response":{}}}]}]}}`)
	out := applyAntigravityAgentEnvelope(payload, "claude-sonnet-4-6", "proj", "-1", time.Now())
	labels := gjson.GetBytes(out, "request.labels")
	for _, key := range []string{"used_claude", "used_claude_conservative", "used_non_gemini_model"} {
		if labels.Get(key).String() != "true" {
			t.Fatalf("labels.%s = %q, want true for a Claude model", key, labels.Get(key).String())
		}
	}
	if labels.Get("model_enum").Exists() {
		t.Fatal("model_enum must be omitted when the catalog has not reported it")
	}
}

// The sequence below is the desktop client's captured conversation: two user
// turns, each answered by one view_file call and a text reply. The client sends
// tool results with role "model"; Gemini-style requests use role "user".
func TestAntigravityAgentEnvelopeToolTurnsMatchDesktopCounts(t *testing.T) {
	const (
		user     = `{"role":"user","parts":[{"text":"q"}]}`
		call     = `{"role":"model","parts":[{"functionCall":{"name":"view_file","args":{}}}]}`
		result   = `{"role":"%s","parts":[{"functionResponse":{"name":"view_file","response":{}}}]}`
		answered = `{"role":"model","parts":[{"text":"ok"}]}`
	)
	for _, resultRole := range []string{"model", "user"} {
		toolResult := strings.Replace(result, "%s", resultRole, 1)
		for _, tc := range []struct {
			contents  []string
			step      string
			requestID string
			execution bool
		}{
			{[]string{user}, "1", "0", false},
			{[]string{user, call, toolResult}, "3", "1", false},
			{[]string{user, call, toolResult, answered, user}, "5", "2", true},
			{[]string{user, call, toolResult, answered, user, call, toolResult}, "7", "3", true},
		} {
			payload := []byte(`{"request":{"contents":[` + strings.Join(tc.contents, ",") + `]}}`)
			out := applyAntigravityAgentEnvelope(payload, "gemini-3.8-flash-high", "proj", "-1", time.Now())
			match := antigravityAgentRequestIDPattern.FindStringSubmatch(gjson.GetBytes(out, "requestId").String())
			labels := gjson.GetBytes(out, "request.labels")
			if match == nil || match[4] != tc.step || labels.Get("request_id").String() != labels.Get("trajectory_id").String()+"-"+tc.requestID || labels.Get("last_execution_id").Exists() != tc.execution {
				t.Fatalf("result role %s, %d contents: requestId=%s labels=%s; want step %s, request_id suffix %s, last_execution_id %v",
					resultRole, len(tc.contents), gjson.GetBytes(out, "requestId").String(), labels.Raw, tc.step, tc.requestID, tc.execution)
			}
		}
	}
}

func TestGeminiToAntigravityUsesAgentEnvelopeOnlyForAgentRequests(t *testing.T) {
	agent := geminiToAntigravity("gemini-x", []byte(`{"request":{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}}`), "proj")
	if !strings.HasPrefix(gjson.GetBytes(agent, "requestId").String(), "agent/") || !gjson.GetBytes(agent, "request.labels").Exists() {
		t.Fatalf("agent request envelope missing: %s", agent)
	}
	other := geminiToAntigravity("gemini-x", []byte(`{"requestType":"other","request":{"contents":[]}}`), "proj")
	if gjson.GetBytes(other, "request.labels").Exists() || strings.HasPrefix(gjson.GetBytes(other, "requestId").String(), "agent/") {
		t.Fatalf("non-agent request must keep the plain envelope: %s", other)
	}
}

func TestApplyAntigravityCatalogGenerationConfig(t *testing.T) {
	budget := func(v int64) *int64 { return &v }
	// Values from the live catalog captured on 2026-10-25.
	RecordAntigravityCatalogModels(map[string]AntigravityCatalogModel{
		"catalog-test-gemini-3.8-flash-medium": {ThinkingBudget: budget(4000), MaxOutputTokens: 65536},
		"catalog-test-gemini-3.5-flash-lite":   {HasThinkingLevel: true, MaxOutputTokens: 65535},
	})
	for _, tc := range []struct {
		name, model, in, want string
	}{
		{"tier level becomes catalog budget", "catalog-test-gemini-3.8-flash-medium",
			`{"request":{"generationConfig":{"maxOutputTokens":10,"thinkingConfig":{"thinkingLevel":"medium","includeThoughts":true}}}}`,
			`{"request":{"generationConfig":{"maxOutputTokens":65536,"thinkingConfig":{"includeThoughts":true,"thinkingBudget":4000}}}}`},
		{"other level is kept", "catalog-test-gemini-3.8-flash-medium",
			`{"request":{"generationConfig":{"thinkingConfig":{"thinkingLevel":"high"}}}}`,
			`{"request":{"generationConfig":{"thinkingConfig":{"thinkingLevel":"high"},"maxOutputTokens":65536}}}`},
		{"no thinking amount gets none added", "catalog-test-gemini-3.8-flash-medium",
			`{"request":{"generationConfig":{}}}`,
			`{"request":{"generationConfig":{"maxOutputTokens":65536}}}`},
		{"level models keep their level", "catalog-test-gemini-3.5-flash-lite",
			`{"request":{"generationConfig":{"thinkingConfig":{"thinkingLevel":"lite"}}}}`,
			`{"request":{"generationConfig":{"thinkingConfig":{"thinkingLevel":"lite"},"maxOutputTokens":65535}}}`},
		{"unknown model drops maxOutputTokens as before", "catalog-test-unknown",
			`{"request":{"generationConfig":{"maxOutputTokens":10,"thinkingConfig":{"thinkingLevel":"medium"}}}}`,
			`{"request":{"generationConfig":{"thinkingConfig":{"thinkingLevel":"medium"}}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(applyAntigravityCatalogGenerationConfig([]byte(tc.in), tc.model)); got != tc.want {
				t.Fatalf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

func TestOrderAntigravityPayloadFollowsDesktopFieldOrder(t *testing.T) {
	in := []byte(`{"model":"m","request":{"sessionId":"-1","generationConfig":{"b":1,"thinkingConfig":{"thinkingBudget":4000,"includeThoughts":true},"maxOutputTokens":65536},"labels":{},"extra":true,"tools":[],"systemInstruction":{},"contents":[]},"enabledCreditTypes":["X"],"requestType":"agent","userAgent":"antigravity","requestId":"r","project":"p"}`)
	want := `{"project":"p","requestId":"r","request":{"contents":[],"systemInstruction":{},"tools":[],"labels":{},"generationConfig":{"maxOutputTokens":65536,"thinkingConfig":{"includeThoughts":true,"thinkingBudget":4000},"b":1},"sessionId":"-1","extra":true},"model":"m","userAgent":"antigravity","requestType":"agent","enabledCreditTypes":["X"]}`
	if got := string(orderAntigravityPayload(in)); got != want {
		t.Fatalf("orderAntigravityPayload =\n%s\nwant\n%s", got, want)
	}
	if got := string(orderAntigravityPayload([]byte(`not json`))); got != "not json" {
		t.Fatalf("non-object payload must pass through, got %q", got)
	}
}
