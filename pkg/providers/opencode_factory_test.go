package providers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
)

func TestOpenCodeZenFactoryUsesZenRoutesAndHeaders(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "go-only-key")
	t.Setenv("OPENCODE_ZEN_API_KEY", "zen-env-key")

	tests := []struct {
		name   string
		model  string
		family string
	}{
		{name: "responses model", model: "muse-spark-1.3", family: "responses"},
		{name: "messages model", model: "claude-opus-4-6", family: "messages"},
		{name: "model whose Go route differs", model: "minimax-m3", family: "chat"},
		{name: "chat model", model: "deepseek-v4-flash", family: "chat"},
		{name: "Kimi model", model: "kimi-k3", family: "chat"},
		{name: "Gemini model", model: "gemini-3.8-flash", family: "google"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantPath := "/zen/v1/" + map[string]string{
				"responses": "responses",
				"messages":  "messages",
				"chat":      "chat/completions",
				"google":    "models/" + tc.model + ":generateContent",
			}[tc.family]
			var requestModel string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("request method = %q, want POST", r.Method)
				}
				if r.URL.Path != wantPath {
					t.Errorf("request path = %q, want %q", r.URL.Path, wantPath)
				}
				wantAuthName, wantAuthValue := "Authorization", "Bearer zen-test-key"
				switch tc.family {
				case "messages":
					wantAuthName, wantAuthValue = "X-Api-Key", "zen-test-key"
					if got := r.Header.Get("Anthropic-Version"); got != "2023-06-01" {
						t.Errorf("Anthropic-Version = %q, want 2023-06-01", got)
					}
				case "google":
					wantAuthName, wantAuthValue = "X-Goog-Api-Key", "zen-test-key"
				}
				if got := r.Header.Get(wantAuthName); got != wantAuthValue {
					t.Errorf("%s = %q, want configured Zen key", wantAuthName, got)
				}
				if got := r.Header.Get("x-opencode-session"); got != "conversation:zen" {
					t.Errorf("x-opencode-session = %q, want conversation:zen", got)
				}
				if got := r.Header.Get("User-Agent"); got != "PicoClaw/zen-test" {
					t.Errorf("User-Agent = %q, want PicoClaw/zen-test", got)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode request body: %v", err)
				} else if tc.family != "google" {
					requestModel, _ = body["model"].(string)
					if requestModel != tc.model {
						t.Errorf("request model = %#v, want %q", body["model"], tc.model)
					}
				}

				w.Header().Set("Content-Type", "application/json")
				var response string
				switch tc.family {
				case "responses":
					response = `{"id":"resp_1","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`
				case "messages":
					response = `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","model":"claude-opus-4-6","usage":{"input_tokens":1,"output_tokens":1}}`
				case "google":
					response = `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`
				default:
					response = `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`
				}
				_, _ = w.Write([]byte(response))
			}))
			defer server.Close()

			cfg := &config.ModelConfig{
				Provider:  "opencode",
				Model:     tc.model,
				APIBase:   server.URL + "/zen/v1",
				UserAgent: "PicoClaw/zen-test",
			}
			cfg.SetAPIKey("zen-test-key")
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
				map[string]any{"max_tokens": 1024, "session_key": "conversation:zen"},
			)
			if err != nil {
				t.Fatalf("Chat() error = %v", err)
			}
			if response.Content != "ok" {
				t.Errorf("response content = %q, want ok", response.Content)
			}
			if tc.family != "google" && requestModel != tc.model {
				t.Errorf("wire model = %q, want %q", requestModel, tc.model)
			}
		})
	}
}

func TestOpenCodeProviderMetadataKeepsZenAndGoBasesSeparate(t *testing.T) {
	if got := DefaultAPIBaseForProtocol("opencode"); got != "https://opencode.ai/zen/v1" {
		t.Fatalf("opencode API base = %q, want https://opencode.ai/zen/v1", got)
	}
	if got := DefaultAPIBaseForProtocol("opencode-go"); got != "https://opencode.ai/zen/go/v1" {
		t.Fatalf("opencode-go API base = %q, want https://opencode.ai/zen/go/v1", got)
	}

	var zenFound, goFound bool
	for _, option := range ModelProviderOptions() {
		switch option.ID {
		case "opencode":
			zenFound = option.SupportsFetch && option.CreateAllowed && option.DefaultAPIBase == "https://opencode.ai/zen/v1"
		case "opencode-go":
			goFound = option.SupportsFetch && option.CreateAllowed && option.DefaultAPIBase == "https://opencode.ai/zen/go/v1"
		}
	}
	if !zenFound || !goFound {
		t.Fatalf("OpenCode provider options do not expose separate Zen and Go APIs (zen=%t go=%t)", zenFound, goFound)
	}
}

