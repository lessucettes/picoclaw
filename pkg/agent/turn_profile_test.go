package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/tools"
)

type turnProfileCaptureProvider struct {
	messages []providers.Message
	tools    []providers.ToolDefinition
}

func (p *turnProfileCaptureProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	model string,
	opts map[string]any,
) (*providers.LLMResponse, error) {
	p.messages = append([]providers.Message(nil), messages...)
	p.tools = append([]providers.ToolDefinition(nil), tools...)
	return &providers.LLMResponse{Content: "profile response"}, nil
}

func (p *turnProfileCaptureProvider) GetDefaultModel() string {
	return "test-model"
}

type turnProfileSideQuestionCaptureProvider struct {
	messages []providers.Message
}

func (p *turnProfileSideQuestionCaptureProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	model string,
	opts map[string]any,
) (*providers.LLMResponse, error) {
	p.messages = append([]providers.Message(nil), messages...)
	return &providers.LLMResponse{Content: "side answer"}, nil
}

func (p *turnProfileSideQuestionCaptureProvider) GetDefaultModel() string {
	return "test-model"
}

func newTurnProfileAgentLoop(
	t *testing.T,
	cfg *config.Config,
	provider *turnProfileCaptureProvider,
) *AgentLoop {
	t.Helper()
	t.Setenv("PICOCLAW_BUILTIN_SKILLS", t.TempDir())
	if cfg.Agents.Defaults.Workspace == "" {
		cfg.Agents.Defaults.Workspace = t.TempDir()
	}
	if cfg.Agents.Defaults.ModelName == "" {
		cfg.Agents.Defaults.ModelName = "test-model"
	}
	if cfg.Agents.Defaults.MaxTokens == 0 {
		cfg.Agents.Defaults.MaxTokens = 4096
	}
	if cfg.Agents.Defaults.MaxToolIterations == 0 {
		cfg.Agents.Defaults.MaxToolIterations = 10
	}
	return NewAgentLoop(cfg, bus.NewMessageBus(), provider)
}

func writeTurnProfileSkill(t *testing.T, workspace, name, body string) {
	t.Helper()
	dir := filepath.Join(workspace, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
}

func TestTurnProfile_PerAgentHistoryModes(t *testing.T) {
	cfg := config.DefaultConfig()
	const raw = `{
		"agents": {
			"defaults": {
				"turn_profile": {
					"enabled": true,
					"history": {"mode": "off"},
					"skills": {"mode": "default"},
					"system_prompt": {"mode": "default"}
				}
			},
			"list": [
				{
					"id": "main",
					"default": true,
					"turn_profile": {
						"history": {"mode": "default"}
					}
				},
				{"id": "weather"}
			]
		}
	}`
	if err := json.Unmarshal([]byte(raw), cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if len(cfg.Agents.List) != 2 {
		t.Fatalf("agents.list len = %d, want 2", len(cfg.Agents.List))
	}
	for i := range cfg.Agents.List {
		cfg.Agents.List[i].Workspace = t.TempDir()
	}
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)

	testTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	main, ok := al.GetRegistry().GetAgent("main")
	if !ok {
		t.Fatal("main agent was not registered")
	}
	mainSession := "agent:main:test-per-agent-profile"
	mainHistory := []providers.Message{{Role: "user", Content: "MAIN_HISTORY", CreatedAt: &testTime}}
	main.Sessions.SetHistory(mainSession, mainHistory)
	if _, err := al.runAgentLoop(context.Background(), main, processOptions{
		SessionKey:      mainSession,
		UserMessage:     "main current",
		DefaultResponse: defaultResponse,
	}); err != nil {
		t.Fatalf("runAgentLoop(main) error = %v", err)
	}
	if len(provider.messages) != 3 || provider.messages[1].Content != "MAIN_HISTORY" {
		t.Fatalf("main messages = %#v, want prior history included", provider.messages)
	}
	if got := main.Sessions.GetHistory(mainSession); len(got) != len(mainHistory)+2 {
		t.Fatalf("main history len = %d, want initial history plus current turn", len(got))
	}

	weather, ok := al.GetRegistry().GetAgent("weather")
	if !ok {
		t.Fatal("weather agent was not registered")
	}
	weatherSession := "agent:weather:test-per-agent-profile"
	weatherHistory := []providers.Message{{Role: "user", Content: "WEATHER_HISTORY", CreatedAt: &testTime}}
	weather.Sessions.SetHistory(weatherSession, weatherHistory)
	if _, err := al.runAgentLoop(context.Background(), weather, processOptions{
		SessionKey:      weatherSession,
		UserMessage:     "weather current",
		DefaultResponse: defaultResponse,
	}); err != nil {
		t.Fatalf("runAgentLoop(weather) error = %v", err)
	}
	if len(provider.messages) != 2 || provider.messages[1].Content != "weather current" {
		t.Fatalf("weather messages = %#v, want history omitted", provider.messages)
	}
	if got := weather.Sessions.GetHistory(weatherSession); !reflect.DeepEqual(got, weatherHistory) {
		t.Fatalf("weather history = %#v, want unchanged history while mode is off", got)
	}
}

func TestTurnProfile_DisabledPreservesDefaultHistoryAndPrompt(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				TurnProfile: config.TurnProfileConfig{
					Enabled: false,
					History: config.TurnProfileBlock{
						Mode: config.TurnProfileModeOff,
					},
				},
			},
		},
	}
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)
	agent := al.GetRegistry().GetDefaultAgent()
	sessionKey := "agent:default:test-default"
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	initialHistory := []providers.Message{
		{Role: "user", Content: "old user", CreatedAt: &ts},
		{Role: "assistant", Content: "old assistant", CreatedAt: &ts},
	}
	agent.Sessions.SetHistory(sessionKey, initialHistory)
	agent.Sessions.SetSummary(sessionKey, "old summary")

	got, err := al.runAgentLoop(context.Background(), agent, processOptions{
		SessionKey:      sessionKey,
		UserMessage:     "new user",
		DefaultResponse: defaultResponse,
		EnableSummary:   false,
	})
	if err != nil {
		t.Fatalf("runAgentLoop() error = %v", err)
	}
	if got != "profile response" {
		t.Fatalf("runAgentLoop() = %q, want profile response", got)
	}
	if len(provider.messages) != 4 {
		t.Fatalf("provider messages len = %d, want system + history + user", len(provider.messages))
	}
	if !reflect.DeepEqual(provider.messages[1:3], initialHistory) {
		t.Fatalf("provider history = %#v, want %#v", provider.messages[1:3], initialHistory)
	}
	if !strings.Contains(provider.messages[0].Content, "CONTEXT_SUMMARY") {
		t.Fatalf("system prompt missing summary in default mode:\n%s", provider.messages[0].Content)
	}
	history := agent.Sessions.GetHistory(sessionKey)
	if len(history) != len(initialHistory)+2 {
		t.Fatalf("history len = %d, want initial + user + assistant", len(history))
	}
}

