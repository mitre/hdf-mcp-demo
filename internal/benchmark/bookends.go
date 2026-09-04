package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mitre/hdf-mcp-demo/internal/mcpclient"
	"github.com/mitre/hdf-mcp-demo/internal/tok"
)

// Bookend is one question's model-free token bracket — the two assumptions the
// ingest-ceiling analysis makes, computed per graded question so a real model
// run can be located between them. RawCeiling is the whole-file assumption (every
// raw source byte enters context); HDFOracle is the ideal-play assumption (the
// hand-written optimal call's response). A real raw arm should land under its
// ceiling (grep beats whole-file ingest) and a real HDF arm above its oracle
// (the schema tax and exploration ride only in real usage) — by how much is what
// the bookend check reports.
type Bookend struct {
	ID          string
	RawCeiling  int      // O200k tokens of every raw source file, summed
	HDFOracle   int      // O200k tokens of the oracle responses, summed (0 when unreachable)
	Unreachable string   // why no bounded call answers; mutually exclusive with HDFOracle
	Responses   []string // raw oracle payloads, for the gated ground-truth pin
}

// ComputeBookends stages and normalizes the bank's fixtures under root (exactly
// as a graded run does), then computes each question's bookends: tokenize the raw
// sources, execute the hand-written oracle calls against the live session, and
// tokenize their responses. Entirely model-free — this is token accounting, not
// inference — so it also backs the offline -bookends mode.
func ComputeBookends(ctx context.Context, sess *mcpclient.Session, bin, fixturesDir, root string, bank []Question) ([]Bookend, error) {
	rawBytes, err := stageAndConvert(ctx, sess, bin, fixturesDir, root, bank)
	if err != nil {
		return nil, err
	}
	out := make([]Bookend, 0, len(bank))
	for _, q := range bank {
		b := Bookend{ID: q.ID, Unreachable: q.OracleUnreachable}
		for _, src := range q.Sources {
			n, err := tok.Count(string(rawBytes[src.Fixture]))
			if err != nil {
				return nil, fmt.Errorf("[%s] tokenize %s: %w", q.ID, src.Fixture, err)
			}
			b.RawCeiling += n
		}
		for _, c := range q.Oracle {
			payload, err := sess.Call(ctx, c.Tool, c.Args)
			if err != nil {
				return nil, fmt.Errorf("[%s] oracle %s: %w", q.ID, c.Tool, err)
			}
			n, err := tok.Count(payload)
			if err != nil {
				return nil, fmt.Errorf("[%s] tokenize oracle response: %w", q.ID, err)
			}
			b.HDFOracle += n
			b.Responses = append(b.Responses, payload)
		}
		out = append(out, b)
	}
	return out, nil
}

// OracleReading is the answer-bearing fields of an oracle payload: hdf_query
// reports total (the match count even at limit 1), hdf_compliance reports the
// compliance percentage. The gated oracle test compares these to ground truth.
type OracleReading struct {
	Total         int
	HasTotal      bool
	Compliance    float64
	HasCompliance bool
}

// ReadOracle extracts the answer-bearing fields from a tool payload.
func ReadOracle(payload string) (OracleReading, error) {
	var v struct {
		Total      *int     `json:"total"`
		Compliance *float64 `json:"compliance"`
	}
	if err := json.Unmarshal([]byte(payload), &v); err != nil {
		return OracleReading{}, fmt.Errorf("parse oracle payload: %w", err)
	}
	var r OracleReading
	if v.Total != nil {
		r.Total, r.HasTotal = *v.Total, true
	}
	if v.Compliance != nil {
		r.Compliance, r.HasCompliance = *v.Compliance, true
	}
	return r, nil
}

