package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers"
)

type openCodeFixtureTool struct{}

func (openCodeFixtureTool) Name() string        { return "fixture" }
func (openCodeFixtureTool) Description() string { return "Return a fixed test result" }
func (openCodeFixtureTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func (openCodeFixtureTool) Execute(context.Context, map[string]any) *ToolResult {
	return NewToolResult("fixture tool result")
}

func TestOpenCodeGoLegacyToolLoopKeepsSession(t *testing.T) {
	sessions := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessions <- r.Header.Get("X-Opencode-Session")
		var request struct {
			Messages []providers.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Messages) == 0 {
			t.Errorf("bad request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		n := len(request.Messages)
		if last := request.Messages[n-1]; n >= 2 && last.Role == "tool" && last.ToolCallID == "call_fixture" {
			if last.Content != "fixture tool result" || request.Messages[n-2].ReasoningContent != "fixture trace" {
				t.Error("tool result or DeepSeek reasoning missing in continuation")
			}
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
			return
		}
		_, _ = w.Write(
			[]byte(
				`{"choices":[{"message":{"role":"assistant","reasoning_content":"fixture trace","tool_calls":[{"id":"call_fixture","type":"function","function":{"name":"fixture","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
			),
		)
	}))
	defer server.Close()
	var cfg config.ModelConfig
	cfg.Provider, cfg.Model, cfg.APIBase = "opencode-go", "deepseek-v4.1-flash", server.URL
	cfg.SetAPIKey("fixture-key")
	provider, model, err := providers.CreateProviderFromConfig(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	options := map[string]any{"max_tokens": 128}
	registry := NewToolRegistry()
	registry.Register(openCodeFixtureTool{})
	loop := ToolLoopConfig{
		Provider: provider, Model: model, MaxIterations: 2, LLMOptions: options, Tools: registry, MediaResolver: nil,
	}
	var message providers.Message
	message.Role, message.Content = "user", "test"
	var previous string
	for range 2 {
		result, err := RunToolLoop(t.Context(), loop, []providers.Message{message}, "", "")
		if err != nil || result.Content != "ok" || result.Iterations != 2 {
			t.Fatalf("tool loop = %+v, %v", result, err)
		}
		first, second := <-sessions, <-sessions
		if first == "" || second != first || first == previous {
			t.Fatalf(
				"session must be stable within, but not across, tool loops: %q %q (prior %q)",
				first,
				second,
				previous,
			)
		}
		previous = first
	}
	if _, ok := options["session_key"]; ok {
		t.Fatal("tool loop mutated its caller's options")
	}
}
