package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/seahorse"
)

func newOpenCodeSessionTestProvider(t *testing.T, handler http.HandlerFunc) providers.LLMProvider {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := openCodeSessionModel("fixture", "hy4-flash-free", "chat", server.URL, "fixture-key")
	p, _, err := providers.CreateProviderFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func openCodeSessionModel(alias, model, format, base, key string) *config.ModelConfig {
	var cfg config.ModelConfig
	cfg.ModelName, cfg.Provider, cfg.Model = alias, "opencode-go", model
	cfg.APIFormat, cfg.APIBase = format, base
	cfg.SetAPIKey(key)
	return &cfg
}

func openCodeSessionTurn(sessionKey string) processOptions {
	var opts processOptions
	opts.Dispatch.SessionKey = sessionKey
	opts.Dispatch.UserMessage = "fixture"
	return opts
}

func TestOpenCodeGoTurnPreservesSessionAfterHookReplacesOptions(t *testing.T) {
	const sessionKey = "agent:main:fixture"
	p := newOpenCodeSessionTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Opencode-Session"); got != sessionKey {
			t.Errorf("x-opencode-session = %q, want %q", got, sessionKey)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	})
	al, instance, cleanup := newHookTestLoop(t, p)
	defer cleanup()
	if err := al.MountHook(NamedHook("clear-options", clearOptionsHook{})); err != nil {
		t.Fatal(err)
	}
	response, err := al.runAgentLoop(t.Context(), instance, openCodeSessionTurn(sessionKey))
	if err != nil || response != "ok" {
		t.Fatalf("turn = %q, %v", response, err)
	}
}

func TestOpenCodeGoFallbackKeepsSessionAndUsesCandidateTransport(t *testing.T) {
	const sessionKey = "agent:main:fixture"
	requests := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Path
		if got := r.Header.Get("X-Opencode-Session"); got != sessionKey {
			t.Errorf("%s: x-opencode-session = %q, want %q", r.URL.Path, got, sessionKey)
		}
		switch r.URL.Path {
		case "/primary/v1/chat/completions":
			if r.Header.Get("Authorization") != "Bearer primary-key" {
				t.Error("missing primary authorization")
			}
			http.Error(w, `{"error":{"message":"fixture model retired"}}`, http.StatusGone)
		case "/fallback/v1/messages":
			if r.Header.Get("X-Api-Key") != "fallback-key" || r.Header.Get("Authorization") != "" {
				t.Error("wrong fallback authentication")
			}
			fmt.Fprint(
				w,
				`{"role":"assistant","content":[{"type":"text","text":"fallback reply"}],"stop_reason":"end_turn"}`,
			)
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	var cfg config.Config
	cfg.Agents.Defaults.Workspace = t.TempDir()
	cfg.Agents.Defaults.ModelName = "primary"
	cfg.Agents.Defaults.ModelFallbacks = []string{"fallback"}
	cfg.Agents.Defaults.MaxTokens = 512
	cfg.Agents.Defaults.MaxToolIterations = 3
	cfg.ModelList = []*config.ModelConfig{
		openCodeSessionModel("primary", "hy4-flash-free", "chat", server.URL+"/primary/v1", "primary-key"),
		openCodeSessionModel("fallback", "deepseek-v4.1-flash", "messages", server.URL+"/fallback/v1", "fallback-key"),
	}
	p, _, err := providers.CreateProvider(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	al := NewAgentLoop(&cfg, bus.NewMessageBus(), p)
	defer al.Close()
	response, err := al.runAgentLoop(t.Context(), al.registry.GetDefaultAgent(), openCodeSessionTurn(sessionKey))
	if err != nil || response != "fallback reply" {
		t.Fatalf("turn = %q, %v", response, err)
	}
	if len(requests) != 2 || <-requests != "/primary/v1/chat/completions" || <-requests != "/fallback/v1/messages" {
		t.Fatal("expected one Chat request followed by one Messages fallback")
	}
}

func TestOpenCodeGoLegacySummaryPreservesSession(t *testing.T) {
	const sessionKey = "agent:main:meshtastic:channel:0"
	type request struct{ session, prompt string }
	requests := make(chan request, 12)
	p := newOpenCodeSessionTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []providers.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Messages) != 1 {
			t.Errorf("invalid summary request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		session := r.Header.Get("X-Opencode-Session")
		requests <- request{session, body.Messages[0].Content}
		if session != sessionKey {
			http.Error(w, `{"error":{"type":"MissingSessionID"}}`, http.StatusBadRequest)
			return
		}
		if len(requests) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"fixture summary"},"finish_reason":"stop"}]}`)
	})
	al, instance, cleanup := newHookTestLoop(t, p)
	defer cleanup()
	history := make([]providers.Message, 0, 20)
	for i := range 10 {
		var user, assistant providers.Message
		user.Role, user.Content = "user", fmt.Sprint(i)
		assistant.Role, assistant.Content = "assistant", "answer"
		history = append(history, user, assistant)
	}
	instance.Sessions.SetHistory(sessionKey, history)
	var cm legacyContextManager
	cm.al = al
	cm.summarizeSession(instance, sessionKey)
	close(requests)
	var prompts []string
	for got := range requests {
		if got.session != sessionKey {
			t.Fatalf("summary sent x-opencode-session=%q, want %q", got.session, sessionKey)
		}
		prompts = append(prompts, got.prompt)
	}
	if len(prompts) != 4 || prompts[0] != prompts[1] ||
		!strings.HasPrefix(prompts[3], "Merge these two conversation summaries") {
		t.Fatalf("expected both chunks, one retry and final merge, got %d requests", len(prompts))
	}
	if got := instance.Sessions.GetSummary(sessionKey); got != "fixture summary" {
		t.Fatalf("summary = %q", got)
	}
}

func TestOpenCodeGoSeahorseSummaryPreservesSession(t *testing.T) {
	const sessionKey = "agent:main:seahorse:test"
	requests := make(chan string, 16)
	p := newOpenCodeSessionTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-Opencode-Session")
		requests <- got
		if got != sessionKey {
			http.Error(w, `{"error":{"type":"MissingSessionID"}}`, http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"short summary"},"finish_reason":"stop"}]}`)
	})
	var cfg seahorse.Config
	cfg.DBPath = filepath.Join(t.TempDir(), "sessions.db")
	e, err := seahorse.NewEngine(cfg, providerToCompleteFn(p, "hy4-flash-free"))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	messages := make([]seahorse.Message, 40)
	for i := range messages {
		messages[i].Role, messages[i].Content, messages[i].TokenCount = "user", fmt.Sprint(i), 128
	}
	if _, err := e.Ingest(t.Context(), sessionKey, messages); err != nil {
		t.Fatal(err)
	}
	budget := 100000
	if r, err := e.Compact(t.Context(), sessionKey, seahorse.CompactInput{Budget: &budget, Force: false}); err != nil ||
		r.LeafSummaries != 1 {
		t.Fatalf("expected a leaf summary: %+v, %v", r, err)
	}
	select {
	case got := <-requests:
		if got != sessionKey {
			t.Fatalf("summary sent x-opencode-session=%q, want %q", got, sessionKey)
		}
	default:
		t.Fatal("no request to the fixture")
	}
}
