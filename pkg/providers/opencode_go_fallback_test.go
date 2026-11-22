package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers/common"
)

func TestHTTPGoneFallback(t *testing.T) {
	for _, tc := range []struct {
		name, provider, format, path, contentType, body string
		status                                          int
		wantFallback                                    bool
	}{
		{"retired model", "opencode-go", "", "/v1/chat/completions", "application/json", `{"type":"error","error":{"type":"ModelDeprecated","message":"Model demo has been deprecated."}}`, http.StatusGone, true},
		{"messages route", "opencode-go", "messages", "/v1/messages", "text/plain", "Endpoint withdrawn", http.StatusGone, true},
		{"responses route", "opencode-go", "responses", "/v1/responses", "text/plain", "", http.StatusGone, true},
		{"HTML proxy response", "openai", "", "/v1/chat/completions", "text/html", "<html>Endpoint withdrawn</html>", http.StatusGone, true},
		{"bad request quoting 410", "opencode-go", "", "/v1/chat/completions", "text/plain", "Request id 410", http.StatusBadRequest, false},
		{"unknown error", "openai", "", "/v1/chat/completions", "text/plain", "Unrecognized failure", http.StatusTeapot, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var primaryCalls, backupCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("request method = %s, want POST", r.Method)
				}
				switch r.URL.Path {
				case tc.path:
					primaryCalls.Add(1)
					w.Header().Set("Content-Type", tc.contentType)
					if tc.format == "responses" {
						// Close without the declared body, simulating a truncated response.
						w.Header().Set("Content-Length", "100")
					}
					w.WriteHeader(tc.status)
					_, _ = io.WriteString(w, tc.body)
				case "/backup/v1/chat/completions":
					backupCalls.Add(1)
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"backup replied"},"finish_reason":"stop"}]}`)
				default:
					t.Errorf("unexpected request path %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			// The same model ID may still be reachable via a different endpoint.
			candidates := []FallbackCandidate{
				{Provider: tc.provider, Model: "demo", IdentityKey: "primary"},
				{Provider: "openai", Model: "demo", IdentityKey: "backup"},
			}
			clients := make(map[string]LLMProvider)
			for i, base := range []string{server.URL + "/v1", server.URL + "/backup/v1"} {
				c := candidates[i]
				cfg := &config.ModelConfig{Provider: c.Provider, Model: c.Model, APIBase: base}
				if i == 0 {
					cfg.APIFormat = tc.format
				}
				cfg.SetAPIKey("synthetic-test-key")
				client, _, err := CreateProviderFromConfig(cfg)
				if err != nil {
					t.Fatal(err)
				}
				clients[c.IdentityKey] = client
			}
			chain := NewFallbackChain(NewCooldownTracker(), nil)
			result, err := chain.ExecuteCandidate(t.Context(), candidates, func(ctx context.Context, c FallbackCandidate) (*LLMResponse, error) {
				return clients[c.IdentityKey].Chat(ctx, []Message{{Role: "user", Content: "probe"}}, nil, c.Model, map[string]any{"max_tokens": 32})
			})
			if tc.wantFallback {
				if err != nil || result == nil || result.IdentityKey != "backup" || result.Response.Content != "backup replied" {
					t.Fatalf("expected backup reply, got result=%+v err=%v", result, err)
				}
				var original *common.HTTPError
				if len(result.Attempts) != 1 || !errors.As(result.Attempts[0].Error, &original) || original.StatusCode != http.StatusGone {
					t.Fatalf("original HTTP 410 missing from attempts: %+v", result.Attempts)
				}
				if backupCalls.Load() != 1 {
					t.Errorf("backup calls=%d, want 1", backupCalls.Load())
				}
			} else if err == nil || result != nil || backupCalls.Load() != 0 {
				t.Fatalf("expected original failure without fallback, got result=%+v err=%v backup calls=%d", result, err, backupCalls.Load())
			}
			if primaryCalls.Load() != 1 {
				t.Errorf("primary calls=%d, want 1", primaryCalls.Load())
			}
		})
	}
}

func TestHTTPGoneFallbackDoesNotInferStatusFromText(t *testing.T) {
	if got := ClassifyError(errors.New("local error: status 410"), "opencode-go", "demo"); got != nil {
		t.Fatalf("unstructured local error classified as %v", got)
	}
}
