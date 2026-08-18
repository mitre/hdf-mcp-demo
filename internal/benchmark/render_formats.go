package benchmark

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mitre/hdf-mcp-demo/internal/truth"
)

// ModelRun pairs a model's name with its graded results, so multi-model runs can
// be rendered together in the machine-readable formats.
type ModelRun struct {
	Model   string
	Results []QuestionResult
}

// RunMeta describes how a benchmark run was configured, so a saved artifact is
// self-describing. It deliberately omits the endpoint URL (internal infra) — only
// the model names and grading-relevant settings are recorded.
type RunMeta struct {
	Timestamp   string // caller-supplied RFC3339; empty is fine (tests, reproducibility)
	Models      []string
	Concurrency int
	MaxTokens   int
	MaxIters    int
	AdHoc       bool
	Repeat      int
	Temperature float64
}

// MetaText renders the run metadata as a short plain-text header.
func MetaText(m RunMeta) string {
	var b strings.Builder
	b.WriteString("HDF-MCP benchmark\n")
	if m.Timestamp != "" {
		fmt.Fprintf(&b, "  timestamp: %s\n", m.Timestamp)
	}
	fmt.Fprintf(&b, "  models:    %s\n", strings.Join(m.Models, ", "))
	fmt.Fprintf(&b, "  settings:  concurrency=%d max-tokens=%d max-iters=%d ad-hoc=%v repeat=%d temperature=%s\n",
		m.Concurrency, m.MaxTokens, m.MaxIters, m.AdHoc, m.Repeat, tempStr(m.Temperature))
	return b.String()
}

// tempStr renders a temperature, or "omitted" for the negative sentinel.
func tempStr(t float64) string {
	if t < 0 {
		return "omitted"
	}
	return fmt.Sprintf("%.2g", t)
}

// --- JSON ---

type jsonMeta struct {
	Timestamp   string   `json:"timestamp,omitempty"`
	Models      []string `json:"models"`
	Concurrency int      `json:"concurrency"`
	MaxTokens   int      `json:"maxTokens"`
	MaxIters    int      `json:"maxIters"`
	AdHoc       bool     `json:"adHoc"`
	Repeat      int      `json:"repeat"`
	Temperature float64  `json:"temperature"`
}

type jsonAnswer struct {
	Value      string `json:"value,omitempty"`
	Answerable bool   `json:"answerable"`
}

type jsonArm struct {
	Arm              string  `json:"arm"`
	Verdict          string  `json:"verdict"`
	Scored           bool    `json:"scored"`
	Samples          int     `json:"samples"`
	Correct          int     `json:"correct"`
	Agreement        float64 `json:"agreement"`
	Answer           string  `json:"answer"`
	Error            string  `json:"error,omitempty"`
	MeanPromptTokens int     `json:"meanPromptTokens"`
	MeanCompletion   int     `json:"meanCompletionTokens"`
	MeanTotalTokens  int     `json:"meanTotalTokens"`
	TokenStdDev      float64 `json:"tokenStdDev"`
	MeanToolCalls    int     `json:"meanToolCalls"`
	MeanIterations   int     `json:"meanIterations"`
	MeanElapsedSec   float64 `json:"meanElapsedSeconds"`
}

type jsonQuestion struct {
	ID       string     `json:"id"`
	Ask      string     `json:"ask"`
	Type     string     `json:"type"`
	RawView  jsonAnswer `json:"rawView"`
	HDFView  jsonAnswer `json:"hdfView"`
	Raw      jsonArm    `json:"raw"`
	HDF      jsonArm    `json:"hdf"`
	HDFAdHoc *jsonArm   `json:"hdfAdHoc,omitempty"`
}

type jsonAccuracy struct {
	Correct int `json:"correct"`
	Scored  int `json:"scored"`
	Failed  int `json:"failed"`
}

type jsonClassAccuracy struct {
	Raw jsonAccuracy `json:"raw"`
	HDF jsonAccuracy `json:"hdf"`
}

type jsonRemit struct {
	Hallucinated int `json:"hallucinated"`
	Abstained    int `json:"abstained"`
	Total        int `json:"total"`
}

type jsonCost struct {
	RawTokens         int      `json:"rawTokens"`
	HDFPipelineTokens int      `json:"hdfPipelineTokens"`
	HDFPipelineVsRaw  float64  `json:"hdfPipelineVsRaw"`
	HDFAdHocTokens    *int     `json:"hdfAdHocTokens,omitempty"`
	HDFAdHocVsRaw     *float64 `json:"hdfAdHocVsRaw,omitempty"`
}

type jsonSummary struct {
	ByType        map[string]jsonClassAccuracy `json:"byType"`
	All           jsonClassAccuracy            `json:"all"`
	RawOutOfRemit *jsonRemit                   `json:"rawOutOfRemit,omitempty"`
	HDFOutOfRemit *jsonRemit                   `json:"hdfOutOfRemit,omitempty"`
	Cost          jsonCost                     `json:"cost"`
}

