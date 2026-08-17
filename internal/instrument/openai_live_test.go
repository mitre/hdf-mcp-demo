package instrument

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestOpenAI_LiveEndpoint hits a REAL OpenAI-compatible endpoint when configured
// via the environment — a connectivity/usage smoke test for e.g. an internal
// LiteLLM proxy. It embeds no token; the key is read from OPENAI_API_KEY at run
// time. Skipped unless OPENAI_BASE_URL and OPENAI_MODEL are set.
//
// Run it in YOUR OWN shell (so the token never enters an agent session):
//
//	export OPENAI_BASE_URL=http://<your-litellm-host>/v1
//	export OPENAI_MODEL=<a model your endpoint serves>
//	export OPENAI_API_KEY=<your key>       # optional if the endpoint is unauthenticated
//	go test ./internal/instrument/ -run Live -v
func TestOpenAI_LiveEndpoint(t *testing.T) {
	base := os.Getenv("OPENAI_BASE_URL")
	model := os.Getenv("OPENAI_MODEL")
	if base == "" || model == "" {
		t.Skip("set OPENAI_BASE_URL and OPENAI_MODEL (and OPENAI_API_KEY if required) to run the live check")
	}
	c := NewOpenAI(base, model, os.Getenv("OPENAI_API_KEY"))
	c.MaxTokens = 256 // enough for a reasoning model to finish; keeps the smoke test bounded

	// Generous: a cold model load on a shared gateway can take a couple of minutes.
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	res, err := c.Chat(ctx, []Message{{Role: "user", Content: "Reply with exactly the word: pong"}}, nil)
	if err != nil {
		t.Fatalf("live chat to %s failed: %v", base, err)
	}
	reply := strings.TrimSpace(res.Message.Content)
	if reply == "" {
		t.Error("expected a non-empty reply")
	}
	t.Logf("endpoint=%s model=%s reply=%q prompt_tokens=%d completion_tokens=%d",
		base, model, reply, res.PromptTokens, res.CompletionTokens)
	if res.PromptTokens == 0 && res.CompletionTokens == 0 {
		t.Log("note: endpoint returned no usage counts — connectivity works, but the two-arm study needs usage; check the proxy's usage passthrough")
	}
}

// TestOpenAI_LiveToolRoundTrip exercises the exact multi-turn tool protocol the
// two-arm loop depends on, through OUR adapter against the REAL endpoint: send a
// tool, parse the model's tool_calls, send the assistant call + a tool RESULT
// back, and get a final answer. This is the piece a picky proxy/vLLM most often
// trips on. Prefer a fast model (OPENAI_MODEL=gemma-4). Same env gating as above.
func TestOpenAI_LiveToolRoundTrip(t *testing.T) {
	base := os.Getenv("OPENAI_BASE_URL")
	model := os.Getenv("OPENAI_MODEL")
	if base == "" || model == "" {
		t.Skip("set OPENAI_BASE_URL and OPENAI_MODEL (and OPENAI_API_KEY if required) to run the live check")
	}
	c := NewOpenAI(base, model, os.Getenv("OPENAI_API_KEY"))
	c.MaxTokens = 512

	tools := []Tool{{
		Name:        "get_failed_count",
		Description: "Return the number of failed findings in a named scan.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"scan": map[string]any{"type": "string", "description": "the scan name, e.g. gosec"}},
			"required":   []string{"scan"},
		},
	}}
	convo := []Message{
		{Role: "system", Content: "Use the provided tool to get data, then answer the user in one short sentence."},
		{Role: "user", Content: "How many failed findings are in the gosec scan?"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	// Turn 1: expect a tool call.
	r1, err := c.Chat(ctx, convo, tools)
	if err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	if len(r1.Message.ToolCalls) == 0 {
		t.Fatalf("turn 1 returned no tool_calls (content=%q) — model/endpoint did not call the tool", r1.Message.Content)
	}
	tc := r1.Message.ToolCalls[0]
	t.Logf("turn 1: tool=%s args=%s (prompt=%d completion=%d)", tc.Name, string(tc.Arguments), r1.PromptTokens, r1.CompletionTokens)

	// Turn 2: feed the assistant's call + a tool RESULT back, expect a final answer.
	convo = append(convo,
		r1.Message,
		Message{Role: "tool", ToolCallID: tc.ID, Content: `{"failed": 7}`},
	)
	r2, err := c.Chat(ctx, convo, tools)
	if err != nil {
		t.Fatalf("turn 2 (tool result round-trip): %v", err)
	}
	if strings.TrimSpace(r2.Message.Content) == "" {
		t.Fatalf("turn 2 returned empty content after the tool result (finish path broken)")
	}
	t.Logf("turn 2 final answer: %q (prompt=%d completion=%d)", strings.TrimSpace(r2.Message.Content), r2.PromptTokens, r2.CompletionTokens)
	if !strings.Contains(r2.Message.Content, "7") {
		t.Logf("note: final answer did not echo the tool result (7) verbatim — acceptable, but worth a glance: %q", r2.Message.Content)
	}
}
