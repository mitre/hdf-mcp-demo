package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
// constant token counts, so repeat aggregation is deterministic. name, when set,
// overrides the default so a test can hand the harness a path-hostile model name.
type fixedInstrument struct{ answer, name string }

func (f fixedInstrument) Name() string {
	if f.name != "" {
		return f.name
	}
	return "fixed"
}
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

// asArmCost is the identity on ArmCost. Go has no implicit conversion between
// named struct types, so passing ArmResult.Cost through it fails to COMPILE if
// that field is ever widened back to agent.Result or anything else.
func asArmCost(c ArmCost) ArmCost { return c }

// TestRunArm_MeanCostHasNoSampleFields pins the shape of the aggregate, not just
// its numbers. A mean over samples has no answer, no chain-of-thought and no
// transcript — those belong to ONE run — so ArmResult.Cost must be a
// benchmark-owned ArmCost carrying exactly the five mean fields the renderers
// read, and must not be (or embed) agent.Result, where a reader has to know which
// half of the struct is real. agent.Result stays what a single sample carries.
func TestRunArm_MeanCostHasNoSampleFields(t *testing.T) {
	tb := agent.NewRawFileToolBox(t.TempDir())
	got := runArm(context.Background(), fixedInstrument{answer: "ANSWER: 3"}, tb, ArmHDF,
		Options{Repeat: 3}, "q?", truth.Answered("3"), KindCount, "q.hdf")

	cost := asArmCost(got.Cost) // will not compile unless Cost is exactly ArmCost
	if cost.TotalTokens() != 110 {
		t.Errorf("mean total tokens = %d, want 110 (100 prompt + 10 completion, constant across 3 samples)", cost.TotalTokens())
	}
	if cost.PromptTokens != 100 || cost.CompletionTokens != 10 {
		t.Errorf("mean prompt/completion = %d/%d, want 100/10", cost.PromptTokens, cost.CompletionTokens)
	}

	// The whole point: the mean type has the five mean fields and nothing else.
	// A per-sample field surviving into the aggregate is what this pins against.
	typ := reflect.TypeOf(ArmCost{})
	want := map[string]bool{"PromptTokens": true, "CompletionTokens": true, "ToolCalls": true, "Iterations": true, "Elapsed": true}
	if typ.NumField() != len(want) {
		t.Errorf("ArmCost has %d fields, want exactly %d", typ.NumField(), len(want))
	}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !want[f.Name] {
			t.Errorf("ArmCost carries %q, which has no meaning as a mean over samples", f.Name)
		}
		if f.Anonymous {
			t.Errorf("ArmCost embeds %q; the mean must have FEWER fields than a sample, not inherit them", f.Name)
		}
	}
	for _, name := range []string{"Answer", "ReasoningTokens", "Transcript"} {
		if _, ok := typ.FieldByName(name); ok {
			t.Errorf("ArmCost exposes %q, a per-sample field that is meaningless as a mean", name)
		}
	}

	// agent.Result is unchanged: it is still what one sample and one transcript
	// carry, and it still has the per-sample fields ArmCost must not.
	sample := reflect.TypeOf(agent.Result{})
	for _, name := range []string{"Answer", "ReasoningTokens", "Transcript"} {
		if _, ok := sample.FieldByName(name); !ok {
			t.Errorf("agent.Result lost %q; this card must not change the per-sample type", name)
		}
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
	// A path-hostile model name pins that the transcript directory goes through
	// SafeModelName — the caller, not only the helper, is what must not drift.
	got := runArm(context.Background(), fixedInstrument{answer: "ANSWER: 3", name: `org\fixed:1b`}, tb, ArmHDF,
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
		p := filepath.Join(dir, "org_fixed_1b", "grype-match-count.hdf.s"+string(rune('0'+sample))+".json")
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("transcript %s: %v", p, err)
		}
		if err := json.Unmarshal(b, &tr); err != nil {
			t.Fatalf("transcript is not valid JSON: %v", err)
		}
		if tr.Model != `org\fixed:1b` || tr.Arm != string(ArmHDF) || tr.Verdict != string(Correct) || tr.Sample != sample {
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

// TestSafeModelName pins the one mapping from a model name to a filesystem-safe
// name that every artifact named after a model must share — transcript
// directories and provenance files alike. Ollama tags carry ':', gateway names
// may carry '/', and a Windows-style name may carry '\\'.
func TestSafeModelName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"ollama:gpt-oss:20b", "ollama_gpt-oss_20b"},
		{"open/ai:x", "open_ai_x"},
		{`org\model:7b`, "org_model_7b"},
		{"plain-name", "plain-name"},
	} {
		if got := SafeModelName(tc.in); got != tc.want {
			t.Errorf("SafeModelName(%q) = %q, want %q", tc.in, got, tc.want)
		}
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
