package openai_compat

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenCodeSessionChatHeader(t *testing.T) {
	tests := []struct {
		name          string
		options       map[string]any
		sessionHeader bool
		customHeaders map[string]string
		stream        bool
		wantHeader    string
	}{
		{
			name:          "opt-in session",
			options:       map[string]any{"session_key": "conversation:1"},
			sessionHeader: true,
			wantHeader:    "conversation:1",
		},
		{
			name:          "no session option",
			sessionHeader: true,
		},
		{
			name:    "provider not opted in",
			options: map[string]any{"session_key": "conversation:1"},
		},
		{
			name:          "custom header overrides session option",
			options:       map[string]any{"session_key": "conversation:1"},
			sessionHeader: true,
			customHeaders: map[string]string{"X-OpenCode-Session": "pinned"},
			wantHeader:    "pinned",
		},
		{
			name:          "streaming session",
			options:       map[string]any{"session_key": "conversation:1"},
			sessionHeader: true,
			stream:        true,
			wantHeader:    "conversation:1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/chat/completions" {
					t.Errorf("request path = %q, want /chat/completions", r.URL.Path)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
					t.Errorf("Authorization = %q, want Bearer test-key", got)
				}
				if got := r.Header.Get("x-opencode-session"); got != tc.wantHeader {
					t.Errorf("x-opencode-session = %q, want %q", got, tc.wantHeader)
				}
				if tc.stream {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
					return
				}
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
			}))
			defer server.Close()

			options := []Option{}
			if tc.sessionHeader {
				options = append(options, WithSessionHeaderName("x-opencode-session"))
			}
			if tc.customHeaders != nil {
				options = append(options, WithCustomHeaders(tc.customHeaders))
			}
			provider := NewProvider("test-key", server.URL, "", options...)
			messages := []Message{{Role: "user", Content: "hi"}}
			var (
				response *LLMResponse
				err      error
			)
			if tc.stream {
				response, err = provider.ChatStreamEvents(t.Context(), messages, nil, "kimi-k3", tc.options, nil)
			} else {
				response, err = provider.Chat(t.Context(), messages, nil, "kimi-k3", tc.options)
			}
			if err != nil {
				t.Fatalf("Chat() error = %v", err)
			}
			if response.Content != "ok" {
				t.Errorf("response content = %q, want ok", response.Content)
			}
			if tc.stream && !strings.Contains(response.FinishReason, "stop") {
				t.Errorf("stream finish reason = %q, want stop", response.FinishReason)
			}
		})
	}
}

func TestProviderGetDefaultModelReturnsEmpty(t *testing.T) {
	provider := NewProvider("test-key", "http://127.0.0.1", "")
	if got := provider.GetDefaultModel(); got != "" {
		t.Fatalf("GetDefaultModel() = %q, want empty model", got)
	}
}
