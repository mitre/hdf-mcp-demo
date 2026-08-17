// Command benchmark runs the graded, model-driven two-arm study (ADR-0002): each
// vetted question is answered by the same model once over raw scan files and once
// over the HDF MCP tools, graded against class-appropriate ground truth, and
// reported per class with real token cost. Unlike `cmd/demo` (offline token
// accounting), this drives a real model, so it needs an OpenAI-compatible endpoint.
//
// Usage (run in your own shell so the token never enters an agent session):
//
//	export OPENAI_BASE_URL=http://<litellm-host>/v1
//	export OPENAI_MODEL_LIST="gemma-4,gpt-oss-120b"   # or OPENAI_MODEL=<one>
//	export OPENAI_API_KEY=<key>                        # if the gateway requires it
//	export HDF_BIN=/path/to/hdf                        # or put hdf on PATH
//	go run ./cmd/benchmark [-adhoc] [-maxiters 6] [-fixtures ./fixtures]
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/mitre/hdf-mcp-demo/internal/benchmark"
	"github.com/mitre/hdf-mcp-demo/internal/instrument"
	"github.com/mitre/hdf-mcp-demo/internal/mcpclient"
)

func main() {
	fixturesDir := flag.String("fixtures", "fixtures", "directory holding the raw source fixtures")
	adhoc := flag.Bool("adhoc", false, "also measure the conversion-included (ad-hoc) HDF cost view")
	maxIters := flag.Int("maxiters", 6, "tool round-trip cap per arm")
	timeout := flag.Duration("timeout", 30*time.Minute, "overall run timeout (raise for slow reasoning models)")
	flag.Parse()

	if err := run(*fixturesDir, *adhoc, *maxIters, *timeout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(fixturesDir string, adhoc bool, maxIters int, timeout time.Duration) error {
	base := os.Getenv("OPENAI_BASE_URL")
	models := instrument.ModelsFromEnv()
	if base == "" || len(models) == 0 {
		return fmt.Errorf("set OPENAI_BASE_URL and OPENAI_MODEL or OPENAI_MODEL_LIST (and OPENAI_API_KEY if required)")
	}
	bin, err := locateHDF()
	if err != nil {
		return err
	}

	root, err := os.MkdirTemp("", "hdf-mcp-demo-bench-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	env := append(os.Environ(), "HDF_MCP_ROOT="+root, "HDF_MCP_ENABLE_WRITES=1")
	sess, err := mcpclient.Connect(ctx, bin, env)
	if err != nil {
		return err
	}
	defer func() { _ = sess.Close() }()

	bank := benchmark.Bank()
	opts := benchmark.Options{MaxIters: maxIters, AdHoc: adhoc}

	fmt.Printf("endpoint: %s   hdf: %s   questions: %d   ad-hoc view: %v\n\n", base, bin, len(bank), adhoc)
	for _, model := range models {
		inst := instrument.NewOpenAI(base, model, os.Getenv("OPENAI_API_KEY"))
		inst.MaxTokens = 1024 // generous enough for a reasoning model to finish

		results, err := benchmark.Run(ctx, inst, sess, fixturesDir, root, bank, opts)
		if err != nil {
			return fmt.Errorf("model %s: %w", model, err)
		}
		benchmark.SortByID(results)
		fmt.Println(benchmark.Render(inst.Name(), results, adhoc))
		fmt.Println()
	}
	return nil
}

func locateHDF() (string, error) {
	if b := os.Getenv("HDF_BIN"); b != "" {
		return b, nil
	}
	if p, err := exec.LookPath("hdf"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("no hdf binary found — set HDF_BIN=/path/to/hdf or put hdf on PATH " +
		"(build: cd ../hdf-libs/hdf-cli && go build -o hdf ./cmd/hdf)")
}
