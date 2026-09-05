package benchmark

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/mitre/hdf-mcp-demo/internal/agent"
	"github.com/mitre/hdf-mcp-demo/internal/truth"
)

func sampleRuns() []ModelRun {
	arm := func(a Arm, v Verdict, scored bool, prompt, compl int) ArmResult {
		c := 0
		if v == Correct {
			c = 1
		}
		return ArmResult{Arm: a, Verdict: v, Scored: scored, Samples: 1, Correct: c, Agreement: 1, Answer: "x",
			Cost: agent.Result{PromptTokens: prompt, CompletionTokens: compl, ToolCalls: 1, Iterations: 1}}
	}
	results := []QuestionResult{
		// objective (A): both arms scored, both correct.
		{ID: "q-a", Ask: "?", Class: truth.ClassA, RawView: truth.Answered("7"), HDFView: truth.Answered("7"),
			Raw: arm(ArmRaw, Correct, true, 100, 10), HDF: arm(ArmHDF, Correct, true, 200, 12)},
		// interpretive (B): raw wrong, hdf correct — both scored.
		{ID: "q-b", Ask: "?", Class: truth.ClassB, RawView: truth.Answered("7"), HDFView: truth.Answered("3"),
			Raw: arm(ArmRaw, Wrong, true, 100, 10), HDF: arm(ArmHDF, Correct, true, 200, 12)},
		// hdf-only (C): raw out of remit + hallucinated (NOT scored); hdf correct.
		{ID: "q-c", Ask: "?", Class: truth.ClassC, RawView: truth.Unanswerable(), HDFView: truth.Answered("40"),
			Raw: arm(ArmRaw, Hallucinated, false, 100, 10), HDF: arm(ArmHDF, Correct, true, 200, 12)},
	}
	return []ModelRun{{Model: "stub", Results: results}}
}

func TestRenderJSON(t *testing.T) {
	meta := RunMeta{Models: []string{"stub"}, Concurrency: 4, MaxTokens: 1024, MaxIters: 6}
	s, err := RenderJSON(meta, sampleRuns(), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Meta struct {
			Models []string `json:"models"`
		} `json:"meta"`
		Runs []struct {
			Model     string `json:"model"`
			Questions []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
				Raw  struct {
					MeanTotalTokens int  `json:"meanTotalTokens"`
					Scored          bool `json:"scored"`
				} `json:"raw"`
			} `json:"questions"`
			Summary struct {
				All           jsonClassAccuracy `json:"all"`
				RawOutOfRemit *jsonRemit        `json:"rawOutOfRemit"`
				Cost          jsonCost          `json:"cost"`
			} `json:"summary"`
		} `json:"runs"`
	}
	if err := json.Unmarshal([]byte(s), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, s)
	}
	if len(got.Meta.Models) != 1 || got.Meta.Models[0] != "stub" {
		t.Errorf("meta.models = %v, want [stub]", got.Meta.Models)
	}
	if len(got.Runs) != 1 {
		t.Fatalf("want one run, got %d", len(got.Runs))
	}
	r := got.Runs[0]
	if got := r.Questions[0].Type; got != "objective" {
		t.Errorf("q-a type = %q, want objective", got)
	}
	if r.Questions[2].Raw.Scored {
		t.Errorf("q-c raw should be out of remit (scored=false)")
	}
	// raw scored over A+B only: 1 correct of 2. hdf scored over all: 3 of 3.
	if r.Summary.All.Raw.Correct != 1 || r.Summary.All.Raw.Scored != 2 {
		t.Errorf("raw scored accuracy = %+v, want 1/2", r.Summary.All.Raw)
	}
	if r.Summary.All.HDF.Correct != 3 || r.Summary.All.HDF.Scored != 3 {
		t.Errorf("hdf scored accuracy = %+v, want 3/3", r.Summary.All.HDF)
	}
	if r.Summary.RawOutOfRemit == nil || r.Summary.RawOutOfRemit.Hallucinated != 1 || r.Summary.RawOutOfRemit.Total != 1 {
		t.Errorf("rawOutOfRemit = %+v, want 1 hallucinated of 1", r.Summary.RawOutOfRemit)
	}
	// cost totals: raw 110*3=330, hdf 212*3=636.
	if r.Summary.Cost.RawTokens != 330 || r.Summary.Cost.HDFPipelineTokens != 636 {
		t.Errorf("cost = %+v, want raw 330 / hdf 636", r.Summary.Cost)
	}
}

