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
//	go run ./cmd/benchmark [-adhoc] [-concurrency 4] [-maxtokens 1024] \
//	    [-models gemma-4] [-timeout 90m] [-model-timeout 25m] [-maxiters 6]
//
// Questions run concurrently within a model (the far-side gateway batches them);
// models are iterated serially to avoid thrashing a memory-constrained backend.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/mitre/hdf-mcp-demo/internal/benchmark"
	"github.com/mitre/hdf-mcp-demo/internal/instrument"
	"github.com/mitre/hdf-mcp-demo/internal/mcpclient"
)

func main() {
	fixturesDir := flag.String("fixtures", "fixtures", "directory holding the raw source fixtures")
	adhoc := flag.Bool("adhoc", false, "also measure the conversion-included (ad-hoc) HDF cost view")
	maxIters := flag.Int("maxiters", 6, "tool round-trip cap per arm")
	maxTokens := flag.Int("maxtokens", 1024, "per-request completion-token cap; RAISE for reasoning models (they spend tokens on hidden reasoning before the answer)")
	concurrency := flag.Int("concurrency", 4, "max questions in flight at once per model (server-side batched; keep models serial)")
	modelsFlag := flag.String("models", "", "comma-separated models to run; overrides OPENAI_MODEL_LIST/OPENAI_MODEL when set")
	timeout := flag.Duration("timeout", 30*time.Minute, "overall run timeout for the whole invocation (raise when a slow/cold reasoning model is in the list)")
	perModel := flag.Duration("model-timeout", 0, "optional per-model timeout; 0 = share the overall -timeout across all models")
	format := flag.String("format", "text", "output format: text | json | markdown")
	outPath := flag.String("out", "", "write results to this file instead of stdout (progress still goes to stderr)")
	flag.Parse()

	cfg := runConfig{
		fixturesDir: *fixturesDir, adhoc: *adhoc, maxIters: *maxIters, maxTokens: *maxTokens,
		concurrency: *concurrency, models: splitModels(*modelsFlag), timeout: *timeout, perModel: *perModel,
		format: strings.ToLower(*format), outPath: *outPath,
	}
	if err := run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

type runConfig struct {
	fixturesDir       string
	adhoc             bool
	maxIters          int
	maxTokens         int
	concurrency       int
	models            []string
	timeout, perModel time.Duration
	format            string
	outPath           string
}

// splitModels parses the comma-separated -models flag, trimming blanks.
func splitModels(s string) []string {
	var out []string
	for _, m := range strings.Split(s, ",") {
		if m = strings.TrimSpace(m); m != "" {
			out = append(out, m)
		}
	}
	return out
}

func run(cfg runConfig) error {
	base := os.Getenv("OPENAI_BASE_URL")
	models := cfg.models
	if len(models) == 0 {
		models = instrument.ModelsFromEnv()
	}
	if base == "" || len(models) == 0 {
		return fmt.Errorf("set OPENAI_BASE_URL and OPENAI_MODEL or OPENAI_MODEL_LIST (or pass -models); and OPENAI_API_KEY if required")
	}
	switch cfg.format {
	case "text", "json", "markdown", "md":
	default:
		return fmt.Errorf("unknown -format %q (want text | json | markdown)", cfg.format)
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

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	env := append(os.Environ(), "HDF_MCP_ROOT="+root, "HDF_MCP_ENABLE_WRITES=1")
	sess, err := mcpclient.Connect(ctx, bin, env)
	if err != nil {
		return err
	}
	defer func() { _ = sess.Close() }()

	bank := benchmark.Bank()
	opts := benchmark.Options{MaxIters: cfg.maxIters, AdHoc: cfg.adhoc, Concurrency: cfg.concurrency}

	// Echo exactly what will run (to stderr, so stdout stays clean for json/markdown)
	// — catches a stale OPENAI_MODEL_LIST silently overriding an intended single model.
	fmt.Fprintf(os.Stderr, "endpoint: %s   hdf: %s\nmodels: %s   questions: %d   concurrency: %d   ad-hoc: %v   format: %s\n\n",
		base, bin, strings.Join(models, ", "), len(bank), cfg.concurrency, cfg.adhoc, cfg.format)

	meta := benchmark.RunMeta{
		Timestamp: time.Now().UTC().Format(time.RFC3339), Models: models,
		Concurrency: cfg.concurrency, MaxTokens: cfg.maxTokens, MaxIters: cfg.maxIters, AdHoc: cfg.adhoc,
	}
	incremental := cfg.format == "text" && cfg.outPath == ""
	if incremental {
		fmt.Println(benchmark.MetaText(meta))
	}
	var runs []benchmark.ModelRun
	for _, model := range models {
		inst := instrument.NewOpenAI(base, model, os.Getenv("OPENAI_API_KEY"))
		inst.MaxTokens = cfg.maxTokens

		// A per-model timeout (if set) isolates a slow/cold model so it can't spend
		// the whole budget and starve the rest; a failure is reported and skipped
		// rather than aborting the run.
		mctx, mcancel := ctx, func() {}
		if cfg.perModel > 0 {
			mctx, mcancel = context.WithTimeout(ctx, cfg.perModel)
		}
		results, err := benchmark.Run(mctx, inst, sess, cfg.fixturesDir, root, bank, opts)
		mcancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "model %s failed (skipped): %v\n\n", model, err)
			continue
		}
		benchmark.SortByID(results)
		runs = append(runs, benchmark.ModelRun{Model: inst.Name(), Results: results})
		if incremental {
			fmt.Println(benchmark.Render(inst.Name(), results, cfg.adhoc))
			fmt.Println()
		} else {
			fmt.Fprintf(os.Stderr, "  %s done (%d questions)\n", inst.Name(), len(results))
		}
	}
	if len(runs) == 0 {
		return fmt.Errorf("no model completed the study (see per-model errors above)")
	}
	if incremental {
		return nil // already streamed to stdout
	}
	return emit(cfg, meta, runs)
}

// emit renders the collected runs in the chosen format and writes them to the
// output file (or stdout).
func emit(cfg runConfig, meta benchmark.RunMeta, runs []benchmark.ModelRun) error {
	var out string
	switch cfg.format {
	case "json":
		s, err := benchmark.RenderJSON(meta, runs, cfg.adhoc)
		if err != nil {
			return err
		}
		out = s
	case "markdown", "md":
		out = benchmark.RenderMarkdown(meta, runs, cfg.adhoc)
	default: // text with -out: metadata header + per-model text tables
		var b strings.Builder
		b.WriteString(benchmark.MetaText(meta) + "\n")
		for _, r := range runs {
			b.WriteString(benchmark.Render(r.Model, r.Results, cfg.adhoc))
			b.WriteString("\n")
		}
		out = b.String()
	}
	if cfg.outPath == "" {
		fmt.Println(out)
		return nil
	}
	if err := os.WriteFile(cfg.outPath, []byte(out), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", cfg.outPath, err)
	}
	fmt.Fprintf(os.Stderr, "wrote %s (%s)\n", cfg.outPath, cfg.format)
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