func TestTurnProfile_HistoryOffSuppressesHistoryAndPersistence(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				TurnProfile: config.TurnProfileConfig{
					Enabled: true,
					History: config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
				},
			},
		},
	}
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)
	agent := al.GetRegistry().GetDefaultAgent()
	sessionKey := "agent:default:test-history-off"
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	initialHistory := []providers.Message{
		{Role: "user", Content: "old user", CreatedAt: &ts},
		{Role: "assistant", Content: "old assistant", CreatedAt: &ts},
	}
	agent.Sessions.SetHistory(sessionKey, initialHistory)
	agent.Sessions.SetSummary(sessionKey, "old summary")

	_, err := al.runAgentLoop(context.Background(), agent, processOptions{
		SessionKey:      sessionKey,
		UserMessage:     "new user",
		DefaultResponse: defaultResponse,
		EnableSummary:   true,
	})
	if err != nil {
		t.Fatalf("runAgentLoop() error = %v", err)
	}
	if len(provider.messages) != 2 {
		t.Fatalf("provider messages len = %d, want system + current user", len(provider.messages))
	}
	if provider.messages[1].Content != "new user" {
		t.Fatalf("current message = %q, want new user", provider.messages[1].Content)
	}
	if strings.Contains(provider.messages[0].Content, "old summary") {
		t.Fatalf("system prompt includes suppressed summary:\n%s", provider.messages[0].Content)
	}
	history := agent.Sessions.GetHistory(sessionKey)
	if !reflect.DeepEqual(history, initialHistory) {
		t.Fatalf("history = %#v, want unchanged %#v", history, initialHistory)
	}
}

func TestTurnProfile_ProcessMessageUsesEnabledTurnProfile(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				TurnProfile: config.TurnProfileConfig{
					Enabled: true,
					History: config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
				},
			},
		},
	}
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)

	_, err := al.processMessage(context.Background(), bus.InboundMessage{
		Context: bus.InboundContext{
			Channel:  "pico",
			ChatID:   "pico:sess-1",
			ChatType: "direct",
			SenderID: "pico-user",
		},
		Content: "hello from pico",
	})
	if err != nil {
		t.Fatalf("processMessage() error = %v", err)
	}
	if len(provider.messages) != 2 {
		t.Fatalf("provider messages len = %d, want system + current user", len(provider.messages))
	}
	if provider.messages[1].Content != "hello from pico" {
		t.Fatalf("current message = %q, want hello from pico", provider.messages[1].Content)
	}
}

func TestTurnProfile_BtwCommandUsesEnabledTurnProfile(t *testing.T) {
	workspace := t.TempDir()
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         workspace,
				ModelName:         "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
				TurnProfile: config.TurnProfileConfig{
					Enabled: true,
					History: config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
					Tools:   config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
				},
			},
		},
		ModelList: []*config.ModelConfig{{
			ModelName: "test-model",
			Model:     "openai/test-model",
		}},
	}
	t.Setenv("PICOCLAW_BUILTIN_SKILLS", t.TempDir())
	sideProvider := &turnProfileSideQuestionCaptureProvider{}
	al := NewAgentLoop(cfg, bus.NewMessageBus(), &turnProfileCaptureProvider{})
	al.providerFactory = func(mc *config.ModelConfig) (providers.LLMProvider, string, error) {
		return sideProvider, "test-model", nil
	}

	_, err := al.processMessage(context.Background(), bus.InboundMessage{
		Context: bus.InboundContext{
			Channel:  "pico",
			ChatID:   "pico:btw",
			ChatType: "direct",
			SenderID: "pico-user",
		},
		Content: "/btw explain privately",
	})
	if err != nil {
		t.Fatalf("processMessage() error = %v", err)
	}
	if len(sideProvider.messages) < 2 {
		t.Fatalf("side question messages len = %d, want system + user", len(sideProvider.messages))
	}
	systemPrompt := sideProvider.messages[0].Content
	blockedSnippets := []string{
		"ALWAYS use tools",
		"When using tools",
		"read_file tool",
		"update " + workspace + "/memory/MEMORY.md",
	}
	for _, snippet := range blockedSnippets {
		if strings.Contains(systemPrompt, snippet) {
			t.Fatalf("side question system prompt includes %q despite tools.mode=off:\n%s", snippet, systemPrompt)
		}
	}
}

