package instrument

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// OpenAI is an OpenAI-compatible /v1/chat/completions instrument. It works with
// any endpoint speaking that API — an internal LiteLLM proxy, OpenAI, Azure
// OpenAI, vLLM, Ollama's /v1 shim. BaseURL/Model/APIKey are pure configuration;
// the API key is supplied by the caller (from the environment at run time) and
// is never logged or persisted here.
type OpenAI struct {
	BaseURL   string // e.g. https://api.openai.com/v1, http://litellm.internal/v1
	Model     string
	APIKey    string // Bearer token; supplied by the caller, not hard-coded
	MaxTokens int    // when > 0, cap the completion — keep it generous for reasoning models so they finish rather than truncating to content:null
	HTTP      *http.Client
}

// NewOpenAI builds an OpenAI-compatible instrument. A trailing /v1 on baseURL is
// respected as-is (do not append another path segment).
func NewOpenAI(baseURL, model, apiKey string) *OpenAI {
	return &OpenAI{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Model:   model,
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 5 * time.Minute},
	}
}

// Name identifies the instrument (host + model, never the key).
func (o *OpenAI) Name() string { return "openai:" + o.Model }

// wire types (OpenAI-compatible chat completions)
type oaMessage struct {
	Role             string       `json:"role"`
	Content          string       `json:"content"`
	ReasoningContent string       `json:"reasoning_content,omitempty"`
	ToolCalls        []oaToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string       `json:"tool_call_id,omitempty"`
}

type oaToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // OpenAI returns arguments as a JSON *string*
	} `json:"function"`
}

type oaTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

// Chat performs one /v1/chat/completions round-trip.
func (o *OpenAI) Chat(ctx context.Context, msgs []Message, tools []Tool) (Result, error) {
	body := map[string]any{
		"model":    o.Model,
		"messages": toOAMessages(msgs),
	}
	if o.MaxTokens > 0 {
		body["max_tokens"] = o.MaxTokens
	}
	if len(tools) > 0 {
		body["tools"] = toOATools(tools)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.BaseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if o.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.APIKey)
	}

	resp, err := o.HTTP.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("openai-compatible chat to %s: %w", o.BaseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	var out struct {
		Choices []struct {
			Message oaMessage `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Result{}, fmt.Errorf("decode response from %s: %w", o.BaseURL, err)
	}
	if out.Error != nil {
		return Result{}, fmt.Errorf("openai-compatible error: %s", out.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("openai-compatible endpoint returned %s", resp.Status)
	}
	if len(out.Choices) == 0 {
		return Result{}, fmt.Errorf("no choices in response")
	}
	return Result{
		Message:          fromOAMessage(out.Choices[0].Message),
		PromptTokens:     out.Usage.PromptTokens,
		CompletionTokens: out.Usage.CompletionTokens,
	}, nil
}

func toOAMessages(msgs []Message) []oaMessage {
	out := make([]oaMessage, len(msgs))
	for i, m := range msgs {
		om := oaMessage{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			var otc oaToolCall
			otc.ID = tc.ID
			otc.Type = "function"
			otc.Function.Name = tc.Name
			otc.Function.Arguments = string(tc.Arguments) // neutral raw JSON -> OpenAI string
			om.ToolCalls = append(om.ToolCalls, otc)
		}
		out[i] = om
	}
	return out
}

func toOATools(tools []Tool) []oaTool {
	out := make([]oaTool, len(tools))
	for i, t := range tools {
		var ot oaTool
		ot.Type = "function"
		ot.Function.Name = t.Name
		ot.Function.Description = t.Description
		ot.Function.Parameters = t.Parameters
		out[i] = ot
	}
	return out
}

func fromOAMessage(m oaMessage) Message {
	out := Message{Role: m.Role, Content: m.Content, ReasoningContent: m.ReasoningContent}
	for _, tc := range m.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: json.RawMessage(tc.Function.Arguments), // OpenAI string is already JSON
		})
	}
	return out
}
