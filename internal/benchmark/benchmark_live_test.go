package benchmark

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/mitre/hdf-mcp-demo/internal/instrument"
	"github.com/mitre/hdf-mcp-demo/internal/mcpclient"
)

// TestBenchmark_Live runs a small slice of the graded study end-to-end against a
// real model, so the whole pipeline (classify → two arms → grade → report) can be
// exercised before a full run. Gated on OPENAI_BASE_URL + a model + HDF_BIN.
//
//	export OPENAI_BASE_URL=… OPENAI_MODEL_LIST="gemma-4" OPENAI_API_KEY=…
//	export HDF_BIN=/path/to/hdf
//	go test ./internal/benchmark/ -run Live -v -timeout 30m
func TestBenchmark_Live(t *testing.T) {
	base, models := os.Getenv("OPENAI_BASE_URL"), instrument.ModelsFromEnv()
	if base == "" || len(models) == 0 {
		t.Skip("set OPENAI_BASE_URL and OPENAI_MODEL or OPENAI_MODEL_LIST (and OPENAI_API_KEY) to run the live benchmark smoke")
	}
	bin := os.Getenv("HDF_BIN")
	if bin == "" {
		p, err := exec.LookPath("hdf")
		if err != nil {
			t.Skip("set HDF_BIN=/path/to/hdf (or put hdf on PATH) to run the benchmark")
		}
		bin = p
	}

	root := t.TempDir()
	env := append(os.Environ(), "HDF_MCP_ROOT="+root, "HDF_MCP_ENABLE_WRITES=1")
	sess, err := mcpclient.Connect(context.Background(), bin, env)
	if err != nil {
		t.Fatalf("connect hdf mcp: %v", err)
	}
	defer func() { _ = sess.Close() }()

	// One gosec (small, Class B) + one grype (Class A existence) question keeps the
	// smoke bounded while covering two classes and both fixtures.
	bank := []Question{Bank()[0], Bank()[3]}

	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			inst := instrument.NewOpenAI(base, model, os.Getenv("OPENAI_API_KEY"))
			inst.MaxTokens = 1024
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
			defer cancel()

			results, err := Run(ctx, inst, sess, "../../fixtures", root, bank, Options{MaxIters: 6})
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			SortByID(results)
			t.Logf("\n%s", Render(inst.Name(), results, false))

			if len(results) != len(bank) {
				t.Fatalf("got %d results, want %d", len(results), len(bank))
			}
			for _, r := range results {
				if r.Class == "" {
					t.Errorf("%s: no class computed", r.ID)
				}
				if r.Raw.Cost.TotalTokens() == 0 || r.HDF.Cost.TotalTokens() == 0 {
					t.Errorf("%s: an arm reported zero tokens (raw=%d hdf=%d) — check usage passthrough",
						r.ID, r.Raw.Cost.TotalTokens(), r.HDF.Cost.TotalTokens())
				}
			}
		})
	}
}
