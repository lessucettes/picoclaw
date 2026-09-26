package providers

import (
	"fmt"
	"os"
	"strings"

	"github.com/sipeed/picoclaw/pkg/config"
)

// openCodeZenFamilies follows OpenCode's Zen endpoint table and live Zen model
// catalog. Model IDs are explicit because regular Zen and OpenCode Go can route
// the same model name through different API protocols.
var openCodeZenFamilies = map[string]string{
	// OpenAI Responses API.
	"gpt-6-astra":         "responses",
	"gpt-6-sol":           "responses",
	"gpt-6-luna":          "responses",
	"gpt-5.6-sol":         "responses",
	"gpt-5.6-terra":       "responses",
	"gpt-5.6-luna":        "responses",
	"gpt-5.5":             "responses",
	"gpt-5.5-pro":         "responses",
	"gpt-5.4":             "responses",
	"gpt-5.4-pro":         "responses",
	"gpt-5.4-mini":        "responses",
	"gpt-5.4-nano":        "responses",
	"gpt-5.3-codex":       "responses",
	"gpt-5.3-codex-spark": "responses",
	"gpt-5.2":             "responses",
	"gpt-5.2-codex":       "responses",
	"gpt-5.1":             "responses",
	"gpt-5.1-codex":       "responses",
	"gpt-5.1-codex-max":   "responses",
	"gpt-5.1-codex-mini":  "responses",
	"gpt-5":               "responses",
	"gpt-5-codex":         "responses",
	"gpt-5-nano":          "responses",
	"grok-4.7":            "responses",
	"grok-4.6":            "responses",
	"grok-4.5":            "responses",
	"grok-build-0.1":      "responses",
	"muse-spark-1.3":      "responses",
	"muse-spark-1.2":      "responses",

	// Anthropic Messages API.
	"claude-fable-5-1":  "messages",
	"claude-fable-5":    "messages",
	"claude-opus-5-5":   "messages",
	"claude-opus-5":     "messages",
	"claude-opus-4-8":   "messages",
	"claude-opus-4-7":   "messages",
	"claude-opus-4-6":   "messages",
	"claude-opus-4-5":   "messages",
	"claude-sonnet-5":   "messages",
	"claude-sonnet-4-6": "messages",
	"claude-sonnet-4-5": "messages",
	"claude-haiku-4-5":  "messages",
	"qwen3.8-flash":     "messages",
	"qwen3.7-max":       "messages",
	"qwen3.7-plus":      "messages",
	"qwen3.6-plus":      "messages",
	"qwen3.5-plus":      "messages",

	// Gemini generateContent API.
	"gemini-3.8-flash":      "google",
	"gemini-3.7-flash":      "google",
	"gemini-3.6-flash":      "google",
	"gemini-3.5-flash":      "google",
	"gemini-3.5-flash-lite": "google",
	"gemini-3.1-pro":        "google",
	"gemini-3-flash":        "google",

	// OpenAI Chat Completions API.
	"deepseek-v4.1-flash":          "chat",
	"deepseek-v4-pro":              "chat",
	"deepseek-v4-flash":            "chat",
	"deepseek-v4-flash-vision-exp": "chat",
	"minimax-m3":                   "chat",
	"minimax-m2.7":                 "chat",
	"minimax-m2.5":                 "chat",
	"glm-5.3-flash":                "chat",
	"glm-5.3":                      "chat",
	"glm-5.2":                      "chat",
	"glm-5.1":                      "chat",
	"glm-5":                        "chat",
	"kimi-k2.5":                    "chat",
	"kimi-k2.6":                    "chat",
	"kimi-k2.7-code":               "chat",
	"kimi-k3":                      "chat",
	// SystemOne is documented by OpenCode Zen but is not a conversational LLM API.
	"jev-1.13": "systemone",
}

func openCodeZenEndpointFamily(modelID string) string {
	return openCodeZenFamilies[modelID]
}

// IsSupportedOpenCodeModel reports whether an OpenCode Zen or OpenCode Go
// model has a verified conversational endpoint family.
func IsSupportedOpenCodeModel(provider, modelID string) bool {
	switch NormalizeProvider(provider) {
	case "opencode":
		family := openCodeZenEndpointFamily(modelID)
		return family != "" && family != "systemone"
	case "opencode-go":
		return openCodeGoEndpointFamily(modelID) != ""
	default:
		return false
	}
}

func openCodeZenAPIKey(cfg *config.ModelConfig) (string, error) {
	if cfg != nil {
		if apiKey := cfg.APIKey(); strings.TrimSpace(apiKey) != "" {
			return apiKey, nil
		}
	}
	for _, name := range []string{"OPENCODE_ZEN_API_KEY", "OPENCODE_API_KEY"} {
		if apiKey := strings.TrimSpace(os.Getenv(name)); apiKey != "" {
			return apiKey, nil
		}
	}
	return "", fmt.Errorf("api_key, OPENCODE_ZEN_API_KEY, or OPENCODE_API_KEY is required for opencode")
}
