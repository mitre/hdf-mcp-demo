// Package agent is the two-arm study loop: it drives a model (any Instrument)
// through a tool-call → execute → feed-result-back cycle until it produces a
// final answer, tallying REAL tokens (summed over every turn), tool calls, and
// wall-clock. The only difference between the two arms is the ToolBox handed in:
//
//   - the raw-file arm gives the model a generic read_file tool (it must ingest
//     whole scan files);
//   - the HDF arm gives the model the HDF MCP tools (bounded, normalized).
//
// Everything else — model, prompts, token accounting — is identical, so the
// measured delta is attributable to the tool surface.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mitre/hdf-mcp-demo/internal/instrument"
)

// ToolBox is the set of tools an arm exposes to the model, and the executor for
// calls the model makes.
type ToolBox interface {
	Definitions() []instrument.Tool
	Execute(ctx context.Context, name string, args json.RawMessage) (string, error)
}

// Result is one question answered by one arm.
type Result struct {
	Answer           string
	PromptTokens     int // summed over all turns
	CompletionTokens int // summed over all turns
	ToolCalls        int
	Iterations       int
	Elapsed          time.Duration
	ReasoningTokens  string // last turn's reasoning_content, if any (observability)
}

// TotalTokens is the end-to-end token cost of answering (prompt + completion,
// all turns) — the honest measure the study compares across arms.
func (r Result) TotalTokens() int { return r.PromptTokens + r.CompletionTokens }

// Run executes the agent loop for one question. maxIters caps tool round-trips so
// a confused model cannot loop forever.
func Run(ctx context.Context, inst instrument.Instrument, tb ToolBox, systemPrompt, userPrompt string, maxIters int) (Result, error) {
	tools := tb.Definitions()
	var msgs []instrument.Message
	if systemPrompt != "" {
		msgs = append(msgs, instrument.Message{Role: "system", Content: systemPrompt})
	}
	msgs = append(msgs, instrument.Message{Role: "user", Content: userPrompt})

	start := time.Now()
	var res Result
	for res.Iterations = 0; res.Iterations < maxIters; res.Iterations++ {
		out, err := inst.Chat(ctx, msgs, tools)
		if err != nil {
			res.Elapsed = time.Since(start)
			return res, err
		}
		res.PromptTokens += out.PromptTokens
		res.CompletionTokens += out.CompletionTokens
		res.ReasoningTokens = out.Message.ReasoningContent
		msgs = append(msgs, out.Message)

		if len(out.Message.ToolCalls) == 0 {
			res.Answer = out.Message.Content
			res.Elapsed = time.Since(start)
			return res, nil
		}
		for _, tc := range out.Message.ToolCalls {
			res.ToolCalls++
			toolOut, terr := tb.Execute(ctx, tc.Name, tc.Arguments)
			if terr != nil {
				// Feed the error back so the model can recover rather than aborting.
				toolOut = "ERROR: " + terr.Error()
			}
			msgs = append(msgs, instrument.Message{Role: "tool", ToolCallID: tc.ID, Content: toolOut})
		}
	}
	res.Elapsed = time.Since(start)
	return res, fmt.Errorf("reached max iterations (%d) without a final answer", maxIters)
}
