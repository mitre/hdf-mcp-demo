package benchmark

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mitre/hdf-mcp-demo/internal/truth"
)

// Render formats a model's results: a per-question detail table, per-question-type
// accuracy for each arm (scored only over questions the arm can answer), both
// token-cost views, and an honesty footer. model names the instrument.
func Render(model string, results []QuestionResult, adHoc bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "model: %s   (%d questions)\n\n", model, len(results))

	// Per-question detail.
	hdr := fmt.Sprintf("%-22s %-13s %-13s %-13s %8s %8s", "question", "type", "raw", "hdf", "rawTok", "hdfTok")
	if adHoc {
		hdr += fmt.Sprintf(" %8s", "adhocTok")
	}
	b.WriteString(hdr + "\n" + strings.Repeat("-", len(hdr)) + "\n")
	for _, r := range results {
		line := fmt.Sprintf("%-22s %-13s %-13s %-13s %8d %8d",
			trunc(r.ID, 22), typeLabel(r.Class), string(r.Raw.Verdict), string(r.HDF.Verdict),
			r.Raw.Cost.TotalTokens(), r.HDF.Cost.TotalTokens())
		if adHoc {
			ah := 0
			if r.HDFAdHoc != nil {
				ah = r.HDFAdHoc.Cost.TotalTokens()
			}
			line += fmt.Sprintf(" %8d", ah)
		}
		b.WriteString(line + "\n")
	}

	// Accuracy by type — scored only over the questions each arm can answer, so the
	// raw arm is never penalized on hdf-only questions it isn't meant to answer.
	b.WriteString("\naccuracy by type (correct / questions the arm can answer):\n")
	fmt.Fprintf(&b, "%-26s %-12s %-12s\n", "type", "raw-file", "hdf-mcp")
	b.WriteString(strings.Repeat("-", 50) + "\n")
	for _, c := range []truth.Class{truth.ClassA, truth.ClassB, truth.ClassC} {
		sub := filterClass(results, c)
		if len(sub) == 0 {
			continue
		}
		rc, rn := scoredAccuracy(sub, ArmRaw)
		hc, hn := scoredAccuracy(sub, ArmHDF)
		fmt.Fprintf(&b, "%-26s %-12s %-12s\n", classRowLabel(c), frac(rc, rn), frac(hc, hn))
	}
	rc, rn := scoredAccuracy(results, ArmRaw)
	hc, hn := scoredAccuracy(results, ArmHDF)
	fmt.Fprintf(&b, "%-26s %-12s %-12s\n", "ALL (scored)", frac(rc, rn), frac(hc, hn))

	// Out-of-remit behavior: on hdf-only questions the raw arm can't answer, did it
	// correctly abstain or invent a plausible-but-wrong answer? (Informational — not
	// scored. Frequent hallucination here is itself a point FOR HDF.)
	if hall, abst, outOf := remit(results, ArmRaw); outOf > 0 {
		fmt.Fprintf(&b, "\nraw-file arm on hdf-only questions (out of remit, not scored): %d hallucinated / %d abstained of %d\n",
			hall, abst, outOf)
	}

	// Failed arms (errored/over-context/timeout) among scored questions.
	if rf, hf := failures(results, ArmRaw), failures(results, ArmHDF); rf > 0 || hf > 0 {
		fmt.Fprintf(&b, "failed (errored/over-context/timeout): raw %d, hdf %d\n", rf, hf)
	}

	// Cost views.
	rawTok, hdfTok, adhocTok := totals(results)
	b.WriteString("\ntoken cost (real prompt+completion usage, tool-schema tax included):\n")
	fmt.Fprintf(&b, "  raw-file arm ................. %8d\n", rawTok)
	fmt.Fprintf(&b, "  hdf-mcp arm (pipeline) ....... %8d   %s vs raw\n", hdfTok, ratio(hdfTok, rawTok))
	if adHoc {
		fmt.Fprintf(&b, "  hdf-mcp arm (ad-hoc convert) . %8d   %s vs raw\n", adhocTok, ratio(adhocTok, rawTok))
	}

	b.WriteString(footer(adHoc))
	return b.String()
}

// typeLabel is the compact, non-hierarchical name for a question's grading nature.
// The A/B/C classes are an internal code; readers see descriptive words that don't
// imply one type is "better" than another.
func typeLabel(c truth.Class) string {
	switch c {
	case truth.ClassA:
		return "objective"
	case truth.ClassB:
		return "interpretive"
	default:
		return "hdf-only"
	}
}

