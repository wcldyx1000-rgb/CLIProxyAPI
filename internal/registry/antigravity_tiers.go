package registry

import (
	"strings"

	"github.com/tidwall/gjson"
)

// antigravityTierFamilies lists models that Antigravity exposes as one upstream
// model ID per thinking tier (<family>-low, -medium, -high, -tiered) instead of
// a single ID with a thinking parameter.
var antigravityTierFamilies = []string{"gemini-3.8-flash"}

// AntigravityTierSuffixes are the upstream tier suffixes of a tiered family.
var AntigravityTierSuffixes = []string{"low", "medium", "high", "tiered"}

// AntigravityTierDefaultSuffix is the tier used when a request sets no thinking.
const AntigravityTierDefaultSuffix = "high"

// AntigravityTierFamilies returns the family IDs that accept a thinking level
// on a single model ID and are routed to the matching upstream tier ID.
func AntigravityTierFamilies() []string {
	return append([]string(nil), antigravityTierFamilies...)
}

// IsAntigravityTierFamily reports whether model is a tiered family ID itself.
func IsAntigravityTierFamily(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	for _, family := range antigravityTierFamilies {
		if model == family {
			return true
		}
	}
	return false
}

// ResolveAntigravityTierModel maps a tiered family ID plus the thinking config
// already written into the translated Antigravity payload to the upstream tier
// ID. Any other model is returned unchanged.
func ResolveAntigravityTierModel(model string, payload []byte) string {
	if !IsAntigravityTierFamily(model) {
		return model
	}
	tier := AntigravityTierDefaultSuffix
	cfg := gjson.GetBytes(payload, "request.generationConfig.thinkingConfig")
	if level := strings.ToLower(strings.TrimSpace(cfg.Get("thinkingLevel").String())); level != "" {
		switch level {
		case "minimal", "low":
			tier = "low"
		case "medium":
			tier = "medium"
		case "tiered":
			tier = "tiered"
		}
	} else if budget := cfg.Get("thinkingBudget"); budget.Type == gjson.Number {
		switch b := budget.Int(); {
		case b < 0:
		case b <= 4096:
			tier = "low"
		case b <= 16384:
			tier = "medium"
		}
	}
	return strings.ToLower(strings.TrimSpace(model)) + "-" + tier
}
