package providers

// Explicit endpoint families follow OpenCode Go's published endpoint table.
var openCodeGoFamilies = map[string]string{
	"grok-4.7":                     "responses",
	"grok-4.6":                     "responses",
	"gpt-6-luna":                   "responses",
	"gpt-5.6-luna":                 "responses",
	"muse-spark-1.3-contributor":   "responses",
	"muse-spark-1.2-contributor":   "responses",
	"minimax-m3":                   "messages",
	"minimax-m2.7":                 "messages",
	"minimax-m2.5":                 "messages",
	"qwen3.8-max":                  "messages",
	"qwen3.8-flash":                "messages",
	"qwen3.7-max":                  "messages",
	"qwen3.7-plus":                 "messages",
	"qwen3.6-plus":                 "messages",
	"glm-5.3-flash":                "chat",
	"glm-5.3":                      "chat",
	"glm-5.2":                      "chat",
	"glm-5.1":                      "chat",
	"kimi-k3":                      "chat",
	"kimi-k2.7-code":               "chat",
	"kimi-k2.6":                    "chat",
	"longcat-2.0":                  "chat",
	"deepseek-v4.1-flash":          "chat",
	"deepseek-v4-pro":              "chat",
	"deepseek-v4-flash":            "chat",
	"deepseek-v4-flash-vision-exp": "chat",
	"mimo-v2.6-flash":              "chat",
	"mimo-v2.6-pro":                "chat",
	"mimo-v2.5":                    "chat",
	"mimo-v2.5-pro":                "chat",
	"hy4-preview":                  "chat",
	"hy3":                          "chat",
	"space-bunny-free":             "chat",
}

func openCodeGoEndpointFamily(modelID string) string {
	return openCodeGoFamilies[modelID]
}
