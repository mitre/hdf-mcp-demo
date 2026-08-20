package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mitre/hdf-mcp-demo/internal/agent"
	"github.com/mitre/hdf-mcp-demo/internal/instrument"
	"github.com/mitre/hdf-mcp-demo/internal/truth"
)

// transcript is the on-disk diagnostic record of one arm sample: everything the
// model was given (system prompt, user prompt, tool definitions), everything it
// said and called (messages, with raw tool-call arguments), and how the sample
// was judged. It exists so a zero-scoring arm can be diagnosed from evidence —
// which failing tool call, which malformed argument, which unparsed answer —
// rather than inference.
type transcript struct {
	Model            string       `json:"model"`
	Label            string       `json:"label"`
	Arm              string       `json:"arm"`
	Sample           int          `json:"sample"`
	System           string       `json:"system"`
	Prompt           string       `json:"prompt"`
	Tools            []tTool      `json:"tools"`
	Messages         []tMessage   `json:"messages"`
	Answer           string       `json:"answer"`
	Verdict          string       `json:"verdict"`
	Error            string       `json:"error,omitempty"`
	PromptTokens     int          `json:"promptTokens"`
	CompletionTokens int          `json:"completionTokens"`
	ToolCallCount    int          `json:"toolCallCount"`
	Iterations       int          `json:"iterations"`
	ElapsedSeconds   float64      `json:"elapsedSeconds"`
	Key              truth.Answer `json:"-"`
}

type tTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type tMessage struct {
	Role             string      `json:"role"`
	Content          string      `json:"content,omitempty"`
	ReasoningContent string      `json:"reasoningContent,omitempty"`
	ToolCalls        []tToolCall `json:"toolCalls,omitempty"`
	ToolCallID       string      `json:"toolCallId,omitempty"`
}

type tToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// writeTranscript records one sample under <dir>/<model>/<label>.s<N>.json.
func writeTranscript(dir, model, label string, sample int, arm Arm, system, prompt string,
	tools []instrument.Tool, res agent.Result, v Verdict, runErr error) error {

	tr := transcript{
		Model: model, Label: label, Arm: string(arm), Sample: sample,
		System: system, Prompt: prompt,
		Answer: res.Answer, Verdict: string(v),
		PromptTokens: res.PromptTokens, CompletionTokens: res.CompletionTokens,
		ToolCallCount: res.ToolCalls, Iterations: res.Iterations,
		ElapsedSeconds: res.Elapsed.Seconds(),
	}
	if runErr != nil {
		tr.Error = runErr.Error()
	}
	for _, t := range tools {
		tr.Tools = append(tr.Tools, tTool{Name: t.Name, Description: t.Description, Parameters: t.Parameters})
	}
	for _, m := range res.Transcript {
		tm := tMessage{Role: m.Role, Content: m.Content, ReasoningContent: m.ReasoningContent, ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			tm.ToolCalls = append(tm.ToolCalls, tToolCall{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments})
		}
		tr.Messages = append(tr.Messages, tm)
	}

	sub := filepath.Join(dir, sanitizeModelDir(model))
	if err := os.MkdirAll(sub, 0o750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(tr, "", "  ")
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%s.s%d.json", label, sample)
	return os.WriteFile(filepath.Join(sub, name), b, 0o600)
}

// sanitizeModelDir maps a model name to a filesystem-safe directory name
// (ollama tags carry ':', gateway names may carry '/').
func sanitizeModelDir(model string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ':', '/', '\\':
			return '_'
		}
		return r
	}, model)
}
