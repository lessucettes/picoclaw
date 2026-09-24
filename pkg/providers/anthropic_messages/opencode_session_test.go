package anthropicmessages

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenCodeSessionMessagesHeader(t *testing.T) {
	tests := []struct {
		name          string
		customHeaders map[string]string
		sessionHeader bool
		wantHeader    string
	}{
		{
			name:          "opt-in session",
			sessionHeader: true,
			wantHeader:    "conversation:1",
		},
		{
			name: "provider not opted in",
		},
		{
			name:          "custom header overrides session option",
			customHeaders: map[string]string{"X-OpenCode-Session": "pinned"},
			sessionHeader: true,
			wantHeader:    "pinned",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/messages" {
					t.Errorf("request path = %q, want /v1/messages", r.URL.Path)
				}
				if got := r.Header.Get("X-API-Key"); got != "test-key" {
					t.Errorf("X-API-Key = %q, want test-key", got)
				}
				if got := r.Header.Get("Anthropic-Version"); got != "2023-06-01" {
					t.Errorf("Anthropic-Version = %q, want 2023-06-01", got)
				}
				if got := r.Header.Get("x-opencode-session"); got != tc.wantHeader {
					t.Errorf("x-opencode-session = %q, want %q", got, tc.wantHeader)
				}
				_, _ = w.Write([]byte(`{"id":"m","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","model":"minimax-m3","usage":{"input_tokens":1,"output_tokens":1}}`))
			}))
			defer server.Close()

			provider := NewProviderWithHeaders(
				"test-key",
				server.URL,
				"PicoClaw/test",
				0,
				tc.customHeaders,
				func() string {
					if tc.sessionHeader {
						return "x-opencode-session"
					}
					return ""
				}(),
			)
			response, err := provider.Chat(
				t.Context(),
				[]Message{{Role: "user", Content: "hi"}},
				nil,
				"minimax-m3",
				map[string]any{"max_tokens": 1024, "session_key": "conversation:1"},
			)
			if err != nil {
				t.Fatalf("Chat() error = %v", err)
			}
			if response.Content != "ok" {
				t.Errorf("response content = %q, want ok", response.Content)
			}
		})
	}
}
