package cliproxy

import "testing"

func TestFilterAntigravityModelsExposesDeclaredTiers(t *testing.T) {
	catalog := []*ModelInfo{{ID: "gemini-3.8-flash-high", Name: "gemini-3.8-flash-high", Description: "Gemini 3.8 Flash (High)"}}
	hints := antigravityModelCapabilityHints{ModelIDs: map[string]struct{}{
		"gemini-3.8-flash-high":   {},
		"gemini-3.8-flash-low":    {},
		"gemini-3.8-flash-medium": {},
	}}

	got := filterAntigravityModels(catalog, hints)
	ids := map[string]string{}
	for _, m := range got {
		ids[m.ID] = m.Description
	}
	if len(ids) != 3 {
		t.Fatalf("got %v, want high/low/medium", ids)
	}
	if ids["gemini-3.8-flash-low"] != "Gemini 3.8 Flash (Low)" {
		t.Fatalf("low description = %q", ids["gemini-3.8-flash-low"])
	}
	if _, ok := ids["gemini-3.8-flash-tiered"]; ok {
		t.Fatal("tiered must not appear when upstream does not declare it")
	}
	if len(catalog) != 1 {
		t.Fatal("catalog slice must not be mutated")
	}
}
