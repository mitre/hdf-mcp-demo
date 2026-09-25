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
	"errors"
	"net"
	"net/http"
	"time"
)

// DefaultRequestTimeout caps ONE HTTP round-trip to a model endpoint. It is a
// deliberate cap, not an absence of one: without it a hung connection would stall
// an arm until the run-wide timeout fired. Five minutes is generous for a chat
// completion and was the value both instruments hard-coded before it became a
// flag (-request-timeout), so exposing the knob leaves existing runs unchanged.
//
// It interacts with the retry loop: a response that outlives the cap fails as a
// transport error, which the OpenAI adapter treats as retryable, so the worst
// case inside one arm is (1 + MaxRetries) x this value. Raising it for a slow
// reasoning model therefore has to be a visible decision, which is why the value
// is recorded in the report when it differs from this default.
const DefaultRequestTimeout = 5 * time.Minute

// newHTTPClient builds the per-request-capped client both instruments use.
// A non-positive timeout means "unspecified" and takes the default — never "no
// cap", which is the one value the study must not silently acquire.
func newHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = DefaultRequestTimeout
	}
	return &http.Client{Timeout: timeout}
}

// setRequestTimeout re-caps an existing client in place, so whoever holds it
// (including a caller that swapped in its own) keeps the same client value.
func setRequestTimeout(c *http.Client, d time.Duration) {
	if d <= 0 {
		d = DefaultRequestTimeout
	}
	c.Timeout = d
}

// timedOut reports whether err is a client-side timeout rather than some other
// transport failure. http.Client.Timeout surfaces as a *url.Error whose Timeout()
// is true; a dial or response-header deadline reports the same way, and all of
// them mean the same thing to a report reader: nothing came back in time.
func timedOut(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

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
