package demo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mitre/hdf-mcp-demo/internal/mcpclient"
)

func hdfBinary(t *testing.T) string {
	t.Helper()
	if b := os.Getenv("HDF_BIN"); b != "" {
		return b
	}
	if _, err := exec.LookPath("hdf"); err == nil {
		return "hdf"
	}
	t.Skip("no hdf binary found — set HDF_BIN=/path/to/hdf or put hdf on PATH")
	return ""
}

// TestBank_Parses guards the embedded question bank (no binary needed).
func TestBank_Parses(t *testing.T) {
	b, err := LoadBank()
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Fixtures) == 0 || len(b.Questions) == 0 {
		t.Fatalf("bank must define fixtures and questions, got %d/%d", len(b.Fixtures), len(b.Questions))
	}
	for _, q := range b.Questions {
		if q.Category == "" || len(q.RawFiles) == 0 || len(q.Calls) == 0 {
			t.Errorf("malformed question: %+v", q)
		}
	}
}

// TestDemo_EmitsTable is the card's first failing test: an external-stdio run
// over the fixtures converts each, answers the questions via the real tools, and
// emits a per-category raw-vs-HDF token table where HDF is leaner overall.
func TestDemo_EmitsTable(t *testing.T) {
	bin := hdfBinary(t)
	bank, err := LoadBank()
	if err != nil {
		t.Fatal(err)
	}
	fixturesDir, err := filepath.Abs(filepath.Join("..", "..", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	env := append(os.Environ(), "HDF_MCP_ROOT="+root, "HDF_MCP_ENABLE_WRITES=1")
	sess, err := mcpclient.Connect(ctx, bin, env)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = sess.Close() }()

	rows, err := Run(ctx, sess, fixturesDir, root, bank)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rows) != len(bank.Questions) {
		t.Fatalf("expected %d rows, got %d", len(bank.Questions), len(rows))
	}

	var totalRaw, totalHDF int
	for _, r := range rows {
		if r.HDFTokens <= 0 || r.RawTokens <= 0 {
			t.Errorf("non-positive tokens in row %q: raw=%d hdf=%d", r.Category, r.RawTokens, r.HDFTokens)
		}
		totalRaw += r.RawTokens
		totalHDF += r.HDFTokens
	}
	if totalHDF >= totalRaw {
		t.Errorf("expected HDF leaner overall, got raw=%d hdf=%d", totalRaw, totalHDF)
	}

	table := RenderTable(rows)
	if !strings.Contains(table, "TOTAL") || !strings.Contains(table, "raw/HDF") {
		t.Errorf("table missing expected structure:\n%s", table)
	}
	t.Logf("\n%s", table)
}
