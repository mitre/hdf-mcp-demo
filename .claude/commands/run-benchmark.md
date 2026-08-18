---
description: Run the graded two-arm HDF-MCP benchmark (raw-file agent vs HDF-MCP agent) and read the results honestly. Use when asked to run the benchmark, measure HDF-MCP vs raw performance, or produce benchmark results.
allowed-tools: Bash, Read, AskUserQuestion
---

# Run the HDF-MCP benchmark

`cmd/benchmark` runs the same model twice per question — once over raw scan files
(grep + paginated reads, no format hints) and once over the HDF MCP tools — grades
each answer against ground truth computed from the data, and reports per-type
accuracy plus real token cost. It is model-driven, so it needs a model endpoint.

## Two hard rules (read before anything)

1. **Zero-charge instrument only.** Never point the benchmark at an endpoint that
   can bill an org card. Use a **local** model (Ollama/vLLM) or a **no-payment
   free-tier** API. Paid PAYG endpoints are out.
2. **Never handle the user's API token.** For any endpoint that needs a key, you
   **compose the exact command and the user runs it in their own shell** — the
   token must not enter this session. You may run the benchmark directly **only**
   when the endpoint needs no key (e.g. local Ollama on localhost).

## Phase 1 — Preconditions

```bash
# Build the shipped hdf binary the benchmark drives over stdio:
cd ../hdf-libs/hdf-cli && go build -o /tmp/hdf ./cmd/hdf && export HDF_BIN=/tmp/hdf
# Confirm the demo builds:
cd ../../hdf-mcp-demo && go build ./...
```

If `HDF_BIN` is unset and `hdf` isn't on PATH, the run's preflight fails fast with
the build hint — so this is cheap to get wrong and easy to fix.

## Phase 2 — Pick the provider (ask if unclear)

| Setup | Flag + env |
|---|---|
| Remote OpenAI-compatible (LiteLLM, vLLM, OpenAI, Azure) | `-provider openai` (default); `OPENAI_BASE_URL`, `OPENAI_API_KEY` |
| Local, native Ollama | `-provider ollama`; base from `OLLAMA_HOST` or default `localhost:11434`; **no key** |
| Local, Ollama's OpenAI shim | `-provider openai` + `OPENAI_BASE_URL=http://localhost:11434/v1` |

**Tool-calling is the gating factor, not the endpoint.** The HDF arm needs the
model to drive 9 function-call tools. Weak/small models can't, and will pile up
`failed`/`abstained` on the HDF arm — that's honest signal, not a bug. For local,
use a tool-calling model (qwen2.5, llama3.1/3.2, mistral-nemo).

## Phase 3 — Compose the command

**Always select models with `-models`, never rely on env.** A stale
`OPENAI_MODEL_LIST` in the shell silently overrides `OPENAI_MODEL` — we lost a run
to exactly this. The startup banner echoes the models to stderr; verify them.

| Flag | When to change it |
|---|---|
| `-models a,b` | required; the clean, footgun-free way to pick models |
| `-repeat 3` | **use ≥3** for credibility — accuracy becomes a rate, cost gets mean±stddev |
| `-maxtokens 4096` | **reasoning models** (gpt-oss, o-series): they spend tokens thinking before the `ANSWER:` line; too low → empty answer. Leave 1024 for fast models |
| `-model-timeout 25m` | when a slow/cold reasoning model is in the list — isolates it so a timeout can't starve the fast model |
| `-timeout 90m` | overall budget; raise for slow models |
| `-temperature 0` | default (determinism); `-temperature -1` to omit it for models that reject an explicit temperature |
| `-adhoc` | also measure the conversion-included cost view (charges an on-demand `hdf_convert` round-trip) |
| `-concurrency 4` | default; questions run parallel per model, models stay serial |
| `-format markdown\|json -out FILE -overwrite` | machine artifact; preflight refuses to clobber `FILE` unless `-overwrite` |

**Do a fast sanity pass first** (one fast model, `-repeat 1`, no `-out`) to eyeball
the table before committing to a long `-repeat 3` two-model run.

Example full run (reasoning model in the mix, markdown artifact):
```bash
go run ./cmd/benchmark -models gemma-4,gpt-oss-120b -repeat 3 -adhoc \
  -format markdown -out results.md -overwrite \
  -model-timeout 25m -maxtokens 4096 -timeout 90m
```

## Phase 4 — Run it

- Key-bearing endpoint → **hand the composed command to the user to run** (token
  safety). Remind them to `export HDF_BIN`, `OPENAI_BASE_URL`, `OPENAI_API_KEY`.
- Local no-key endpoint → you may run it directly.

Liveness prints to **stderr** (per-question ticks + per-model start/done timing),
so a long run visibly isn't hung; stdout/`-out` stays clean for the artifact.

## Phase 5 — Read the results honestly

- **Type column is a grading distinction, NOT a ranking:** `objective` (both arms
  should agree), `interpretive` (graded to the question's intent, bidirectional —
  e.g. distinct rule violations vs raw finding volume), `hdf-only` (raw can't
  natively express it — the raw arm is out of remit), `raw-only` (HDF dropped the
  field — the HDF arm is out of remit; rare, these converters are near-lossless).
- **Accuracy is scored only over questions an arm can answer** — `n/a` where an arm
  is out of remit. That arm's **hallucinate-vs-abstain** is reported separately;
  frequent hallucination on `hdf-only` (the model inventing compliance numbers
  without HDF) is a real point *for* HDF.
- **Two cost views:** `pipeline` (conversion amortized out-of-band) and, with
  `-adhoc`, conversion-included. Raw is the denominator.
- **Don't over-read a small/`-repeat 1` run** — treat as directional. Grading is a
  lenient `ANSWER:`-line parse (favors abstention over false credit, no LLM judge),
  and **small scans can make HDF cost MORE** (the tool-schema tax) — expected, and
  the point of measuring rather than assuming.

## Footgun cheat-sheet (the scars)

- Stale `OPENAI_MODEL_LIST` overriding your intended model → use `-models`, check the banner.
- `-out` file already exists → preflight refuses without `-overwrite` (fails in the first second, by design).
- `gpt-oss`/reasoning cold start (minutes) → `-model-timeout` + generous `-maxtokens`.
- Weak local model → many `failed`/`abstained` on the HDF arm; try a stronger tool-calling model.
- Raw grype is ~156k tokens; the raw arm greps/paginates now (no more context-overflow hang), but a huge raw scan is still where "just read the file" breaks — visible as raw-arm `failed`.
