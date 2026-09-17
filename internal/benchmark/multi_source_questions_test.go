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

// TestRun_AdHocArmRunsForEveryQuestion: with the ad-hoc view requested, every
// question — the multi-source ones included, since the HDF arm reads the
// per-scanner converted documents — gets an ad-hoc arm. Gated on HDF_BIN
// because staging runs the real binary.
func TestRun_AdHocArmRunsForEveryQuestion(t *testing.T) {
	bin := os.Getenv("HDF_BIN")
	if bin == "" {
		p, err := exec.LookPath("hdf")
		if err != nil {
			t.Skip("set HDF_BIN=/path/to/hdf (or put hdf on PATH) to run the ad-hoc arm test")
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
	if len(results) != len(Bank()) {
		t.Fatalf("got %d results for %d questions", len(results), len(Bank()))
	}
	for _, r := range results {
		if r.HDFAdHoc == nil {
			t.Errorf("%s: every question must have an ad-hoc arm when AdHoc is set", r.ID)
		}
	}
}

// TestArmPrompts_MultiSource pins what each arm is told for the three multi-*
// questions: the HDF arm exactly the three converted documents, the raw arm
// exactly the three raw scans — and the raw arm's file clause carries no hint
// of a set, a merge, a combined view or the HDF documents.
func TestArmPrompts_MultiSource(t *testing.T) {
	seen := 0
	for _, q := range Bank() {
		if !strings.HasPrefix(q.ID, "multi-") {
			continue
		}
		seen++
		if got, want := hdfPrompt(q), q.Ask+" The HDF documents are named gosec.hdf.json, zap.hdf.json and grype.hdf.json."; got != want {
			t.Errorf("%s hdf prompt:\n got %q\nwant %q", q.ID, got, want)
		}
		if got, want := rawPrompt(q), q.Ask+" The scan files are named gosec.json, zap.json and grype.json."; got != want {
			t.Errorf("%s raw prompt:\n got %q\nwant %q", q.ID, got, want)
		}
		clause := strings.ToLower(strings.TrimPrefix(rawPrompt(q), q.Ask))
		for _, leak := range []string{"hdf", "merge", "combin", "set", "sources", "together"} {
			if strings.Contains(clause, leak) {
				t.Errorf("%s: the raw arm's file clause must not mention %q, got %q", q.ID, leak, clause)
			}
		}
	}
	if seen != 3 {
		t.Fatalf("expected the three multi-* questions in the bank, found %d", seen)
	}
	// A single-source question keeps the singular clause, byte for byte.
	one := Question{ID: "one", Ask: "Q?", Sources: []Source{{Fixture: "zap.json", From: "zap", HDFName: "zap.hdf.json"}}}
	if hdfPrompt(one) != "Q? The HDF document is named zap.hdf.json." || rawPrompt(one) != "Q? The scan file is named zap.json." {
		t.Errorf("single-source prompts changed: %q / %q", hdfPrompt(one), rawPrompt(one))
	}
}

// TestAdHocPrompt pins the conversion-included prompt: a single-source question
// keeps the exact wording earlier runs used; a question whose sources are all
// hdf_convert-able names every source with its own from and output; a source
// that is normalized outside hdf_convert keeps the primary-only wording.
func TestAdHocPrompt(t *testing.T) {
	single := Question{ID: "one", Ask: "Q?", Sources: []Source{{Fixture: "zap.json", From: "zap", HDFName: "zap.hdf.json"}}}
	if got, want := adHocPrompt(single), "Q? The raw scan file is named zap.json. First convert it to HDF using hdf_convert with from=zap and output=one.adhoc.hdf.json, then analyze that HDF document."; got != want {
		t.Errorf("single-source prompt changed:\n got %q\nwant %q", got, want)
	}

	multi := Question{ID: "multi-x", Ask: "Q?", Sources: []Source{
		{Fixture: "gosec.json", From: "gosec", HDFName: "gosec.hdf.json"},
		{Fixture: "zap.json", From: "zap", HDFName: "zap.hdf.json"},
		{Fixture: "grype.json", From: "grype", HDFName: "grype.hdf.json"},
	}}
	got := adHocPrompt(multi)
	for _, want := range []string{
		"gosec.json (from=gosec, output=multi-x.adhoc.gosec.hdf.json)",
		"zap.json (from=zap, output=multi-x.adhoc.zap.hdf.json)",
		"grype.json (from=grype, output=multi-x.adhoc.grype.hdf.json)",
		"convert each one",
		"analyze those HDF documents together",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("multi-source prompt must contain %q, got %q", want, got)
		}
	}
	if strings.Contains(got, "merge") {
		t.Errorf("nothing about merging may reach the agent: %q", got)
	}

	cliPrep := Question{ID: "sbom", Ask: "Q?", Sources: []Source{
		{Fixture: "spdx.json", CLIPrep: []string{"system", "create"}, HDFName: "spdx.hdf-system.json"},
		{Fixture: "grype.json", From: "grype", HDFName: "grype.hdf.json"},
	}}
	if got := adHocPrompt(cliPrep); !strings.Contains(got, "The raw scan file is named spdx.json.") {
		t.Errorf("a CLIPrep question keeps the primary-only wording, got %q", got)
	}

	// The bank's three multi-* questions take the multi-source wording, and
	// every one of their sources is named.
	for _, q := range Bank() {
		if !strings.HasPrefix(q.ID, "multi-") {
			continue
		}
		p := adHocPrompt(q)
		for _, s := range q.Sources {
			if !strings.Contains(p, s.Fixture+" (from="+s.From) {
				t.Errorf("%s: ad-hoc prompt must name %s, got %q", q.ID, s.Fixture, p)
			}
		}
	}
}

// TestRender_AdHocCellBlankForQuestionsWithoutAnArm: a question with no ad-hoc
// arm renders "—" in the ad-hoc column of both the text and markdown reports,
// and contributes nothing to the ad-hoc total — only the arms that ran are
// summed. No question lacks the arm in a full ad-hoc run any more, but the
// pointer remains the representation of "did not run" and absence must never
// render as a zero cost.
func TestRender_AdHocCellBlankForQuestionsWithoutAnArm(t *testing.T) {
	arm := func(a Arm, prompt int) ArmResult {
		return ArmResult{Arm: a, Verdict: Correct, Scored: true, Samples: 1, Correct: 1, Agreement: 1,
			Cost: agent.Result{PromptTokens: prompt, CompletionTokens: 50}}
	}
	adhoc := arm(ArmHDF, 300) // 350 tokens
	plain := QuestionResult{ID: "plain-question", Ask: "?", Class: truth.ClassA,
		RawView: truth.Answered("1"), HDFView: truth.Answered("1"),
		Raw: arm(ArmRaw, 100), HDF: arm(ArmHDF, 100), HDFAdHoc: &adhoc}
	noArm := QuestionResult{ID: "noarm-question", Ask: "?", Class: truth.ClassA,
		RawView: truth.Answered("1"), HDFView: truth.Answered("1"),
		Raw: arm(ArmRaw, 100), HDF: arm(ArmHDF, 100), HDFAdHoc: nil}
	results := []QuestionResult{plain, noArm}

	text := Render("stub", results, true, nil)
	md := RenderMarkdown(RunMeta{Models: []string{"stub"}}, []ModelRun{{Model: "stub", Results: results}}, true, nil)

	for _, out := range []struct{ name, s string }{{"text", text}, {"markdown", md}} {
		var noArmRow, plainRow string
		for _, line := range strings.Split(out.s, "\n") {
			if strings.Contains(line, "noarm-question") && strings.Contains(line, "objective") {
				noArmRow = line
			}
			if strings.Contains(line, "plain-question") && strings.Contains(line, "objective") {
				plainRow = line
			}
		}
		if noArmRow == "" || plainRow == "" {
			t.Fatalf("%s: could not find both question rows:\n%s", out.name, out.s)
		}
		if !strings.HasSuffix(strings.TrimSpace(noArmRow), "—") && !strings.HasSuffix(strings.TrimSpace(noArmRow), "— |") {
			t.Errorf("%s: the row without an arm must end with the blank ad-hoc cell —, got %q", out.name, noArmRow)
		}
		if !strings.Contains(plainRow, "350") {
			t.Errorf("%s: plain row must carry its ad-hoc tokens 350, got %q", out.name, plainRow)
		}
		if !strings.Contains(out.s, "350") || strings.Contains(out.s, "700") {
			t.Errorf("%s: ad-hoc total must be 350 (the arm-less question contributes nothing):\n%s", out.name, out.s)
		}
	}
	if !strings.Contains(text, "hdf-mcp arm (ad-hoc convert) .      350") {
		t.Errorf("text ad-hoc total line wrong:\n%s", text)
	}
}
