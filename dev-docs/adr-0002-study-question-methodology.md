# ADR-0002: Fair two-arm question methodology, and multi-model runs

**Date:** 2026-08-17
**Status:** proposed
**Deciders:** Will Dower

## Context

The Phase-3 two-arm loop (ADR-0001) works, but the first live run (gemma-4 over
gosec) exposed a methodology problem that would poison any naive accuracy measure:

**HDF normalization changes the data's semantics, so "the same question" can have
different *correct* answers per arm.** Concretely, raw `gosec.json` has **7**
findings; converting to HDF yields **3** rule-level requirements (gosec's findings
deduplicate by rule). The raw-file arm answered "7", the HDF arm "3" — and *neither
is wrong*. They answered different questions over differently-shaped data.

Two more realities frame the study and must be recorded:

- **The divergence is often a feature.** "3 distinct rule violations" is frequently
  the more useful signal than "7 raw findings" — but not always ("total findings
  emitted" is the right number for triage volume / SLA counting). So the useful
  intent runs in *both* directions depending on the question.
- **Two real cost workflows.** In a *pipeline*, a bulk `hdf convert`+`validate` job
  normalizes all artifacts out-of-band, then an MCP-armed agent queries them —
  conversion cost ≈ 0 per query. In an *ad-hoc* workflow (a non-technical user
  asking their agent about a raw scan), the agent must convert on demand or work
  raw — conversion cost is borne per session. Both are legitimate.

Finally, a convenience need surfaced while iterating: running several models in one
invocation rather than re-exporting `OPENAI_MODEL` each time.

## Decision

**1. Classify every question, compute ground truth per data view, and grade each
arm against the class-appropriate answer.** Three classes:

- **A — Answer-preserving.** Facts that survive normalization identically (extrema
  like "highest CVSS", existence like "is CVE-X present", specific field values).
  One ground truth, verified identical raw↔HDF. This is the fair apples-to-apples
  backbone: same answer expected, so it measures **reliability + cost** on a shared
  fact (where HDF's edge is deterministic tools + fewer tokens at scale, not a
  different number).
- **B — Normalization-divergent.** Counts/groupings where dedup or status/severity
  mapping changes the value (the 7-vs-3). Graded against the question's **stated
  intent**, and the set is **bidirectional**: some questions target the rule-level
  intent ("how many distinct rule violations" — HDF wins, raw must manually dedup),
  some target the raw-volume intent ("how many findings did the scanner emit" — raw
  wins, HDF's dedup makes it wrong). Never only the HDF-flattering direction.
- **C — HDF-native.** Capabilities raw scanners don't natively express (compliance
  %, effective-status, threshold verdicts, cross-format joins, temporal diffs). HDF
  *adds* capability; grade the HDF arm for correctness and measure how often the raw
  arm invents a plausible-but-wrong answer.

**2. Vet every question with a ground-truth *classifier* before the grader runs.**
For each candidate question, compute the **raw-view answer** (from the raw fixture)
and the **HDF-view answer** (from the converted document) independently and
programmatically, and tag it A/B/C by whether they agree. A question's fairness
becomes a *computed fact*, not a judgment call — and the 7-vs-3 case is mechanically
flagged (Class B, raw=7 / HDF=3) rather than silently mis-graded.

**3. Report two cost views, tied to workflows.** *Amortized/pipeline* (HDF cost
excluding conversion) and *conversion-included/ad-hoc* (HDF cost including
`hdf_convert`). Report both, labeled by workflow; do not pick a winner.

**4. Multi-model runs.** Support `OPENAI_MODEL_LIST` (comma-delimited) so one
invocation iterates models, falling back to the singleton `OPENAI_MODEL`. Iterate
**serially by default** (each model is a Go sub-test via `t.Run(model, …)`), with
parallelism opt-in — a shared, cold-start-prone gateway thrashes under concurrent
model loads, so serial is the safe default.

## Alternatives Considered

### Alternative A: One ground truth per question for all arms
- **Pros:** simplest grader.
- **Cons:** unfair on any normalization-divergent question — penalizes whichever arm's (correct-for-its-view) answer doesn't match the single chosen number.
- **Why rejected:** it would score the 7-vs-3 divergence as an accuracy failure, turning noise into fake signal.

### Alternative B: Exclude divergent questions entirely
- **Pros:** avoids the fairness problem cheaply.
- **Cons:** throws away the questions that best characterize HDF's actual value (deterministic dedup/normalization) and its semantic footprint.
- **Why rejected:** the divergence is informative; the fix is to grade to intent, not to avoid it.

### Alternative C: Keep divergent questions but only the HDF-favorable intent
- **Pros:** makes HDF look strong.
- **Cons:** rigs the result — dishonest.
- **Why rejected:** the benchmark's worth is its honesty; Class B is bidirectional by decision.

### Alternative D: Do nothing — build the grader against the current loop
- **Consequence:** accuracy numbers dominated by semantic divergence, not model capability — noise dressed as signal.
- **Why rejected:** it's the exact trap the gemma run exposed.

## Consequences

**What becomes easier:**
- A defensible accuracy measure — every question's fairness is computed, not asserted.
- HDF's real value *and* its semantic footprint are both characterized honestly.
- Convenient multi-model runs.

**What becomes harder:**
- Dual ground-truth computation per question means the classifier must parse each raw scanner format (gosec, ZAP, grype, InSpec ExecJSON, SPDX) *and* the HDF view — real work, format by format.

**Risks:**
- **Mis-classification** → mitigated by the classifier computing both views from data rather than guessing.
- **Parallel model runs thrash the shared gateway** (cold-start eviction, rate limits) → mitigated by serial-by-default iteration.
- **A "fact" assumed answer-preserving actually diverges on some fixture** → the classifier runs across all fixtures and demotes it to Class B if any disagree.

## Implementation Plan

### Scope

**IN:** the classifier harness, the vetted question bank (A + bidirectional B + C),
the grader + per-model/per-category report with both cost views, and the
multi-model refactor.

**OUT:** changing HDF's normalization behavior; any paid instrument (ADR-0001
constraint stands).

