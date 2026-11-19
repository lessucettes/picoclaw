package providers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
)

func TestOpenCodeGoRejectsTermsRestrictedFreeModel(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "account-key")
	t.Setenv("OPENCODE_ZEN_API_KEY", "zen-only-key")
	cfg := &config.ModelConfig{Provider: "opencode-go", Model: "space-bunny-free"}
	cfg.SetAPIKey("account-key")
	_, _, err := CreateProviderFromConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "no verified endpoint") {
		t.Fatalf("CreateProviderFromConfig() error = %v, want unsupported model error", err)
	}
}

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
		{name: "messages model routed differently in regular Zen", model: "minimax-m3", family: "messages"},
		{name: "messages legacy", model: "qwen3.8-flash", family: "messages", legacyID: true},
		{name: "chat explicit", model: "kimi-k3", family: "chat"},
		{name: "chat legacy", model: "kimi-k3", family: "chat", legacyID: true},
		{name: "chat longcat free", model: "longcat-2.5-free", family: "chat"},
		{name: "chat iceman contributor free", model: "iceman-contributor-free", family: "chat"},
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
	t.Setenv("OPENCODE_API_KEY", "")
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

func TestOpenCodeGoFactoryUsesEnvironmentKeyForNormalizedProvider(t *testing.T) {
	tests := []struct {
		name       string
		provider   string
		model      string
		storedKey  string
		wantModel  string
		wantAPIKey string
	}{
		{name: "canonical provider", provider: "opencode-go", model: "kimi-k3", wantModel: "kimi-k3", wantAPIKey: "environment-test-key"},
		{name: "normalized provider", provider: " OpenCode-Go ", model: "kimi-k3", wantModel: "kimi-k3", wantAPIKey: "environment-test-key"},
		{name: "normalized legacy prefix", model: "OpenCode-Go/kimi-k3", wantModel: "kimi-k3", wantAPIKey: "environment-test-key"},
		{name: "blank stored key falls back", provider: "opencode-go", model: "kimi-k3", storedKey: " ", wantModel: "kimi-k3", wantAPIKey: "environment-test-key"},
		{name: "stored key takes precedence", provider: "opencode-go", model: "kimi-k3", storedKey: "configured-test-key", wantModel: "kimi-k3", wantAPIKey: "configured-test-key"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OPENCODE_API_KEY", "environment-test-key")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != "Bearer "+tc.wantAPIKey {
					t.Errorf("Authorization = %q, want configured test key", got)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
			}))
			defer server.Close()

			cfg := &config.ModelConfig{
				Provider: tc.provider,
				Model:    tc.model,
				APIBase:  server.URL + "/zen/go/v1",
			}
			if tc.storedKey != "" {
				cfg.SetAPIKey(tc.storedKey)
			}
			provider, modelID, err := CreateProviderFromConfig(cfg)
			if err != nil {
				t.Fatalf("CreateProviderFromConfig() error = %v", err)
			}
			if modelID != tc.wantModel {
				t.Fatalf("model ID = %q, want %q", modelID, tc.wantModel)
			}
			if _, err = provider.Chat(t.Context(), []Message{{Role: "user", Content: "hi"}}, nil, modelID, nil); err != nil {
				t.Fatalf("Chat() error = %v", err)
			}
		})
	}
}

func TestOpenCodeGoFactoryUsesEnvironmentKeyForResponses(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "environment-test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/zen/go/v1/responses" {
			t.Errorf("request path = %q, want /zen/go/v1/responses", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer environment-test-key" {
			t.Errorf("Authorization = %q, want environment API key", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","status":"completed","output":[]}`))
	}))
	defer server.Close()

	cfg := &config.ModelConfig{
		Provider: "opencode-go",
		Model:    "grok-4.7",
		APIBase:  server.URL + "/zen/go/v1",
	}
	provider, modelID, err := CreateProviderFromConfig(cfg)
	if err != nil {
		t.Fatalf("CreateProviderFromConfig() error = %v", err)
	}
	if _, err := provider.Chat(t.Context(), []Message{{Role: "user", Content: "hi"}}, nil, modelID, nil); err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
}

func TestOpenCodeGoFactoryUsesProxyForMessagesModels(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "")
	proxyHit := make(chan struct{}, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Scheme + "://" + r.URL.Host; got != "http://opencode-go.invalid" {
			t.Errorf("proxy request target = %q, want http://opencode-go.invalid", got)
		}
		if got := r.URL.Path; got != "/zen/go/v1/messages" {
			t.Errorf("proxy request path = %q, want /zen/go/v1/messages", got)
		}
		if got := r.Header.Get("X-API-Key"); got != "proxy-test-key" {
			t.Errorf("X-API-Key = %q, want configured key", got)
		}
		proxyHit <- struct{}{}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","model":"qwen3.8-flash","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer proxy.Close()

	cfg := &config.ModelConfig{
		Provider: "opencode-go",
		Model:    "qwen3.8-flash",
		APIBase:  "http://opencode-go.invalid/zen/go/v1",
		Proxy:    proxy.URL,
	}
	cfg.SetAPIKey("proxy-test-key")
	provider, modelID, err := CreateProviderFromConfig(cfg)
	if err != nil {
		t.Fatalf("CreateProviderFromConfig() error = %v", err)
	}
	response, err := provider.Chat(t.Context(), []Message{{Role: "user", Content: "hi"}}, nil, modelID, map[string]any{"max_tokens": 1024})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if response.Content != "ok" {
		t.Fatalf("response content = %q, want ok", response.Content)
	}
	select {
	case <-proxyHit:
	default:
		t.Fatal("Messages request did not pass through configured proxy")
	}
}
