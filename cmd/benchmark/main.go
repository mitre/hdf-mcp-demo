// Command benchmark runs the graded, model-driven two-arm study (ADR-0002): each
// vetted question is answered by the same model once over raw scan files and once
// over the HDF MCP tools, graded against class-appropriate ground truth, and
// reported per class with real token cost. The -bookends flag computes the
// model-free token ceiling and oracle offline; everything else drives a real
// model, so it needs a provider endpoint.
//
// Usage (run in your own shell so the token never enters an agent session):
//
//	export OPENAI_BASE_URL=http://<litellm-host>/v1
//	export OPENAI_MODEL_LIST="gemma-4,gpt-oss-120b"   # or OPENAI_MODEL=<one>
//	export OPENAI_API_KEY=<key>                        # if the gateway requires it
//	export HDF_BIN=/path/to/hdf                        # or put hdf on PATH
//	go run ./cmd/benchmark [-adhoc] [-concurrency 4] [-maxtokens 1024] \
//	    [-models gemma-4] [-timeout 90m] [-model-timeout 25m] [-maxiters 12]
//
// Fully local instead:
//
//	go run ./cmd/benchmark -provider ollama -models gpt-oss:20b [-numctx 32768]
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
	"path/filepath"
	"strings"
	"time"

	"github.com/mitre/hdf-mcp-demo/internal/agent"
	"github.com/mitre/hdf-mcp-demo/internal/aibom"
	"github.com/mitre/hdf-mcp-demo/internal/benchmark"
	"github.com/mitre/hdf-mcp-demo/internal/instrument"
	"github.com/mitre/hdf-mcp-demo/internal/mcpclient"
)

