package providers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
)

func TestOpenCodeGoChatReplaysDeepSeekReasoningWithTools(t *testing.T) {
	for _, tc := range []struct {
		model string
		tools bool
		want  bool
	}{
		{"deepseek-v4.1-flash", true, true},
		{"deepseek-v4.1-flash", false, false},
		{"hy4-flash-free", true, false},
	} {
		t.Run(tc.model+"/tools="+strconv.FormatBool(tc.tools), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Messages []Message `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Messages) != 5 {
					t.Errorf("unexpected continuation request: %v, %d messages", err, len(body.Messages))
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				for i, want := range map[int]string{1: "previous trace", 3: "tool trace"} {
					if !tc.want {
						want = ""
					}
					if got := body.Messages[i].ReasoningContent; got != want {
						t.Errorf("message %d reasoning_content = %q, want %q", i, got, want)
					}
				}
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
			}))
			defer server.Close()
			var cfg config.ModelConfig
			p, err := newOpenCodeProvider("opencode-go", "chat", "test-key", server.URL, "PicoClaw/test", &cfg)
			if err != nil {
				t.Fatal(err)
			}
			var msgs [5]Message
			msgs[0].Role, msgs[0].Content = "user", "previous question"
			msgs[1].Role, msgs[1].Content, msgs[1].ReasoningContent = "assistant", "previous answer", "previous trace"
			msgs[2].Role, msgs[2].Content = "user", "new question"
			msgs[3].Role, msgs[3].ReasoningContent = "assistant", "tool trace"
			var call ToolCall
			call.ID, call.Type = "call_test", "function"
			call.Function = new(FunctionCall)
			call.Function.Name, call.Function.Arguments = "fixture", "{}"
			msgs[3].ToolCalls = []ToolCall{call}
			msgs[4].Role, msgs[4].ToolCallID, msgs[4].Content = "tool", "call_test", "tool answer"
			var tools []ToolDefinition
			if tc.tools {
				var tool ToolDefinition
				tool.Type, tool.Function.Name = "function", "fixture"
				tools = []ToolDefinition{tool}
			}
			options := map[string]any{"session_key": "conversation:test"}
			if _, err := p.Chat(t.Context(), msgs[:], tools, tc.model, options); err != nil {
				t.Fatal(err)
			}
		})
	}
}
