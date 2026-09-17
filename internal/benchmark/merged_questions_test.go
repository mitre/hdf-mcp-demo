package benchmark

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/mitre/hdf-mcp-demo/internal/agent"
	"github.com/mitre/hdf-mcp-demo/internal/mcpclient"
	"github.com/mitre/hdf-mcp-demo/internal/truth"
)

// TestRun_AdHocSkipsMergedQuestions: with the ad-hoc view requested, a
// merged-document question gets no ad-hoc arm — merging is a pipeline step, not
// something the agent can do on demand (ADR-0016 §7) — while every other
// question still does. Gated on HDF_BIN because staging runs the real binary.
func TestRun_AdHocSkipsMergedQuestions(t *testing.T) {
	bin := os.Getenv("HDF_BIN")
	if bin == "" {
		p, err := exec.LookPath("hdf")
		if err != nil {
			t.Skip("set HDF_BIN=/path/to/hdf (or put hdf on PATH) to run the ad-hoc skip test")
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

	results, err := Run(context.Background(), stubInstrument{}, sess, bin, "../../fixtures", root, Bank(),
		Options{MaxIters: 2, Concurrency: 3, AdHoc: true})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	merged := map[string]bool{}
	for _, q := range Bank() {
		merged[q.ID] = q.Merged != ""
	}
	seenMerged, seenPlain := 0, 0
	for _, r := range results {
		switch {
		case merged[r.ID] && r.HDFAdHoc != nil:
			t.Errorf("%s: merged question must have no ad-hoc arm, got %+v", r.ID, r.HDFAdHoc)
		case !merged[r.ID] && r.HDFAdHoc == nil:
			t.Errorf("%s: non-merged question must have an ad-hoc arm when AdHoc is set", r.ID)
		case merged[r.ID]:
			seenMerged++
		default:
			seenPlain++
		}
	}
	if seenMerged != 3 || seenPlain != len(Bank())-3 {
		t.Errorf("saw %d merged / %d plain questions, want 3 / %d", seenMerged, seenPlain, len(Bank())-3)
	}
}

// TestRender_AdHocCellBlankForMergedQuestions: a question with no ad-hoc arm
// renders "—" in the ad-hoc column of both the text and markdown reports, and
// contributes nothing to the ad-hoc total — only the arms that ran are summed.
func TestRender_AdHocCellBlankForMergedQuestions(t *testing.T) {
	arm := func(a Arm, prompt int) ArmResult {
		return ArmResult{Arm: a, Verdict: Correct, Scored: true, Samples: 1, Correct: 1, Agreement: 1,
			Cost: agent.Result{PromptTokens: prompt, CompletionTokens: 50}}
	}
	adhoc := arm(ArmHDF, 300) // 350 tokens
	plain := QuestionResult{ID: "plain-question", Ask: "?", Class: truth.ClassA,
		RawView: truth.Answered("1"), HDFView: truth.Answered("1"),
		Raw: arm(ArmRaw, 100), HDF: arm(ArmHDF, 100), HDFAdHoc: &adhoc}
	merged := QuestionResult{ID: "merged-question", Ask: "?", Class: truth.ClassA,
		RawView: truth.Answered("1"), HDFView: truth.Answered("1"),
		Raw: arm(ArmRaw, 100), HDF: arm(ArmHDF, 100), HDFAdHoc: nil}
	results := []QuestionResult{plain, merged}

	text := Render("stub", results, true, nil)
	md := RenderMarkdown(RunMeta{Models: []string{"stub"}}, []ModelRun{{Model: "stub", Results: results}}, true, nil)

	for _, out := range []struct{ name, s string }{{"text", text}, {"markdown", md}} {
		var mergedRow, plainRow string
		for _, line := range strings.Split(out.s, "\n") {
			if strings.Contains(line, "merged-question") && strings.Contains(line, "objective") {
				mergedRow = line
			}
			if strings.Contains(line, "plain-question") && strings.Contains(line, "objective") {
				plainRow = line
			}
		}
		if mergedRow == "" || plainRow == "" {
			t.Fatalf("%s: could not find both question rows:\n%s", out.name, out.s)
		}
		if !strings.HasSuffix(strings.TrimSpace(mergedRow), "—") && !strings.HasSuffix(strings.TrimSpace(mergedRow), "— |") {
			t.Errorf("%s: merged row must end with the blank ad-hoc cell —, got %q", out.name, mergedRow)
		}
		if !strings.Contains(plainRow, "350") {
			t.Errorf("%s: plain row must carry its ad-hoc tokens 350, got %q", out.name, plainRow)
		}
		// The ad-hoc total is exactly the one arm that ran: 350, not 350 + anything.
		if !strings.Contains(out.s, "350") || strings.Contains(out.s, "700") {
			t.Errorf("%s: ad-hoc total must be 350 (the merged question contributes nothing):\n%s", out.name, out.s)
		}
	}
	if !strings.Contains(text, "hdf-mcp arm (ad-hoc convert) .      350") {
		t.Errorf("text ad-hoc total line wrong:\n%s", text)
	}
}

// TestMergedGroups pins the staging guard: questions sharing a merged document
// must declare the same sources, one merge runs per distinct name, and the
// shipped bank satisfies both.
func TestMergedGroups(t *testing.T) {
	src := func(names ...string) []Source {
		out := make([]Source, 0, len(names))
		for _, n := range names {
			out = append(out, Source{Fixture: n + ".json", From: n, HDFName: n + ".hdf.json"})
		}
		return out
	}
	consistent := []Question{
		{ID: "a", Merged: "m.hdf.json", Sources: src("gosec", "zap")},
		{ID: "b", Merged: "m.hdf.json", Sources: src("gosec", "zap")},
		{ID: "c", Sources: src("grype")},
		{ID: "d", Merged: "n.hdf.json", Sources: src("grype", "zap")},
	}
	groups, err := mergedGroups(consistent)
	if err != nil {
		t.Fatalf("consistent bank rejected: %v", err)
	}
	if len(groups) != 2 || groups[0].ID != "a" || groups[1].ID != "d" {
		t.Errorf("groups = %v, want the first question per merged name in bank order [a d]", ids(groups))
	}

	conflicting := []Question{
		{ID: "a", Merged: "m.hdf.json", Sources: src("gosec", "zap")},
		{ID: "b", Merged: "m.hdf.json", Sources: src("zap", "gosec")}, // same files, different order → different document
	}
	if _, err := mergedGroups(conflicting); err == nil || !strings.Contains(err.Error(), "m.hdf.json") {
		t.Errorf("conflicting sources must be refused naming the merged document; got %v", err)
	}

	if _, err := mergedGroups(Bank()); err != nil {
		t.Errorf("the shipped bank must be consistent: %v", err)
	}
}

func ids(qs []Question) []string {
	out := make([]string, len(qs))
	for i, q := range qs {
		out[i] = q.ID
	}
	return out
}