func main() {
	fixturesDir := flag.String("fixtures", "fixtures", "directory holding the raw source fixtures")
	adhoc := flag.Bool("adhoc", false, "also measure the conversion-included (ad-hoc) HDF cost view")
	// 12, not 6: with a cap of 6 the first gateway study truncated 13 of 28
	// raw-arm runs, and the runs that DID answer reached 5 — the distribution was
	// cut off at the boundary rather than tapering, so the raw arm's accuracy
	// measured its turn budget. The HDF arm answers in 1-4, so the higher cap
	// costs it nothing; it buys the raw arm room to show where it actually stops.
	maxIters := flag.Int("maxiters", 12, "tool round-trip cap per arm; an arm that hits it is reported as capped, and its accuracy is a lower bound")
	maxTokens := flag.Int("maxtokens", 4096, "per-request completion-token cap. Reasoning models spend tokens on hidden reasoning before the answer, so a low cap truncates them into a false abstention")
	concurrency := flag.Int("concurrency", 0, "max questions in flight at once per model; 0 = 4 for a batching gateway, 2 for local Ollama (each in-flight request needs its own KV cache, so a large local model can exhaust memory)")
	modelsFlag := flag.String("models", "", "comma-separated models to run; overrides OPENAI_MODEL_LIST/OPENAI_MODEL when set")
	timeout := flag.Duration("timeout", 0, "overall run timeout for the whole invocation; 0 = scale automatically (see defaultTimeout) from the model count and provider")
	perModel := flag.Duration("model-timeout", 0, "per-model timeout, isolating one stalled model from the rest; 0 = auto (60m for local Ollama, shared -timeout for a gateway); negative disables it")
	format := flag.String("format", "text", "output format: text | json | markdown")
	outPath := flag.String("out", "", "write results to this file instead of stdout (progress still goes to stderr)")
	overwrite := flag.Bool("overwrite", false, "allow -out to overwrite an existing file")
	repeat := flag.Int("repeat", 1, "runs per question per arm; >1 reports accuracy as a rate and cost as mean±stddev")
	temperature := flag.Float64("temperature", 0, "sampling temperature (0 = deterministic); negative to omit it entirely for models that reject it")
	provider := flag.String("provider", "openai", "model provider: openai (OpenAI-compatible /v1 — LiteLLM, vLLM, Ollama's /v1 shim) | ollama (native /api/chat, fully local)")
	toolset := flag.String("tools", "", "comma-separated HDF tools the arm may advertise (default: the full read surface). Use to measure whether surface size affects tool selection — see hdf-libs-uqhe.18")
	numCtx := flag.Int("numctx", 32768, "ollama only: context window (num_ctx). Ollama's own default is 4096 whatever the model advertises, and it truncates silently — too small for the raw-file arm, so leaving it unset understates raw. 0 = defer to the server")
	bookendsOnly := flag.Bool("bookends", false, "compute only the model-free token bookends (whole-file ceiling + hand-optimal oracle per question) and exit — needs no model, endpoint, or provider")
	transcripts := flag.String("transcripts", "", "write one JSON transcript per (question, arm, sample) under this directory — every prompt, tool definition, tool call, and the graded outcome — for diagnosing a failing or abstaining arm from evidence")
	flag.Parse()

	cfg := runConfig{
		fixturesDir: *fixturesDir, adhoc: *adhoc, maxIters: *maxIters, maxTokens: *maxTokens,
		concurrency: *concurrency, models: splitModels(*modelsFlag), timeout: *timeout, perModel: *perModel,
		format: strings.ToLower(*format), outPath: *outPath, overwrite: *overwrite,
		repeat: *repeat, temperature: *temperature, provider: strings.ToLower(*provider), numCtx: *numCtx,
		bookendsOnly: *bookendsOnly, transcripts: *transcripts,
		toolset: splitModels(*toolset),
	}
	if cfg.concurrency <= 0 {
		cfg.concurrency = defaultConcurrency(cfg.provider)
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
	overwrite         bool
	repeat            int
	temperature       float64
	provider          string
	numCtx            int
	bookendsOnly      bool
	transcripts       string
	toolset           []string // advertised HDF tools; empty = the full read surface
}

// endpoint resolves the provider's base URL. For ollama it is optional (the
// adapter defaults to localhost); OLLAMA_HOST/OLLAMA_BASE_URL override it.
func endpoint(cfg runConfig) string {
	if cfg.provider == "ollama" {
		if b := os.Getenv("OLLAMA_HOST"); b != "" {
			return b
		}
		return os.Getenv("OLLAMA_BASE_URL")
	}
	return os.Getenv("OPENAI_BASE_URL")
}

// buildInstrument constructs the provider's instrument for one model, applying the
// shared max-tokens / temperature knobs to whichever provider is selected.
func buildInstrument(cfg runConfig, base, model string) instrument.Instrument {
	var setTemp = cfg.temperature >= 0 // negative sentinel = omit
	if cfg.provider == "ollama" {
		o := instrument.NewOllama(base, model)
		o.NumPredict = cfg.maxTokens
		o.NumCtx = cfg.numCtx
		if setTemp {
			t := cfg.temperature
			o.Temperature = &t
		}
		return o
	}
	o := instrument.NewOpenAI(base, model, os.Getenv("OPENAI_API_KEY"))
	o.MaxTokens = cfg.maxTokens
	if setTemp {
		t := cfg.temperature
		o.Temperature = &t
	}
	return o
}

// writeModelBOMs records which weights produced a run. For each local model it
// reads the model's own reported identity, emits a CycloneDX ML-BOM, and ingests
// it with `hdf system create` into an HDF System document written beside the
// report — so the benchmark that produces HDF records its own provenance AS HDF.
//
// Failure here is never fatal: provenance is worth having, but not worth losing
// a completed study over, so a problem is reported and the run keeps its results.
func writeModelBOMs(ctx context.Context, cfg runConfig, bin string, models []string) []benchmark.ModelProvenance {
	if cfg.provider != "ollama" || cfg.outPath == "" {
		return nil // nowhere to put them, or no local model to describe
	}
	dir := filepath.Dir(cfg.outPath)
	base := instrument.NewOllama(endpoint(cfg), "")
	var written []benchmark.ModelProvenance
	for _, m := range models {
		id, err := base.ShowModel(ctx, m)
		if err != nil {
			fmt.Fprintf(os.Stderr, "model provenance for %s unavailable (skipped): %v\n", m, err)
			continue
		}
		doc, err := aibom.CycloneDX(aibom.Model{
			Name: id.Name, Digest: id.Digest, Family: id.Family, Architecture: id.Architecture,
			ParameterSize: id.ParameterSize, ParameterCount: id.ParameterCount,
			Quantization: id.Quantization, Format: id.Format, License: id.License,
			Capabilities: id.Capabilities,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "model provenance for %s unavailable (skipped): %v\n", m, err)
			continue
		}
		safe := strings.NewReplacer(":", "_", "/", "_").Replace(m)
		bomPath := filepath.Join(dir, safe+".model.cdx.json")
		sysPath := filepath.Join(dir, safe+".model.system.json")
		if err := os.WriteFile(bomPath, doc, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "write model BOM for %s (skipped): %v\n", m, err)
			continue
		}
		out, err := exec.CommandContext(ctx, bin, "system", "create", bomPath,
			"--from", "cyclonedx-mlbom", "-o", sysPath).CombinedOutput()
		if err != nil {
			fmt.Fprintf(os.Stderr, "hdf system create for %s (skipped): %v: %s\n", m, err, strings.TrimSpace(string(out)))
			continue
		}
		// Record basenames: both documents sit beside the report, so relative names
		// keep a published report portable instead of embedding the path of
		// whatever machine happened to run it. The AI-BOM and the HDF System that
		// wraps it are named separately — they are different artifacts.
		written = append(written, benchmark.ModelProvenance{
			Model: m, BOM: filepath.Base(bomPath), System: filepath.Base(sysPath),
		})
	}
	return written
}

