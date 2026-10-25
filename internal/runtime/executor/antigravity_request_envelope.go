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
//     step equals the number of request.contents entries in plain chat turns
//     (1, 3, 5, ...). Turns with tool calls were not captured, so using the
//     contents count there is an inference.
//   - request.labels carries the same conversation identifiers. Its keys are
//     emitted in sorted order.
//   - Top-level and request-level fields follow a fixed order.
//
// Project-scoped deterministic IDs keep one conversation's identifiers stable
// across turns and retries without storing per-conversation state.

var (
	antigravityModelEnumsMu sync.RWMutex
	antigravityModelEnums   = map[string]string{}
)

// RecordAntigravityModelEnums stores the model enum names ("MODEL_PLACEHOLDER_M20"
// and similar) that fetchAvailableModels reports in each model's "model" field.
// The desktop client sends the selected model's enum as labels.model_enum.
func RecordAntigravityModelEnums(enums map[string]string) {
	if len(enums) == 0 {
		return
	}
	antigravityModelEnumsMu.Lock()
	defer antigravityModelEnumsMu.Unlock()
	for modelID, enum := range enums {
		modelID = strings.ToLower(strings.TrimSpace(modelID))
		enum = strings.TrimSpace(enum)
		if modelID != "" && enum != "" {
			antigravityModelEnums[modelID] = enum
		}
	}
}

func antigravityModelEnum(modelName string) string {
	antigravityModelEnumsMu.RLock()
	defer antigravityModelEnumsMu.RUnlock()
	return antigravityModelEnums[strings.ToLower(strings.TrimSpace(modelName))]
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

// antigravityUserTurnIndex counts user turns before the latest one. A user
// content made only of functionResponse parts is a tool result, not a turn.
func antigravityUserTurnIndex(contents gjson.Result) int {
	turns := 0
	contents.ForEach(func(_, content gjson.Result) bool {
		if content.Get("role").String() != "user" {
			return true
		}
		hasUserPart := false
		content.Get("parts").ForEach(func(_, part gjson.Result) bool {
			if !part.Get("functionResponse").Exists() {
				hasUserPart = true
				return false
			}
			return true
		})
		if hasUserPart {
			turns++
		}
		return true
	})
	return max(turns-1, 0)
}

// applyAntigravityAgentEnvelope sets requestId and request.labels on an agent
// request whose request.sessionId is already set.
func applyAntigravityAgentEnvelope(payload []byte, modelName, projectID, sessionID string, now time.Time) []byte {
	contents := gjson.GetBytes(payload, "request.contents")
	step := max(int(contents.Get("#").Int()), 1)
	turn := antigravityUserTurnIndex(contents)

	cascadeID := antigravityDerivedUUID("cascade", projectID, sessionID)
	trajectoryID := antigravityDerivedUUID("trajectory", projectID, sessionID)
	requestID := "agent/" + cascadeID + "/" + strconv.FormatInt(now.UnixMilli(), 10) + "/" + trajectoryID + "/" + strconv.Itoa(step)
	payload, _ = sjson.SetBytes(payload, "requestId", requestID)

	lowerModel := strings.ToLower(modelName)
	usedClaude := strconv.FormatBool(strings.Contains(lowerModel, "claude"))
	labels := map[string]string{
		"cascade_id":               cascadeID,
		"last_step_index":          strconv.Itoa(step - 1),
		"request_id":               trajectoryID + "-" + strconv.Itoa(turn),
		"root_cascade_id":          cascadeID,
		"trajectory_id":            trajectoryID,
		"used_claude":              usedClaude,
		"used_claude_conservative": usedClaude,
		"used_non_gemini_model":    strconv.FormatBool(!strings.HasPrefix(lowerModel, "gemini")),
	}
	// The client adds last_execution_id from the second turn on. Its value has no
	// observed relation to the other IDs, so a per-turn derived ID stands in.
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
		orderedRequest = orderJSONObject(request, antigravityRequestFieldOrder, nil)
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