func TestTurnProfile_BtwCommandDoesNotAddToolFallbackWhenSystemPromptOff(t *testing.T) {
	workspace := t.TempDir()
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         workspace,
				ModelName:         "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
				TurnProfile: config.TurnProfileConfig{
					Enabled:      true,
					History:      config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
					SystemPrompt: config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
					Tools: config.TurnProfileBlock{
						Mode:  config.TurnProfileModeCustom,
						Allow: []string{"echo_text"},
					},
				},
			},
		},
		ModelList: []*config.ModelConfig{{
			ModelName: "test-model",
			Model:     "openai/test-model",
		}},
	}
	t.Setenv("PICOCLAW_BUILTIN_SKILLS", t.TempDir())
	sideProvider := &turnProfileSideQuestionCaptureProvider{}
	al := NewAgentLoop(cfg, bus.NewMessageBus(), &turnProfileCaptureProvider{})
	al.RegisterTool(&echoTextTool{})
	al.providerFactory = func(mc *config.ModelConfig) (providers.LLMProvider, string, error) {
		return sideProvider, "test-model", nil
	}

	_, err := al.processMessage(context.Background(), bus.InboundMessage{
		Context: bus.InboundContext{
			Channel:  "pico",
			ChatID:   "pico:btw-system-off",
			ChatType: "direct",
			SenderID: "pico-user",
		},
		Content: "/btw explain privately",
	})
	if err != nil {
		t.Fatalf("processMessage() error = %v", err)
	}
	for _, msg := range sideProvider.messages {
		if msg.Role == "system" && strings.Contains(msg.Content, toolUseSystemPromptRule()) {
			t.Fatalf("side question system prompt includes tool fallback despite no tools:\n%s", msg.Content)
		}
	}
}

func TestTurnProfile_BtwHookCannotReenableNativeSearchWhenToolsOff(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         t.TempDir(),
				ModelName:         "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
				TurnProfile: config.TurnProfileConfig{
					Enabled: true,
					History: config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
					Tools:   config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
				},
			},
		},
		ModelList: []*config.ModelConfig{{
			ModelName: "test-model",
			Model:     "openai/test-model",
		}},
	}
	provider := &nativeSearchCaptureProvider{}
	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)
	al.providerFactory = func(mc *config.ModelConfig) (providers.LLMProvider, string, error) {
		return provider, "test-model", nil
	}
	if err := al.MountHook(NamedHook("enable-native-search", turnProfileEnableNativeSearchHook{})); err != nil {
		t.Fatalf("MountHook() error = %v", err)
	}

	_, err := al.processMessage(context.Background(), bus.InboundMessage{
		Context: bus.InboundContext{
			Channel:  "pico",
			ChatID:   "pico:btw-native-search",
			ChatType: "direct",
			SenderID: "pico-user",
		},
		Content: "/btw search privately",
	})
	if err != nil {
		t.Fatalf("processMessage() error = %v", err)
	}
	if provider.lastOpts["turn_profile_test_hook"] != true {
		t.Fatalf("BeforeLLM hook did not run for /btw: %#v", provider.lastOpts)
	}
	if provider.lastOpts["native_search"] == true {
		t.Fatalf("native_search option enabled by /btw hook despite tools.mode=off: %#v", provider.lastOpts)
	}
}

func TestTurnProfile_SubTurnInheritsParentToolProfile(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         t.TempDir(),
				ModelName:         "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
				TurnProfile: config.TurnProfileConfig{
					Enabled: true,
					History: config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
					Tools: config.TurnProfileBlock{
						Mode:  config.TurnProfileModeCustom,
						Allow: []string{"echo_text"},
					},
				},
			},
		},
	}
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)
	al.RegisterTool(&echoTextTool{})
	al.RegisterTool(&echoTextRewrittenTool{})
	agent := al.GetRegistry().GetDefaultAgent()
	profile, ok, err := cfg.Agents.Defaults.ResolveTurnProfile()
	if err != nil {
		t.Fatalf("ResolveTurnProfile() error = %v", err)
	}
	if !ok {
		t.Fatal("ResolveTurnProfile() did not return enabled profile")
	}
	parentOpts := processOptions{
		Dispatch: DispatchRequest{
			SessionKey:  "agent:default:test-parent",
			UserMessage: "parent",
		},
		TurnProfile: profile,
	}
	parentTS := newTurnState(agent, parentOpts, turnEventScope{
		turnID: "parent-turn-profile",
	})

	_, err = spawnSubTurn(context.Background(), al, parentTS, SubTurnConfig{
		Model:        "test-model",
		SystemPrompt: "child task",
		Timeout:      time.Second,
	})
	if err != nil {
		t.Fatalf("spawnSubTurn() error = %v", err)
	}
	if len(provider.tools) != 1 {
		t.Fatalf("child provider tools len = %d, want 1: %#v", len(provider.tools), provider.tools)
	}
	if provider.tools[0].Function.Name != "echo_text" {
		t.Fatalf("child provider tool = %q, want echo_text", provider.tools[0].Function.Name)
	}
}