// allowedTools is the HDF tool set the arm advertises. Empty means the full read
// surface; a subset exists so the study can measure whether surface SIZE changes
// which tool a model reaches for, not merely what it costs to advertise.
func allowedTools(cfg runConfig) map[string]bool {
	if len(cfg.toolset) == 0 {
		return agent.ReadTools
	}
	allow := map[string]bool{}
	for _, t := range cfg.toolset {
		allow[t] = true
	}
	return allow
}

// metaNumCtx reports the context window to record in the run metadata. It is an
// Ollama-only knob, so an openai-provider run records 0 rather than the flag's
// default — the recorded value must describe what the model actually got.
func metaNumCtx(cfg runConfig) int {
	if cfg.provider == "ollama" {
		return cfg.numCtx
	}
	return 0
}

// defaultConcurrency picks how many questions may be in flight at once. A remote
// gateway batches them, so more is free; local Ollama allocates a separate KV
// cache per in-flight request, so a 30B model at a large num_ctx can run the
// machine out of memory. Keep the local default modest.
func defaultConcurrency(provider string) int {
	if provider == "ollama" {
		return 2
	}
	return 4
}

// defaultTimeout picks an overall budget when -timeout is 0. Local inference is
// far slower than a batching gateway — a single 30B model can spend half an hour
// on the bank alone — so the old fixed 30m default made a multi-model local run
// fail by timeout through no fault of the user's. Scale with the model count and
// give the local provider a much larger per-model allowance.
func defaultTimeout(provider string, models int) time.Duration {
	if models < 1 {
		models = 1
	}
	return time.Duration(models) * perModelBudget(provider)
}

// perModelBudget is one model's share of the automatic timeout.
func perModelBudget(provider string) time.Duration {
	if provider == "ollama" {
		return 60 * time.Minute
	}
	return 15 * time.Minute
}

// defaultPerModel decides the per-model timeout when -model-timeout is 0. Without
// one, a single wedged model silently consumes the entire remaining budget and
// starves every model after it. That is worth defaulting against locally, where a
// too-large model swapping to disk can hang effectively forever. A gateway run
// keeps the historical behaviour of sharing the overall timeout, since its
// failure modes are the endpoint's rather than the machine's.
func defaultPerModel(provider string) time.Duration {
	if provider == "ollama" {
		return perModelBudget(provider)
	}
	return 0
}

