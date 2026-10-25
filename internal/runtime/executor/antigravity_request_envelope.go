package executor

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// The request envelope below mirrors what the Antigravity desktop client
// (2.26.0, language_server) sends for agent requests, as captured from its
// decrypted traffic:
//
//   - requestId is agent/<cascade_id>/<unix_ms>/<trajectory_id>/<step>, where
//     step equals the number of request.contents entries, in plain turns and in
//     tool-call continuations alike.
//   - request.labels carries the same conversation identifiers. Its keys are
//     emitted in sorted order. labels.request_id ends with the number of model
//     outputs (text or functionCall) already in the conversation, so it grows
//     by one per generate call; last_execution_id appears once the user has
//     spoken more than once.
//   - Top-level and request-level fields follow a fixed order.
//
// Project-scoped deterministic IDs keep one conversation's identifiers stable
// across turns and retries without storing per-conversation state.

// AntigravityCatalogModel holds the fetchAvailableModels fields the desktop
// client copies into its generate requests.
type AntigravityCatalogModel struct {
	// Enum is the "model" field ("MODEL_PLACEHOLDER_M20" and similar), sent as
	// labels.model_enum.
	Enum string
	// ThinkingBudget is the catalog thinkingBudget, sent as
	// generationConfig.thinkingConfig.thinkingBudget; nil when absent.
	ThinkingBudget *int64
	// HasThinkingLevel reports a catalog thinkingLevel; such models are sent a
	// level instead of a budget.
	HasThinkingLevel bool
	// MaxOutputTokens is sent as generationConfig.maxOutputTokens; 0 when absent.
	MaxOutputTokens int64
}

var (
	antigravityCatalogModelsMu sync.RWMutex
	antigravityCatalogModels   = map[string]AntigravityCatalogModel{}
)

// RecordAntigravityCatalogModels stores catalog details per model ID, replacing
// earlier entries for the same IDs.
func RecordAntigravityCatalogModels(models map[string]AntigravityCatalogModel) {
	if len(models) == 0 {
		return
	}
	antigravityCatalogModelsMu.Lock()
	defer antigravityCatalogModelsMu.Unlock()
	for modelID, details := range models {
		if modelID = strings.ToLower(strings.TrimSpace(modelID)); modelID != "" {
			details.Enum = strings.TrimSpace(details.Enum)
			antigravityCatalogModels[modelID] = details
		}
	}
}

func antigravityCatalogModel(modelName string) (AntigravityCatalogModel, bool) {
	antigravityCatalogModelsMu.RLock()
	defer antigravityCatalogModelsMu.RUnlock()
	details, ok := antigravityCatalogModels[strings.ToLower(strings.TrimSpace(modelName))]
	return details, ok
}

func antigravityModelEnum(modelName string) string {
	details, _ := antigravityCatalogModel(modelName)
	return details.Enum
}

// applyAntigravityCatalogGenerationConfig fills a Gemini request's output limit
// and thinking amount from the model catalog, as the desktop client does (it
// sent maxOutputTokens 65536 and thinkingBudget -1 for gemini-3.8-flash-high,
// whose catalog entry carries exactly those values and no thinkingLevel).
//
//   - maxOutputTokens becomes the catalog value. The service already dropped
//     the client's own value for Gemini models, so this only adds the field.
//   - A thinkingLevel equal to the model's tier suffix (the "medium" that
//     selected gemini-3.8-flash-medium) is replaced by that model's catalog
//     budget; both name the same tier. Any other level is left alone.
//   - A request without any thinking amount gets none added. At this point an
//     explicitly disabled request (whose thinkingConfig was removed) looks the
//     same, and adding a budget would turn its thinking back on.
//
// Models without a catalog entry keep the previous behavior: no maxOutputTokens.
func applyAntigravityCatalogGenerationConfig(payload []byte, modelName string) []byte {
	details, ok := antigravityCatalogModel(modelName)
	if !ok {
		payload, _ = sjson.DeleteBytes(payload, "request.generationConfig.maxOutputTokens")
		return payload
	}
	if details.MaxOutputTokens > 0 {
		payload, _ = sjson.SetBytes(payload, "request.generationConfig.maxOutputTokens", details.MaxOutputTokens)
	} else {
		payload, _ = sjson.DeleteBytes(payload, "request.generationConfig.maxOutputTokens")
	}
	if details.ThinkingBudget == nil || details.HasThinkingLevel {
		return payload
	}
	const thinkingPath = "request.generationConfig.thinkingConfig"
	level := gjson.GetBytes(payload, thinkingPath+".thinkingLevel")
	if level.Exists() && !gjson.GetBytes(payload, thinkingPath+".thinkingBudget").Exists() &&
		strings.HasSuffix(strings.ToLower(modelName), "-"+strings.ToLower(level.String())) {
		payload, _ = sjson.DeleteBytes(payload, thinkingPath+".thinkingLevel")
		payload, _ = sjson.SetBytes(payload, thinkingPath+".thinkingBudget", *details.ThinkingBudget)
	}
	return payload
}

// antigravityDerivedUUID returns a version-4 formatted UUID derived from the
// given parts, matching the format of the client's random conversation IDs.
func antigravityDerivedUUID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	id := uuid.UUID(sum[:16])
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return id.String()
}

// antigravityCountContents counts contents with the given role, skipping tool
// results: a content made only of functionResponse parts. Tool results carry
// role "model" for Gemini models and "user" for Claude models, in the desktop
// client and in this service alike (normalizeAntigravityGeminiFunctionResponseRoles);
// neither is a user turn or a model output.
func antigravityCountContents(contents gjson.Result, role string) int {
	count := 0
	contents.ForEach(func(_, content gjson.Result) bool {
		if content.Get("role").String() != role {
			return true
		}
		content.Get("parts").ForEach(func(_, part gjson.Result) bool {
			if !part.Get("functionResponse").Exists() {
				count++
				return false
			}
			return true
		})
		return true
	})
	return count
}

