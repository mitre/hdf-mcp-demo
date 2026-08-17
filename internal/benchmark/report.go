package benchmark

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mitre/hdf-mcp-demo/internal/truth"
)

// Render formats a model's results: a per-question detail table, per-class
// accuracy for each arm, both token-cost views, and an honesty footer stating the
// method's limitations. model names the instrument. adHoc reflects whether the
// conversion-included view was measured.
func Render(model string, results []QuestionResult, adHoc bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "model: %s   (%d questions)\n\n", model, len(results))

	// Per-question detail.
	if adHoc {
		fmt.Fprintf(&b, "%-22s %-2s %-9s %-9s %8s %8s %8s\n", "question", "cl", "raw", "hdf", "rawTok", "hdfTok", "adhocTok")
	} else {
		fmt.Fprintf(&b, "%-22s %-2s %-9s %-9s %8s %8s\n", "question", "cl", "raw", "hdf", "rawTok", "hdfTok")
	}
	b.WriteString(strings.Repeat("-", ruleWidth(adHoc)) + "\n")
	for _, r := range results {
		if adHoc {
			ah := 0
			if r.HDFAdHoc != nil {
				ah = r.HDFAdHoc.Cost.TotalTokens()
			}
			fmt.Fprintf(&b, "%-22s %-2s %-9s %-9s %8d %8d %8d\n",
				trunc(r.ID, 22), string(r.Class), string(r.Raw.Verdict), string(r.HDF.Verdict),
				r.Raw.Cost.TotalTokens(), r.HDF.Cost.TotalTokens(), ah)
		} else {
			fmt.Fprintf(&b, "%-22s %-2s %-9s %-9s %8d %8d\n",
				trunc(r.ID, 22), string(r.Class), string(r.Raw.Verdict), string(r.HDF.Verdict),
				r.Raw.Cost.TotalTokens(), r.HDF.Cost.TotalTokens())
		}
	}

	// Accuracy by class.
	b.WriteString("\naccuracy by class (correct / questions):\n")
	fmt.Fprintf(&b, "%-30s %-12s %-12s\n", "class", "raw-file", "hdf-mcp")
	b.WriteString(strings.Repeat("-", 54) + "\n")
	for _, c := range []truth.Class{truth.ClassA, truth.ClassB, truth.ClassC} {
		sub := filterClass(results, c)
		if len(sub) == 0 {
			continue
		}
		rc, hc := correct(sub, ArmRaw), correct(sub, ArmHDF)
		fmt.Fprintf(&b, "%-30s %-12s %-12s\n", classLabel(c), frac(rc, len(sub)), frac(hc, len(sub)))
	}
	rc, hc := correct(results, ArmRaw), correct(results, ArmHDF)
	fmt.Fprintf(&b, "%-30s %-12s %-12s\n", "ALL", frac(rc, len(results)), frac(hc, len(results)))

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

func ruleWidth(adHoc bool) int {
	if adHoc {
		return 72
	}
	return 63
}

func footer(adHoc bool) string {
	lines := []string{
		"",
		"notes / limitations (read before trusting a number):",
		"  - Class A grades both arms to the same value; Class B grades to the question's",
		"    stated intent (bidirectional — some favor raw volume, some favor HDF dedup);",
		"    Class C has no raw-answerable truth, so a concrete raw answer counts as a",
		"    hallucination (wrong), an abstention as 'abstained'.",
		"  - Grading parses an 'ANSWER: <value>' line; a correct answer buried in prose",
		"    without that line may read as abstained. Grading favors abstention over false",
		"    credit. No LLM judge is used.",
		"  - Cost is real endpoint token usage; the hdf-mcp prompt tokens already include",
		"    the tool-schema tax. Small scans can make HDF cost MORE — that is expected and",
		"    is the point of measuring rather than assuming.",
		"  - N is small and a single run is noisy; treat as directional, not definitive.",
	}
	if adHoc {
		lines = append(lines,
			"  - Pipeline view assumes conversion happened out-of-band (cost ~0/query); ad-hoc",
			"    view charges the agent's on-demand hdf_convert round-trip.")
	}
	return strings.Join(lines, "\n") + "\n"
}

func classLabel(c truth.Class) string {
	switch c {
	case truth.ClassA:
		return "A answer-preserving"
	case truth.ClassB:
		return "B normalization-divergent"
	default:
		return "C HDF-native"
	}
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

func correct(rs []QuestionResult, arm Arm) int {
	n := 0
	for _, r := range rs {
		v := r.Raw.Verdict
		if arm == ArmHDF {
			v = r.HDF.Verdict
		}
		if v == Correct {
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
		return "-"
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