// checkOllamaModels verifies, before any inference, that the local server is up
// and every requested model is pulled and can call tools. Each of these fails
// mid-run otherwise — after other models may already have spent an hour — and a
// missing tools capability surfaces as a baffling empty-answer loop rather than
// an obvious cause.
func checkOllamaModels(ctx context.Context, base string, models []string) error {
	local, err := instrument.NewOllama(base, "").LocalModels(ctx)
	if err != nil {
		return err
	}
	byName := map[string]instrument.LocalModel{}
	for _, m := range local {
		byName[m.Name] = m
		// `ollama run llama3.2` resolves the implicit :latest tag; accept the
		// bare name too so a user who omits the tag is not falsely rejected.
		if base, _, found := strings.Cut(m.Name, ":"); found {
			if _, taken := byName[base]; !taken {
				byName[base] = m
			}
		}
	}
	var problems []string
	for _, want := range models {
		m, ok := byName[want]
		if !ok {
			problems = append(problems, fmt.Sprintf("  %s: not pulled — run `ollama pull %s`", want, want))
			continue
		}
		if !m.HasTools() {
			problems = append(problems, fmt.Sprintf("  %s: does not support tool calling, so it cannot drive either arm — pick a model listed at https://ollama.com/search?c=tools", want))
		}
	}
	if len(problems) > 0 {
		avail := make([]string, 0, len(local))
		for _, m := range local {
			if m.HasTools() {
				avail = append(avail, m.Name)
			}
		}
		msg := "unusable model(s):\n" + strings.Join(problems, "\n")
		if len(avail) > 0 {
			msg += "\n\ntool-capable models already pulled: " + strings.Join(avail, ", ")
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
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

// preflight validates every cheap precondition BEFORE the expensive model run, so
// a config mistake (missing endpoint, bad format, an -out file that already
// exists) fails in the first second instead of after an hour. It returns the
// located hdf binary.
func preflight(ctx context.Context, cfg runConfig, base string, models []string) (string, error) {
	// -bookends is model-free token accounting: no provider, endpoint, or model
	// may be demanded for it.
	if !cfg.bookendsOnly {
		switch cfg.provider {
		case "openai":
			if base == "" {
				return "", fmt.Errorf("set OPENAI_BASE_URL (or use -provider ollama); and OPENAI_API_KEY if the endpoint requires it")
			}
		case "ollama":
			// base is optional — the adapter defaults to localhost:11434
			if len(models) > 0 {
				if err := checkOllamaModels(ctx, base, models); err != nil {
					return "", err
				}
			}
		default:
			return "", fmt.Errorf("unknown -provider %q (want openai | ollama)", cfg.provider)
		}
		if len(models) == 0 {
			return "", fmt.Errorf("set -models (or OPENAI_MODEL / OPENAI_MODEL_LIST)")
		}
	}
	switch cfg.format {
	case "text", "json", "markdown", "md":
	default:
		return "", fmt.Errorf("unknown -format %q (want text | json | markdown)", cfg.format)
	}
	if cfg.repeat < 1 {
		return "", fmt.Errorf("-repeat must be >= 1")
	}
	if cfg.numCtx < 0 {
		return "", fmt.Errorf("-numctx must be >= 0")
	}
	if cfg.outPath != "" {
		if _, err := os.Stat(cfg.outPath); err == nil && !cfg.overwrite {
			return "", fmt.Errorf("output file %q already exists; pass -overwrite to replace it", cfg.outPath)
		} else if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("stat -out %q: %w", cfg.outPath, err)
		}
		if dir := filepath.Dir(cfg.outPath); dir != "" {
			if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
				return "", fmt.Errorf("output directory %q does not exist", dir)
			}
		}
	}
	if fi, err := os.Stat(cfg.fixturesDir); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("fixtures directory %q not found (pass -fixtures)", cfg.fixturesDir)
	}
	// Create the transcript dir now, so an unwritable path fails in the first
	// second rather than losing the first arm's evidence mid-run.
	if cfg.transcripts != "" {
		if err := os.MkdirAll(cfg.transcripts, 0o750); err != nil {
			return "", fmt.Errorf("create -transcripts dir: %w", err)
		}
	}
	bin, err := locateHDF()
	if err != nil {
		return "", err
	}
	return bin, nil
}