func TestTurnProfile_SystemPromptOffUsesMinimalPrompt(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "AGENT.md"), []byte(`---
name: Frontmatter Name
model: frontmatter-model
tools: [echo_text]
skills: [minimal-skill]
---
MINIMAL_AGENT_BODY
`), 0o644); err != nil {
		t.Fatalf("write AGENT.md: %v", err)
	}
	for name, content := range map[string]string{
		"SOUL.md":     "SOUL_SECRET",
		"USER.md":     "USER_SECRET",
		"IDENTITY.md": "IDENTITY_SECRET",
	} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(workspace, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(workspace, "memory", "MEMORY.md"),
		[]byte("MEMORY_SECRET"),
		0o644,
	); err != nil {
		t.Fatalf("write memory: %v", err)
	}
	writeTurnProfileSkill(
		t,
		workspace,
		"minimal-skill",
		"---\ndescription: minimal skill\n---\nACTIVE_SKILL_SECRET",
	)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace: workspace,
				TurnProfile: config.TurnProfileConfig{
					Enabled:      true,
					SystemPrompt: config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
				},
			},
		},
	}
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)
	al.RegisterTool(&echoTextTool{})
	al.RegisterTool(&echoTextRewrittenTool{})
	agent := al.GetRegistry().GetDefaultAgent()
	if agent.Model != "frontmatter-model" {
		t.Fatalf("agent model = %q, want AGENT.md frontmatter model", agent.Model)
	}
	sessionKey := "agent:default:test-minimal-prompt"
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	history := []providers.Message{
		{Role: "user", Content: "old user", CreatedAt: &ts},
		{Role: "assistant", Content: "old assistant", CreatedAt: &ts},
	}
	agent.Sessions.SetHistory(sessionKey, history)
	agent.Sessions.SetSummary(sessionKey, "SUMMARY_SECRET")

	_, err := al.runAgentLoop(context.Background(), agent, processOptions{
		SessionKey:           sessionKey,
		Channel:              "telegram",
		ChatID:               "chat-42",
		SenderID:             "sender-42",
		SenderDisplayName:    "Ada",
		UserMessage:          "hello",
		ForcedSkills:         []string{"minimal-skill"},
		DefaultResponse:      defaultResponse,
		SystemPromptOverride: "EXTERNAL_PROMPT_SECRET",
	})
	if err != nil {
		t.Fatalf("runAgentLoop() error = %v", err)
	}
	if len(provider.messages) != 4 {
		t.Fatalf("messages len = %d, want system + history + user", len(provider.messages))
	}
	if !reflect.DeepEqual(provider.messages[1:3], history) {
		t.Fatalf("provider history = %#v, want %#v", provider.messages[1:3], history)
	}

	prompt := provider.messages[0].Content
	parts := strings.Split(prompt, "\n\n")
	if len(parts) != 4 {
		t.Fatalf("minimal prompt parts = %d, want AGENT body + time + session + sender:\n%s", len(parts), prompt)
	}
	if parts[0] != "MINIMAL_AGENT_BODY" {
		t.Fatalf("AGENT.md body = %q, want body only", parts[0])
	}
	const timePrefix = "## Current Time\n"
	if !strings.HasPrefix(parts[1], timePrefix) {
		t.Fatalf("current time section = %q", parts[1])
	}
	if _, err := time.Parse("2006-01-02 15:04 (Monday)", strings.TrimPrefix(parts[1], timePrefix)); err != nil {
		t.Fatalf("current time uses unexpected format: %q: %v", parts[1], err)
	}
	if parts[2] != "## Current Session\nChannel: telegram\nChat ID: chat-42" {
		t.Fatalf("current session section = %q, want channel and chat ID", parts[2])
	}
	wantSender := "## Current Sender\n" + formatCurrentSenderLine("sender-42", "Ada")
	if parts[3] != wantSender {
		t.Fatalf("current sender section = %q, want %q", parts[3], wantSender)
	}

	for _, forbidden := range []string{
		"name: Frontmatter Name",
		"model: frontmatter-model",
		"tools: [echo_text]",
		"# picoclaw",
		"## Workspace",
		"## Important Rules",
		"SOUL_SECRET",
		"USER_SECRET",
		"IDENTITY_SECRET",
		"MEMORY_SECRET",
		"# Skills",
		"# Active Skills",
		"ACTIVE_SKILL_SECRET",
		"## Runtime",
		"CONTEXT_SUMMARY",
		"SUMMARY_SECRET",
		"EXTERNAL_PROMPT_SECRET",
		toolUseSystemPromptRule(),
	} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("minimal prompt contains forbidden content %q:\n%s", forbidden, prompt)
		}
	}
	if len(provider.tools) != 1 || provider.tools[0].Function.Name != "echo_text" {
		t.Fatalf("provider tools = %#v, want AGENT.md-allowed echo_text", provider.tools)
	}
}

func TestTurnProfile_SystemPromptOffWithoutAgentStillIncludesTimeAndSender(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(workspace, "AGENTS.md"),
		[]byte("LEGACY_AGENT_SECRET"),
		0o644,
	); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace: workspace,
				TurnProfile: config.TurnProfileConfig{
					Enabled:      true,
					SystemPrompt: config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
				},
			},
		},
	}
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)

	_, err := al.runAgentLoop(context.Background(), al.GetRegistry().GetDefaultAgent(), processOptions{
		SessionKey:      "agent:default:test-missing-agent-system-off",
		SenderID:        "sender-only",
		UserMessage:     "hello",
		DefaultResponse: defaultResponse,
	})
	if err != nil {
		t.Fatalf("runAgentLoop() error = %v", err)
	}
	if len(provider.messages) != 2 {
		t.Fatalf("provider messages len = %d, want system + user: %#v", len(provider.messages), provider.messages)
	}
	prompt := provider.messages[0].Content
	parts := strings.Split(prompt, "\n\n")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "## Current Time\n") {
		t.Fatalf("minimal prompt without AGENT.md = %q", prompt)
	}
	wantSender := "## Current Sender\n" + formatCurrentSenderLine("sender-only", "")
	if parts[1] != wantSender {
		t.Fatalf("current sender section = %q, want %q", parts[1], wantSender)
	}
	if strings.Contains(prompt, "LEGACY_AGENT_SECRET") {
		t.Fatalf("minimal prompt used legacy AGENTS.md: %q", prompt)
	}
}