// classRowLabel is typeLabel plus a short gloss, for the accuracy table.
func classRowLabel(c truth.Class) string {
	switch c {
	case truth.ClassA:
		return "objective (shared fact)"
	case truth.ClassB:
		return "interpretive (to intent)"
	default:
		return "hdf-only (raw n/a)"
	}
}

func footer(adHoc bool) string {
	lines := []string{
		"",
		"notes / limitations (read before trusting a number):",
		"  - Question types are a grading distinction, NOT a ranking. 'objective': one",
		"    answer both arms should reach. 'interpretive': the fair answer depends on the",
		"    question's intent, graded bidirectionally (e.g. distinct rule violations vs raw",
		"    finding volume). 'hdf-only': raw scanners can't natively express it (compliance",
		"    %, effective status) — the raw arm is out of remit and not scored on these.",
		"  - Accuracy is scored only over questions an arm can answer. On hdf-only questions",
		"    the raw arm's hallucinate-vs-abstain is reported separately, not as a failure.",
		"  - Grading parses an 'ANSWER: <value>' line; a correct answer buried in prose",
		"    without that line may read as abstained. Grading favors abstention over false",
		"    credit. No LLM judge is used.",
		"  - Cost is real endpoint token usage; the hdf-mcp prompt tokens already include",
		"    the tool-schema tax. Small scans can make HDF cost MORE — expected, and the",
		"    point of measuring rather than assuming.",
		"  - A 'failed' arm errored or timed out before answering. The raw arm uses grep +",
		"    paginated reads with NO format hints, so it must discover the schema itself.",
		"  - N is small and a single run is noisy; treat as directional, not definitive.",
	}
	if adHoc {
		lines = append(lines,
			"  - Pipeline view assumes conversion happened out-of-band (cost ~0/query); ad-hoc",
			"    view charges the agent's on-demand hdf_convert round-trip.")
	}
	return strings.Join(lines, "\n") + "\n"
}

func filterClass(rs []QuestionResult, c truth.Class) []QuestionResult {
	var out []QuestionResult
	for _, r := range rs {
		if r.Class == c {
			out = append(out, r)
		}
	}
	return out
}

func armOf(r QuestionResult, a Arm) ArmResult {
	if a == ArmHDF {
		return r.HDF
	}
	return r.Raw
}

// scoredAccuracy returns correct answers and the number of in-remit (scored)
// questions for an arm — questions out of the arm's remit are excluded.
func scoredAccuracy(rs []QuestionResult, arm Arm) (correct, scored int) {
	for _, r := range rs {
		a := armOf(r, arm)
		if !a.Scored {
			continue
		}
		scored++
		if a.Verdict == Correct {
			correct++
		}
	}
	return
}

// remit reports, over an arm's OUT-of-remit questions, how many it answered anyway
// (hallucinated) vs correctly declined (abstained), and the out-of-remit total.
func remit(rs []QuestionResult, arm Arm) (hallucinated, abstained, outOf int) {
	for _, r := range rs {
		a := armOf(r, arm)
		if a.Scored {
			continue
		}
		outOf++
		switch a.Verdict {
		case Hallucinated:
			hallucinated++
		case Abstained:
			abstained++
		}
	}
	return
}

// failures counts arms that errored or timed out before answering.
func failures(rs []QuestionResult, arm Arm) int {
	n := 0
	for _, r := range rs {
		if armOf(r, arm).Verdict == Failed {
			n++
		}
	}
	return n
}

func totals(rs []QuestionResult) (raw, hdf, adhoc int) {
	for _, r := range rs {
		raw += r.Raw.Cost.TotalTokens()
		hdf += r.HDF.Cost.TotalTokens()
		if r.HDFAdHoc != nil {
			adhoc += r.HDFAdHoc.Cost.TotalTokens()
		}
	}
	return
}

func frac(n, d int) string {
	if d == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%d/%d (%d%%)", n, d, n*100/d)
}

func ratio(n, d int) string {
	if d == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.2fx", float64(n)/float64(d))
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// SortByID orders results deterministically for stable output.
func SortByID(rs []QuestionResult) {
	sort.Slice(rs, func(i, j int) bool { return rs[i].ID < rs[j].ID })
}
