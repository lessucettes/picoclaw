package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenCodeGoEnvironmentKeyIsAvailableWithoutExposingIt(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		model    string
		envKey   string
		stored   string
		wantAPI  string
		wantHas  bool
	}{
		{name: "explicit provider", provider: "opencode-go", model: "kimi-k3", envKey: "env-key", wantHas: true},
		{name: "provider normalization", provider: " OpenCode-Go ", model: "kimi-k3", envKey: "env-key", wantHas: true},
		{name: "legacy model prefix normalization", model: "OpenCode-Go/kimi-k3", envKey: "env-key", wantHas: true},
		{name: "bare unprefixed model does not infer provider", model: "opencode-go", envKey: "env-key"},
		{name: "explicit provider takes precedence", provider: "openai", model: "opencode-go/kimi-k3", envKey: "env-key"},
		{name: "configured key wins", provider: "opencode-go", model: "kimi-k3", envKey: "env-key", stored: "configured-key", wantAPI: "configured-key", wantHas: true},
		{name: "blank stored key falls back to environment", provider: "opencode-go", model: "kimi-k3", envKey: "env-key", stored: " ", wantAPI: " ", wantHas: true},
		{name: "other provider ignores environment", provider: "openai", model: "gpt-5.4", envKey: "env-key"},
		{name: "blank environment is ignored", provider: "opencode-go", model: "kimi-k3", envKey: "  	"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OPENCODE_API_KEY", tc.envKey)
			model := &ModelConfig{Provider: tc.provider, Model: tc.model}
			if tc.stored != "" {
				model.SetAPIKey(tc.stored)
			}
			if got := model.APIKey(); got != tc.wantAPI {
				t.Fatalf("APIKey() = %q, want stored key %q", got, tc.wantAPI)
			}
			if got := model.HasAPIKey(); got != tc.wantHas {
				t.Fatalf("HasAPIKey() = %t, want %t", got, tc.wantHas)
			}
			wantStoredCount := 0
			if tc.stored != "" {
				wantStoredCount = 1
			}
			if len(model.APIKeys) != wantStoredCount {
				t.Fatalf("stored API key count = %d, want %d", len(model.APIKeys), wantStoredCount)
			}
		})
	}

	t.Run("environment secret does not enter model serialization", func(t *testing.T) {
		const secret = "env-only-secret"
		t.Setenv("OPENCODE_API_KEY", secret)
		model := &ModelConfig{Provider: "opencode-go", Model: "kimi-k3"}
		if !model.HasAPIKey() || model.APIKey() != "" || len(model.APIKeys) != 0 {
			t.Fatalf("environment key leaked into stored model state")
		}
		data, err := json.Marshal(model)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		if strings.Contains(string(data), secret) {
			t.Fatalf("serialized model contains environment secret")
		}
	})
}