// The raw-only (Class D) path is symmetric to hdf-only: the HDF arm is out of
// remit. The bank's live occupant is sbom-vuln-free-packages (the SBOM inventory
// is carried as a reference, not embedded); this exercises the same path
// synthetically so the render logic is pinned without a conversion.
func TestClassD_RawOnly(t *testing.T) {
	q := Question{Intent: IntentHDF}
	if k := q.key(truth.ClassD, truth.Answered("apk"), truth.Unanswerable(), ArmHDF); k.Answerable {
		t.Errorf("Class D hdf key should be unanswerable")
	}
	if k := q.key(truth.ClassD, truth.Answered("apk"), truth.Unanswerable(), ArmRaw); k.Value != "apk" {
		t.Errorf("Class D raw key should be the raw view")
	}
	if typeLabel(truth.ClassD) != "raw-only" {
		t.Errorf("typeLabel(D) = %q, want raw-only", typeLabel(truth.ClassD))
	}

	rs := []QuestionResult{{
		ID: "d1", Ask: "?", Class: truth.ClassD,
		Raw: ArmResult{Arm: ArmRaw, Verdict: Correct, Scored: true, Samples: 1, Correct: 1, Cost: agent.Result{PromptTokens: 10}},
		HDF: ArmResult{Arm: ArmHDF, Verdict: Hallucinated, Scored: false, Samples: 1, Cost: agent.Result{PromptTokens: 20}},
	}}
	if rc, rn := scoredAccuracy(rs, ArmRaw); rc != 1 || rn != 1 {
		t.Errorf("raw scored = %d/%d, want 1/1", rc, rn)
	}
	if _, hn := scoredAccuracy(rs, ArmHDF); hn != 0 {
		t.Errorf("hdf scored total = %d, want 0 (out of remit)", hn)
	}
	if _, _, hOut := remit(rs, ArmHDF); hOut != 1 {
		t.Errorf("hdf out-of-remit total = %d, want 1", hOut)
	}
	js, err := RenderJSON(RunMeta{Models: []string{"m"}}, []ModelRun{{Model: "m", Results: rs}}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(js, "hdfOutOfRemit") || !strings.Contains(js, "raw-only") {
		t.Errorf("json missing hdfOutOfRemit / raw-only:\n%s", js)
	}
}

func TestRenderMarkdown(t *testing.T) {
	meta := RunMeta{Models: []string{"stub"}, Concurrency: 4, MaxTokens: 1024, MaxIters: 6}
	md := RenderMarkdown(meta, sampleRuns(), false, nil)
	for _, must := range []string{
		"# HDF-MCP benchmark", "**models:** stub", "## stub", "| question | type |",
		"objective", "interpretive", "hdf-only", "out of remit", "docs/interpreting-results.md",
	} {
		if !strings.Contains(md, must) {
			t.Errorf("markdown missing %q\n---\n%s", must, md)
		}
	}
	if strings.Contains(md, "adhocTok") {
		t.Errorf("markdown should not have an ad-hoc column when adHoc=false")
	}
}

// TestMetaNumCtx checks the context window is reported when a run had one and
// stays out of the output entirely when it did not — an OpenAI-side run has no
// such knob, so printing num-ctx=0 there would describe a setting that does not
// exist.
func TestMetaNumCtx(t *testing.T) {
	set := RunMeta{Models: []string{"m"}, NumCtx: 32768}
	if got := MetaText(set); !strings.Contains(got, "num-ctx=32768") {
		t.Errorf("MetaText omitted num-ctx:\n%s", got)
	}
	if got := RenderMarkdown(set, nil, false, nil); !strings.Contains(got, "num-ctx=32768") {
		t.Errorf("RenderMarkdown omitted num-ctx:\n%s", got)
	}
	js, err := RenderJSON(set, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(js, `"numCtx": 32768`) {
		t.Errorf("RenderJSON omitted numCtx:\n%s", js)
	}

	unset := RunMeta{Models: []string{"m"}}
	if got := MetaText(unset); strings.Contains(got, "num-ctx") {
		t.Errorf("MetaText reported num-ctx for a run without one:\n%s", got)
	}
	if got := RenderMarkdown(unset, nil, false, nil); strings.Contains(got, "num-ctx") {
		t.Errorf("RenderMarkdown reported num-ctx for a run without one:\n%s", got)
	}
	js, err = RenderJSON(unset, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(js, "numCtx") {
		t.Errorf("RenderJSON reported numCtx for a run without one:\n%s", js)
	}
}

// TestChargeStatement pins an explicit acceptance criterion of the study: the
// report must STATE what the run cost externally, not leave it to be inferred.
func TestChargeStatement(t *testing.T) {
	local := RunMeta{Models: []string{"m"}, Provider: "ollama"}
	if got := MetaText(local); !strings.Contains(got, "zero charge") {
		t.Errorf("local run must state zero charge:\n%s", got)
	}
	if got := RenderMarkdown(local, nil, false, nil); !strings.Contains(got, "zero charge") {
		t.Errorf("markdown must state zero charge:\n%s", got)
	}
	js, err := RenderJSON(local, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(js, "costStatement") {
		t.Errorf("json must carry the cost statement:\n%s", js)
	}
	// A gateway run must NOT claim zero charge — the harness cannot know.
	remote := RunMeta{Models: []string{"m"}, Provider: "openai"}
	if got := MetaText(remote); strings.Contains(got, "zero charge incurred") {
		t.Errorf("gateway run must not claim zero charge:\n%s", got)
	}
}

// TestWallClockReported pins the other unmet criterion: wall-clock was measured
// but only ever surfaced in JSON.
func TestWallClockReported(t *testing.T) {
	runs := sampleRuns()
	if got := Render(runs[0].Model, runs[0].Results, false, nil); !strings.Contains(got, "wall-clock") {
		t.Errorf("text report omits wall-clock:\n%s", got)
	}
	if got := RenderMarkdown(RunMeta{Models: []string{"m"}}, runs, false, nil); !strings.Contains(got, "Wall-clock") {
		t.Errorf("markdown report omits wall-clock:\n%s", got)
	}
}

// TestPerQuestionMultiplier pins the per-question mult column: the aggregate
// "Nx vs raw" line hides which questions are the peaks and valleys, so each
// detail row carries its own hdfTok/rawTok multiplier in every format. Sample
// arms cost raw 110 / hdf 212 → 1.93x; a zero-raw row must render n/a, not Inf.
func TestPerQuestionMultiplier(t *testing.T) {
	runs := sampleRuns()
	if got := Render(runs[0].Model, runs[0].Results, false, nil); !strings.Contains(got, "1.93x") {
		t.Errorf("text report missing per-question mult:\n%s", got)
	}
	md := RenderMarkdown(RunMeta{Models: []string{"stub"}}, runs, false, nil)
	if !strings.Contains(md, "| mult |") || !strings.Contains(md, "| 1.93x |") {
		t.Errorf("markdown report missing per-question mult:\n%s", md)
	}
	js, err := RenderJSON(RunMeta{Models: []string{"stub"}}, runs, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Runs []struct {
			Questions []struct {
				HDFVsRaw float64 `json:"hdfVsRaw"`
			} `json:"questions"`
		} `json:"runs"`
	}
	if err := json.Unmarshal([]byte(js), &got); err != nil {
		t.Fatal(err)
	}
	if r := got.Runs[0].Questions[0].HDFVsRaw; r < 1.92 || r > 1.94 {
		t.Errorf("json hdfVsRaw = %v, want ~1.93", r)
	}

	// Both-failed row: the multiplier is suppressed as an outcome mismatch before
	// the zero-denominator case can arise, so it renders "—" rather than "n/a".
	// Either way it must never be Inf or NaN.
	zero := []ModelRun{{Model: "z", Results: []QuestionResult{{
		ID: "q", Class: truth.ClassA, RawView: truth.Answered("1"), HDFView: truth.Answered("1"),
		Raw: ArmResult{Arm: ArmRaw, Verdict: Failed, Scored: true, Samples: 1},
		HDF: ArmResult{Arm: ArmHDF, Verdict: Failed, Scored: true, Samples: 1},
	}}}}
	got2 := RenderMarkdown(RunMeta{Models: []string{"z"}}, zero, false, nil)
	if !strings.Contains(got2, "| — |") {
		t.Errorf("both-failed row should suppress the multiplier:\n%s", got2)
	}
	for _, bad := range []string{"Inf", "NaN"} {
		if strings.Contains(got2, bad) {
			t.Errorf("row rendered %s:\n%s", bad, got2)
		}
	}
}

// TestFailureReasonsSurfaced pins the diagnosability fix: an all-failed run must
// say WHY each arm failed — the transport error vs. the max-iters timeout — not
// just print "failed". Without this a run where the endpoint was down and a run
// where the HDF arm never converged are indistinguishable in the human report,
// which is exactly the wall of "failed" the first live runs produced. The two
// distinct reasons must appear; a reason shared across questions is deduped to a
// single counted line rather than repeated per question.
func TestFailureReasonsSurfaced(t *testing.T) {
	const transport = "dial tcp 127.0.0.1:4000: connect: connection refused"
	const maxIters = "reached max iterations (6) without a final answer"
	failed := func(a Arm, msg string) ArmResult {
		return ArmResult{Arm: a, Verdict: Failed, Scored: true, Samples: 1, Err: msg}
	}
	q := func(id string) QuestionResult {
		return QuestionResult{ID: id, Ask: "?", Class: truth.ClassA,
			RawView: truth.Answered("1"), HDFView: truth.Answered("1"),
			Raw: failed(ArmRaw, transport), HDF: failed(ArmHDF, maxIters)}
	}
	runs := []ModelRun{{Model: "stub", Results: []QuestionResult{q("q1"), q("q2")}}}

	text := Render(runs[0].Model, runs[0].Results, false, nil)
	md := RenderMarkdown(RunMeta{Models: []string{"stub"}}, runs, false, nil)
	for _, out := range []struct{ name, s string }{{"text", text}, {"markdown", md}} {
		for _, want := range []string{transport, maxIters} {
			if !strings.Contains(out.s, want) {
				t.Errorf("%s report omitted failure reason %q:\n%s", out.name, want, out.s)
			}
		}
		if n := strings.Count(out.s, transport); n != 1 {
			t.Errorf("%s report should dedupe the shared raw error to one line, saw %d:\n%s", out.name, n, out.s)
		}
	}
}

// TestVarianceShownWhenRepeated pins that a multi-trial run SHOWS its variance.
// Token stddev was computed and carried in JSON but never rendered in the human
// formats, so a K=3 report looked identical to a single run and invited reading
// a mean as an exact measurement.
func TestVarianceShownWhenRepeated(t *testing.T) {
	runs := sampleRuns()
	runs[0].Results[0].Raw.TokenStdDev = 42.5
	runs[0].Results[0].HDF.TokenStdDev = 17.25

	txt := Render(runs[0].Model, runs[0].Results, false, nil)
	if !strings.Contains(txt, "±42") || !strings.Contains(txt, "±17") {
		t.Errorf("text report omits token stddev:\n%s", txt)
	}
	md := RenderMarkdown(RunMeta{Models: []string{"m"}, Repeat: 3}, runs, false, nil)
	if !strings.Contains(md, "±42") || !strings.Contains(md, "±17") {
		t.Errorf("markdown report omits token stddev:\n%s", md)
	}

	// A single-trial run has no variance to show and must stay uncluttered.
	runs[0].Results[0].Raw.TokenStdDev = 0
	runs[0].Results[0].HDF.TokenStdDev = 0
	if got := Render(runs[0].Model, runs[0].Results, false, nil); strings.Contains(got, "±") {
		t.Errorf("single-trial report should not show a ± column:\n%s", got)
	}
}

// TestProvenanceNamesBothArtifacts fixes a labelling error: a run emits TWO
// documents per model — the CycloneDX AI-BOM itself, and an HDF System document
// that wraps it — and the report called the System document "the BOM". They are
// not the same artifact, and a provenance record that misnames its own contents
// is worse than one that says less.
func TestProvenanceNamesBothArtifacts(t *testing.T) {
	meta := RunMeta{
		Models:   []string{"granite4.1:8b"},
		Provider: "ollama",
		ModelProvenance: []ModelProvenance{{
			Model: "granite4.1:8b", BOM: "granite4.1_8b.model.cdx.json", System: "granite4.1_8b.model.system.json",
		}},
	}
	for name, got := range map[string]string{"text": MetaText(meta), "markdown": RenderMarkdown(meta, nil, false, nil)} {
		if !strings.Contains(got, "granite4.1_8b.model.cdx.json") {
			t.Errorf("%s report omits the AI-BOM itself:\n%s", name, got)
		}
		if !strings.Contains(got, "granite4.1_8b.model.system.json") {
			t.Errorf("%s report omits the HDF System document:\n%s", name, got)
		}
		// The System document must not be presented as the BOM.
		if strings.Contains(got, "BOM:** `granite4.1_8b.model.system.json`") ||
			strings.Contains(got, "BOM: granite4.1_8b.model.system.json") {
			t.Errorf("%s report labels the HDF System document as the BOM:\n%s", name, got)
		}
	}
}

// TestFooterPointsAtDocsRatherThanEmbedding pins the second correction: the
// interpretation notes are reference material that belongs in one maintained
// document, not twenty-odd quoted lines appended to every report a run produces.
func TestFooterPointsAtDocsRatherThanEmbedding(t *testing.T) {
	md := RenderMarkdown(RunMeta{Models: []string{"m"}}, sampleRuns(), false, nil)
	if !strings.Contains(md, "docs/interpreting-results.md") {
		t.Errorf("report should point at the interpretation guide:\n%s", md)
	}
	// The long-form prose must no longer be inlined.
	for _, inlined := range []string{
		"Question types are a grading distinction",
		"Grading favors abstention over false credit",
		"treat as directional, not definitive",
	} {
		if strings.Contains(md, inlined) {
			t.Errorf("report still embeds the notes prose (%q) instead of referencing it:\n%s", inlined, md)
		}
	}
}

// TestMultSuppressedWhenAnArmDidNotAnswer pins that the cost multiplier is only
// shown when both arms produced an answer. Comparing an arm's cost-to-answer
// against the other's cost-to-give-up measures nothing: in this study failed arms
// averaged 7,955 tokens against 4,552 for correct ones, because a failure burns
// the whole iteration budget. A ratio across mismatched outcomes reads as a cost
// finding and is an artefact.
func TestMultSuppressedWhenAnArmDidNotAnswer(t *testing.T) {
	for _, tc := range []struct {
		name     string
		raw, hdf Verdict
		wantMult bool
	}{
		{"both correct", Correct, Correct, true},
		{"both answered, one wrong", Correct, Wrong, true},
		{"raw failed", Failed, Correct, false},
		{"hdf failed", Correct, Failed, false},
		{"both failed", Failed, Failed, false},
		{"hdf abstained", Correct, Abstained, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runs := sampleRuns()
			runs[0].Results = runs[0].Results[:1]
			runs[0].Results[0].Raw.Verdict = tc.raw
			runs[0].Results[0].HDF.Verdict = tc.hdf

			id := runs[0].Results[0].ID
			txt := Render(runs[0].Model, runs[0].Results, false, nil)
			md := RenderMarkdown(RunMeta{Models: []string{"m"}}, runs, false, nil)
			for name, got := range map[string]string{"text": txt, "markdown": md} {
				// Scope to the question's OWN row: the report carries other
				// ratios (per-arm cost summaries) that are not this column.
				row := ""
				for _, line := range strings.Split(got, "\n") {
					if strings.Contains(line, id) {
						row = line
						break
					}
				}
				if row == "" {
					t.Fatalf("%s report has no row for %s:\n%s", name, id, got)
				}
				hasMult := regexp.MustCompile(`\d\.\d+x`).MatchString(row)
				if hasMult != tc.wantMult {
					verb := "omit"
					if tc.wantMult {
						verb = "show"
					}
					t.Errorf("%s report should %s a multiplier for raw=%s hdf=%s, row was: %s", name, verb, tc.raw, tc.hdf, row)
				}
			}
		})
	}
}

// TestIterationCapCaveatSurfaced pins the lesson of the first live gateway run:
// the raw arm hit the iteration cap on 13 of 28 runs, so its accuracy measured
// its turn budget rather than its capability — yet the headline "53% vs 84%" was
// quotable without that context, because the cap only showed up in a separate
// failure block further down. When the cap binds, the accuracy figure must carry
// the caveat next to it.
func TestIterationCapCaveatSurfaced(t *testing.T) {
	const capMsg = "reached max iterations (6) without a final answer"
	arm := func(a Arm, v Verdict, err string) ArmResult {
		return ArmResult{Arm: a, Verdict: v, Scored: true, Samples: 1, Correct: 0, Err: err}
	}
	capped := QuestionResult{ID: "q1", Ask: "?", Class: truth.ClassA,
		RawView: truth.Answered("1"), HDFView: truth.Answered("1"),
		Raw: arm(ArmRaw, Failed, capMsg), HDF: arm(ArmHDF, Wrong, "")}
	runs := []ModelRun{{Model: "stub", Results: []QuestionResult{capped}}}

	text := Render(runs[0].Model, runs[0].Results, false, nil)
	md := RenderMarkdown(RunMeta{Models: []string{"stub"}, MaxIters: 6}, runs, false, nil)
	for _, out := range []struct{ name, s string }{{"text", text}, {"markdown", md}} {
		if !strings.Contains(out.s, "iteration cap") {
			t.Errorf("%s report must flag that the iteration cap bound the run:\n%s", out.name, out.s)
		}
		if !strings.Contains(out.s, "lower bound") {
			t.Errorf("%s report must say a capped arm's accuracy is a lower bound:\n%s", out.name, out.s)
		}
	}
}

// TestNoIterationCapCaveatWhenUnbound keeps the caveat honest: a run where no arm
// hit the cap must not carry it, or the warning becomes noise readers skip.
func TestNoIterationCapCaveatWhenUnbound(t *testing.T) {
	ok := QuestionResult{ID: "q1", Ask: "?", Class: truth.ClassA,
		RawView: truth.Answered("1"), HDFView: truth.Answered("1"),
		Raw: ArmResult{Arm: ArmRaw, Verdict: Correct, Scored: true, Samples: 1, Correct: 1},
		HDF: ArmResult{Arm: ArmHDF, Verdict: Correct, Scored: true, Samples: 1, Correct: 1}}
	runs := []ModelRun{{Model: "stub", Results: []QuestionResult{ok}}}
	md := RenderMarkdown(RunMeta{Models: []string{"stub"}, MaxIters: 6}, runs, false, nil)
	if strings.Contains(md, "iteration cap") {
		t.Errorf("unbound run must not carry the cap caveat:\n%s", md)
	}
}
