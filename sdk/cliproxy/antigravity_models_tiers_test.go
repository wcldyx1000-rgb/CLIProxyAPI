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
	for _, want := range []string{"gemini-3.8-flash-high", "gemini-3.8-flash-low", "gemini-3.8-flash-medium", "gemini-3.8-flash"} {
		if _, ok := ids[want]; !ok {
			t.Fatalf("missing %s in %v", want, ids)
		}
	}
	if len(ids) != 4 {
		t.Fatalf("got %v, want 4 models", ids)
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

func TestAddAntigravityTierFamilyModelsKeepsOthers(t *testing.T) {
	models := []*ModelInfo{{ID: "gemini-3-flash"}}
	if got := addAntigravityTierFamilyModels(models); len(got) != 1 {
		t.Fatalf("got %d models, want 1 when no tier template exists", len(got))
	}
}