func run(cfg runConfig) error {
	base := endpoint(cfg)
	models := cfg.models
	if len(models) == 0 {
		models = instrument.ModelsFromEnv()
	}
	if cfg.timeout <= 0 {
		cfg.timeout = defaultTimeout(cfg.provider, len(models))
	}
	if cfg.perModel == 0 {
		cfg.perModel = defaultPerModel(cfg.provider)
	}

	// Preflight gets its own short-lived context: it must not consume the run
	// budget, and a hung local server should fail here in seconds, not at the end.
	pctx, pcancel := context.WithTimeout(context.Background(), 30*time.Second)
	bin, err := preflight(pctx, cfg, base, models)
	pcancel()
	if err != nil {
		return err
	}

	baseRoot, err := os.MkdirTemp("", "hdf-mcp-demo-bench-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(baseRoot) }()

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	bank := benchmark.Bank()
	opts := benchmark.Options{MaxIters: cfg.maxIters, AdHoc: cfg.adhoc, Concurrency: cfg.concurrency, Repeat: cfg.repeat, TranscriptDir: cfg.transcripts}
	// Liveness: report each question as it finishes (to stderr, so stdout stays
	// clean for -format json/markdown), so a long run visibly isn't hung.
	opts.Progress = func(model, qID string, done, total int) {
		fmt.Fprintf(os.Stderr, "  [%d/%d] %s  %s\n", done, total, model, qID)
	}

	// Echo exactly what will run (to stderr, so stdout stays clean for json/markdown)
	// — catches a stale OPENAI_MODEL_LIST silently overriding an intended single model.
	shownBase := base
	if shownBase == "" && cfg.provider == "ollama" {
		shownBase = instrument.DefaultOllamaURL + " (default)"
	}
	fmt.Fprintf(os.Stderr, "provider: %s   endpoint: %s   hdf: %s\nmodels: %s   questions: %d   concurrency: %d   ad-hoc: %v   format: %s\n\n",
		cfg.provider, shownBase, bin, strings.Join(models, ", "), len(bank), cfg.concurrency, cfg.adhoc, cfg.format)

	meta := benchmark.RunMeta{
		Timestamp: time.Now().UTC().Format(time.RFC3339), Provider: cfg.provider, Models: models,
		Concurrency: cfg.concurrency, MaxTokens: cfg.maxTokens, MaxIters: cfg.maxIters, AdHoc: cfg.adhoc,
		Repeat: cfg.repeat, Temperature: cfg.temperature, NumCtx: metaNumCtx(cfg),
		ModelProvenance: writeModelBOMs(ctx, cfg, bin, models),
	}

	// Bookends: the model-free token bracket (whole-file ceiling + hand-optimal
	// oracle per question), computed once per run in its own root — they depend on
	// the fixtures and the tool surface, not on any model.
	fmt.Fprintln(os.Stderr, "computing token bookends (model-free)...")
	bks, err := computeBookends(ctx, cfg, bin, baseRoot)
	if err != nil {
		return fmt.Errorf("compute bookends: %w", err)
	}
	if cfg.bookendsOnly {
		meta.Provider, meta.Models = "", nil // no model ran; the meta must not imply one
		return emit(cfg, meta, nil, bks)
	}

	incremental := cfg.format == "text" && cfg.outPath == ""
	if incremental {
		fmt.Println(benchmark.MetaText(meta))
		fmt.Println(benchmark.RenderBookends(bks))
	}
	var runs []benchmark.ModelRun
	for mi, model := range models {
		inst := buildInstrument(cfg, base, model)
		fmt.Fprintf(os.Stderr, "[model %d/%d] %s — running %d questions (x%d)...\n",
			mi+1, len(models), inst.Name(), len(bank), opts.Repeat)
		start := time.Now()

		// A per-model timeout (if set) isolates a slow/cold model so it can't spend
		// the whole budget and starve the rest; a failure is reported and skipped
		// rather than aborting the run.
		mctx, mcancel := ctx, func() {}
		if cfg.perModel > 0 {
			mctx, mcancel = context.WithTimeout(ctx, cfg.perModel)
		}
		results, err := runModel(mctx, cfg, bin, baseRoot, model, inst, bank, opts)
		mcancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[model %d/%d] %s FAILED after %s (skipped): %v\n\n",
				mi+1, len(models), inst.Name(), time.Since(start).Round(time.Second), err)
			continue
		}
		fmt.Fprintf(os.Stderr, "[model %d/%d] %s done in %s\n\n",
			mi+1, len(models), inst.Name(), time.Since(start).Round(time.Second))
		benchmark.SortByID(results)
		runs = append(runs, benchmark.ModelRun{Model: inst.Name(), Results: results})
		if incremental {
			fmt.Println(benchmark.Render(inst.Name(), results, cfg.adhoc, bks))
			fmt.Println()
		}
	}
	if len(runs) == 0 {
		return fmt.Errorf("no model completed the study (see per-model errors above)")
	}
	if incremental {
		return nil // already streamed to stdout
	}
	return emit(cfg, meta, runs, bks)
}