func TestTurnProfile_DefaultSystemPromptBehaviorIsUnchanged(t *testing.T) {
	workspace := t.TempDir()
	for name, content := range map[string]string{
		"AGENT.md": "NORMAL_AGENT_BODY",
		"SOUL.md":  "NORMAL_SOUL_BODY",
		"USER.md":  "NORMAL_USER_BODY",
	} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	writeTurnProfileSkill(
		t,
		workspace,
		"normal-skill",
		"---\ndescription: normal skill\n---\nNORMAL_ACTIVE_SKILL_BODY",
	)
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace: workspace,
				TurnProfile: config.TurnProfileConfig{
					Enabled:      true,
					SystemPrompt: config.TurnProfileBlock{Mode: config.TurnProfileModeDefault},
				},
			},
		},
	}
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)

	_, err := al.runAgentLoop(context.Background(), al.GetRegistry().GetDefaultAgent(), processOptions{
		SessionKey:        "agent:default:test-default-system-prompt",
		Channel:           "telegram",
		ChatID:            "chat-42",
		SenderID:          "sender-42",
		ForcedSkills:      []string{"normal-skill"},
		UserMessage:       "hello",
		DefaultResponse:   defaultResponse,
		SenderDisplayName: "Ada",
	})
	if err != nil {
		t.Fatalf("runAgentLoop() error = %v", err)
	}
	if len(provider.messages) != 2 || provider.messages[0].Role != "system" {
		t.Fatalf("provider messages = %#v, want system + user", provider.messages)
	}
	prompt := provider.messages[0].Content
	for _, expected := range []string{
		"# picoclaw",
		"## Workspace",
		"## Important Rules",
		"NORMAL_AGENT_BODY",
		"NORMAL_SOUL_BODY",
		"NORMAL_USER_BODY",
		"# Skills",
		"# Active Skills",
		"NORMAL_ACTIVE_SKILL_BODY",
		"## Current Time",
		"## Runtime",
		"## Current Session",
		"## Current Sender",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("default system prompt missing %q:\n%s", expected, prompt)
		}
	}
}

func TestTurnProfile_SystemPromptOffDoesNotAddToolFallbackWhenToolsVisible(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				TurnProfile: config.TurnProfileConfig{
					Enabled:      true,
					History:      config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
					SystemPrompt: config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
					Skills:       config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
					Tools: config.TurnProfileBlock{
						Mode:  config.TurnProfileModeCustom,
						Allow: []string{"echo_text"},
					},
				},
			},
		},
	}
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)
	al.RegisterTool(&echoTextTool{})
	agent := al.GetRegistry().GetDefaultAgent()

	_, err := al.runAgentLoop(context.Background(), agent, processOptions{
		SessionKey:      "agent:default:test-tool-fallback",
		UserMessage:     "hello",
		DefaultResponse: defaultResponse,
	})
	if err != nil {
		t.Fatalf("runAgentLoop() error = %v", err)
	}
	if len(provider.tools) != 1 || provider.tools[0].Function.Name != "echo_text" {
		t.Fatalf("provider tools = %#v, want echo_text", provider.tools)
	}
	if got := provider.messages[0].Content; strings.Contains(got, toolUseSystemPromptRule()) {
		t.Fatalf("minimal prompt contains tool fallback: %q", got)
	}
	if !strings.HasPrefix(provider.messages[0].Content, "## Current Time\n") {
		t.Fatalf("minimal prompt missing current time: %q", provider.messages[0].Content)
	}
}

func TestTurnProfile_SkillsOffAndCustomControlCatalogAndActiveSkills(t *testing.T) {
	workspace := t.TempDir()
	writeTurnProfileSkill(
		t,
		workspace,
		"shell",
		"---\ndescription: shell skill\n---\n# shell\n\nUse shell carefully.",
	)
	writeTurnProfileSkill(
		t,
		workspace,
		"paint",
		"---\ndescription: paint skill\n---\n# paint\n\nUse paint vividly.",
	)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace: workspace,
				TurnProfile: config.TurnProfileConfig{
					Enabled: true,
					History: config.TurnProfileBlock{
						Mode: config.TurnProfileModeOff,
					},
					Skills: config.TurnProfileBlock{
						Mode: config.TurnProfileModeOff,
					},
				},
			},
		},
	}
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)
	agent := al.GetRegistry().GetDefaultAgent()

	_, err := al.runAgentLoop(context.Background(), agent, processOptions{
		SessionKey:      "agent:default:test-skills-off",
		UserMessage:     "hello",
		DefaultResponse: defaultResponse,
		ForcedSkills:    []string{"shell"},
	})
	if err != nil {
		t.Fatalf("runAgentLoop(no-skills) error = %v", err)
	}
	noSkillsPrompt := provider.messages[0].Content
	if strings.Contains(noSkillsPrompt, "# Skills") ||
		strings.Contains(noSkillsPrompt, "# Active Skills") {
		t.Fatalf("skills-off prompt includes skill context:\n%s", noSkillsPrompt)
	}

	cfg.Agents.Defaults.TurnProfile.Skills = config.TurnProfileBlock{
		Mode:  config.TurnProfileModeCustom,
		Allow: []string{"shell"},
	}
	provider = &turnProfileCaptureProvider{}
	al = newTurnProfileAgentLoop(t, cfg, provider)
	agent = al.GetRegistry().GetDefaultAgent()

	_, err = al.runAgentLoop(context.Background(), agent, processOptions{
		SessionKey:      "agent:default:test-skills-custom",
		UserMessage:     "hello",
		DefaultResponse: defaultResponse,
		ForcedSkills:    []string{"shell", "paint"},
	})
	if err != nil {
		t.Fatalf("runAgentLoop(shell-only) error = %v", err)
	}
	customPrompt := provider.messages[0].Content
	if !strings.Contains(customPrompt, "<name>shell</name>") ||
		!strings.Contains(customPrompt, "### Skill: shell") {
		t.Fatalf("custom skills prompt missing allowed shell context:\n%s", customPrompt)
	}
	if strings.Contains(customPrompt, "<name>paint</name>") ||
		strings.Contains(customPrompt, "### Skill: paint") {
		t.Fatalf("custom skills prompt includes disallowed paint context:\n%s", customPrompt)
	}
}

