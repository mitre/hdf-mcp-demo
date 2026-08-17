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
