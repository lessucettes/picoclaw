// Package openairesponses provides a small HTTP transport for the OpenAI Responses API.
package openairesponses

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"

	"github.com/sipeed/picoclaw/pkg/providers/common"
	orc "github.com/sipeed/picoclaw/pkg/providers/openai_responses_common"
	"github.com/sipeed/picoclaw/pkg/providers/protocoltypes"
)

type Provider struct {
	key, base, userAgent string
	client               *http.Client
	headers              map[string]string
	sessionHeaderName    string
}

// NewProvider creates a Responses API provider using the OpenCode Go session header.
func NewProvider(key, base, proxy, userAgent string, timeoutSeconds int, headers map[string]string) *Provider {
	return NewProviderWithSessionHeader(key, base, proxy, userAgent, timeoutSeconds, headers, "x-opencode-session")
}

// NewProviderWithSessionHeader creates a Responses API provider with an optional session header.
func NewProviderWithSessionHeader(
	key, base, proxy, userAgent string,
	timeoutSeconds int,
	headers map[string]string,
	sessionHeaderName string,
) *Provider {
	client := common.NewHTTPClient(proxy)
	if timeoutSeconds > 0 {
		client.Timeout = time.Duration(timeoutSeconds) * time.Second
	}
	return &Provider{
		key:               key,
		base:              strings.TrimRight(base, "/"),
		userAgent:         userAgent,
		client:            client,
		headers:           headers,
		sessionHeaderName: sessionHeaderName,
	}
}

func (p *Provider) GetDefaultModel() string {
	return ""
}

func (p *Provider) Chat(
	ctx context.Context,
	messages []protocoltypes.Message,
	tools []protocoltypes.ToolDefinition,
	model string,
	options map[string]any,
) (*protocoltypes.LLMResponse, error) {
	if p.key == "" {
		return nil, fmt.Errorf("API key is required")
	}

	endpoint, err := url.JoinPath(p.base, "responses")
	if err != nil {
		return nil, fmt.Errorf("building Responses URL: %w", err)
	}

	input, instructions := orc.TranslateMessages(messages)
	body := responses.ResponseNewParams{
		Model: model,
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: input},
		Store: openai.Opt(false),
	}
	if instructions != "" {
		body.Instructions = openai.Opt(instructions)
	}
	if len(tools) > 0 {
		body.Tools = orc.TranslateTools(tools, false)
		body.ToolChoice = responses.ResponseNewParamsToolChoiceUnion{
			OfToolChoiceMode: openai.Opt(responses.ToolChoiceOptionsAuto),
		}
	}
	if maxTokens, ok := common.AsInt(options["max_tokens"]); ok {
		body.MaxOutputTokens = openai.Opt(int64(maxTokens))
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encoding Responses request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("creating Responses request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+p.key)
	if p.userAgent != "" {
		request.Header.Set("User-Agent", p.userAgent)
	}
	for name, value := range p.headers {
		if strings.TrimSpace(name) != "" {
			request.Header.Set(name, value)
		}
	}
	common.ApplySessionHeader(request.Header, options, p.sessionHeaderName)

	response, err := p.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("sending Responses request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, common.HandleErrorResponse(response, p.base)
	}

	result, err := orc.ParseResponseBody(response.Body)
	if err != nil {
		return nil, fmt.Errorf("decoding Responses result: %w", err)
	}
	return result, nil
}