func TestOpenCodeZenFactorySupportsZenEnvironmentKeyAlias(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "shared-open-code-key")
	t.Setenv("OPENCODE_ZEN_API_KEY", "zen-env-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer zen-env-key" {
			t.Errorf("Authorization = %q, want Zen-specific environment key alias", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	cfg := &config.ModelConfig{Provider: "opencode", Model: "kimi-k3", APIBase: server.URL + "/zen/v1"}
	provider, modelID, err := CreateProviderFromConfig(cfg)
	if err != nil {
		t.Fatalf("CreateProviderFromConfig() error = %v", err)
	}
	if _, err := provider.Chat(t.Context(), []Message{{Role: "user", Content: "hi"}}, nil, modelID, nil); err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
}

func TestOpenCodeZenFactoryUsesCanonicalOpenCodeEnvironmentKey(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "shared-open-code-key")
	t.Setenv("OPENCODE_ZEN_API_KEY", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer shared-open-code-key" {
			t.Errorf("Authorization = %q, want canonical OpenCode API key", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	cfg := &config.ModelConfig{Provider: "opencode", Model: "kimi-k3", APIBase: server.URL + "/zen/v1"}
	provider, modelID, err := CreateProviderFromConfig(cfg)
	if err != nil {
		t.Fatalf("CreateProviderFromConfig() error = %v", err)
	}
	if _, err := provider.Chat(t.Context(), []Message{{Role: "user", Content: "hi"}}, nil, modelID, nil); err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
}

func TestOpenCodeZenFactoryRejectsTermsRestrictedFreeModels(t *testing.T) {
	for _, model := range []string{
		"big-pickle",
		"deepseek-v4-flash-free",
		"jev-1.13-free",
		"ling-3.0-flash-fin-free",
		"muse-spark-1.3-contributor-free",
		"muse-spark-1.2-contributor-free",
		"mimo-v2.6-flash-free",
		"mimo-v2.5-free",
		"space-bunny-free",
		"nemotron-3-ultra-free",
		"nemotron-3.5-lightning-free",
	} {
		t.Run(model, func(t *testing.T) {
			cfg := &config.ModelConfig{Provider: "opencode", Model: model}
			cfg.SetAPIKey("test-key")
			_, _, err := CreateProviderFromConfig(cfg)
			if err == nil {
				t.Fatal("CreateProviderFromConfig() succeeded for a terms-restricted free model")
			}
		})
	}
}

func TestOpenCodeZenFactoryStillRequiresKeyForPaidAndCustomBaseModels(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("OPENCODE_ZEN_API_KEY", "")

	for _, tc := range []struct {
		name    string
		model   string
		apiBase string
	}{
		{name: "paid model", model: "claude-opus-4-6"},
		{name: "paid model on custom base", model: "kimi-k3", apiBase: "https://gateway.example/v1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.ModelConfig{Provider: "opencode", Model: tc.model, APIBase: tc.apiBase}
			_, _, err := CreateProviderFromConfig(cfg)
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
			}
			if err == nil || !strings.Contains(errMsg, "OPENCODE_ZEN_API_KEY") || !strings.Contains(errMsg, "OPENCODE_API_KEY") {
				t.Fatalf("CreateProviderFromConfig() error = %v, want missing Zen key error naming both environment variables", err)
			}
		})
	}
}

func TestOpenCodeZenFactoryRejectsDeprecatedUnverifiedModel(t *testing.T) {
	cfg := &config.ModelConfig{Provider: "opencode", Model: "claude-sonnet-4"}
	cfg.SetAPIKey("test-key")
	_, _, err := CreateProviderFromConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "no verified endpoint") {
		t.Fatalf("CreateProviderFromConfig() error = %v, want deprecated unverified model to fail closed", err)
	}
}

func TestOpenCodeZenFactoryRejectsSystemOneWithoutAnAPIKey(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("OPENCODE_ZEN_API_KEY", "")
	cfg := &config.ModelConfig{Provider: "opencode", Model: "jev-1.13"}
	_, _, err := CreateProviderFromConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "SystemOne") {
		t.Fatalf("CreateProviderFromConfig() error = %v, want clear SystemOne incompatibility", err)
	}
}

func TestOpenCodeZenModelFamiliesStaySeparateFromOpenCodeGo(t *testing.T) {
	if got := openCodeZenEndpointFamily("minimax-m3"); got != "chat" {
		t.Fatalf("Zen minimax-m3 family = %q, want chat", got)
	}
	if got := openCodeGoEndpointFamily("minimax-m3"); got != "messages" {
		t.Fatalf("Go minimax-m3 family = %q, want messages", got)
	}
	if openCodeZenEndpointFamily("longcat-2.0") != "" || openCodeGoEndpointFamily("big-pickle") != "" {
		t.Fatal("provider-specific model IDs leaked across endpoint-family maps")
	}
	if got := openCodeZenEndpointFamily("unlisted-model"); got != "" {
		t.Fatalf("unlisted model family = %q, want empty", got)
	}
}
