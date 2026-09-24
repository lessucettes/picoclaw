package agent

import (
	"context"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers"
	anthropicmessages "github.com/sipeed/picoclaw/pkg/providers/anthropic_messages"
)

type sessionKeyCaptureProvider struct {
	options []map[string]any
}

func (p *sessionKeyCaptureProvider) Chat(
	_ context.Context,
	_ []providers.Message,
	_ []providers.ToolDefinition,
	_ string,
	options map[string]any,
) (*providers.LLMResponse, error) {
	copied := make(map[string]any, len(options))
	for key, value := range options {
		copied[key] = value
	}
	p.options = append(p.options, copied)
	return &providers.LLMResponse{Content: "ok", FinishReason: "stop"}, nil
}

func (p *sessionKeyCaptureProvider) GetDefaultModel() string { return "test-model" }

func TestPipeline_CallLLM_ForwardsSessionKey(t *testing.T) {
	provider := &sessionKeyCaptureProvider{}
	al, agent, cleanup := newTurnCoordTestLoop(t, provider)
	defer cleanup()

	pipeline := NewPipeline(al)
	processOpts := makeTestProcessOpts("cli:stable")
	processOpts.Dispatch.SessionKey = "cli:stable"
	ts := newTurnState(agent, processOpts, turnEventScope{
		turnID:  "turn-session-key",
		context: newTurnContext(nil, nil, nil),
	})
	exec, err := pipeline.SetupTurn(context.Background(), ts)
	if err != nil {
		t.Fatalf("SetupTurn() error = %v", err)
	}
	if _, err := pipeline.CallLLM(context.Background(), context.Background(), ts, exec, 1); err != nil {
		t.Fatalf("CallLLM() error = %v", err)
	}
	if len(provider.options) != 1 {
		t.Fatalf("provider call count = %d, want 1", len(provider.options))
	}
	if got := provider.options[0]["session_key"]; got != "cli:stable" {
		t.Fatalf("session_key = %v, want cli:stable", got)
	}
}

func TestAskSideQuestion_ForwardsSessionKey(t *testing.T) {
	provider := &sessionKeyCaptureProvider{}
	al, agent, cleanup := newTurnCoordTestLoop(t, provider)
	defer cleanup()

	al.providerFactory = func(cfg *config.ModelConfig) (providers.LLMProvider, string, error) {
		return provider, cfg.Model, nil
	}
	opts := makeTestProcessOpts("cli:stable")
	opts.NoHistory = true
	if _, err := al.askSideQuestion(context.Background(), agent, &opts, "what time is it?"); err != nil {
		t.Fatalf("askSideQuestion() error = %v", err)
	}
	if len(provider.options) != 1 {
		t.Fatalf("provider call count = %d, want 1", len(provider.options))
	}
	if got := provider.options[0]["session_key"]; got != "cli:stable" {
		t.Fatalf("session_key = %v, want cli:stable", got)
	}
}

func TestOpenCodeGoFallbackWithEnvironmentKeyGetsItsOwnTransport(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "environment-test-key")
	workspace := t.TempDir()
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{Provider: "opencode-go", Workspace: workspace},
		},
		ModelList: []*config.ModelConfig{
			{ModelName: "go-kimi", Provider: "opencode-go", Model: "kimi-k3", Enabled: true},
			{ModelName: "go-qwen", Provider: "opencode-go", Model: "qwen3.8-flash", Enabled: true},
		},
	}
	primaryCandidate := providers.FallbackCandidate{Provider: "opencode-go", Model: "kimi-k3"}
	fallbackCandidate := providers.FallbackCandidate{
		Provider:    "opencode-go",
		Model:       "qwen3.8-flash",
		ConfigIndex: 2,
	}
	primary := &sessionKeyCaptureProvider{}
	out := make(map[string]providers.LLMProvider)

	inheritPrimaryProviderForCandidates(cfg, workspace, primaryCandidate, []providers.FallbackCandidate{fallbackCandidate}, primary, out)
	if _, inherited := out[candidateProviderKey(fallbackCandidate)]; inherited {
		t.Fatal("fallback candidate reused the primary provider despite its environment key")
	}

	populateCandidateProvidersFromCandidates(cfg, workspace, []providers.FallbackCandidate{fallbackCandidate}, out)
	fallback := out[candidateProviderKey(fallbackCandidate)]
	if fallback == nil {
		t.Fatal("fallback candidate provider was not created")
	}
	if fallback == primary {
		t.Fatal("fallback candidate reused the primary provider")
	}
	if _, ok := fallback.(*anthropicmessages.Provider); !ok {
		t.Fatalf("fallback provider type = %T, want Anthropic Messages transport", fallback)
	}
}