// applyAntigravityAgentEnvelope sets requestId and request.labels on an agent
// request whose request.sessionId is already set.
func applyAntigravityAgentEnvelope(payload []byte, modelName, projectID, sessionID string, now time.Time) []byte {
	contents := gjson.GetBytes(payload, "request.contents")
	step := max(int(contents.Get("#").Int()), 1)
	turn := max(antigravityCountContents(contents, "user")-1, 0)
	modelOutputs := antigravityCountContents(contents, "model")

	cascadeID := antigravityDerivedUUID("cascade", projectID, sessionID)
	trajectoryID := antigravityDerivedUUID("trajectory", projectID, sessionID)
	requestID := "agent/" + cascadeID + "/" + strconv.FormatInt(now.UnixMilli(), 10) + "/" + trajectoryID + "/" + strconv.Itoa(step)
	payload, _ = sjson.SetBytes(payload, "requestId", requestID)

	lowerModel := strings.ToLower(modelName)
	usedClaude := strconv.FormatBool(strings.Contains(lowerModel, "claude"))
	labels := map[string]string{
		"cascade_id":               cascadeID,
		"last_step_index":          strconv.Itoa(step - 1),
		"request_id":               trajectoryID + "-" + strconv.Itoa(modelOutputs),
		"root_cascade_id":          cascadeID,
		"trajectory_id":            trajectoryID,
		"used_claude":              usedClaude,
		"used_claude_conservative": usedClaude,
		"used_non_gemini_model":    strconv.FormatBool(!strings.HasPrefix(lowerModel, "gemini")),
	}
	// The client adds last_execution_id from the second user turn on, including
	// that turn's tool-call continuations. Its value has no observed relation to
	// the other IDs, so a per-turn derived ID stands in.
	if turn > 0 {
		labels["last_execution_id"] = antigravityDerivedUUID("execution", projectID, sessionID, strconv.Itoa(turn-1))
	}
	if enum := antigravityModelEnum(modelName); enum != "" {
		labels["model_enum"] = enum
	}
	payload, _ = sjson.SetRawBytes(payload, "request.labels", sortedStringMapJSON(labels))
	return payload
}

func sortedStringMapJSON(values map[string]string) []byte {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, key := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		encodedKey, _ := json.Marshal(key)
		encodedValue, _ := json.Marshal(values[key])
		buf.Write(encodedKey)
		buf.WriteByte(':')
		buf.Write(encodedValue)
	}
	buf.WriteByte('}')
	return buf.Bytes()
}

// Field orders observed in the desktop client's generate requests. toolConfig
// did not appear in the captures; it is placed next to tools.
var (
	antigravityTopLevelFieldOrder = []string{"project", "requestId", "request", "model", "userAgent", "requestType"}
	antigravityRequestFieldOrder  = []string{"contents", "systemInstruction", "tools", "toolConfig", "labels", "generationConfig", "sessionId"}
	// The client sends {"maxOutputTokens":…,"thinkingConfig":{"includeThoughts":…,"thinkingBudget":…}}.
	antigravityGenerationConfigFieldOrder = []string{"maxOutputTokens", "thinkingConfig"}
	antigravityThinkingConfigFieldOrder   = []string{"includeThoughts", "thinkingBudget", "thinkingLevel"}
)

// orderAntigravityPayload rewrites the top-level object and its request object
// so known fields come first in the client's order; other fields follow in
// their original order. Values are copied byte for byte.
func orderAntigravityPayload(payload []byte) []byte {
	root := gjson.ParseBytes(payload)
	if !root.IsObject() {
		return payload
	}
	request := root.Get("request")
	var orderedRequest []byte
	if request.IsObject() {
		var orderedGeneration []byte
		if generation := request.Get("generationConfig"); generation.IsObject() {
			var orderedThinking []byte
			if thinkingConfig := generation.Get("thinkingConfig"); thinkingConfig.IsObject() {
				orderedThinking = orderJSONObject(thinkingConfig, antigravityThinkingConfigFieldOrder, nil)
			}
			orderedGeneration = orderJSONObject(generation, antigravityGenerationConfigFieldOrder, map[string][]byte{"thinkingConfig": orderedThinking})
		}
		orderedRequest = orderJSONObject(request, antigravityRequestFieldOrder, map[string][]byte{"generationConfig": orderedGeneration})
	}
	return orderJSONObject(root, antigravityTopLevelFieldOrder, map[string][]byte{"request": orderedRequest})
}

func orderJSONObject(object gjson.Result, order []string, replacements map[string][]byte) []byte {
	type field struct{ key, raw string }
	var fields []field
	object.ForEach(func(key, value gjson.Result) bool {
		fields = append(fields, field{key: key.Str, raw: key.Raw + ":" + value.Raw})
		return true
	})
	rank := make(map[string]int, len(order))
	for i, key := range order {
		rank[key] = i
	}
	sort.SliceStable(fields, func(i, j int) bool {
		ri, okI := rank[fields[i].key]
		rj, okJ := rank[fields[j].key]
		if okI != okJ {
			return okI
		}
		return okI && ri < rj
	})
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, f := range fields {
		if i > 0 {
			buf.WriteByte(',')
		}
		if replacement := replacements[f.key]; replacement != nil {
			encodedKey, _ := json.Marshal(f.key)
			buf.Write(encodedKey)
			buf.WriteByte(':')
			buf.Write(replacement)
			continue
		}
		buf.WriteString(f.raw)
	}
	buf.WriteByte('}')
	return buf.Bytes()
}
