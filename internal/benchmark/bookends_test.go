package benchmark

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/mitre/hdf-mcp-demo/internal/mcpclient"
	"github.com/mitre/hdf-mcp-demo/internal/truth"
)

// Every bank question must either carry a hand-written oracle (the ideal-play
// bookend) or an explicit reason no bounded call can answer it. Both unreachable
// questions are pinned by name: gosec-total-findings asks for result-level volume
// and the read surface projects requirement counts only; grype-related-vulns
// lives in the unprojected `code` field. If a tool change ever makes one
// answerable, this test forces the oracle to be written rather than left off.
func TestBankOracles(t *testing.T) {
	wantUnreachable := map[string]bool{"gosec-total-findings": true, "grype-related-vulns": true}
	for _, q := range Bank() {
		hasOracle := len(q.Oracle) > 0
		hasReason := q.OracleUnreachable != ""
		if hasOracle == hasReason {
			t.Errorf("%s: want exactly one of Oracle / OracleUnreachable (oracle=%v reason=%q)", q.ID, hasOracle, q.OracleUnreachable)
		}
		if wantUnreachable[q.ID] != hasReason {
			t.Errorf("%s: unreachable=%v, want %v", q.ID, hasReason, wantUnreachable[q.ID])
		}
	}
}

// ReadOracle pulls the answer-bearing fields out of a tool payload.
func TestReadOracle(t *testing.T) {
	r, err := ReadOracle(`{"handle":"h1","total":89,"returned":1,"requirements":[{"id":"x"}]}`)
	if err != nil || !r.HasTotal || r.Total != 89 {
		t.Errorf("query payload: %+v, err %v; want total 89", r, err)
	}
	r, err = ReadOracle(`{"handle":"h1","compliance":33.33,"counts":{}}`)
	if err != nil || !r.HasCompliance || math.Abs(r.Compliance-33.33) > 1e-9 {
		t.Errorf("compliance payload: %+v, err %v; want compliance 33.33", r, err)
	}
	if _, err := ReadOracle(`not json`); err == nil {
		t.Error("want error on malformed payload")
	}
}

// The bookend sections must render in every format: the run-level table (with an
// unreachable question showing its reason, never a number), and the per-model
// check lines comparing real arm spend to the two bookends.
func TestBookendsRendered(t *testing.T) {
	bks := []Bookend{
		{ID: "q-a", RawCeiling: 1000, HDFOracle: 100},
		{ID: "q-b", RawCeiling: 2000, HDFOracle: 50},
		{ID: "q-c", RawCeiling: 3000, Unreachable: "field not projected"},
	}
	runs := sampleRuns() // q-a, q-b, q-c: raw 110 / hdf 212 tokens each

	table := RenderBookends(bks)
	for _, want := range []string{"q-a", "1000", "100", "field not projected"} {
		if !strings.Contains(table, want) {
			t.Errorf("bookend table missing %q:\n%s", want, table)
		}
	}

	// Real raw total 330 of ceiling 6000 = 5.5%. Real hdf on reachable (q-a,q-b) =
	// 424 of oracle 150 = 2.83x. Per question: q-a raw 110/1000 = 0.11x, hdf
	// 212/100 = 2.1x; unreachable q-c renders an em dash, never a number.
	text := Render(runs[0].Model, runs[0].Results, false, bks)
	for _, want := range []string{"bookend check", "5.5%", "2.83x", "vsCeil", "vsOracle", "0.11x", "2.1x", "—",
		"O200k", "tool-schema tax"} {
		if !strings.Contains(text, want) {
			t.Errorf("text report missing %q:\n%s", want, text)
		}
	}

	md := RenderMarkdown(RunMeta{Models: []string{"stub"}}, runs, false, bks)
	for _, want := range []string{"Bookends", "field not projected", "2.83x", "vsCeil | vsOracle", "0.11x", "O200k"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown report missing %q:\n%s", want, md)
		}
	}

	js, err := RenderJSON(RunMeta{Models: []string{"stub"}}, runs, false, bks)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Bookends []struct {
			ID          string `json:"id"`
			RawCeiling  int    `json:"rawCeilingTokens"`
			HDFOracle   int    `json:"hdfOracleTokens,omitempty"`
			Unreachable string `json:"oracleUnreachable,omitempty"`
		} `json:"bookends"`
		Runs []struct {
			Questions []struct {
				ID           string   `json:"id"`
				RawVsCeiling *float64 `json:"rawVsCeiling"`
				HDFVsOracle  *float64 `json:"hdfVsOracle"`
			} `json:"questions"`
			Summary struct {
				BookendCheck *struct {
					RawArmVsCeiling float64 `json:"rawArmVsCeiling"`
					HDFArmVsOracle  float64 `json:"hdfArmVsOracle"`
					Reachable       int     `json:"reachableQuestions"`
				} `json:"bookendCheck"`
			} `json:"summary"`
		} `json:"runs"`
	}
	if err := json.Unmarshal([]byte(js), &got); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if len(got.Bookends) != 3 || got.Bookends[2].Unreachable == "" || got.Bookends[2].HDFOracle != 0 {
		t.Errorf("json bookends = %+v; want 3 with q-c unreachable and no oracle number", got.Bookends)
	}
	bc := got.Runs[0].Summary.BookendCheck
	if bc == nil || math.Abs(bc.RawArmVsCeiling-0.055) > 0.001 || math.Abs(bc.HDFArmVsOracle-424.0/150.0) > 0.001 || bc.Reachable != 2 {
		t.Errorf("json bookendCheck = %+v; want raw 0.055, hdf 2.827, reachable 2", bc)
	}
	// Per-question ratios: q-a raw 110/1000, hdf 212/100; unreachable q-c has a
	// ceiling ratio but no oracle ratio.
	qs := got.Runs[0].Questions
	if qs[0].RawVsCeiling == nil || math.Abs(*qs[0].RawVsCeiling-0.11) > 0.001 ||
		qs[0].HDFVsOracle == nil || math.Abs(*qs[0].HDFVsOracle-2.12) > 0.001 {
		t.Errorf("q-a per-question ratios wrong: %+v", qs[0])
	}
	if qs[2].RawVsCeiling == nil || qs[2].HDFVsOracle != nil {
		t.Errorf("q-c should have a ceiling ratio and no oracle ratio: %+v", qs[2])
	}

	// Renderers without bookends must not emit the sections (old callers pass nil).
	if s := Render(runs[0].Model, runs[0].Results, false, nil); strings.Contains(s, "bookend") {
		t.Errorf("nil bookends should omit the check:\n%s", s)
	}
}

