# hdf-mcp-demo

A worked **primer on using the [HDF MCP server](https://github.com/mitre/hdf-libs)** (`hdf mcp`) well — and a harness for **measuring** what its bounded, normalized tool surface is worth to an AI agent compared with reading raw scan files.

This repository is an **example consumer** (a sibling to `hdf-conmon-demo`): it drives the shipped `hdf` binary over stdio exactly the way a real MCP client does, so nothing here reaches into hdf-libs internals.

It gives you two numbers, and they answer different questions:

| | What it measures | What it needs |
|---|---|---|
| [Ingest ceiling](#the-ingest-ceiling-offline-model-free) (`./run.sh`) | Best-case bytes saved, under ideal play by both sides | Nothing — offline, seconds |
| [Graded study](#the-graded-study-run-it-on-your-own-hardware) (`./bench.sh`) | Whether a real model gets the right answer, and what it actually spent | A local model via Ollama, or any OpenAI-compatible endpoint (e.g. LiteLLM) |

The ceiling is large. The graded result is the honest one, and on small scans it can favour the raw arm. Both are here on purpose.

> Design rationale: [`dev-docs/adr-0001-hdf-mcp-demo.md`](dev-docs/adr-0001-hdf-mcp-demo.md).

## Why an HDF MCP at all

Security pipelines produce *unlike* scan output — SAST, DAST, SBOM, vulnerability, and compliance (CCE) results, each in its own vendor shape. An agent asked to reason across them can either haul every raw file into its context and reconcile five schemas by hand, or normalize everything to one HDF shape and ask a small, typed, token-bounded tool surface. This repo measures the difference.

## Requirements

- Go (matching hdf-libs' toolchain).
- A built `hdf` binary with the `mcp` command. Point the demo at it with `HDF_BIN=/path/to/hdf`, or put `hdf` on your `PATH`:

  ```bash
  (cd ../hdf-libs/hdf-cli && go build -o hdf ./cmd/hdf)
  export HDF_BIN="$PWD/../hdf-libs/hdf-cli/hdf"
  ```

- **Only for the graded study** ([below](#the-graded-study-run-it-on-your-own-hardware)): either [Ollama](https://ollama.com/download) for local models, or an OpenAI-compatible endpoint. The offline demo above needs neither.

## Run

```bash
./run.sh          # offline: locates hdf, runs the tests, prints the demo table
./bench.sh        # graded study against your local models (needs Ollama)
```

`run.sh` is model-free — it counts tokens and finishes in seconds. `bench.sh` drives real models through both arms and is the study proper; see [below](#the-graded-study-run-it-on-your-own-hardware).

## Using the HDF MCP well — the patterns this demo shows

**1. Talk to it over stdio.** The client launches `hdf mcp` as a subprocess and speaks JSON-RPC over stdin/stdout. See [`internal/mcpclient`](internal/mcpclient) for a ~60-line client over the official MCP SDK — that is the whole integration.

**2. Normalize once, then query.** Bring every source into HDF first, then ask questions against the normalized documents:

- Scan formats (gosec, ZAP, Grype, InSpec, any other scan tool that can convert to HDF…) → the **`hdf_convert`** tool.
- SBOMs (SPDX/CycloneDX) → **`hdf system create --from spdx`** (inventory becomes an HDF *System* document).

**3. Pass handles, not documents.** Every tool returns a compact **summary plus a reusable handle** — never a multi-megabyte body. Reuse the handle across a multi-step workflow instead of re-sending the document.

**4. Sequence the read tools by intent:**

| Want | Call |
|------|------|
| Detect type + validity + a headline summary | `hdf_open` |
| Structure/metadata (counts, components, inventories) | `hdf_inspect` |
| The requirements themselves (filter by status/severity/id/tag) | `hdf_query` |
| Compliance % + status/severity rollups + threshold verdict | `hdf_compliance` |
| What changed between two documents | `hdf_diff` |

**5. Reads degrade, writes refuse.** A structurally-valid but schema-imperfect document still opens (`valid:false` + best-effort summary); a write tool refuses to emit a document that does not validate. Writes are gated by `HDF_MCP_ENABLE_WRITES` (off by default) — a disabled deployment returns a preview, never a silent no-op.

## The ingest ceiling (offline, model-free)

This is an **upper bound on what normalization can save on ingest**, not a prediction of what an agent will cost you. Read the assumptions before the table, because they are what produce the large numbers:

- **The raw side charges for the entire file, every byte** — as if the agent read all 1.2MB of a compliance run to answer one question. A real agent greps and paginates, which is far cheaper. (The graded study's raw arm does exactly that; these two baselines deliberately differ, and this one is the pessimistic bookend.)
- **The HDF side is a hand-written, optimal call** — the right tool with the right arguments, chosen by a human, first try. A real model explores, mis-calls, and retries.
- **Neither side pays the tool-schema tax or multi-turn accumulation.** One round-trip is counted, not a conversation that re-sends itself each turn.

So the ratios below are best-case-HDF over worst-case-raw. They are useful for sizing the *headroom* normalization buys, and useless as a cost forecast. For what a real model actually spends — often with HDF costing **more** on small scans — see [the graded study](#the-graded-study-run-it-on-your-own-hardware).

Counted with the same O200k encoding the server budgets against. Model-free, offline, reproducible via `./run.sh`.

```
category                              raw tok    HDF tok   raw/HDF
------------------------------------------------------------------
1 single-fact (small src)                1537        274      5.6x
1 single-fact (large src)              155964       2774     56.2x
2 cross-format aggregate               185656        704    263.7x
2 cross-doc join (SBOM x vuln)         156715       3026     51.8x
3 compliance rollup (vuln)             155964        240    649.9x
3 compliance rollup (CCE)              336002        268   1253.7x
4 temporal diff                        185759       2669     69.6x
5 tool-specific field (HDF loses)      155964     158739      1.0x
------------------------------------------------------------------
TOTAL                                 1333561     168694      7.9x
```

Under those assumptions HDF is leaner everywhere the question is answerable from normalized fields. **Category 5 is deliberately the honest counter-example:** it asks for a tool-specific field (grype's `matchDetails`), and the HDF arm has to pay for the raw bytes anyway — HDF adds overhead and **loses** (1.0×).

The mechanism is worth stating precisely, because it is not information loss. Conversion preserves the original scanner finding **byte-for-byte** in the requirement's `code` field, so nothing is dropped. What costs the HDF arm is that no read tool *projects* `code` — `hdf_query`'s full-verbosity row carries id, title, status, severity, impact, baseline, tags and descriptions, and nothing else. So the limit here is the **bounded read surface**, not lossy normalization: the data is in the document and unreachable through the tools. The graded study's `grype-related-vulns` question measures exactly this, and grades the HDF arm on it rather than excusing it.

### Read the numbers honestly

- **Token *ingest*, not end-to-end agent cost.** This measures what enters context under ideal play. Real accuracy and cost are the graded study.
- **Do not quote these as agent savings.** A real two-arm run over these same fixtures produces very different numbers, sometimes favouring the raw arm — which is the point of running it rather than assuming.
- **Amortized (pipeline) case.** The HDF side assumes documents are already normalized. A one-shot "convert then ask once" pays the conversion cost too.
- **A whole-system result.** The win is HDF-MCP-as-a-system (normalization + a token-bounded surface), not the schema alone.

## The graded study: run it on your own hardware

`./run.sh` answers "how many tokens does each approach cost?" without a model. The graded study (`cmd/benchmark`, ADR-0002) answers the harder question: **does a model actually get the right answer**, and at what real token cost? The bank spans small scans (gosec, ZAP), a medium one (grype), a 1.2MB InSpec compliance run, and the category-5 case where the HDF arm is expected to lose, so the large-document regime and the honest counter-example are both covered. Each vetted question is put to the same model twice — once over raw scan files with grep and paginated reads, once over the HDF MCP tools — and graded against class-appropriate ground truth.

It runs entirely on your own machine through Ollama, so it costs nothing and no data leaves the box. This — not the [ingest ceiling](#the-ingest-ceiling-offline-model-free) above — is the number to quote about agent cost.

```bash
ollama serve                     # in another terminal, if not already running
ollama pull gpt-oss:20b          # any tool-capable model

export HDF_BIN="$PWD/../hdf-libs/hdf-cli/hdf"
./bench.sh                       # every tool-capable model you have pulled
./bench.sh gpt-oss:20b           # or name them
./bench.sh gpt-oss:20b -- -repeat 3   # extra flags pass through to cmd/benchmark
```

Results land in `results/` as markdown. `cmd/benchmark` is also usable directly (`go run ./cmd/benchmark -provider ollama -models gpt-oss:20b`), and speaks to any OpenAI-compatible gateway with `-provider openai`.

### The eleven questions

One constraint shapes the whole bank: every question's ground truth must be **computable from the data by code** — once over the raw scanner output, once over the converted HDF document — never asserted by hand and never judged by an LLM. That limits the bank to question shapes with closed-form answers (counts, existence, thresholds, rates), which is also why there are eleven questions and not fifty. Where the two computed answers agree, the question is *objective*; where they legitimately differ, it is *interpretive* and graded to its stated intent; where only one view can express an answer at all, it is *hdf-only*. The classification is recomputed from the data at run time, so a converter change that altered the semantics would reclassify the question rather than silently grade against a stale key.

| Question | Asks | Type | Why it is in the bank |
|---|---|---|---|
| `gosec-distinct-rules` | distinct rule violations in the gosec scan | interpretive (key = 3, the HDF view) | gosec emits one finding per code site; conversion dedups to rule level (7 findings → 3 rules). Asking for *distinct rules* matches HDF's shape — the raw arm must dedup by hand. |
| `gosec-total-findings` | total findings the scanner emitted, pre-dedup | interpretive (key = 7, the raw view) | The same divergence graded the **other** way: here the raw count is the intended answer and HDF's dedup produces the wrong number. Paired with the above so the study never reports only the HDF-flattering direction. |
| `grype-match-count` | vulnerability matches in the grype scan | objective (89) | grype does not dedup, so raw == HDF. The contrast with gosec shows divergence is a property of the *scanner's* output shape, not of normalization in general. |
| `grype-cve-present` | is CVE-2021-36159 present? | objective (yes) | Needle-in-a-haystack existence over a ~1 MB scan — the everyday lookup shape. |
| `grype-has-critical` | any Critical-severity finding? | objective | Severity threshold across mapped vocabularies: the raw arm greps severity strings, the HDF arm filters impact ≥ 0.9. Tests whether the mapping survives an agent round-trip. |
| `grype-compliance-rate` | what percentage of the scan is passing? | hdf-only | A vuln scanner has no native pass rate; HDF's normalization adds one. The raw arm is out of remit — measured on whether it honestly declines or invents a rate. |
| `grype-related-vulns` | matches listing at least one related vulnerability (45) | objective — **expected HDF loss** | Category 5, the honest counter-example. Conversion preserves the field byte-for-byte inside the requirement's `code`, but no HDF read tool projects `code` — so the HDF arm must get there through a bounded surface that doesn't expose the answer. Graded, not excused. |
| `zap-alert-count` | alerts in the ZAP scan | objective | A second no-dedup scanner from a different tool family (DAST), so the answer-preserving case isn't a single-scanner artifact. |
| `zap-high-severity-count` | high-risk alerts in the ZAP scan | objective | Count-above-threshold over ZAP's numeric riskcodes vs. HDF impact ≥ 0.7 — vocabulary mapping on a *count*, stricter than mere existence. |
| `inspec-control-count` | controls in the InSpec compliance run | objective (192) | The large-document regime: a 1.2 MB compliance run. Everything else is small enough that grep competes; this is the scale bounded tool responses are actually for. |
| `inspec-compliance-rate` | percentage of InSpec results passing | objective (80%) | The rate question where the raw arm **can** answer — a compliance run states pass/fail natively — making it the fair counterpart to the hdf-only grype rate. |

Why this particular selection, compressed:

- **Every grading class is occupied, and interpretive runs both directions.** The gosec pair grades the identical 7-vs-3 divergence once in HDF's favor and once in raw's, so the taxonomy can't be gamed by question wording.
- **The expected loss is in the bank and graded.** `grype-related-vulns` exists so the study measures where HDF's bounded read surface hurts, instead of only sampling questions the tools are good at.
- **Four tool families, two output shapes.** SAST (gosec, dedups), vuln scan (grype, doesn't), DAST (ZAP, doesn't), compliance (InSpec, already rule-shaped) — so "normalization changes the answer" appears exactly where the scanner's shape predicts it, and nowhere else.
- **Three size regimes.** Small (gosec, ZAP), medium (grype, ~155k tokens raw), large (InSpec, 1.2 MB) — the raw arm's viability degrades with size, and the bank has to cover the whole ramp for that to show up in the accuracy table.

What's deliberately *not* here: prose questions ("summarize the risk posture") that would need an LLM judge, and a genuine raw-only question (a field HDF *drops*), because these converters are near-lossless — the closest real case is `grype-related-vulns`, where the field survives conversion but not the read surface.

### Picking a model

**The model must support tool calling.** Both arms are tool-driven — the raw arm greps and paginates files, the HDF arm calls the MCP tools — so a model without function calling cannot participate at all. `bench.sh` filters to tool-capable models automatically; browse the rest at [ollama.com/search?c=tools](https://ollama.com/search?c=tools).

Rough guide to what fits, by system memory:

| RAM | Practical ceiling | Examples |
|-----|-------------------|----------|
| 8 GB | 3–4B | `llama3.2:3b`, `granite4.1:3b` |
| 16 GB | 8–12B | `granite4.1:8b`, `gemma4:12b` |
| 32 GB | ~20B | `gpt-oss:20b` |
| 64 GB+ | 30B+ | `gemma4:31b`, `nemotron3:33b` |

Leave headroom: the context window costs memory on top of the weights, and each concurrent question needs its own KV cache.

### What to expect

- **Small models mostly fail, and that is a real result rather than a broken harness.** `llama3.2:3b` scores near zero on *both* arms here — it cannot hold a multi-step tool loop together long enough to answer. The tool loop, not the security data, is the binding constraint at that size. Expect useful signal from roughly 8B upward.
- **Local inference is slow.** A 30B model can take ~30 minutes for the eleven questions; the whole ladder is an afternoon. The InSpec pair reads a 1.2MB document, so the raw arm works hardest there. Timeouts scale automatically with the model count, and a model that stalls is skipped rather than taking the run down with it.
- **N is small and one run is noisy.** Eleven questions at `-repeat 1` is directional at best. Use `-repeat 3` before believing any single number.
- **Cheap wrong answers are not a cost win.** If an arm fails or gives up early, it also spends few tokens. Read the accuracy table and the cost table together — a token ratio from a run where one arm never really engaged means nothing.

### Useful flags

| Flag | Why you would change it |
|------|------------------------|
| `-numctx` | Context window for Ollama (default 32768). Ollama's own default is 4096 whatever the model advertises, and it truncates **silently** — too small for the raw arm. |
| `-maxtokens` | Completion cap (default 4096). Reasoning models spend tokens thinking before answering; too low a cap truncates them into a false abstention. |
| `-repeat` | Runs per question per arm; reports accuracy as a rate and cost as mean±stddev. |
| `-concurrency` | Questions in flight per model (2 locally, 4 for a gateway). Raise only if you have memory to spare. |
| `-format` | `text`, `json`, or `markdown` — for a saved, self-describing artifact. |
| `-adhoc` | Also measure the conversion-included cost view, rather than assuming documents are already normalized. |

## Fixtures

Real security-tool output, vendored with provenance — see [`fixtures/PROVENANCE.md`](fixtures/PROVENANCE.md).

## Status

Phased (see the ADR): Phase 1 (foundation), Phase 2 (this demo + primer), and Phase 3 (the model-driven two-arm graded study, `cmd/benchmark` — runnable locally and at zero cost via Ollama) are in place. Phase 4 (an optional manual CI run) is future work.
