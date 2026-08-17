package benchmark

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/mitre/hdf-mcp-demo/internal/instrument"
	"github.com/mitre/hdf-mcp-demo/internal/mcpclient"
	"github.com/mitre/hdf-mcp-demo/internal/truth"
)

// stubInstrument is a deterministic, model-free Instrument: on the first turn it
// calls one safe tool (read_file for the raw arm, hdf_query for the HDF arm) using
// the filename named in the prompt; once a tool result is present it emits a fixed
// ANSWER line. It fabricates non-zero token counts so cost accounting is exercised.
// This lets the whole pipeline (convert → classify → two arms → grade → report)
// run end-to-end with no model endpoint.
type stubInstrument struct{}

var jsonNameRE = regexp.MustCompile(`[\w.-]+\.json`)

func (stubInstrument) Name() string { return "stub" }

func (stubInstrument) Chat(_ context.Context, msgs []instrument.Message, tools []instrument.Tool) (instrument.Result, error) {
	haveToolResult := false
	var lastUser string
	for _, m := range msgs {
		if m.Role == "tool" {
			haveToolResult = true
		}
		if m.Role == "user" {
			lastUser = m.Content
		}
	}
	if haveToolResult {
		return instrument.Result{
			Message:      instrument.Message{Role: "assistant", Content: "Based on the data.\nANSWER: 3"},
			PromptTokens: 200, CompletionTokens: 12,
		}, nil
	}

	file := jsonNameRE.FindString(lastUser)
	name, args := chooseTool(tools, file)
	return instrument.Result{
		Message: instrument.Message{Role: "assistant", ToolCalls: []instrument.ToolCall{
			{ID: "call_1", Name: name, Arguments: args},
		}},
		PromptTokens: 400, CompletionTokens: 8,
	}, nil
}

// chooseTool picks a safe tool and builds valid arguments for it.
func chooseTool(tools []instrument.Tool, file string) (string, json.RawMessage) {
	has := map[string]bool{}
	for _, t := range tools {
		has[t.Name] = true
	}
	switch {
	case has["read_file"]:
		return "read_file", mustJSON(map[string]any{"name": file})
	case has["hdf_query"]:
		return "hdf_query", mustJSON(map[string]any{"source": map[string]any{"path": file}, "verbosity": "concise"})
	default:
		return tools[0].Name, mustJSON(map[string]any{"source": map[string]any{"path": file}})
	}
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// TestBenchmark_Pipeline exercises the full graded pipeline with the stub model,
// gated only on the hdf binary (no endpoint). It asserts classes are computed
// from data, both arms report cost, and the report renders.
func TestBenchmark_Pipeline(t *testing.T) {
	bin := os.Getenv("HDF_BIN")
	if bin == "" {
		p, err := exec.LookPath("hdf")
		if err != nil {
			t.Skip("set HDF_BIN=/path/to/hdf (or put hdf on PATH) to run the pipeline integration test")
		}
		bin = p
	}

	root := t.TempDir()
	env := append(os.Environ(), "HDF_MCP_ROOT="+root, "HDF_MCP_ENABLE_WRITES=1")
	sess, err := mcpclient.Connect(context.Background(), bin, env)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = sess.Close() }()

	// Concurrency > 1 exercises the parallel path and the shared-session mutex.
	results, err := Run(context.Background(), stubInstrument{}, sess, "../../fixtures", root, Bank(), Options{MaxIters: 4, Concurrency: 3})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	SortByID(results)

	if len(results) != len(Bank()) {
		t.Fatalf("got %d results, want %d", len(results), len(Bank()))
	}

	// Ground truth is computed from data — pin the headline classes.
	want := map[string]truth.Class{
		"gosec-distinct-rules":  truth.ClassB, // 7 raw vs 3 hdf
		"gosec-total-findings":  truth.ClassB,
		"grype-match-count":     truth.ClassA, // 89 == 89 (no dedup)
		"grype-cve-present":     truth.ClassA,
		"grype-compliance-rate": truth.ClassC, // no native raw pass rate
	}
	for _, r := range results {
		if want[r.ID] != r.Class {
			t.Errorf("%s: class = %s, want %s (raw=%+v hdf=%+v)", r.ID, r.Class, want[r.ID], r.RawView, r.HDFView)
		}
		if r.Raw.Cost.TotalTokens() == 0 || r.HDF.Cost.TotalTokens() == 0 {
			t.Errorf("%s: an arm reported zero tokens", r.ID)
		}
		if r.Raw.Cost.ToolCalls == 0 || r.HDF.Cost.ToolCalls == 0 {
			t.Errorf("%s: an arm made no tool call", r.ID)
		}
	}

	out := Render("stub", results, false)
	for _, must := range []string{"accuracy by class", "token cost", "notes / limitations", "hdf-mcp arm (pipeline)"} {
		if !strings.Contains(out, must) {
			t.Errorf("report missing %q", must)
		}
	}
	t.Logf("\n%s", out)
}
