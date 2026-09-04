package benchmark

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/mitre/hdf-mcp-demo/internal/truth"
)

// Render formats a model's results: a per-question detail table, per-question-type
// accuracy for each arm (scored only over questions the arm can answer), both
// token-cost views, the bookend check (when bks is non-nil), and an honesty
// footer. model names the instrument.
func Render(model string, results []QuestionResult, adHoc bool, bks []Bookend) string {
	var b strings.Builder
	fmt.Fprintf(&b, "model: %s   (%d questions)\n\n", model, len(results))

	// Per-question detail. mult is that question's hdfTok/rawTok — the aggregate
	// cost ratio hides which questions are the peaks and valleys. When bookends
	// were computed, vsCeil and vsOracle locate each arm against its model-free
	// assumption (raw spend / whole-file ceiling; hdf spend / hand-optimal oracle).
	bkByID := make(map[string]Bookend, len(bks))
	for _, bk := range bks {
		bkByID[bk.ID] = bk
	}
	hdr := fmt.Sprintf("%-22s %-13s %-13s %-13s %12s %12s %7s %7s %7s", "question", "type", "raw", "hdf", "rawTok", "hdfTok", "mult", "rawSec", "hdfSec")
	if len(bks) > 0 {
		hdr += fmt.Sprintf(" %8s %8s", "vsCeil", "vsOracle")
	}
	if adHoc {
		hdr += fmt.Sprintf(" %8s", "adhocTok")
	}
	b.WriteString(hdr + "\n" + strings.Repeat("-", len(hdr)) + "\n")
	for _, r := range results {
		line := fmt.Sprintf("%-22s %-13s %-13s %-13s %12s %12s %7s %7.1f %7.1f",
			trunc(r.ID, 22), typeLabel(r.Class), string(r.Raw.Verdict), string(r.HDF.Verdict),
			tokensWithSpread(r.Raw), tokensWithSpread(r.HDF),
			costMultiplier(r),
			r.Raw.Cost.Elapsed.Seconds(), r.HDF.Cost.Elapsed.Seconds())
		if len(bks) > 0 {
			vc, vo := questionBookendRatios(r, bkByID)
			line += fmt.Sprintf(" %8s %8s", vc, vo)
		}
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
	for _, c := range allClasses {
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
	if hall, abst, outOf := remit(results, ArmHDF); outOf > 0 {
		fmt.Fprintf(&b, "hdf-mcp arm on raw-only questions (out of remit, not scored): %d hallucinated / %d abstained of %d\n",
			hall, abst, outOf)
	}

	// Failed arms (errored/over-context/timeout) among scored questions, followed
	// by the distinct reasons so an all-failed run is diagnosable ("connection
	// refused" vs. "reached max iterations") rather than a wall of "failed".
	if rf, hf := failures(results, ArmRaw), failures(results, ArmHDF); rf > 0 || hf > 0 {
		fmt.Fprintf(&b, "failed (errored/over-context/timeout): raw %d, hdf %d\n", rf, hf)
		for _, arm := range []Arm{ArmRaw, ArmHDF} {
			for _, fr := range failureDetail(results, arm) {
				fmt.Fprintf(&b, "  %-3s %d×  %s\n", armShort(arm), fr.Count, fr.Msg)
			}
		}
	}

	// Cost views.
	rawTok, hdfTok, adhocTok := totals(results)
	b.WriteString("\ntoken cost (real prompt+completion usage, tool-schema tax included):\n")
	fmt.Fprintf(&b, "  raw-file arm ................. %8d\n", rawTok)
	fmt.Fprintf(&b, "  hdf-mcp arm (pipeline) ....... %8d   %s vs raw\n", hdfTok, ratio(hdfTok, rawTok))
	if adHoc {
		fmt.Fprintf(&b, "  hdf-mcp arm (ad-hoc convert) . %8d   %s vs raw\n", adhocTok, ratio(adhocTok, rawTok))
	}

	// Wall-clock, reported next to tokens because they can disagree: a bounded
	// tool response is cheap in tokens yet costs a round trip, and locally the
	// round trip is often what the user actually waits on.
	rawSec, hdfSec, adhocSec := elapsedTotals(results)
	b.WriteString("\nwall-clock (sum of arm latencies, seconds):\n")
	fmt.Fprintf(&b, "  raw-file arm ................. %8.1f\n", rawSec)
	fmt.Fprintf(&b, "  hdf-mcp arm (pipeline) ....... %8.1f   %s vs raw\n", hdfSec, ratioF(hdfSec, rawSec))
	if adHoc {
		fmt.Fprintf(&b, "  hdf-mcp arm (ad-hoc convert) . %8.1f   %s vs raw\n", adhocSec, ratioF(adhocSec, rawSec))
	}

	if c, ok := checkBookends(results, bks); ok {
		b.WriteString(renderBookendCheckText(c))
	}

	b.WriteString(footer(adHoc, len(bks) > 0))
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
	case truth.ClassD:
		return "raw-only"
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
	case truth.ClassD:
		return "raw-only (hdf n/a)"
	default:
		return "hdf-only (raw n/a)"
	}
}

// allClasses is the display order for the accuracy tables.
var allClasses = []truth.Class{truth.ClassA, truth.ClassB, truth.ClassC, truth.ClassD}

// InterpretationDoc is where the long-form guidance lives. Reports link to it
// rather than restating it: the notes are reference material that changes with
// the methodology, and twenty-odd quoted lines appended to every artifact a run
// produces is a copy that goes stale in every one of them independently.
const InterpretationDoc = "docs/interpreting-results.md"

// footer is the short pointer a report carries in place of the full notes.
func footer(adHoc, bookends bool) string {
	var b strings.Builder
	b.WriteString("\nhow to read these numbers — question types, grading, the two cost views,\n")
	b.WriteString("and what they do not settle: " + InterpretationDoc + "\n")
	if adHoc {
		b.WriteString("  (this run reports both cost views: pipeline assumes conversion happened\n")
		b.WriteString("   out of band; ad-hoc charges the agent's on-demand hdf_convert)\n")
	}
	if bookends {
		b.WriteString("  (bookends are O200k-counted; real usage uses each model's own tokenizer,\n")
		b.WriteString("   so bookend ratios are approximate)\n")
	}
	return b.String()
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

// scoredAccuracy returns correct answers and the number of scored samples for an
// arm — summed over runs (Samples) so a -repeat run reports a sample-level rate.
// Questions out of the arm's remit are excluded.
func scoredAccuracy(rs []QuestionResult, arm Arm) (correct, scored int) {
	for _, r := range rs {
		a := armOf(r, arm)
		if !a.Scored {
			continue
		}
		scored += a.Samples
		correct += a.Correct
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

// failureReason is one distinct error message among an arm's failed questions,
// with how many questions shared it.
type failureReason struct {
	Msg   string
	Count int
}

// failureDetail collects the distinct errors among an arm's failed questions,
// most frequent first (message-sorted within a count for determinism). A single
// endpoint outage collapses to one line; a mix of transport errors and max-iters
// timeouts shows each. This is what turns the report's bare "failed" count into
// something a reader can act on.
func failureDetail(rs []QuestionResult, arm Arm) []failureReason {
	counts := map[string]int{}
	for _, r := range rs {
		a := armOf(r, arm)
		if a.Verdict != Failed {
			continue
		}
		msg := a.Err
		if msg == "" {
			msg = "(no error recorded)"
		}
		counts[msg]++
	}
	out := make([]failureReason, 0, len(counts))
	for m, c := range counts {
		out = append(out, failureReason{Msg: m, Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Msg < out[j].Msg
	})
	return out
}

// armShort is the compact arm tag used in the failure-reason lines.
func armShort(a Arm) string {
	if a == ArmHDF {
		return "hdf"
	}
	return "raw"
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

// costMultiplier is hdfTok/rawTok, shown only when BOTH arms produced an answer.
//
// A ratio between an arm that answered and one that gave up measures nothing.
// It is not even conservatively wrong: in this study a failed arm averaged 7,955
// tokens against 4,552 for a correct one, because failure burns the whole
// iteration budget — so a mismatched pair can make either side look efficient
// depending on which one quit. Printing "—" says that plainly.
func costMultiplier(r QuestionResult) string {
	if !answered(r.Raw.Verdict) || !answered(r.HDF.Verdict) {
		return "—"
	}
	return ratio(r.HDF.Cost.TotalTokens(), r.Raw.Cost.TotalTokens())
}

// answered reports whether an arm produced an answer at all — correct, wrong, or
// out-of-remit-but-asserted. Abstaining and failing are both "no answer".
func answered(v Verdict) bool {
	return v == Correct || v == Wrong || v == Hallucinated
}

// tokensWithSpread renders an arm's mean token cost, carrying the spread across
// trials when the question was repeated. A K>1 run whose report looks identical
// to a single run invites reading a mean as an exact measurement.
func tokensWithSpread(a ArmResult) string {
	if a.TokenStdDev < 0.5 {
		return strconv.Itoa(a.Cost.TotalTokens())
	}
	return fmt.Sprintf("%d±%.0f", a.Cost.TotalTokens(), a.TokenStdDev)
}

// elapsedTotals sums each arm's wall-clock across questions.
func elapsedTotals(rs []QuestionResult) (raw, hdf, adhoc float64) {
	for _, r := range rs {
		raw += r.Raw.Cost.Elapsed.Seconds()
		hdf += r.HDF.Cost.Elapsed.Seconds()
		if r.HDFAdHoc != nil {
			adhoc += r.HDFAdHoc.Cost.Elapsed.Seconds()
		}
	}
	return
}

// ratioF is ratio for float measures.
func ratioF(n, d float64) string {
	if d == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.2fx", n/d)
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