// RenderBookends formats the run-level bookend table (model-free, so it renders
// once per run, not per model). idealMult is hdfOracle/rawCeiling — the same
// hdf/raw direction as the per-question mult column, so the two are directly
// comparable: mult near idealMult means the model played near-optimally.
func RenderBookends(bks []Bookend) string {
	var b strings.Builder
	b.WriteString("token bookends (model-free: rawCeil = whole raw file(s); oracle = hand-optimal call response):\n")
	hdr := fmt.Sprintf("%-24s %8s %8s %10s   %s", "question", "rawCeil", "oracle", "idealMult", "note")
	b.WriteString(hdr + "\n" + strings.Repeat("-", len(hdr)) + "\n")
	for _, bk := range bks {
		if bk.Unreachable != "" {
			fmt.Fprintf(&b, "%-24s %8d %8s %10s   oracle unreachable: %s\n", trunc(bk.ID, 24), bk.RawCeiling, "—", "—", bk.Unreachable)
			continue
		}
		fmt.Fprintf(&b, "%-24s %8d %8d %10s\n", trunc(bk.ID, 24), bk.RawCeiling, bk.HDFOracle, multStr(bk.HDFOracle, bk.RawCeiling))
	}
	return b.String()
}

// multStr renders an hdf/raw multiplier with two significant figures, so an
// ideal mult of 0.0019x stays legible instead of collapsing to 0.00x.
func multStr(n, d int) string {
	if d == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.2gx", float64(n)/float64(d))
}

// bookendCheck aggregates a model's real spend against the bookends: the raw arm
// against the whole-file ceiling over every bookended question, and the HDF arm
// against the oracle over the reachable ones only (an unreachable oracle has no
// ideal-play cost to compare to).
type bookendCheck struct {
	RawSpent, RawCeiling  int
	HDFSpent, OracleTotal int
	Reachable             int
}

func checkBookends(results []QuestionResult, bks []Bookend) (bookendCheck, bool) {
	if len(bks) == 0 {
		return bookendCheck{}, false
	}
	byID := make(map[string]Bookend, len(bks))
	for _, b := range bks {
		byID[b.ID] = b
	}
	var c bookendCheck
	for _, r := range results {
		bk, ok := byID[r.ID]
		if !ok {
			continue
		}
		c.RawSpent += r.Raw.Cost.TotalTokens()
		c.RawCeiling += bk.RawCeiling
		if bk.Unreachable == "" {
			c.HDFSpent += r.HDF.Cost.TotalTokens()
			c.OracleTotal += bk.HDFOracle
			c.Reachable++
		}
	}
	return c, c.RawCeiling > 0
}

// questionBookendRatios renders one question's two bookend ratios for the detail
// tables: real raw spend over the whole-file ceiling, and real hdf spend over the
// hand-optimal oracle ("—" when the oracle is unreachable).
func questionBookendRatios(r QuestionResult, byID map[string]Bookend) (vsCeil, vsOracle string) {
	bk, ok := byID[r.ID]
	if !ok || bk.RawCeiling == 0 {
		return "n/a", "n/a"
	}
	vsCeil = multStr(r.Raw.Cost.TotalTokens(), bk.RawCeiling)
	if bk.Unreachable != "" || bk.HDFOracle == 0 {
		return vsCeil, "—"
	}
	return vsCeil, multStr(r.HDF.Cost.TotalTokens(), bk.HDFOracle)
}

// renderBookendCheckText is the per-model assumption check appended to the text
// report.
func renderBookendCheckText(c bookendCheck) string {
	var b strings.Builder
	b.WriteString("\nbookend check (real arm spend vs the model-free bookends):\n")
	fmt.Fprintf(&b, "  raw arm ...................... %8d = %.1f%% of the whole-file ceiling (%d)\n",
		c.RawSpent, 100*ratioFloat(c.RawSpent, c.RawCeiling), c.RawCeiling)
	if c.Reachable > 0 {
		fmt.Fprintf(&b, "  hdf arm ...................... %8d = %s the hand-optimal oracle (%d; over %d reachable questions)\n",
			c.HDFSpent, ratio(c.HDFSpent, c.OracleTotal), c.OracleTotal, c.Reachable)
	}
	return b.String()
}
