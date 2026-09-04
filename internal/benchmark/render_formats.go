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
	Provider    string
	Models      []string
	Concurrency int
	MaxTokens   int
	MaxIters    int
	AdHoc       bool
	Repeat      int
	Temperature float64
	NumCtx      int // ollama only: the context window the run was given; 0 = provider default
	// ModelProvenance names, per model, the two documents a run emits beside the
	// report so it records WHICH weights produced it rather than only a tag.
	ModelProvenance []ModelProvenance
}

// ModelProvenance is one model's provenance pair. They are distinct artifacts
// and are named separately: BOM is the CycloneDX AI-BOM describing the model,
// System is the HDF System document that ingests it. Calling the System document
// "the BOM" would misdescribe the very record whose job is to be accurate.
type ModelProvenance struct {
	Model  string // the model tag the run used
	BOM    string // CycloneDX ML-BOM filename, beside the report
	System string // HDF System document filename, beside the report
}

// MetaText renders the run metadata as a short plain-text header.
func MetaText(m RunMeta) string {
	var b strings.Builder
	b.WriteString("HDF-MCP benchmark\n")
	if m.Timestamp != "" {
		fmt.Fprintf(&b, "  timestamp: %s\n", m.Timestamp)
	}
	if m.Provider != "" {
		fmt.Fprintf(&b, "  provider:  %s\n", m.Provider)
	}
	if len(m.Models) > 0 {
		fmt.Fprintf(&b, "  models:    %s\n", strings.Join(m.Models, ", "))
	}
	fmt.Fprintf(&b, "  settings:  concurrency=%d max-tokens=%d max-iters=%d ad-hoc=%v repeat=%d temperature=%s%s\n",
		m.Concurrency, m.MaxTokens, m.MaxIters, m.AdHoc, m.Repeat, tempStr(m.Temperature), numCtxStr(m.NumCtx, " "))
	if m.Provider != "" {
		fmt.Fprintf(&b, "  cost:      %s\n", chargeStatement(m.Provider))
	}
	for _, p := range m.ModelProvenance {
		fmt.Fprintf(&b, "  provenance: %s — AI-BOM %s, HDF System %s\n", p.Model, p.BOM, p.System)
	}
	return b.String()
}

// numCtxStr renders the context-window setting for the providers that have one,
// and nothing at all when it is unset — an OpenAI-side run has no such knob, so
// printing "num-ctx=0" there would imply a setting that does not exist.
func numCtxStr(n int, sep string) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("%snum-ctx=%d", sep, n)
}

// chargeStatement records what the run cost externally. The study's
// pre-registration requires a zero-charge instrument and requires the report to
// SAY so, rather than leaving a reader to infer it from a provider name: a local
// Ollama run cannot bill, while an OpenAI-compatible gateway depends on whose
// endpoint it was, which this harness cannot know.
func chargeStatement(provider string) string {
	if provider == "ollama" {
		return "local Ollama inference — no external API call, zero charge incurred"
	}
	return "OpenAI-compatible endpoint — charge depends on the endpoint; the study requires a zero-charge instrument (free tier with no payment method, or self-hosted)"
}

// toJSONProvenance converts the provenance pairs for the JSON artifact.
func toJSONProvenance(in []ModelProvenance) []jsonProvenance {
	if len(in) == 0 {
		return nil
	}
	out := make([]jsonProvenance, 0, len(in))
	for _, p := range in {
		out = append(out, jsonProvenance{Model: p.Model, BOM: p.BOM, System: p.System})
	}
	return out
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
	Timestamp       string           `json:"timestamp,omitempty"`
	Provider        string           `json:"provider,omitempty"`
	Models          []string         `json:"models"`
	Concurrency     int              `json:"concurrency"`
	MaxTokens       int              `json:"maxTokens"`
	MaxIters        int              `json:"maxIters"`
	AdHoc           bool             `json:"adHoc"`
	Repeat          int              `json:"repeat"`
	Temperature     float64          `json:"temperature"`
	NumCtx          int              `json:"numCtx,omitempty"`
	Cost            string           `json:"costStatement,omitempty"`
	ModelProvenance []jsonProvenance `json:"modelProvenance,omitempty"`
}

