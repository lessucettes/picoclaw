package openairesponses

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/providers/protocoltypes"
)

func TestProviderChatSendsResponsesRequest(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("request method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/zen/go/v1/responses" {
			t.Errorf("request path = %q, want /zen/go/v1/responses", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", got)
		}
		if got := r.Header.Get("User-Agent"); got != "PicoClaw/test" {
			t.Errorf("User-Agent = %q, want PicoClaw/test", got)
		}
		if got := r.Header.Get("x-opencode-session"); got != "conversation:1" {
			t.Errorf("x-opencode-session = %q, want conversation:1", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","object":"response","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Hello"}]},{"type":"function_call","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"Seattle\"}"}],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`))
	}))
	defer server.Close()

	provider := NewProvider("test-key", server.URL+"/zen/go/v1/", "", "PicoClaw/test", 0, nil)
	tools := []protocoltypes.ToolDefinition{{
		Type: "function",
		Function: protocoltypes.ToolFunctionDefinition{
			Name:        "get_weather",
			Description: "Get the weather for a city.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{"type": "string"},
				},
			},
		},
	}}
	response, err := provider.Chat(
		t.Context(),
		[]protocoltypes.Message{{Role: "system", Content: "Be concise."}, {Role: "user", Content: "Weather in Seattle?"}},
		tools,
		"grok-4.7",
		map[string]any{"max_tokens": 1024, "session_key": "conversation:1"},
	)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	if requestBody["model"] != "grok-4.7" {
		t.Errorf("request model = %v, want grok-4.7", requestBody["model"])
	}
	if requestBody["store"] != false {
		t.Errorf("request store = %v, want false", requestBody["store"])
	}
	if requestBody["instructions"] != "Be concise." {
		t.Errorf("request instructions = %v, want Be concise.", requestBody["instructions"])
	}
	if requestBody["max_output_tokens"] != float64(1024) {
		t.Errorf("max_output_tokens = %v, want 1024", requestBody["max_output_tokens"])
	}
	input, ok := requestBody["input"].([]any)
	if !ok || len(input) != 1 {
		t.Errorf("input = %#v, want an array with one user item", requestBody["input"])
	}
	if _, exists := requestBody["temperature"]; exists {
		t.Errorf("request unexpectedly contains unsupported temperature field")
	}
	if _, exists := requestBody["prompt_cache_key"]; exists {
		t.Errorf("request unexpectedly contains prompt_cache_key")
	}
	requestTools, ok := requestBody["tools"].([]any)
	if !ok || len(requestTools) != 1 {
		t.Fatalf("tools = %#v, want one function tool", requestBody["tools"])
	}
	tool, ok := requestTools[0].(map[string]any)
	if !ok {
		t.Fatalf("tool = %#v, want object", requestTools[0])
	}
	if tool["type"] != "function" || tool["name"] != "get_weather" || tool["strict"] != false {
		t.Errorf("function tool = %#v, want get_weather with strict=false", tool)
	}

	if response.Content != "Hello" {
		t.Errorf("Content = %q, want Hello", response.Content)
	}
	if response.FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q, want tool_calls", response.FinishReason)
	}
	if len(response.ToolCalls) != 1 || response.ToolCalls[0].Name != "get_weather" || response.ToolCalls[0].Arguments["city"] != "Seattle" {
		t.Errorf("ToolCalls = %+v, want get_weather(city=Seattle)", response.ToolCalls)
	}
	if response.Usage == nil || response.Usage.PromptTokens != 10 || response.Usage.CompletionTokens != 5 || response.Usage.TotalTokens != 15 {
		t.Errorf("Usage = %+v, want input=10 output=5 total=15", response.Usage)
	}
}

func TestProviderChatUsesConfiguredSessionHeaderName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Conversation-Token"); got != "conversation:1" {
			t.Errorf("X-Conversation-Token = %q, want conversation:1", got)
		}
		if got := r.Header.Get("x-opencode-session"); got != "" {
			t.Errorf("unexpected x-opencode-session header = %q", got)
		}
		_, _ = w.Write([]byte(`{"id":"resp_1","status":"completed","output":[]}`))
	}))
	defer server.Close()

	provider := NewProviderWithSessionHeader("test-key", server.URL, "", "PicoClaw/test", 0, nil, "X-Conversation-Token")
	_, err := provider.Chat(t.Context(), []protocoltypes.Message{{Role: "user", Content: "hi"}}, nil, "grok-4.7", map[string]any{"session_key": "conversation:1"})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
}

func TestProviderChatCustomSessionHeaderOverridesOption(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-opencode-session"); got != "pinned" {
			t.Errorf("x-opencode-session = %q, want pinned", got)
		}
		_, _ = w.Write([]byte(`{"id":"resp_1","status":"completed","output":[]}`))
	}))
	defer server.Close()

	provider := NewProvider("test-key", server.URL, "", "PicoClaw/test", 0, map[string]string{"X-OpenCode-Session": "pinned"})
	_, err := provider.Chat(t.Context(), []protocoltypes.Message{{Role: "user", Content: "hi"}}, nil, "grok-4.7", map[string]any{"session_key": "conversation:1"})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
}

func TestProviderChatRejectsMissingKey(t *testing.T) {
	provider := NewProvider("", "https://example.invalid/zen/go/v1", "", "PicoClaw/test", 0, nil)
	_, err := provider.Chat(t.Context(), []protocoltypes.Message{{Role: "user", Content: "hi"}}, nil, "grok-4.7", nil)
	if err == nil || !strings.Contains(err.Error(), "API key is required") {
		t.Fatalf("Chat() error = %v, want missing-key error", err)
	}
}

func TestProviderChatHandlesHTTPAndMalformedResponses(t *testing.T) {
	for _, tc := range []struct {
		name       string
		statusCode int
		body       string
	}{
		{name: "unauthorized", statusCode: http.StatusUnauthorized, body: `{"error":"unauthorized"}`},
		{name: "malformed response", statusCode: http.StatusOK, body: `{invalid`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			provider := NewProvider("test-key", server.URL, "", "PicoClaw/test", 0, nil)
			_, err := provider.Chat(t.Context(), []protocoltypes.Message{{Role: "user", Content: "hi"}}, nil, "grok-4.7", nil)
			if err == nil {
				t.Fatal("Chat() error = nil, want error")
			}
			if strings.Contains(err.Error(), "test-key") {
				t.Errorf("error exposes API key: %v", err)
			}
		})
	}
}

func TestProviderChatPropagatesContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	provider := NewProvider("test-key", server.URL, "", "PicoClaw/test", 0, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := provider.Chat(ctx, []protocoltypes.Message{{Role: "user", Content: "hi"}}, nil, "grok-4.7", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Chat() error = %v, want context.Canceled", err)
	}
}
