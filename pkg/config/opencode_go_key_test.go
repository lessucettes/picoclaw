package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenCodeGoAPIKeyEnvFallback(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		model    string
		envKey   string
		stored   string
		want     string
	}{
		{name: "explicit provider uses environment", provider: "opencode-go", model: "kimi-k3", envKey: "env-key", want: "env-key"},
		{name: "legacy model prefix uses environment", model: "opencode-go/kimi-k3", envKey: "env-key", want: "env-key"},
		{name: "configured key wins", provider: "opencode-go", model: "kimi-k3", envKey: "env-key", stored: "configured-key", want: "configured-key"},
		{name: "other provider ignores environment", provider: "openai", model: "gpt-5.4", envKey: "env-key"},
		{name: "blank environment is ignored", provider: "opencode-go", model: "kimi-k3", envKey: "  \t"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OPENCODE_API_KEY", tc.envKey)
			model := &ModelConfig{Provider: tc.provider, Model: tc.model}
			if tc.stored != "" {
				model.SetAPIKey(tc.stored)
			}
			if got := model.APIKey(); got != tc.want {
				t.Fatalf("APIKey() = %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("environment value is not serialized", func(t *testing.T) {
		const secret = "env-only-secret"
		t.Setenv("OPENCODE_API_KEY", secret)
		model := &ModelConfig{Provider: "opencode-go", Model: "kimi-k3"}
		data, err := json.Marshal(model)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		if strings.Contains(string(data), secret) {
			t.Fatalf("serialized model contains environment secret")
		}
	})
}
