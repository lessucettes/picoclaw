package providers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
)

func TestOpenCodeGoFactoryRoutesAllFamilies(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		family   string
		legacyID bool
	}{
		{name: "responses explicit", model: "grok-4.7", family: "responses"},
		{name: "responses legacy", model: "grok-4.7", family: "responses", legacyID: true},
		{name: "messages explicit", model: "qwen3.8-flash", family: "messages"},
		{name: "messages legacy", model: "qwen3.8-flash", family: "messages", legacyID: true},
		{name: "chat explicit", model: "kimi-k3", family: "chat"},
		{name: "chat legacy", model: "kimi-k3", family: "chat", legacyID: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := "/zen/go/v1/" + map[string]string{
				"responses": "responses",
				"messages":  "messages",
				"chat":      "chat/completions",
			}[tc.family]
			var requestModel string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("request method = %q, want POST", r.Method)
				}
				if r.URL.Path != path {
					t.Errorf("request path = %q, want %q", r.URL.Path, path)
				}
				wantAuthName, wantAuthValue := "Authorization", "Bearer test-key"
				if tc.family == "messages" {
					wantAuthName, wantAuthValue = "X-Api-Key", "test-key"
					if got := r.Header.Get("Anthropic-Version"); got != "2023-06-01" {
						t.Errorf("Anthropic-Version = %q, want 2023-06-01", got)
					}
				}
				if got := r.Header.Get(wantAuthName); got != wantAuthValue {
					t.Errorf("%s = %q, want %q", wantAuthName, got, wantAuthValue)
				}
				if got := r.Header.Get("x-opencode-session"); got != "conversation:factory" {
					t.Errorf("x-opencode-session = %q, want conversation:factory", got)
				}
				if got := r.Header.Get("User-Agent"); got != "PicoClaw/factory-test" {
					t.Errorf("User-Agent = %q, want PicoClaw/factory-test", got)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode request body: %v", err)
				} else if got, ok := body["model"].(string); ok {
					requestModel = got
				} else {
					t.Errorf("request model = %#v, want string %q", body["model"], tc.model)
				}
				w.Header().Set("Content-Type", "application/json")
				var response string
				switch tc.family {
				case "responses":
					response = `{"id":"resp_1","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`
				case "messages":
					response = `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","model":"qwen3.8-flash","usage":{"input_tokens":1,"output_tokens":1}}`
				default:
					response = `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`
				}
				_, _ = w.Write([]byte(response))
			}))
			defer server.Close()

			model := tc.model
			providerName := "opencode-go"
			if tc.legacyID {
				model = "opencode-go/" + model
				providerName = ""
			}
			cfg := &config.ModelConfig{
				Provider:  providerName,
				Model:     model,
				APIBase:   server.URL + "/zen/go/v1",
				UserAgent: "PicoClaw/factory-test",
			}
			cfg.SetAPIKey("test-key")

			provider, modelID, err := CreateProviderFromConfig(cfg)
			if err != nil {
				t.Fatalf("CreateProviderFromConfig() error = %v", err)
			}
			if modelID != tc.model {
				t.Fatalf("effective model ID = %q, want %q", modelID, tc.model)
			}
			response, err := provider.Chat(
				t.Context(),
				[]Message{{Role: "user", Content: "say ok"}},
				nil,
				tc.model,
				map[string]any{"max_tokens": 1024, "session_key": "conversation:factory"},
			)
			if err != nil {
				t.Fatalf("Chat() error = %v", err)
			}
			if response.Content != "ok" {
				t.Errorf("response content = %q, want ok", response.Content)
			}
			if requestModel != tc.model {
				t.Errorf("wire model = %q, want %q", requestModel, tc.model)
			}
		})
	}
}

func TestOpenCodeGoFactoryRejectsUnverifiedAndKeylessModels(t *testing.T) {
	t.Run("unverified model", func(t *testing.T) {
		cfg := &config.ModelConfig{Provider: "opencode-go", Model: "omen-alpha"}
		cfg.SetAPIKey("test-key")
		_, _, err := CreateProviderFromConfig(cfg)
		if err == nil || !strings.Contains(err.Error(), "no verified endpoint") {
			t.Fatalf("CreateProviderFromConfig() error = %v, want unverified endpoint error", err)
		}
	})

	t.Run("keyless custom base", func(t *testing.T) {
		cfg := &config.ModelConfig{
			Provider: "opencode-go",
			Model:    "kimi-k3",
			APIBase:  "http://127.0.0.1:1/zen/go/v1",
		}
		_, _, err := CreateProviderFromConfig(cfg)
		if err == nil || !strings.Contains(err.Error(), "api_key or OPENCODE_API_KEY is required") {
			t.Fatalf("CreateProviderFromConfig() error = %v, want missing-key error", err)
		}
	})

	t.Run("route message is useful", func(t *testing.T) {
		cfg := &config.ModelConfig{Provider: "opencode-go", Model: "grok-4.7-pro"}
		cfg.SetAPIKey("test-key")
		_, _, err := CreateProviderFromConfig(cfg)
		if err == nil || !strings.Contains(err.Error(), "https://opencode.ai/docs/go/#endpoints") {
			t.Fatalf("CreateProviderFromConfig() error = %v, want endpoint documentation URL", err)
		}
	})
}
