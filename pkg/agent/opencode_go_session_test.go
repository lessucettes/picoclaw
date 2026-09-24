package agent

import (
	"context"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers"
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