type jsonRun struct {
	Model     string         `json:"model"`
	Questions []jsonQuestion `json:"questions"`
	Summary   jsonSummary    `json:"summary"`
}

type jsonReport struct {
	Meta jsonMeta  `json:"meta"`
	Runs []jsonRun `json:"runs"`
}

func toJSONAnswer(a truth.Answer) jsonAnswer {
	return jsonAnswer{Value: a.Value, Answerable: a.Answerable}
}

func toJSONArm(a ArmResult) jsonArm {
	return jsonArm{
		Arm: string(a.Arm), Verdict: string(a.Verdict), Scored: a.Scored,
		Samples: a.Samples, Correct: a.Correct, Agreement: a.Agreement,
		Answer: a.Answer, Error: a.Err,
		MeanPromptTokens: a.Cost.PromptTokens, MeanCompletion: a.Cost.CompletionTokens,
		MeanTotalTokens: a.Cost.TotalTokens(), TokenStdDev: a.TokenStdDev,
		MeanToolCalls: a.Cost.ToolCalls, MeanIterations: a.Cost.Iterations,
		MeanElapsedSec: a.Cost.Elapsed.Seconds(),
	}
}

func jsonAcc(rs []QuestionResult, arm Arm) jsonAccuracy {
	c, s := scoredAccuracy(rs, arm)
	return jsonAccuracy{Correct: c, Scored: s, Failed: failures(rs, arm)}
}

func classAccuracy(rs []QuestionResult) jsonClassAccuracy {
	return jsonClassAccuracy{Raw: jsonAcc(rs, ArmRaw), HDF: jsonAcc(rs, ArmHDF)}
}

