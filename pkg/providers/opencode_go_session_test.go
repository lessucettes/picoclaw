package providers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
)

func TestOpenCodeGoSessionlessRequestsGetSeparateIDs(t *testing.T) {
	for _, family := range []string{"chat", "chat-stream", "messages", "responses"} {
		t.Run(family, func(t *testing.T) {
			headers := make(chan string, 4)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				headers <- r.Header.Get("X-Opencode-Session")
				switch family {
				case "chat-stream":
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = w.Write(
						[]byte(
							"data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n",
						),
					)
				case "messages":
					_, _ = w.Write(
						[]byte(`{"role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`),
					)
				case "responses":
					_, _ = w.Write(
						[]byte(
							`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`,
						),
					)
				default:
					_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
				}
			}))
			defer srv.Close()
			var cfg config.ModelConfig
			cfg.Provider, cfg.Model, cfg.APIBase = "opencode-go", "fixture", srv.URL
			cfg.APIFormat = strings.TrimSuffix(family, "-stream")
			cfg.SetAPIKey("test-key")
			call := func(options map[string]any) string {
				t.Helper()
				if family == "messages" {
					if options == nil {
						options = map[string]any{}
					}
					options["max_tokens"] = 128
				}
				p, model, err := CreateProviderFromConfig(&cfg)
				if err != nil {
					t.Fatal(err)
				}
				var message Message
				message.Role, message.Content = "user", "fixture"
				messages := []Message{message}
				if family == "chat-stream" {
					_, err = p.(StreamingProvider).ChatStream(t.Context(), messages, nil, model, options, nil)
				} else {
					_, err = p.Chat(t.Context(), messages, nil, model, options)
				}
				if err != nil {
					t.Fatal(err)
				}
				return <-headers
			}
			first, second := call(nil), call(map[string]any{"session_key": " "})
			if first == "" || second == "" || first == second {
				t.Fatalf("unrelated one-off requests need unique nonempty sessions; got %q and %q", first, second)
			}
			if got := call(map[string]any{"session_key": "conversation:go"}); got != "conversation:go" {
				t.Fatalf("explicit session = %q", got)
			}
			cfg.CustomHeaders = map[string]string{"X-OpenCode-Session": "pinned"}
			if got := call(map[string]any{"session_key": "conversation:go"}); got != "pinned" {
				t.Fatalf("custom session = %q", got)
			}
		})
	}
}
