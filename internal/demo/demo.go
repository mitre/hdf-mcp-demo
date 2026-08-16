// Package demo is the token-efficiency comparison engine: it normalizes real
// scan fixtures to HDF through the shipped MCP server, then, for each question,
// measures the tokens a raw-file agent must ingest (the whole source file[s])
// against the tokens of the bounded HDF MCP tool response(s) that answer it.
//
// The comparison is offline and model-free — pure token accounting with the same
// O200k encoding the server budgets against — so the numbers are reproducible and
// zero-cost. It is a demonstration of ingest cost, NOT end-to-end agent cost
// (see the amortized-vs-one-shot note in the README).
package demo

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mitre/hdf-mcp-demo/internal/mcpclient"
	"github.com/mitre/hdf-mcp-demo/internal/tok"
)

//go:embed questions.json
var questionsJSON []byte

// Fixture is a raw source scan and how to normalize it to HDF via hdf_convert.
type Fixture struct {
	Raw   string `json:"raw"`   // raw filename (under the fixtures dir and staged under the root)
	HDF   string `json:"hdf"`   // HDF output filename written under the root
	From  string `json:"from"`  // hdf_convert source format
	Label string `json:"label"` // human label (SAST/DAST/vuln/...)
}

// Call is one MCP tool invocation answering a question.
type Call struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args"`
}

// Question pairs the raw files a raw-file agent must read with the HDF MCP calls
// that answer the same question against normalized HDF.
type Question struct {
	Category string   `json:"category"`
	Prompt   string   `json:"prompt"`
	RawFiles []string `json:"rawFiles"`
	Calls    []Call   `json:"calls"`
}

// Bank is the demo corpus: fixtures to normalize + the question set.
type Bank struct {
	Fixtures  []Fixture  `json:"fixtures"`
	Questions []Question `json:"questions"`
}

// Row is one measured comparison.
type Row struct {
	Category  string
	Prompt    string
	RawTokens int
	HDFTokens int
	Ratio     float64
}

// LoadBank returns the embedded question bank.
func LoadBank() (Bank, error) {
	var b Bank
	if err := json.Unmarshal(questionsJSON, &b); err != nil {
		return Bank{}, fmt.Errorf("parse questions.json: %w", err)
	}
	return b, nil
}

// Run stages the raw fixtures under root, normalizes each to HDF via the shipped
// MCP server (hdf_convert), then measures raw-vs-HDF token cost per question. The
// session must already be connected with HDF_MCP_ROOT=root and writes enabled.
func Run(ctx context.Context, sess *mcpclient.Session, fixturesDir, root string, bank Bank) ([]Row, error) {
	rawTokens := map[string]int{}
	for _, f := range bank.Fixtures {
		b, err := os.ReadFile(filepath.Join(fixturesDir, f.Raw))
		if err != nil {
			return nil, fmt.Errorf("read fixture %s: %w", f.Raw, err)
		}
		if err := os.WriteFile(filepath.Join(root, f.Raw), b, 0o600); err != nil {
			return nil, fmt.Errorf("stage fixture %s: %w", f.Raw, err)
		}
		n, err := tok.Count(string(b))
		if err != nil {
			return nil, err
		}
		rawTokens[f.Raw] = n

		if _, err := sess.Call(ctx, "hdf_convert", map[string]any{
			"source": map[string]any{"path": f.Raw}, "from": f.From, "output": f.HDF,
		}); err != nil {
			return nil, fmt.Errorf("normalize %s: %w", f.Raw, err)
		}
	}

	var rows []Row
	for _, q := range bank.Questions {
		raw := 0
		for _, rf := range q.RawFiles {
			raw += rawTokens[rf]
		}
		hdf := 0
		for _, c := range q.Calls {
			payload, err := sess.Call(ctx, c.Tool, c.Args)
			if err != nil {
				return nil, fmt.Errorf("[%s] %s: %w", q.Category, c.Tool, err)
			}
			n, err := tok.Count(payload)
			if err != nil {
				return nil, err
			}
			hdf += n
		}
		ratio := 0.0
		if hdf > 0 {
			ratio = float64(raw) / float64(hdf)
		}
		rows = append(rows, Row{q.Category, q.Prompt, raw, hdf, ratio})
	}
	return rows, nil
}

// RenderTable formats the rows as a fixed-width table plus a total line.
func RenderTable(rows []Row) string {
	var b strings.Builder
	b.WriteString("HDF-MCP vs. raw-file token cost (lower HDF is better; ratio = raw/HDF)\n")
	b.WriteString(fmt.Sprintf("%-28s %10s %10s %9s\n", "category", "raw tok", "HDF tok", "raw/HDF"))
	b.WriteString(strings.Repeat("-", 60) + "\n")
	var totalRaw, totalHDF int
	for _, r := range rows {
		b.WriteString(fmt.Sprintf("%-28s %10d %10d %8.1fx\n", r.Category, r.RawTokens, r.HDFTokens, r.Ratio))
		totalRaw += r.RawTokens
		totalHDF += r.HDFTokens
	}
	b.WriteString(strings.Repeat("-", 60) + "\n")
	overall := 0.0
	if totalHDF > 0 {
		overall = float64(totalRaw) / float64(totalHDF)
	}
	b.WriteString(fmt.Sprintf("%-28s %10d %10d %8.1fx\n", "TOTAL", totalRaw, totalHDF, overall))
	return b.String()
}

// SmallestAdvantage returns the row with the lowest raw/HDF ratio — the honesty
// check on where HDF's token edge is thinnest.
func SmallestAdvantage(rows []Row) Row {
	sorted := append([]Row(nil), rows...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Ratio < sorted[j].Ratio })
	return sorted[0]
}
