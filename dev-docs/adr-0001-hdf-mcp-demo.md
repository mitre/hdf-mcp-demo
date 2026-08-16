# ADR-0001: hdf-mcp-demo — a usage primer and efficiency demonstration for the HDF MCP server

**Date:** 2026-08-16
**Status:** proposed
**Deciders:** Will Dower

## Context

The HDF MCP server shipped in hdf-libs (see hdf-libs ADR-0007): it lets an AI agent
read, analyze, and author HDF documents over a small, token-bounded tool surface.
The most common reason people touch HDF at all is to **normalize and aggregate
heterogeneous scan output** — SAST, DAST, SBOM, vulnerability, and compliance
(CCE) results — into one queryable shape inside a pipeline.

Two needs follow, and neither is served by the shipped library alone:

1. **Teaching.** Consumers need a worked example of how to *use the MCP well* — the
   tool-sequencing patterns (`open → query → compliance → diff`, `author → apply`),
   the handle model, the read-degrades / write-refuses posture, and the source
   model — demonstrated end-to-end, not just described in a guide.
2. **Evidence.** The value proposition ("a bounded, normalized surface is far
   cheaper for an agent than dumping raw scans into context") should be shown with
   reproducible numbers, not asserted.

A zero-cost, no-model probe already ran (hdf-libs bead `uqhe.1`): measuring the
tokens an agent must ingest to answer realistic questions, the HDF MCP surface was
**~103× leaner overall** — 5.5× in the worst case (a single fact from a tiny SAST
file) up to 655× (a compliance rollup over a large vulnerability scan), and never a
loss. That signal is strong enough to justify a fuller, teaching-oriented artifact.

Demonstration and benchmark **code does not belong inside the shipped library**.
hdf-libs already follows this pattern with `hdf-conmon-demo` — a sibling repo of
runnable drivers + a README, tracked from the main repo's board, importing nothing
private from hdf-libs.

## Decision

Build **hdf-mcp-demo** as a standalone example-consumer repository whose
**primary purpose is a usage primer** — "how to use the HDF MCP well," demonstrated
end-to-end — **with a token-efficiency comparison as supporting evidence**, not as a
defensive proof. The framing is *demonstrate the tool*, and let the efficiency
numbers fall out of showing it used correctly.

It **drives the shipped `hdf mcp` binary over stdio as a real external MCP client**
(the same path any consumer uses), depends only on public libraries, vendors small
real fixtures with documented provenance, and lives entirely outside hdf-libs.
hdf-libs gains only *doc references* pointing here (hdf-libs bead `uqhe.2`).

Reasoning:

- **Teaching-first beats benchmark-first.** A primer someone can run and learn from
  is a more useful and more durable artifact than a courtroom exhibit; the efficiency
  claim is stronger when it emerges from a correct-usage demo than when it is the
  whole point.
- **External stdio client is both honest and necessary.** A separate Go module
  *cannot* import `hdf-cli/internal/*` (Go's internal rule), so the in-process
  harness used for the probe is not portable here. Driving the shipped binary over
  stdio is exactly how real consumers integrate, so the constraint and the honest
  demonstration coincide.
- **Sibling repo keeps the library clean.** No demo code, fixtures, or benchmark
  harness pollutes the shipped CLI (hdf-conmon-demo precedent).

**Instrument constraint (pre-registered for the model-driven phase).** Any future
model-driven arm MUST use a zero-charge inference path — a hosted free-tier API on
an account with **no payment method attached** (hard-refuses at quota, cannot bill)
or a local/self-hosted model. Azure AI Foundry pay-as-you-go is explicitly
**rejected**: Azure offers no hard spend guarantee on PAYG (its spending limit
exists only on credit subscriptions and deactivates on conversion; budgets are
lagging alerts, not brakes), and any overage would hit an org card-on-file that must
not be charged. GitHub Models was retired 2026-07-30. GitHub Actions compute is free
on public repos; only external inference risks charge.

## Alternatives Considered

### Alternative A: Keep it in hdf-cli (in-process, internal imports)
Put the harness under `hdf-cli/benchmarks/`, importing `internal/mcp/{tools,evalharness}`.
- **Pros:** reuse the tool handlers and pinned tokenizer directly; no binary to drive; fastest to write (this is how the probe was built).
- **Cons:** ships demo code + a sample scan corpus inside the library; measures the *internal* surface rather than the consumer path; couples the demo to hdf-cli's private packages.
- **Why rejected:** demonstration code does not belong in the shipped library, and the internal path is not how anyone actually consumes the MCP. The probe used it only as a throwaway signal check.

### Alternative B: Third-party live-service MCP baseline
Compare against per-format MCPs (Semgrep, Snyk, SonarQube, …) for the "multiple MCPs" story.
- **Pros:** superficially an apples-to-apples "one MCP vs. many" comparison.
- **Cons:** those MCPs are live-service/API interfaces, not static-file parsers, so the comparison measures live-API latency, not normalization; each needs a running backend with seeded data (heavy, brittle).
- **Why rejected:** confounded and expensive; a raw-file agent is the cheaper, more representative baseline for the aggregation use case.

### Alternative C: Copilot Agent as the measurement instrument
Use GitHub Copilot Agent + the HDF MCP (per the GitHub Skills tutorial) to produce the numbers.
- **Pros:** GitHub-native; wires the MCP in for free; good publicity surface.
- **Cons:** black-box on token usage; no temperature/K control; its own hidden scaffolding muddies arm-to-arm comparability.
- **Why rejected as the *measurement* instrument** — kept as an optional, numbers-free "it works on a standard setup" showcase.

### Alternative D: GitHub Models as the inference API
- **Why rejected:** GitHub Models was fully retired 2026-07-30.

### Alternative E: Do nothing
Ship the MCP with only the in-repo guide; leave the efficiency claim anecdotal.
- **Consequence:** no worked example for consumers to learn from; the "cheaper/faster" claim stays unverified prose.
- **Why rejected:** the primer has independent teaching value even setting the benchmark aside.

## Consequences

**What becomes easier:**
- Consumers get a runnable, correct-by-example primer for the HDF MCP.
- The efficiency story has reproducible, offline numbers anyone can rerun.
- hdf-libs stays a clean library — no demo code, no sample corpus.

**What becomes harder:**
- Cross-repo coordination: the demo depends on a specific `hdf` version and must pin/document it.
- No reuse of hdf-libs internal helpers — tokenization and any shared logic must come from public libraries or be re-derived.

**Risks:**
- **`hdf` version drift** → pin a tested `hdf` version in the README/CI and state it; treat a newer binary as a separate validation.
- **Token proxy ≠ real agent tokens** → keep the two phases distinct: Phase 2 is a token-cost *demonstration* (no model), Phase 3 is the model-driven accuracy/latency study; never present Phase 2 numbers as end-to-end agent cost.
- **Accuracy floor on weak free models** (Phase 3) → note it; the HDF-vs-raw *delta* stays valid since both arms share the model, but absolute accuracy may be low.
- **Normalization is lossy by design** → include the category-5 "tool-specific field" probes honestly, where HDF can legitimately lose, before calling the efficiency story complete.

## Implementation Plan

### Scope

**IN scope:**
- A standalone Go module driving the shipped `hdf mcp` binary over stdio (public MCP SDK client).
- Offline, pinned tokenization via `github.com/tiktoken-go/tokenizer` (O200k, embedded).
- Vendored small real fixtures with provenance: gosec (SAST), OWASP ZAP (DAST), Grype (vuln); then SPDX (SBOM → HDF System) and InSpec (CCE via legacy ExecJSON).
- A README that is the primer: wiring the MCP into a client, tool sequencing, the handle + source model, read/write posture, plus the efficiency results table.
- This ADR.

**OUT of scope:**
- Any demo/benchmark code in hdf-libs (references only — hdf-libs bead `uqhe.2`).
- A third-party live-service MCP baseline.
- Any paid or uncapped inference (see the instrument constraint).

### Phases

#### Phase 1: Repo foundation (unblocked — start here)
**Files:**
- Create: `go.mod` (module `github.com/mitre/hdf-mcp-demo`), `README.md` (skeleton), `fixtures/` (vendored real inputs + `PROVENANCE.md`), `internal/mcpclient/` (stdio client wrapper over the public SDK), `internal/tok/` (tiktoken-go wrapper), `run.sh` (build/locate `hdf`, run the demo).
- Test: `internal/mcpclient/*_test.go`, `internal/tok/*_test.go`.

**Acceptance criteria:**
- [ ] Module builds with only public deps (MCP SDK + tiktoken-go); no hdf-libs internal imports.
- [ ] The client wrapper starts `hdf mcp`, initializes, and round-trips a tool call over stdio.
- [ ] Tokenizer wrapper returns O200k counts offline (no network).
- [ ] Fixtures are real with documented provenance.

**Verification:** `go test ./...` and `./run.sh` against a built `hdf`.

#### Phase 2: Token-efficiency demo + primer (blocked by Phase 1)
**Files:**
- Create: `cmd/demo/` (drives the question bank, emits the results table), `questions.yaml`, primer sections in `README.md`.
- Port: the probe's question bank and raw-vs-HDF token method, rewritten to the external stdio path.

**Acceptance criteria:**
- [ ] Runs categories 1–4 over gosec/zap/grype via the shipped binary; emits the per-category raw-vs-HDF token table into the README.
- [ ] Adds SPDX (SBOM cross-doc join) and InSpec (CCE) fixtures and category-5 (tool-specific field) probes — the honesty check.
- [ ] README primer sections show correct tool sequencing with runnable examples.
- [ ] Results reproduce offline; the amortized-vs-one-shot caveat is stated.

**Verification:** `./run.sh` reproduces the README table; primer examples run as written.

#### Phase 3: Model-driven two-arm study (blocked by Phase 2; gated)
**Files:**
- Create: `cmd/study/` (two-arm agent loop: raw-file tools vs. HDF MCP), programmatic grading against scripted ground truth, per-category report.

**Acceptance criteria:**
- [ ] Both arms run the same model/params on a zero-charge instrument (per the constraint); tokens + wall-clock + accuracy captured.
- [ ] Accuracy graded by exact/set match against independent ground truth; K trials, ≥2 models.
- [ ] Report is per-category (incl. category 5) with raw-file as denominator, and both conversion-included and amortized views.

**Verification:** the study run reproduces the report; zero external charge incurred.

#### Phase 4: Publicity (optional; gated)
- A manual-trigger (`workflow_dispatch`) GitHub Actions job that builds `hdf`, runs the demo, and publishes results; optional Copilot-Agent numbers-free showcase.

### Verification Strategy
- End-to-end: a fresh clone + a built `hdf` reproduces the README results offline.
- Edge cases: category-5 tool-specific-field probes (where HDF may lose); amortized vs. one-shot conversion cost.
- Cost: Phases 1–2 are model-free (zero cost); Phase 3 is gated on a zero-charge instrument.

### Relationship to hdf-libs tracking
Tracked from the hdf-libs board under epic `uqhe`: `uqhe.1` (the signal probe — its method ports into Phase 2), `uqhe.2` (hdf-libs doc references to this repo). Future Phase 3/4 cards are created only when their phase is reached.