type turnProfileAddToolHook struct{}

func (h turnProfileAddToolHook) BeforeLLM(
	ctx context.Context,
	req *LLMHookRequest,
) (*LLMHookRequest, HookDecision, error) {
	next := req.Clone()
	next.Tools = append(next.Tools, providers.ToolDefinition{
		Type: "function",
		Function: providers.ToolFunctionDefinition{
			Name:        "echo_text_rewritten",
			Description: "hook-added tool",
			Parameters:  map[string]any{"type": "object"},
		},
	})
	return next, HookDecision{Action: HookActionModify}, nil
}

type turnProfileEnableNativeSearchHook struct{}

func (h turnProfileEnableNativeSearchHook) BeforeLLM(
	ctx context.Context,
	req *LLMHookRequest,
) (*LLMHookRequest, HookDecision, error) {
	next := req.Clone()
	if next.Options == nil {
		next.Options = map[string]any{}
	}
	next.Options["turn_profile_test_hook"] = true
	next.Options["native_search"] = true
	return next, HookDecision{Action: HookActionModify}, nil
}

func (h turnProfileEnableNativeSearchHook) AfterLLM(
	ctx context.Context,
	resp *LLMHookResponse,
) (*LLMHookResponse, HookDecision, error) {
	return resp, HookDecision{Action: HookActionContinue}, nil
}

type turnProfileRespondToolHook struct{}

func (h turnProfileRespondToolHook) BeforeTool(
	ctx context.Context,
	req *ToolCallHookRequest,
) (*ToolCallHookRequest, HookDecision, error) {
	next := req.Clone()
	next.HookResult = &tools.ToolResult{ForLLM: "hook bypassed profile"}
	return next, HookDecision{Action: HookActionRespond}, nil
}

func (h turnProfileRespondToolHook) AfterTool(
	ctx context.Context,
	result *ToolResultHookResponse,
) (*ToolResultHookResponse, HookDecision, error) {
	return result, HookDecision{Action: HookActionContinue}, nil
}

type turnProfileToolCallProvider struct {
	calls    int
	messages []providers.Message
}

func (p *turnProfileToolCallProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	model string,
	opts map[string]any,
) (*providers.LLMResponse, error) {
	p.calls++
	p.messages = append([]providers.Message(nil), messages...)
	if p.calls == 1 {
		return &providers.LLMResponse{
			Content: "calling disallowed",
			ToolCalls: []providers.ToolCall{{
				ID:        "call_1",
				Name:      "echo_text_rewritten",
				Arguments: map[string]any{"text": "blocked"},
			}},
			FinishReason: "tool_calls",
		}, nil
	}
	return &providers.LLMResponse{Content: "done"}, nil
}

func (p *turnProfileToolCallProvider) GetDefaultModel() string {
	return "test-model"
}

func TestTurnProfile_ToolsCustomFiltersProviderToolsAndHookAdditions(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				TurnProfile: config.TurnProfileConfig{
					Enabled: true,
					History: config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
					Tools: config.TurnProfileBlock{
						Mode:  config.TurnProfileModeCustom,
						Allow: []string{"echo_text"},
					},
				},
			},
		},
	}
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)
	al.RegisterTool(&echoTextTool{})
	al.RegisterTool(&echoTextRewrittenTool{})
	if err := al.MountHook(NamedHook("add-disallowed-tool", turnProfileAddToolHook{})); err != nil {
		t.Fatalf("MountHook() error = %v", err)
	}

	_, err := al.runAgentLoop(
		context.Background(),
		al.GetRegistry().GetDefaultAgent(),
		processOptions{
			SessionKey:      "agent:default:test-tools-filter",
			UserMessage:     "hello",
			DefaultResponse: defaultResponse,
		},
	)
	if err != nil {
		t.Fatalf("runAgentLoop() error = %v", err)
	}
	if len(provider.tools) != 1 {
		t.Fatalf("provider tools len = %d, want 1: %#v", len(provider.tools), provider.tools)
	}
	if provider.tools[0].Function.Name != "echo_text" {
		t.Fatalf("provider tool = %q, want echo_text", provider.tools[0].Function.Name)
	}
}

func TestTurnProfile_ToolsOffDisablesProviderAndNativeSearchTools(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Web: config.WebToolsConfig{
				ToolConfig:   config.ToolConfig{Enabled: true},
				PreferNative: true,
			},
		},
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				TurnProfile: config.TurnProfileConfig{
					Enabled: true,
					History: config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
					Tools:   config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
				},
			},
		},
	}
	cfg.Agents.Defaults.Workspace = t.TempDir()
	cfg.Agents.Defaults.ModelName = "test-model"
	cfg.Agents.Defaults.MaxTokens = 4096
	cfg.Agents.Defaults.MaxToolIterations = 10
	provider := &nativeSearchCaptureProvider{}
	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)

	_, err := al.runAgentLoop(
		context.Background(),
		al.GetRegistry().GetDefaultAgent(),
		processOptions{
			SessionKey:      "agent:default:test-tools-off",
			UserMessage:     "hello",
			DefaultResponse: defaultResponse,
		},
	)
	if err != nil {
		t.Fatalf("runAgentLoop() error = %v", err)
	}
	if provider.lastOpts["native_search"] == true {
		t.Fatalf("native_search option enabled despite tools.mode=off: %#v", provider.lastOpts)
	}
}