// computeBookends runs the model-free bookend pass in its own root and MCP
// session (the per-model roots are created later and torn down independently).
func computeBookends(ctx context.Context, cfg runConfig, bin, baseRoot string) ([]benchmark.Bookend, error) {
	root, err := os.MkdirTemp(baseRoot, "bookends-")
	if err != nil {
		return nil, err
	}
	env := append(os.Environ(), "HDF_MCP_ROOT="+root, "HDF_MCP_ENABLE_WRITES=1")
	sess, err := mcpclient.Connect(ctx, bin, env)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sess.Close() }()
	return benchmark.ComputeBookends(ctx, sess, bin, cfg.fixturesDir, root, benchmark.Bank())
}

// runModel gives one model its own MCP server process and its own document root,
// then runs the study against them.
//
// Per-model isolation is deliberate. Sharing one session across models meant a
// server that died during model N took every later model with it — each one
// failing with an opaque "connection closed" long after the real cause — and a
// shared root let model N's normalized artifacts collide with model N+1's
// staging. Neither failure is the later model's fault, and neither should cost
// its results. A transport failure now also reports the server's own last words,
// which are otherwise discarded with the process.
func runModel(ctx context.Context, cfg runConfig, bin, baseRoot, model string, inst instrument.Instrument,
	bank []benchmark.Question, opts benchmark.Options) ([]benchmark.QuestionResult, error) {

	root, err := os.MkdirTemp(baseRoot, "model-")
	if err != nil {
		return nil, err
	}
	env := append(os.Environ(), "HDF_MCP_ROOT="+root, "HDF_MCP_ENABLE_WRITES=1")
	sess, err := mcpclient.Connect(ctx, bin, env)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sess.Close() }()

	opts.Tools = allowedTools(cfg)
	results, err := benchmark.Run(ctx, inst, sess, bin, cfg.fixturesDir, root, bank, opts)
	if err != nil {
		if tail := strings.TrimSpace(sess.ServerLog()); tail != "" {
			return nil, fmt.Errorf("%w\n  hdf mcp server said:\n%s", err, indent(tail, "    "))
		}
		return nil, err
	}
	return results, nil
}

// indent prefixes every line, so a captured server log stays visually distinct
// from the benchmark's own output.
func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

// emit renders the collected runs (and the bookends) in the chosen format and
// writes them to the output file (or stdout). With no runs — the -bookends mode —
// the output is the metadata plus the bookend table alone.
func emit(cfg runConfig, meta benchmark.RunMeta, runs []benchmark.ModelRun, bks []benchmark.Bookend) error {
	var out string
	switch cfg.format {
	case "json":
		s, err := benchmark.RenderJSON(meta, runs, cfg.adhoc, bks)
		if err != nil {
			return err
		}
		out = s
	case "markdown", "md":
		out = benchmark.RenderMarkdown(meta, runs, cfg.adhoc, bks)
	default: // text with -out: metadata header + bookends + per-model text tables
		var b strings.Builder
		b.WriteString(benchmark.MetaText(meta) + "\n")
		if len(bks) > 0 {
			b.WriteString(benchmark.RenderBookends(bks) + "\n")
		}
		for _, r := range runs {
			b.WriteString(benchmark.Render(r.Model, r.Results, cfg.adhoc, bks))
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