// jsonProvenance mirrors ModelProvenance for the machine-readable artifact.
type jsonProvenance struct {
	Model  string `json:"model"`
	BOM    string `json:"aiBom"`
	System string `json:"hdfSystem"`
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
	HDFVsRaw float64    `json:"hdfVsRaw"` // per-question hdf/raw token multiplier (0 when raw is 0)
	// Per-question bookend ratios, when the run computed bookends: real raw spend
	// over the whole-file ceiling, and real hdf spend over the hand-optimal oracle
	// (nil when the oracle is unreachable).
	RawVsCeiling *float64 `json:"rawVsCeiling,omitempty"`
	HDFVsOracle  *float64 `json:"hdfVsOracle,omitempty"`
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
	BookendCheck  *jsonBookendCheck            `json:"bookendCheck,omitempty"`
}

type jsonRun struct {
	Model     string         `json:"model"`
	Questions []jsonQuestion `json:"questions"`
	Summary   jsonSummary    `json:"summary"`
}

type jsonBookend struct {
	ID          string `json:"id"`
	RawCeiling  int    `json:"rawCeilingTokens"`
	HDFOracle   int    `json:"hdfOracleTokens,omitempty"`
	Unreachable string `json:"oracleUnreachable,omitempty"`
}

type jsonBookendCheck struct {
	RawArmVsCeiling float64 `json:"rawArmVsCeiling"`
	HDFArmVsOracle  float64 `json:"hdfArmVsOracle"`
	Reachable       int     `json:"reachableQuestions"`
}