func TestTurnProfile_ToolsOffSuppressesToolUsePromptRule(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				TurnProfile: config.TurnProfileConfig{
					Enabled: true,
					History: config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
					Tools:   config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
				},
			},
		},
	}
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)
	al.RegisterTool(&echoTextTool{})

	_, err := al.runAgentLoop(
		context.Background(),
		al.GetRegistry().GetDefaultAgent(),
		processOptions{
			SessionKey:      "agent:default:test-tools-off-prompt",
			UserMessage:     "hello",
			DefaultResponse: defaultResponse,
		},
	)
	if err != nil {
		t.Fatalf("runAgentLoop() error = %v", err)
	}
	if len(provider.tools) != 0 {
		t.Fatalf("provider tools len = %d, want 0", len(provider.tools))
	}
	if len(provider.messages) == 0 || provider.messages[0].Role != "system" {
		t.Fatalf("first provider message = %#v, want system prompt", provider.messages)
	}
	if strings.Contains(provider.messages[0].Content, toolUseSystemPromptRule()) ||
		strings.Contains(provider.messages[0].Content, "**ALWAYS use tools**") {
		t.Fatalf("tools-off system prompt still asks the model to use tools:\n%s", provider.messages[0].Content)
	}
}

func TestTurnProfile_ToolsCustomMissingToolSuppressesToolUsePromptRule(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				TurnProfile: config.TurnProfileConfig{
					Enabled: true,
					History: config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
					Tools: config.TurnProfileBlock{
						Mode:  config.TurnProfileModeCustom,
						Allow: []string{"web_search"},
					},
				},
			},
		},
	}
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)
	al.RegisterTool(&echoTextTool{})

	_, err := al.runAgentLoop(
		context.Background(),
		al.GetRegistry().GetDefaultAgent(),
		processOptions{
			SessionKey:      "agent:default:test-tools-custom-missing-prompt",
			UserMessage:     "hello",
			DefaultResponse: defaultResponse,
		},
	)
	if err != nil {
		t.Fatalf("runAgentLoop() error = %v", err)
	}
	if len(provider.tools) != 0 {
		t.Fatalf("provider tools len = %d, want 0", len(provider.tools))
	}
	if strings.Contains(provider.messages[0].Content, toolUseSystemPromptRule()) ||
		strings.Contains(provider.messages[0].Content, "**ALWAYS use tools**") {
		t.Fatalf(
			"custom profile with no resolved tools still asks the model to use tools:\n%s",
			provider.messages[0].Content,
		)
	}
}

func TestTurnProfile_ToolsCustomAllowsNativeWebSearch(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Web: config.WebToolsConfig{
				ToolConfig:   config.ToolConfig{Enabled: true},
				PreferNative: true,
			},
		},
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         t.TempDir(),
				ModelName:         "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
				TurnProfile: config.TurnProfileConfig{
					Enabled: true,
					History: config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
					Tools: config.TurnProfileBlock{
						Mode:  config.TurnProfileModeCustom,
						Allow: []string{"web_search"},
					},
				},
			},
		},
	}
	provider := &nativeSearchCaptureProvider{}
	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)

	_, err := al.runAgentLoop(
		context.Background(),
		al.GetRegistry().GetDefaultAgent(),
		processOptions{
			SessionKey:      "agent:default:test-native-web-allowed",
			UserMessage:     "search",
			DefaultResponse: defaultResponse,
		},
	)
	if err != nil {
		t.Fatalf("runAgentLoop() error = %v", err)
	}
	if got, _ := provider.lastOpts["native_search"].(bool); !got {
		t.Fatalf("native_search = %#v, want true", provider.lastOpts["native_search"])
	}
}

func TestTurnProfile_SystemPromptOffOmitsToolFallbackForNativeWebSearch(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Web: config.WebToolsConfig{
				ToolConfig:   config.ToolConfig{Enabled: true},
				PreferNative: true,
			},
		},
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         t.TempDir(),
				ModelName:         "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
				TurnProfile: config.TurnProfileConfig{
					Enabled:      true,
					History:      config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
					SystemPrompt: config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
					Skills:       config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
					Tools: config.TurnProfileBlock{
						Mode:  config.TurnProfileModeCustom,
						Allow: []string{"web_search"},
					},
				},
			},
		},
	}
	provider := &nativeSearchCaptureProvider{}
	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)

	_, err := al.runAgentLoop(
		context.Background(),
		al.GetRegistry().GetDefaultAgent(),
		processOptions{
			SessionKey:      "agent:default:test-native-web-fallback",
			UserMessage:     "search",
			DefaultResponse: defaultResponse,
		},
	)
	if err != nil {
		t.Fatalf("runAgentLoop() error = %v", err)
	}
	if got, _ := provider.lastOpts["native_search"].(bool); !got {
		t.Fatalf("native_search = %#v, want true", provider.lastOpts["native_search"])
	}
	if len(provider.messages) == 0 || !strings.HasPrefix(provider.messages[0].Content, "## Current Time\n") {
		t.Fatalf("native-search-only prompt = %#v, want minimal prompt", provider.messages)
	}
	if strings.Contains(provider.messages[0].Content, toolUseSystemPromptRule()) {
		t.Fatalf("native-search-only prompt contains tool fallback: %#v", provider.messages)
	}
}

