package benchmark

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mitre/hdf-mcp-demo/internal/agent"
	"github.com/mitre/hdf-mcp-demo/internal/truth"
)

func sampleRuns() []ModelRun {
	arm := func(a Arm, v Verdict, scored bool, prompt, compl int) ArmResult {
		return ArmResult{Arm: a, Verdict: v, Scored: scored, Answer: "x",
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
	s, err := RenderJSON(meta, sampleRuns(), false)
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
					TotalTokens int  `json:"totalTokens"`
					Scored      bool `json:"scored"`
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

func TestRenderMarkdown(t *testing.T) {
	meta := RunMeta{Models: []string{"stub"}, Concurrency: 4, MaxTokens: 1024, MaxIters: 6}
	md := RenderMarkdown(meta, sampleRuns(), false)
	for _, must := range []string{
		"# HDF-MCP benchmark", "**models:** stub", "## stub", "| question | type |",
		"objective", "interpretive", "hdf-only", "out of remit", "Notes / limitations",
	} {
		if !strings.Contains(md, must) {
			t.Errorf("markdown missing %q\n---\n%s", must, md)
		}
	}
	if strings.Contains(md, "adhocTok") {
		t.Errorf("markdown should not have an ad-hoc column when adHoc=false")
	}
}
