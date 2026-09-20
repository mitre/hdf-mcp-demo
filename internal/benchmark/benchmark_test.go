package benchmark

import (
	"context"
	"encoding/json"
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
		Options{Repeat: 3}, "q?", truth.Answered("3"), KindCount, "q.hdf")

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

// With Options.TranscriptDir set, every sample of every arm writes a full
// transcript — the system prompt, the user prompt, the tool definitions, every
// message, and the graded outcome — so an abstaining or failing run can be
// diagnosed from evidence instead of inference (uqhe.15's demand). Failures
// especially must be captured: a transcript facility that only records success
// would be useless for the bug it exists to expose.
func TestRunArm_TranscriptWritten(t *testing.T) {
	dir := t.TempDir()
	tb := agent.NewRawFileToolBox(t.TempDir())
	opts := Options{Repeat: 2, TranscriptDir: dir}
	got := runArm(context.Background(), fixedInstrument{answer: "ANSWER: 3"}, tb, ArmHDF,
		opts, "q?", truth.Answered("3"), KindCount, "grype-match-count.hdf")
	if got.Err != "" {
		t.Fatalf("unexpected arm error: %s", got.Err)
	}

	var tr struct {
		Model    string `json:"model"`
		Label    string `json:"label"`
		Arm      string `json:"arm"`
		Sample   int    `json:"sample"`
		System   string `json:"system"`
		Prompt   string `json:"prompt"`
		Verdict  string `json:"verdict"`
		Answer   string `json:"answer"`
		Error    string `json:"error"`
		Tools    []any  `json:"tools"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	for sample := 0; sample < 2; sample++ {
		p := filepath.Join(dir, "fixed", "grype-match-count.hdf.s"+string(rune('0'+sample))+".json")
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("transcript %s: %v", p, err)
		}
		if err := json.Unmarshal(b, &tr); err != nil {
			t.Fatalf("transcript is not valid JSON: %v", err)
		}
		if tr.Model != "fixed" || tr.Arm != string(ArmHDF) || tr.Verdict != string(Correct) || tr.Sample != sample {
			t.Errorf("transcript meta = %+v", tr)
		}
		if tr.Prompt != "q?" || tr.System == "" {
			t.Errorf("transcript missing prompts: %+v", tr)
		}
		if len(tr.Tools) == 0 {
			t.Error("transcript missing tool definitions")
		}
		// system + user + final assistant turn, at minimum.
		if len(tr.Messages) < 3 || tr.Messages[len(tr.Messages)-1].Role != "assistant" {
			t.Errorf("transcript messages incomplete: %d roles", len(tr.Messages))
		}
	}

	// A failing arm still writes its transcript, with the error recorded.
	got = runArm(context.Background(), failingInstrument{}, tb, ArmRaw,
		Options{TranscriptDir: dir}, "q?", truth.Answered("3"), KindCount, "grype-match-count.raw")
	if got.Verdict != Failed {
		t.Fatalf("verdict = %s, want failed", got.Verdict)
	}
	b, err := os.ReadFile(filepath.Join(dir, "failing", "grype-match-count.raw.s0.json"))
	if err != nil {
		t.Fatalf("failing-arm transcript: %v", err)
	}
	if err := json.Unmarshal(b, &tr); err != nil {
		t.Fatal(err)
	}
	if tr.Error == "" || tr.Verdict != string(Failed) {
		t.Errorf("failing transcript should carry the error: %+v", tr)
	}
}

// A model name with path-hostile characters (ollama's "gpt-oss:20b") must map to
// a usable directory name.
func TestTranscriptModelDir(t *testing.T) {
	if got := sanitizeModelDir("ollama:gpt-oss:20b"); got != "ollama_gpt-oss_20b" {
		t.Errorf("sanitizeModelDir = %q", got)
	}
	if got := sanitizeModelDir("open/ai:x"); got != "open_ai_x" {
		t.Errorf("sanitizeModelDir = %q", got)
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
		"How many findings? The scan file is named scan.json.", truth.Answered("3"), KindCount, "q.raw")

	if got.Verdict != Failed {
		t.Errorf("verdict = %s, want Failed", got.Verdict)
	}
	if got.Err == "" {
		t.Error("expected the error to be captured on the ArmResult")
	}
}

// TestDefaultMaxItersIsShared guards the drift the AC review caught: the CLI
// flag default and the library fallback were set independently, so raising one
// silently left a library caller on the old cap.
func TestDefaultMaxItersIsShared(t *testing.T) {
	if got := (Options{}).maxIters(); got != DefaultMaxIters {
		t.Errorf("unset Options must use DefaultMaxIters, got %d want %d", got, DefaultMaxIters)
	}
	if DefaultMaxIters < 10 {
		t.Errorf("DefaultMaxIters=%d is below the range the observed iteration distribution justified", DefaultMaxIters)
	}
	if got := (Options{MaxIters: 3}).maxIters(); got != 3 {
		t.Errorf("explicit MaxIters must win, got %d", got)
	}
}
