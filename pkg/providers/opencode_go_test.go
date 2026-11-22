package providers

import (
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
)

func TestOpenCodeGoAPIBase(t *testing.T) {
	for _, tc := range []struct{ format, override, want string }{
		{"", "", "https://opencode.ai/inference/go/openai/v1"},
		{"chat", "", "https://opencode.ai/inference/go/openai/v1"},
		{"responses", "", "https://opencode.ai/inference/go/openai/v1"},
		{" Messages ", "", "https://opencode.ai/inference/go/anthropic/v1"},
		{"messages", "https://example.invalid/api/", "https://example.invalid/api"},
	} {
		mc := &config.ModelConfig{Provider: "opencode-go", Model: "Future-Model", APIFormat: tc.format, APIBase: tc.override}
		if got := ResolveAPIBase(mc); got != tc.want {
			t.Errorf("ResolveAPIBase(format=%q, override=%q) = %q, want %q", tc.format, tc.override, got, tc.want)
		}
	}
}
