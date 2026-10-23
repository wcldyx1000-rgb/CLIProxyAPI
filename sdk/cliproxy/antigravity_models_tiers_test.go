package cliproxy

import "testing"

func TestFilterAntigravityModelsCollapsesTiersIntoFamily(t *testing.T) {
	catalog := []*ModelInfo{
		{ID: "gemini-3.8-flash-high", Name: "gemini-3.8-flash-high", Description: "Gemini 3.8 Flash (High)"},
		{ID: "gemini-3-flash", Name: "gemini-3-flash"},
	}
	hints := antigravityModelCapabilityHints{ModelIDs: map[string]struct{}{
		"gemini-3.8-flash-high":   {},
		"gemini-3.8-flash-low":    {},
		"gemini-3.8-flash-medium": {},
		"gemini-3-flash":          {},
	}}

	got := filterAntigravityModels(catalog, hints)
	ids := map[string]string{}
	for _, m := range got {
		ids[m.ID] = m.Description
	}
	if len(ids) != 2 {
		t.Fatalf("got %v, want gemini-3.8-flash and gemini-3-flash only", ids)
	}
	if desc, ok := ids["gemini-3.8-flash"]; !ok || desc != "Gemini 3.8 Flash" {
		t.Fatalf("family model missing or wrong description: %v", ids)
	}
	if _, ok := ids["gemini-3-flash"]; !ok {
		t.Fatal("unrelated model must be kept")
	}
	if len(catalog) != 2 {
		t.Fatal("catalog slice must not be mutated")
	}
}

func TestCollapseAntigravityTierModelsKeepsOthersWithoutTemplate(t *testing.T) {
	models := []*ModelInfo{{ID: "gemini-3-flash"}}
	if got := collapseAntigravityTierModels(models); len(got) != 1 {
		t.Fatalf("got %d models, want 1 when no tier template exists", len(got))
	}
}
