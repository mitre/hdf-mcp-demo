package instrument

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// DefaultOllamaURL is the local Ollama server address.
const DefaultOllamaURL = "http://127.0.0.1:11434"

// Ollama is a native Ollama /api/chat instrument — fully local inference, so it
// cannot incur a charge. Requires a tool-calling-capable pulled model (llama3.2,
// granite4.1, gpt-oss, gemma4, …).
type Ollama struct {
	BaseURL    string
	Model      string
	NumPredict int // completion-token cap (Ollama's num_predict); 0 = model default
	// NumCtx is the context window (Ollama's num_ctx). 0 leaves it to the server,
	// whose default is only 4096 regardless of the model's advertised capacity —
	// far below what the raw-file arm needs, and Ollama truncates silently rather
	// than erroring, so an unset value quietly understates the raw arm. Always set
	// it for a benchmark run.
	NumCtx      int
	Temperature *float64 // when non-nil, sampling temperature (0 = deterministic)
	HTTP        *http.Client
}

// NewOllama builds a local Ollama instrument. baseURL == "" uses DefaultOllamaURL.
func NewOllama(baseURL, model string) *Ollama {
	if baseURL == "" {
		baseURL = DefaultOllamaURL
	}
	return &Ollama{BaseURL: baseURL, Model: model, HTTP: &http.Client{Timeout: 5 * time.Minute}}
}

// Name identifies the instrument.
func (o *Ollama) Name() string { return "ollama:" + o.Model }

// options builds Ollama's per-request options map from the configured knobs.
func (o *Ollama) options() map[string]any {
	m := map[string]any{}
	if o.NumPredict > 0 {
		m["num_predict"] = o.NumPredict
	}
	if o.NumCtx > 0 {
		m["num_ctx"] = o.NumCtx
	}
	if o.Temperature != nil {
		m["temperature"] = *o.Temperature
	}
	return m
}

type olMessage struct {
	Role      string       `json:"role"`
	Content   string       `json:"content"`
	ToolCalls []olToolCall `json:"tool_calls,omitempty"`
}

type olToolCall struct {
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"` // Ollama uses an argument *object*
	} `json:"function"`
}

// Chat performs one non-streaming /api/chat round-trip.
func (o *Ollama) Chat(ctx context.Context, msgs []Message, tools []Tool) (Result, error) {
	body := map[string]any{
		"model":    o.Model,
		"messages": toOllamaMessages(msgs),
		"stream":   false,
	}
	if len(tools) > 0 {
		body["tools"] = toOllamaTools(tools)
	}
	if opts := o.options(); len(opts) > 0 {
		body["options"] = opts
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.BaseURL+"/api/chat", bytes.NewReader(raw))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.HTTP.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("ollama chat (is `ollama serve` running at %s?): %w", o.BaseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	var out struct {
		Message         olMessage `json:"message"`
		PromptEvalCount int       `json:"prompt_eval_count"`
		EvalCount       int       `json:"eval_count"`
		Error           string    `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Result{}, fmt.Errorf("decode ollama response: %w", err)
	}
	if out.Error != "" {
		return Result{}, fmt.Errorf("ollama error: %s", out.Error)
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("ollama returned %s", resp.Status)
	}
	return Result{
		Message:          fromOllamaMessage(out.Message),
		PromptTokens:     out.PromptEvalCount,
		CompletionTokens: out.EvalCount,
	}, nil
}

// LocalModel describes one locally-pulled Ollama model, including the capability
// list the server reports. Tool calling is a per-model property — a model without
// it cannot drive either arm of the study — so it is worth checking before a run
// rather than discovering it as a mid-run loop failure.
type LocalModel struct {
	Name          string
	ParameterSize string
	Capabilities  []string
}

// HasTools reports whether the model advertises tool-calling support.
func (m LocalModel) HasTools() bool {
	for _, c := range m.Capabilities {
		if c == "tools" {
			return true
		}
	}
	return false
}

// LocalModels returns the locally-pulled models with their capabilities
// (GET /api/tags).
func (o *Ollama) LocalModels(ctx context.Context) ([]LocalModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.BaseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := o.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama tags (is `ollama serve` running at %s?): %w", o.BaseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama tags returned %s", resp.Status)
	}
	var body struct {
		Models []struct {
			Name         string   `json:"name"`
			Capabilities []string `json:"capabilities"`
			Details      struct {
				ParameterSize string `json:"parameter_size"`
			} `json:"details"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]LocalModel, 0, len(body.Models))
	for _, m := range body.Models {
		out = append(out, LocalModel{Name: m.Name, ParameterSize: m.Details.ParameterSize, Capabilities: m.Capabilities})
	}
	return out, nil
}

// ListModels returns locally-pulled model names (GET /api/tags).
func (o *Ollama) ListModels(ctx context.Context) ([]string, error) {
	models, err := o.LocalModels(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(models))
	for _, m := range models {
		names = append(names, m.Name)
	}
	return names, nil
}

func toOllamaMessages(msgs []Message) []olMessage {
	out := make([]olMessage, len(msgs))
	for i, m := range msgs {
		om := olMessage{Role: m.Role, Content: m.Content}
		for _, tc := range m.ToolCalls {
			var otc olToolCall
			otc.Function.Name = tc.Name
			otc.Function.Arguments = tc.Arguments // neutral raw JSON object passes straight through
			om.ToolCalls = append(om.ToolCalls, otc)
		}
		out[i] = om
	}
	return out
}

func toOllamaTools(tools []Tool) []map[string]any {
	out := make([]map[string]any, len(tools))
	for i, t := range tools {
		out[i] = map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.Parameters,
			},
		}
	}
	return out
}

func fromOllamaMessage(m olMessage) Message {
	out := Message{Role: m.Role, Content: m.Content}
	for _, tc := range m.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	return out
}