// TestOracles_MatchGroundTruth is the steelman guard, against the live server:
// every reachable oracle's RESPONSE must contain the answer the ground truth
// computes — an "optimal call" that does not actually answer its question would
// make the published ceiling a fabrication. Gated on HDF_BIN like the other
// integration tests.
func TestOracles_MatchGroundTruth(t *testing.T) {
	bin := os.Getenv("HDF_BIN")
	if bin == "" {
		p, err := exec.LookPath("hdf")
		if err != nil {
			t.Skip("set HDF_BIN=/path/to/hdf (or put hdf on PATH) to run the oracle pins")
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

	bank := Bank()
	bks, err := ComputeBookends(context.Background(), sess, bin, "../../fixtures", root, bank)
	if err != nil {
		t.Fatalf("compute bookends: %v", err)
	}
	byID := map[string]Bookend{}
	for _, b := range bks {
		byID[b.ID] = b
	}

	for _, q := range bank {
		q := q
		t.Run(q.ID, func(t *testing.T) {
			bk, ok := byID[q.ID]
			if !ok {
				t.Fatalf("no bookend computed")
			}
			if bk.RawCeiling <= 0 {
				t.Errorf("rawCeiling = %d, want > 0", bk.RawCeiling)
			}
			if q.OracleUnreachable != "" {
				if bk.HDFOracle != 0 || len(bk.Responses) != 0 {
					t.Errorf("unreachable question has oracle data: %+v", bk)
				}
				return
			}
			if bk.HDFOracle <= 0 || len(bk.Responses) == 0 {
				t.Fatalf("reachable oracle produced no data: %+v", bk)
			}

			// The fair HDF-arm key for this question, computed from the data.
			rawDocs, hdfDocs := loadDocs(t, root, q)
			cls, err := truth.Classify(q.Truth, rawDocs, hdfDocs)
			if err != nil {
				t.Fatal(err)
			}
			key := q.key(cls.Class, cls.RawAnswer, cls.HDFAnswer, ArmHDF)
			if !key.Answerable {
				t.Fatalf("reachable oracle on a question whose hdf key is unanswerable — mark it unreachable instead")
			}

			r, err := ReadOracle(bk.Responses[len(bk.Responses)-1])
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case r.HasCompliance:
				want, err := strconv.Atoi(key.Value)
				if err != nil {
					t.Fatal(err)
				}
				if math.Abs(r.Compliance-float64(want)) > 1.0 {
					t.Errorf("oracle compliance %.2f vs ground truth %d — the ideal call does not answer the question", r.Compliance, want)
				}
			case q.Kind == KindBool:
				if got := strconv.FormatBool(r.Total > 0); got != key.Value {
					t.Errorf("oracle presence %v vs ground truth %s", got, key.Value)
				}
			default:
				if got := strconv.Itoa(r.Total); got != key.Value {
					t.Errorf("oracle total %s vs ground truth %s — the ideal call does not answer the question", got, key.Value)
				}
			}
		})
	}
}

// loadDocs reads a question's raw and converted documents from the fixtures dir
// and the run root (ComputeBookends already staged and converted them).
func loadDocs(t *testing.T, root string, q Question) (raw, hdf [][]byte) {
	t.Helper()
	for _, src := range q.Sources {
		rb, err := os.ReadFile("../../fixtures/" + src.Fixture)
		if err != nil {
			t.Fatal(err)
		}
		hb, err := os.ReadFile(root + "/" + src.HDFName)
		if err != nil {
			t.Fatal(err)
		}
		raw, hdf = append(raw, rb), append(hdf, hb)
	}
	return
}
