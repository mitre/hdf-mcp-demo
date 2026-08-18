package instrument

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// OpenAI is an OpenAI-compatible /v1/chat/completions instrument. It works with
// any endpoint speaking that API — an internal LiteLLM proxy, OpenAI, Azure
// OpenAI, vLLM, Ollama's /v1 shim. BaseURL/Model/APIKey are pure configuration;
// the API key is supplied by the caller (from the environment at run time) and
// is never logged or persisted here.
type OpenAI struct {
	BaseURL     string // e.g. https://api.openai.com/v1, http://litellm.internal/v1
	Model       string
	APIKey      string   // Bearer token; supplied by the caller, not hard-coded
	MaxTokens   int      // when > 0, cap the completion — keep it generous for reasoning models so they finish rather than truncating to content:null
	Temperature *float64 // when non-nil, sent as-is (0 for determinism); omitted when nil, for models that reject an explicit temperature
	MaxRetries  int      // retries on 429/5xx/transient errors (default 4); needed once arms fan out concurrently
	HTTP        *http.Client
}

// NewOpenAI builds an OpenAI-compatible instrument. A trailing /v1 on baseURL is
// respected as-is (do not append another path segment).
func NewOpenAI(baseURL, model, apiKey string) *OpenAI {
	return &OpenAI{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Model:      model,
		APIKey:     apiKey,
		MaxRetries: 4,
		HTTP:       &http.Client{Timeout: 5 * time.Minute},
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
	if o.Temperature != nil {
		body["temperature"] = *o.Temperature
	}
	if len(tools) > 0 {
		body["tools"] = toOATools(tools)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Result{}, err
	}

	var lastErr error
	for attempt := 0; attempt <= o.MaxRetries; attempt++ {
		if attempt > 0 {
			if err := sleepBackoff(ctx, attempt, lastErr); err != nil {
				return Result{}, err
			}
		}
		res, retryable, err := o.chatOnce(ctx, raw)
		if err == nil {
			return res, nil
		}
		lastErr = err
		if !retryable {
			return Result{}, err
		}
	}
	return Result{}, fmt.Errorf("openai-compatible chat to %s failed after %d retries: %w", o.BaseURL, o.MaxRetries, lastErr)
}

// retryableErr wraps a retry-worthy failure (429/5xx/transient) and carries the
// server's suggested delay from a Retry-After header when present.
type retryableErr struct {
	err        error
	retryAfter time.Duration
}

func (e *retryableErr) Error() string { return e.err.Error() }

// chatOnce performs one HTTP attempt. retryable is true for 429/5xx and transient
// transport errors, so the caller can back off and try again.
func (o *OpenAI) chatOnce(ctx context.Context, raw []byte) (_ Result, retryable bool, _ error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.BaseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return Result{}, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	if o.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.APIKey)
	}

	resp, err := o.HTTP.Do(req)
	if err != nil {
		// A context cancellation/deadline is terminal; other transport errors are transient.
		if ctx.Err() != nil {
			return Result{}, false, fmt.Errorf("openai-compatible chat to %s: %w", o.BaseURL, err)
		}
		return Result{}, true, fmt.Errorf("openai-compatible chat to %s: %w", o.BaseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return Result{}, true, &retryableErr{
			err:        fmt.Errorf("openai-compatible endpoint returned %s: %s", resp.Status, strings.TrimSpace(string(body))),
			retryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}

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
		return Result{}, false, fmt.Errorf("decode response from %s: %w", o.BaseURL, err)
	}
	if out.Error != nil {
		return Result{}, false, fmt.Errorf("openai-compatible error: %s", out.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, false, fmt.Errorf("openai-compatible endpoint returned %s", resp.Status)
	}
	if len(out.Choices) == 0 {
		return Result{}, false, fmt.Errorf("no choices in response")
	}
	return Result{
		Message:          fromOAMessage(out.Choices[0].Message),
		PromptTokens:     out.Usage.PromptTokens,
		CompletionTokens: out.Usage.CompletionTokens,
	}, false, nil
}

// sleepBackoff waits before a retry: the server's Retry-After if given, else
// exponential backoff (0.5s, 1s, 2s, …), capped, and honoring context cancel.
func sleepBackoff(ctx context.Context, attempt int, lastErr error) error {
	d := time.Duration(250<<attempt) * time.Millisecond // 0.5s, 1s, 2s, 4s...
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	var re *retryableErr
	if errors.As(lastErr, &re) && re.retryAfter > 0 {
		d = re.retryAfter
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// parseRetryAfter reads a Retry-After header expressed as delay-seconds.
func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
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
