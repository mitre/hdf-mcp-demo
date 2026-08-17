// Package instrument is the model-agnostic seam for the Phase-3 study. An
// Instrument runs one chat turn (with optional tool/function calling) and returns
// the assistant message plus REAL token accounting from the provider — the
// end-to-end token measurement the study needs, never an estimate.
//
// The study depends only on this interface, so it is not tied to any one
// provider. Two implementations ship:
//
//   - Ollama (native /api/chat) — a fully local, zero-charge instrument.
//   - OpenAI-compatible (/v1/chat/completions) — the de-facto standard spoken by
//     LiteLLM, OpenAI, Azure OpenAI, vLLM, and Ollama's own /v1 endpoint. The
//     endpoint, model, and API key are pure configuration, so pointing the study
//     at (say) an internal LiteLLM proxy is a base-URL value, not a code change.
//     The API key is supplied by the caller (read from the environment at run
//     time); it is never hard-coded, logged, or committed.
package instrument

import (
	"context"
	"encoding/json"
)

// Message is one chat message. Role is system|user|assistant|tool.
type Message struct {
	Role             string     // system|user|assistant|tool
	Content          string     // text content (assistant answer, tool output, or user prompt)
	ReasoningContent string     // reasoning models' chain-of-thought, when the provider exposes it (observability only)
	ToolCalls        []ToolCall // assistant turn: functions the model wants called
	ToolCallID       string     // tool turn: which call this result answers (OpenAI-style; ignored by Ollama)
}

// ToolCall is a function invocation the model requested. Arguments is the raw
// JSON object of the arguments (normalized across providers).
type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

// Tool advertises a callable function via a JSON-Schema parameter object.
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// Result is one assistant turn plus its real token accounting.
type Result struct {
	Message          Message
	PromptTokens     int
	CompletionTokens int
}

// Instrument is a chat model the study drives. Implementations must return the
// provider's real token counts on Result.
type Instrument interface {
	// Name identifies the instrument for the report (e.g. "ollama:qwen2.5:7b").
	Name() string
	// Chat performs one turn. tools may be nil.
	Chat(ctx context.Context, msgs []Message, tools []Tool) (Result, error)
}