func TestTurnProfile_BeforeLLMHookCannotReenableNativeSearchWhenToolsOff(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Web: config.WebToolsConfig{
				ToolConfig:   config.ToolConfig{Enabled: true},
				PreferNative: true,
			},
		},
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         t.TempDir(),
				ModelName:         "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
				TurnProfile: config.TurnProfileConfig{
					Enabled: true,
					History: config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
					Tools:   config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
				},
			},
		},
	}
	provider := &nativeSearchCaptureProvider{}
	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)
	if err := al.MountHook(NamedHook("enable-native-search", turnProfileEnableNativeSearchHook{})); err != nil {
		t.Fatalf("MountHook() error = %v", err)
	}

	_, err := al.runAgentLoop(
		context.Background(),
		al.GetRegistry().GetDefaultAgent(),
		processOptions{
			SessionKey:      "agent:default:test-native-web-hook-denied",
			UserMessage:     "search",
			DefaultResponse: defaultResponse,
		},
	)
	if err != nil {
		t.Fatalf("runAgentLoop() error = %v", err)
	}
	if provider.lastOpts["native_search"] == true {
		t.Fatalf("native_search option enabled by hook despite tools.mode=off: %#v", provider.lastOpts)
	}
}

func TestTurnProfile_BeforeLLMHookCannotReenableNativeSearchWhenCustomToolsResolveEmpty(
	t *testing.T,
) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Web: config.WebToolsConfig{
				ToolConfig:   config.ToolConfig{Enabled: true},
				PreferNative: true,
			},
		},
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         t.TempDir(),
				ModelName:         "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
				TurnProfile: config.TurnProfileConfig{
					Enabled: true,
					History: config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
					Tools: config.TurnProfileBlock{
						Mode:  config.TurnProfileModeCustom,
						Allow: []string{"missing_tool"},
					},
				},
			},
		},
	}
	provider := &nativeSearchCaptureProvider{}
	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)
	if err := al.MountHook(NamedHook("enable-native-search", turnProfileEnableNativeSearchHook{})); err != nil {
		t.Fatalf("MountHook() error = %v", err)
	}

	_, err := al.runAgentLoop(
		context.Background(),
		al.GetRegistry().GetDefaultAgent(),
		processOptions{
			SessionKey:      "agent:default:test-native-web-hook-custom-empty",
			UserMessage:     "search",
			DefaultResponse: defaultResponse,
		},
	)
	if err != nil {
		t.Fatalf("runAgentLoop() error = %v", err)
	}
	if provider.lastOpts["native_search"] == true {
		t.Fatalf(
			"native_search option enabled by hook despite no resolved tools: %#v",
			provider.lastOpts,
		)
	}
}

func TestTurnProfile_ToolExecutionRejectsDisallowedToolCalls(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				TurnProfile: config.TurnProfileConfig{
					Enabled: true,
					History: config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
					Tools: config.TurnProfileBlock{
						Mode:  config.TurnProfileModeCustom,
						Allow: []string{"echo_text"},
					},
				},
			},
		},
	}
	provider := &turnProfileToolCallProvider{}
	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)
	al.RegisterTool(&echoTextTool{})
	al.RegisterTool(&echoTextRewrittenTool{})

	response, err := al.runAgentLoop(
		context.Background(),
		al.GetRegistry().GetDefaultAgent(),
		processOptions{
			SessionKey:      "agent:default:test-tool-exec-deny",
			UserMessage:     "run tool",
			DefaultResponse: defaultResponse,
		},
	)
	if err != nil {
		t.Fatalf("runAgentLoop() error = %v", err)
	}
	if response != "done" {
		t.Fatalf("response = %q, want done", response)
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls = %d, want 2", provider.calls)
	}
	var foundDeniedResult bool
	for _, msg := range provider.messages {
		if msg.Role == "tool" &&
			strings.Contains(msg.Content, "not allowed by the active turn profile") {
			foundDeniedResult = true
			break
		}
	}
	if !foundDeniedResult {
		t.Fatalf("second provider call did not include denied tool result: %#v", provider.messages)
	}
}

func TestTurnProfile_BeforeToolRespondCannotBypassDisallowedTool(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				TurnProfile: config.TurnProfileConfig{
					Enabled: true,
					History: config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
					Tools: config.TurnProfileBlock{
						Mode:  config.TurnProfileModeCustom,
						Allow: []string{"echo_text"},
					},
				},
			},
		},
	}
	provider := &turnProfileToolCallProvider{}
	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)
	al.RegisterTool(&echoTextTool{})
	al.RegisterTool(&echoTextRewrittenTool{})
	if err := al.MountHook(NamedHook("respond-tool", turnProfileRespondToolHook{})); err != nil {
		t.Fatalf("MountHook() error = %v", err)
	}

	_, err := al.runAgentLoop(
		context.Background(),
		al.GetRegistry().GetDefaultAgent(),
		processOptions{
			SessionKey:      "agent:default:test-tool-hook-respond-denied",
			UserMessage:     "run tool",
			DefaultResponse: defaultResponse,
		},
	)
	if err != nil {
		t.Fatalf("runAgentLoop() error = %v", err)
	}
	var foundDeniedResult bool
	for _, msg := range provider.messages {
		if msg.Role != "tool" {
			continue
		}
		if strings.Contains(msg.Content, "hook bypassed profile") {
			t.Fatalf("hook respond result bypassed turn profile: %#v", provider.messages)
		}
		if strings.Contains(msg.Content, "not allowed by the active turn profile") {
			foundDeniedResult = true
		}
	}
	if !foundDeniedResult {
		t.Fatalf("second provider call did not include denied tool result: %#v", provider.messages)
	}
}
