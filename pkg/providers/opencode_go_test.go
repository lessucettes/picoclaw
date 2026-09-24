package providers

import "testing"

func TestOpenCodeGoCatalogAndRoutes(t *testing.T) {
	option, ok := modelProviderOptionForName("opencode-go")
	if !ok || option.DefaultAPIBase != "https://opencode.ai/zen/go/v1" || !option.CreateAllowed || !option.SupportsFetch || !option.DefaultModelAllowed || !option.httpAPI {
		t.Fatalf("OpenCode Go catalog = %+v, found = %v", option, ok)
	}

	for _, tc := range []struct{ model, want string }{
		{"grok-4.7", "responses"},
		{"grok-4.6", "responses"},
		{"gpt-6-luna", "responses"},
		{"gpt-5.6-luna", "responses"},
		{"muse-spark-1.3-contributor", "responses"},
		{"muse-spark-1.2-contributor", "responses"},
		{"minimax-m3", "messages"},
		{"minimax-m2.7", "messages"},
		{"minimax-m2.5", "messages"},
		{"qwen3.8-max", "messages"},
		{"qwen3.8-flash", "messages"},
		{"qwen3.7-max", "messages"},
		{"qwen3.7-plus", "messages"},
		{"qwen3.6-plus", "messages"},
		{"kimi-k3", "chat"},
		{"deepseek-v4-pro", "chat"},
		{"hy4-preview", "chat"},
		{"space-bunny-free", "chat"},
		{"claude-opus-4-6", ""},
		{"grok-4.5", ""},
		{"omen-alpha", ""},
		{"unknown-model", ""},
	} {
		t.Run(tc.model, func(t *testing.T) {
			if got := openCodeGoEndpointFamily(tc.model); got != tc.want {
				t.Errorf("route(%q) = %q, want %q", tc.model, got, tc.want)
			}
		})
	}
}
