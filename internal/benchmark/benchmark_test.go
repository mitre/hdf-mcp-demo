package benchmark

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mitre/hdf-mcp-demo/internal/agent"
	"github.com/mitre/hdf-mcp-demo/internal/instrument"
	"github.com/mitre/hdf-mcp-demo/internal/truth"
)

// failingInstrument always errors on Chat, standing in for an over-context /
// gateway failure so the resilience path can be tested offline (the raw arm needs
// no MCP session).
type failingInstrument struct{}

func (failingInstrument) Name() string { return "failing" }
func (failingInstrument) Chat(_ context.Context, _ []instrument.Message, _ []instrument.Tool) (instrument.Result, error) {
	return instrument.Result{PromptTokens: 5}, errors.New("simulated context-length error")
}

// fixedInstrument answers immediately (no tool call) with a constant reply and
// constant token counts, so repeat aggregation is deterministic.
type fixedInstrument struct{ answer string }

func (fixedInstrument) Name() string { return "fixed" }
func (f fixedInstrument) Chat(_ context.Context, _ []instrument.Message, _ []instrument.Tool) (instrument.Result, error) {
	return instrument.Result{
		Message:      instrument.Message{Role: "assistant", Content: f.answer},
		PromptTokens: 100, CompletionTokens: 10,
	}, nil
}

// runArm with -repeat aggregates: N samples, correct count, agreement, mean cost,
// zero stddev for constant tokens.
func TestRunArm_Repeat(t *testing.T) {
	tb := agent.NewRawFileToolBox(t.TempDir())
	got := runArm(context.Background(), fixedInstrument{answer: "ANSWER: 3"}, tb, ArmHDF,
		Options{Repeat: 3}, "q?", truth.Answered("3"), KindCount)

	if got.Samples != 3 || got.Correct != 3 {
		t.Errorf("samples/correct = %d/%d, want 3/3", got.Samples, got.Correct)
	}
	if got.Verdict != Correct {
		t.Errorf("modal verdict = %s, want correct", got.Verdict)
	}
	if got.Agreement != 1 {
		t.Errorf("agreement = %v, want 1", got.Agreement)
	}
	if got.TokenStdDev != 0 {
		t.Errorf("token stddev = %v, want 0 (constant tokens)", got.TokenStdDev)
	}
	if got.Cost.TotalTokens() != 110 {
		t.Errorf("mean total tokens = %d, want 110", got.Cost.TotalTokens())
	}
}

// A failed arm is recorded as a Failed verdict with the error captured — never
// propagated as a fatal error.
func TestRunArm_FailureIsRecorded(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "scan.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	tb := agent.NewRawFileToolBox(dir)

	got := runArm(context.Background(), failingInstrument{}, tb, ArmRaw, Options{MaxIters: 3},
		"How many findings? The scan file is named scan.json.", truth.Answered("3"), KindCount)

	if got.Verdict != Failed {
		t.Errorf("verdict = %s, want Failed", got.Verdict)
	}
	if got.Err == "" {
		t.Error("expected the error to be captured on the ArmResult")
	}
}