// RenderJSON serializes the run as indented JSON — a stable, machine-readable
// artifact carrying run metadata, every per-arm token count/verdict/scored flag,
// per-type scored accuracy, the raw arm's out-of-remit behavior, and both cost
// views.
func RenderJSON(meta RunMeta, runs []ModelRun, adHoc bool) (string, error) {
	report := jsonReport{Meta: jsonMeta{
		Timestamp: meta.Timestamp, Models: meta.Models, Concurrency: meta.Concurrency,
		MaxTokens: meta.MaxTokens, MaxIters: meta.MaxIters, AdHoc: adHoc,
		Repeat: meta.Repeat, Temperature: meta.Temperature,
	}}
	for _, run := range runs {
		jr := jsonRun{Model: run.Model}
		for _, r := range run.Results {
			jq := jsonQuestion{
				ID: r.ID, Ask: r.Ask, Type: typeLabel(r.Class),
				RawView: toJSONAnswer(r.RawView), HDFView: toJSONAnswer(r.HDFView),
				Raw: toJSONArm(r.Raw), HDF: toJSONArm(r.HDF),
			}
			if r.HDFAdHoc != nil {
				a := toJSONArm(*r.HDFAdHoc)
				jq.HDFAdHoc = &a
			}
			jr.Questions = append(jr.Questions, jq)
		}
		byType := map[string]jsonClassAccuracy{}
		for _, c := range allClasses {
			if sub := filterClass(run.Results, c); len(sub) > 0 {
				byType[typeLabel(c)] = classAccuracy(sub)
			}
		}
		rawTok, hdfTok, adhocTok := totals(run.Results)
		cost := jsonCost{RawTokens: rawTok, HDFPipelineTokens: hdfTok, HDFPipelineVsRaw: ratioFloat(hdfTok, rawTok)}
		if adHoc {
			at, av := adhocTok, ratioFloat(adhocTok, rawTok)
			cost.HDFAdHocTokens, cost.HDFAdHocVsRaw = &at, &av
		}
		summary := jsonSummary{ByType: byType, All: classAccuracy(run.Results), Cost: cost}
		if hall, abst, outOf := remit(run.Results, ArmRaw); outOf > 0 {
			summary.RawOutOfRemit = &jsonRemit{Hallucinated: hall, Abstained: abst, Total: outOf}
		}
		if hall, abst, outOf := remit(run.Results, ArmHDF); outOf > 0 {
			summary.HDFOutOfRemit = &jsonRemit{Hallucinated: hall, Abstained: abst, Total: outOf}
		}
		jr.Summary = summary
		report.Runs = append(report.Runs, jr)
	}
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ratioFloat is n/d as a float (0 when d==0).
func ratioFloat(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

// --- Markdown ---

// RenderMarkdown renders the run as GitHub-flavored markdown: a metadata header, a
// per-model detail table, a per-type scored-accuracy table, the raw arm's
// out-of-remit line, a cost list, and the shared limitations note.
func RenderMarkdown(meta RunMeta, runs []ModelRun, adHoc bool) string {
	var b strings.Builder
	b.WriteString("# HDF-MCP benchmark\n\n")
	if meta.Timestamp != "" {
		fmt.Fprintf(&b, "- **timestamp:** %s\n", meta.Timestamp)
	}
	fmt.Fprintf(&b, "- **models:** %s\n", strings.Join(meta.Models, ", "))
	fmt.Fprintf(&b, "- **settings:** concurrency=%d, max-tokens=%d, max-iters=%d, ad-hoc=%v, repeat=%d, temperature=%s\n\n",
		meta.Concurrency, meta.MaxTokens, meta.MaxIters, adHoc, meta.Repeat, tempStr(meta.Temperature))

	for _, run := range runs {
		fmt.Fprintf(&b, "## %s (%d questions)\n\n", run.Model, len(run.Results))

		if adHoc {
			b.WriteString("| question | type | raw | hdf | rawTok | hdfTok | adhocTok |\n")
			b.WriteString("|---|---|---|---|--:|--:|--:|\n")
		} else {
			b.WriteString("| question | type | raw | hdf | rawTok | hdfTok |\n")
			b.WriteString("|---|---|---|---|--:|--:|\n")
		}
		for _, r := range run.Results {
			row := fmt.Sprintf("| %s | %s | %s | %s | %d | %d |",
				r.ID, typeLabel(r.Class), r.Raw.Verdict, r.HDF.Verdict, r.Raw.Cost.TotalTokens(), r.HDF.Cost.TotalTokens())
			if adHoc {
				ah := 0
				if r.HDFAdHoc != nil {
					ah = r.HDFAdHoc.Cost.TotalTokens()
				}
				row += fmt.Sprintf(" %d |", ah)
			}
			b.WriteString(row + "\n")
		}

		b.WriteString("\n**Accuracy by type** (correct / questions the arm can answer)\n\n")
		b.WriteString("| type | raw-file | hdf-mcp |\n|---|---|---|\n")
		for _, c := range allClasses {
			sub := filterClass(run.Results, c)
			if len(sub) == 0 {
				continue
			}
			rc, rn := scoredAccuracy(sub, ArmRaw)
			hc, hn := scoredAccuracy(sub, ArmHDF)
			fmt.Fprintf(&b, "| %s | %s | %s |\n", classRowLabel(c), frac(rc, rn), frac(hc, hn))
		}
		rc, rn := scoredAccuracy(run.Results, ArmRaw)
		hc, hn := scoredAccuracy(run.Results, ArmHDF)
		fmt.Fprintf(&b, "| **ALL (scored)** | %s | %s |\n", frac(rc, rn), frac(hc, hn))

		if hall, abst, outOf := remit(run.Results, ArmRaw); outOf > 0 {
			fmt.Fprintf(&b, "\n_Raw-file arm on hdf-only questions (out of remit, not scored): %d hallucinated / %d abstained of %d._\n",
				hall, abst, outOf)
		}
		if hall, abst, outOf := remit(run.Results, ArmHDF); outOf > 0 {
			fmt.Fprintf(&b, "\n_HDF-mcp arm on raw-only questions (out of remit, not scored): %d hallucinated / %d abstained of %d._\n",
				hall, abst, outOf)
		}
		if rf, hf := failures(run.Results, ArmRaw), failures(run.Results, ArmHDF); rf > 0 || hf > 0 {
			fmt.Fprintf(&b, "\n_Failed (errored/over-context/timeout): raw %d, hdf %d._\n", rf, hf)
		}

		rawTok, hdfTok, adhocTok := totals(run.Results)
		b.WriteString("\n**Token cost** (real prompt+completion usage, tool-schema tax included)\n\n")
		fmt.Fprintf(&b, "- raw-file arm: **%d**\n", rawTok)
		fmt.Fprintf(&b, "- hdf-mcp arm (pipeline): **%d** (%s vs raw)\n", hdfTok, ratio(hdfTok, rawTok))
		if adHoc {
			fmt.Fprintf(&b, "- hdf-mcp arm (ad-hoc convert): **%d** (%s vs raw)\n", adhocTok, ratio(adhocTok, rawTok))
		}
		b.WriteString("\n")
	}
	b.WriteString(markdownFooter(adHoc))
	return b.String()
}

// markdownFooter renders the text footer's limitations note as a markdown blockquote.
func markdownFooter(adHoc bool) string {
	text := strings.TrimSpace(footer(adHoc))
	var b strings.Builder
	b.WriteString("---\n\n> **Notes / limitations** (read before trusting a number)\n>\n")
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "notes / limitations") {
			continue
		}
		fmt.Fprintf(&b, "> %s\n", strings.TrimPrefix(line, "- "))
	}
	return b.String()
}
