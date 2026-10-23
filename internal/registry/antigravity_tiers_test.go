package registry

import "testing"

func TestResolveAntigravityTierModel(t *testing.T) {
	level := func(l string) []byte {
		return []byte(`{"request":{"generationConfig":{"thinkingConfig":{"thinkingLevel":"` + l + `"}}}}`)
	}
	budget := func(b string) []byte {
		return []byte(`{"request":{"generationConfig":{"thinkingConfig":{"thinkingBudget":` + b + `}}}}`)
	}
	tests := []struct {
		name    string
		model   string
		payload []byte
		want    string
	}{
		{"low", "gemini-3.8-flash", level("low"), "gemini-3.8-flash-low"},
		{"minimal", "gemini-3.8-flash", level("minimal"), "gemini-3.8-flash-low"},
		{"medium", "gemini-3.8-flash", level("medium"), "gemini-3.8-flash-medium"},
		{"high", "gemini-3.8-flash", level("high"), "gemini-3.8-flash-high"},
		{"xhigh", "gemini-3.8-flash", level("xhigh"), "gemini-3.8-flash-high"},
		{"no thinking", "gemini-3.8-flash", []byte(`{}`), "gemini-3.8-flash-high"},
		{"small budget", "gemini-3.8-flash", budget("1024"), "gemini-3.8-flash-low"},
		{"mid budget", "gemini-3.8-flash", budget("8192"), "gemini-3.8-flash-medium"},
		{"large budget", "gemini-3.8-flash", budget("32768"), "gemini-3.8-flash-high"},
		{"dynamic budget", "gemini-3.8-flash", budget("-1"), "gemini-3.8-flash-high"},
		{"explicit tier untouched", "gemini-3.8-flash-low", level("high"), "gemini-3.8-flash-low"},
		{"other model untouched", "gemini-3-flash", level("low"), "gemini-3-flash"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveAntigravityTierModel(tt.model, tt.payload); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