type jsonReport struct {
	Meta     jsonMeta      `json:"meta"`
	Bookends []jsonBookend `json:"bookends,omitempty"`
	Runs     []jsonRun     `json:"runs"`
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
// artifact carrying run metadata, the model-free bookends, every per-arm token
// count/verdict/scored flag, per-type scored accuracy, the raw arm's
// out-of-remit behavior, and both cost views.
func RenderJSON(meta RunMeta, runs []ModelRun, adHoc bool, bks []Bookend) (string, error) {
	report := jsonReport{Meta: jsonMeta{
		Timestamp: meta.Timestamp, Provider: meta.Provider, Models: meta.Models, Concurrency: meta.Concurrency,
		MaxTokens: meta.MaxTokens, MaxIters: meta.MaxIters, AdHoc: adHoc,
		Repeat: meta.Repeat, Temperature: meta.Temperature, NumCtx: meta.NumCtx,
		Cost: chargeStatement(meta.Provider), ModelProvenance: toJSONProvenance(meta.ModelProvenance),
	}}
	for _, b := range bks {
		report.Bookends = append(report.Bookends, jsonBookend{
			ID: b.ID, RawCeiling: b.RawCeiling, HDFOracle: b.HDFOracle, Unreachable: b.Unreachable,
		})
	}
	bkByID := make(map[string]Bookend, len(bks))
	for _, b := range bks {
		bkByID[b.ID] = b
	}
	for _, run := range runs {
		jr := jsonRun{Model: run.Model}
		for _, r := range run.Results {
			jq := jsonQuestion{
				ID: r.ID, Ask: r.Ask, Type: typeLabel(r.Class),
				RawView: toJSONAnswer(r.RawView), HDFView: toJSONAnswer(r.HDFView),
				Raw: toJSONArm(r.Raw), HDF: toJSONArm(r.HDF),
				HDFVsRaw: ratioFloat(r.HDF.Cost.TotalTokens(), r.Raw.Cost.TotalTokens()),
			}
			if bk, ok := bkByID[r.ID]; ok && bk.RawCeiling > 0 {
				v := ratioFloat(r.Raw.Cost.TotalTokens(), bk.RawCeiling)
				jq.RawVsCeiling = &v
				if bk.Unreachable == "" && bk.HDFOracle > 0 {
					w := ratioFloat(r.HDF.Cost.TotalTokens(), bk.HDFOracle)
					jq.HDFVsOracle = &w
				}
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
		if c, ok := checkBookends(run.Results, bks); ok {
			summary.BookendCheck = &jsonBookendCheck{
				RawArmVsCeiling: ratioFloat(c.RawSpent, c.RawCeiling),
				HDFArmVsOracle:  ratioFloat(c.HDFSpent, c.OracleTotal),
				Reachable:       c.Reachable,
			}
		}
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

// RenderMarkdown renders the run as GitHub-flavored markdown: a metadata header,
// the model-free bookend table, a per-model detail table, a per-type
// scored-accuracy table, the raw arm's out-of-remit line, a cost list, the
// bookend check, and the shared limitations note.
func RenderMarkdown(meta RunMeta, runs []ModelRun, adHoc bool, bks []Bookend) string {
	var b strings.Builder
	b.WriteString("# HDF-MCP benchmark\n\n")
	if meta.Timestamp != "" {
		fmt.Fprintf(&b, "- **timestamp:** %s\n", meta.Timestamp)
	}
	if meta.Provider != "" {
		fmt.Fprintf(&b, "- **provider:** %s\n", meta.Provider)
	}
	fmt.Fprintf(&b, "- **models:** %s\n", strings.Join(meta.Models, ", "))
	fmt.Fprintf(&b, "- **settings:** concurrency=%d, max-tokens=%d, max-iters=%d, ad-hoc=%v, repeat=%d, temperature=%s%s\n",
		meta.Concurrency, meta.MaxTokens, meta.MaxIters, adHoc, meta.Repeat, tempStr(meta.Temperature), numCtxStr(meta.NumCtx, ", "))
	if meta.Provider != "" {
		fmt.Fprintf(&b, "- **cost:** %s\n", chargeStatement(meta.Provider))
	}
	for _, p := range meta.ModelProvenance {
		fmt.Fprintf(&b, "- **provenance (%s):** AI-BOM `%s`, HDF System `%s`\n", p.Model, p.BOM, p.System)
	}
	b.WriteString("\n")

	bkByID := make(map[string]Bookend, len(bks))
	for _, bk := range bks {
		bkByID[bk.ID] = bk
	}
	if len(bks) > 0 {
		b.WriteString("## Bookends (model-free)\n\n")
		b.WriteString("rawCeil = whole raw file(s) tokenized; oracle = the hand-written optimal call's response; idealMult = oracle/rawCeil, directly comparable to each question's real mult.\n\n")
		b.WriteString("| question | rawCeil | oracle | idealMult | note |\n|---|--:|--:|--:|---|\n")
		for _, bk := range bks {
			if bk.Unreachable != "" {
				fmt.Fprintf(&b, "| %s | %d | — | — | oracle unreachable: %s |\n", bk.ID, bk.RawCeiling, bk.Unreachable)
				continue
			}
			fmt.Fprintf(&b, "| %s | %d | %d | %s | |\n", bk.ID, bk.RawCeiling, bk.HDFOracle, multStr(bk.HDFOracle, bk.RawCeiling))
		}
		b.WriteString("\n")
	}

	for _, run := range runs {
		fmt.Fprintf(&b, "## %s (%d questions)\n\n", run.Model, len(run.Results))

		bkCols := ""
		bkDashes := ""
		if len(bks) > 0 {
			bkCols, bkDashes = " vsCeil | vsOracle |", "--:|--:|"
		}
		if adHoc {
			fmt.Fprintf(&b, "| question | type | raw | hdf | rawTok | hdfTok | mult | rawSec | hdfSec |%s adhocTok |\n", bkCols)
			fmt.Fprintf(&b, "|---|---|---|---|--:|--:|--:|--:|--:|%s--:|\n", bkDashes)
		} else {
			fmt.Fprintf(&b, "| question | type | raw | hdf | rawTok | hdfTok | mult | rawSec | hdfSec |%s\n", bkCols)
			fmt.Fprintf(&b, "|---|---|---|---|--:|--:|--:|--:|--:|%s\n", bkDashes)
		}
		for _, r := range run.Results {
			row := fmt.Sprintf("| %s | %s | %s | %s | %s | %s | %s | %.1f | %.1f |",
				r.ID, typeLabel(r.Class), r.Raw.Verdict, r.HDF.Verdict, tokensWithSpread(r.Raw), tokensWithSpread(r.HDF),
				costMultiplier(r),
				r.Raw.Cost.Elapsed.Seconds(), r.HDF.Cost.Elapsed.Seconds())
			if len(bks) > 0 {
				vc, vo := questionBookendRatios(r, bkByID)
				row += fmt.Sprintf(" %s | %s |", vc, vo)
			}
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
		if cav := capCaveat(run.Results, meta.MaxIters); cav != "" {
			fmt.Fprintf(&b, "\n> **%s**\n", cav)
		}

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
			var reasons []string
			for _, arm := range []Arm{ArmRaw, ArmHDF} {
				for _, fr := range failureDetail(run.Results, arm) {
					reasons = append(reasons, fmt.Sprintf("- %s ×%d — %s", armShort(arm), fr.Count, fr.Msg))
				}
			}
			if len(reasons) > 0 {
				b.WriteString("\n**Failure reasons** (first error per failed arm)\n\n")
				b.WriteString(strings.Join(reasons, "\n") + "\n")
			}
		}

		rawTok, hdfTok, adhocTok := totals(run.Results)
		b.WriteString("\n**Token cost** (real prompt+completion usage, tool-schema tax included)\n\n")
		fmt.Fprintf(&b, "- raw-file arm: **%d**\n", rawTok)
		fmt.Fprintf(&b, "- hdf-mcp arm (pipeline): **%d** (%s vs raw)\n", hdfTok, ratio(hdfTok, rawTok))
		if adHoc {
			fmt.Fprintf(&b, "- hdf-mcp arm (ad-hoc convert): **%d** (%s vs raw)\n", adhocTok, ratio(adhocTok, rawTok))
		}

		rawSec, hdfSec, adhocSec := elapsedTotals(run.Results)
		b.WriteString("\n**Wall-clock** (sum of arm latencies, seconds)\n\n")
		fmt.Fprintf(&b, "- raw-file arm: **%.1f**\n", rawSec)
		fmt.Fprintf(&b, "- hdf-mcp arm (pipeline): **%.1f** (%s vs raw)\n", hdfSec, ratioF(hdfSec, rawSec))
		if adHoc {
			fmt.Fprintf(&b, "- hdf-mcp arm (ad-hoc convert): **%.1f** (%s vs raw)\n", adhocSec, ratioF(adhocSec, rawSec))
		}
		if c, ok := checkBookends(run.Results, bks); ok {
			b.WriteString("\n**Bookend check** (real arm spend vs the model-free bookends)\n\n")
			fmt.Fprintf(&b, "- raw arm: **%d** = %.1f%% of the whole-file ceiling (%d)\n",
				c.RawSpent, 100*ratioFloat(c.RawSpent, c.RawCeiling), c.RawCeiling)
			if c.Reachable > 0 {
				fmt.Fprintf(&b, "- hdf arm: **%d** = %s the hand-optimal oracle (%d; over %d reachable questions)\n",
					c.HDFSpent, ratio(c.HDFSpent, c.OracleTotal), c.OracleTotal, c.Reachable)
			}
		}
		b.WriteString("\n")
	}
	b.WriteString(markdownFooter(adHoc, len(bks) > 0))
	return b.String()
}

// markdownFooter renders the text footer's limitations note as a markdown blockquote.
// markdownFooter is the report's pointer to the interpretation guide.
func markdownFooter(adHoc, bookends bool) string {
	var b strings.Builder
	b.WriteString("---\n\n**How to read these numbers** — question types, grading, the two cost views, ")
	b.WriteString("and what they do not settle: [`" + InterpretationDoc + "`](../../" + InterpretationDoc + ")\n")
	if adHoc {
		b.WriteString("\n- Both cost views are reported: *pipeline* assumes conversion happened out of band; *ad-hoc* charges the agent's on-demand `hdf_convert`.\n")
	}
	if bookends {
		b.WriteString("- Bookends are counted with O200k; real usage comes from each model's own tokenizer, so bookend ratios are approximate.\n")
	}
	return b.String()
}