### Phases

#### Phase 3a: Multi-model refactor (unblocked — do first; it's the convenience unlock)
**Files:**
- Create: `internal/instrument/models.go` — `ModelsFromEnv()` (`OPENAI_MODEL_LIST` comma-split, trimmed; else `OPENAI_MODEL`; else empty).
- Modify: the live tests (`openai_live_test.go`, `agent_live_test.go`) — wrap each in `for _, m := range ModelsFromEnv() { t.Run(m, …) }`, serial by default.

**Acceptance criteria:**
- [ ] `OPENAI_MODEL_LIST="gemma-4,gpt-oss-120b"` runs both as sub-tests in one invocation; `OPENAI_MODEL` alone still works (back-compatible).
- [ ] Serial by default; no concurrent model loads unless explicitly opted in.

**Verification:** `OPENAI_MODEL_LIST=… go test ./internal/... -run Live -v` shows a sub-test per model.

#### Phase 3b: Ground-truth classifier (blocked by the question set shape)
**Files:**
- Create: `internal/truth/` — per-question `rawAnswer(fixture)` + `hdfAnswer(hdfDoc)` functions and an A/B/C classifier that runs both across all fixtures.
- Test: classifier resolves 7-vs-3 as Class B (raw=7 / HDF=3), and confirms a chosen extremum/existence question as Class A.

**Acceptance criteria:**
- [ ] Every candidate question is tagged A/B/C from computed raw-view and HDF-view answers.
- [ ] Ground truth is derived independently from the data — never read back from the arm under test.

#### Phase 3c: Vetted question bank
Class A backbone + bidirectional Class B + Class C, each with its computed ground
truth and intent annotation.

#### Phase 3d: Grader + report
Score each arm against the class-appropriate truth; per-model, per-category report
with both cost views (amortized and conversion-included), raw-file as denominator.

#### Optional: `nono wrap` tool-exec hardening mode (off by default; ADR-0001).

### Verification Strategy
- The classifier's own tests pin the 7-vs-3 (Class B) and a Class-A example.
- The grader is only run against questions the classifier has tagged.
- Reproducible offline for the classifier/ground-truth; model-driven parts gated on the zero-charge instrument.
