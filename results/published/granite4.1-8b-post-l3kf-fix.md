# HDF-MCP benchmark

- **timestamp:** 2026-08-26T13:49:26Z
- **provider:** ollama
- **models:** granite4.1:8b
- **settings:** concurrency=2, max-tokens=4096, max-iters=6, ad-hoc=false, repeat=1, temperature=0, num-ctx=32768
- **cost:** local Ollama inference — no external API call, zero charge incurred

## Bookends (model-free)

rawCeil = whole raw file(s) tokenized; oracle = the hand-written optimal call's response; idealMult = oracle/rawCeil, directly comparable to each question's real mult.

| question | rawCeil | oracle | idealMult | note |
|---|--:|--:|--:|---|
| gosec-distinct-rules | 1537 | 274 | 0.18x | |
| gosec-total-findings | 1537 | — | — | oracle unreachable: result-level finding volume; the read surface projects requirement counts only |
| grype-match-count | 155964 | 295 | 0.0019x | |
| grype-cve-present | 155964 | 232 | 0.0015x | |
| grype-compliance-rate | 155964 | 237 | 0.0015x | |
| grype-has-critical | 155964 | 295 | 0.0019x | |
| grype-related-vulns | 155964 | — | — | oracle unreachable: preserved verbatim in the requirement's code field, which no read tool projects |
| zap-alert-count | 28155 | 274 | 0.0097x | |
| inspec-control-count | 336002 | 288 | 0.00086x | |
| inspec-compliance-rate | 336002 | 271 | 0.00081x | |
| zap-high-severity-count | 28155 | 270 | 0.0096x | |
| cross-format-high-count | 185656 | 749 | 0.004x | |
| grype-fixed-vulns | 307031 | 2717 | 0.0088x | |
| sbom-vuln-free-packages | 177175 | — | — | oracle unreachable: the join key (package inventory) is carried as a BOM reference, not embedded — no HDF document or read tool holds it |

## ollama:granite4.1:8b (14 questions)

| question | type | raw | hdf | rawTok | hdfTok | mult | rawSec | hdfSec | vsCeil | vsOracle |
|---|---|---|---|--:|--:|--:|--:|--:|--:|--:|
| cross-format-high-count | objective | failed | failed | 6318 | 13241 | 2.10x | 25.7 | 20.7 | 0.034x | 18x |
| gosec-distinct-rules | interpretive | correct | correct | 4301 | 5696 | 1.32x | 25.8 | 15.5 | 2.8x | 21x |
| gosec-total-findings | interpretive | correct | failed | 5454 | 14050 | 2.58x | 27.9 | 28.9 | 3.5x | — |
| grype-compliance-rate | hdf-only | failed | correct | 6684 | 5419 | 0.81x | 39.1 | 16.9 | 0.043x | 23x |
| grype-cve-present | objective | correct | correct | 1226 | 5634 | 4.60x | 24.3 | 21.5 | 0.0079x | 24x |
| grype-fixed-vulns | objective | failed | wrong | 5747 | 43555 | 7.58x | 23.9 | 174.4 | 0.019x | 16x |
| grype-has-critical | objective | correct | abstained | 1887 | 8333 | 4.42x | 6.6 | 20.0 | 0.012x | 28x |
| grype-match-count | objective | wrong | correct | 10138 | 15172 | 1.50x | 38.6 | 45.0 | 0.065x | 51x |
| grype-related-vulns | objective | failed | wrong | 4339 | 16157 | 3.72x | 20.3 | 34.0 | 0.028x | — |
| inspec-compliance-rate | objective | failed | correct | 4398 | 5706 | 1.30x | 23.9 | 9.1 | 0.013x | 21x |
| inspec-control-count | objective | abstained | correct | 5020 | 3422 | 0.68x | 37.3 | 4.9 | 0.015x | 12x |
| sbom-vuln-free-packages | raw-only | failed | failed | 5490 | 13732 | 2.50x | 171.6 | 14.7 | 0.031x | — |
| zap-alert-count | objective | correct | wrong | 5348 | 5676 | 1.06x | 15.7 | 11.8 | 0.19x | 21x |
| zap-high-severity-count | objective | wrong | correct | 2372 | 7711 | 3.25x | 11.9 | 13.9 | 0.084x | 29x |

**Accuracy by type** (correct / questions the arm can answer)

| type | raw-file | hdf-mcp |
|---|---|---|
| objective (shared fact) | 3/10 (30%) | 5/10 (50%) |
| interpretive (to intent) | 2/2 (100%) | 1/2 (50%) |
| hdf-only (raw n/a) | n/a | 1/1 (100%) |
| raw-only (hdf n/a) | 0/1 (0%) | n/a |
| **ALL (scored)** | 5/13 (38%) | 7/13 (53%) |

_Raw-file arm on hdf-only questions (out of remit, not scored): 0 hallucinated / 0 abstained of 1._

_HDF-mcp arm on raw-only questions (out of remit, not scored): 0 hallucinated / 0 abstained of 1._

_Failed (errored/over-context/timeout): raw 6, hdf 3._

**Failure reasons** (first error per failed arm)

- raw ×6 — reached max iterations (6) without a final answer
- hdf ×3 — reached max iterations (6) without a final answer

**Token cost** (real prompt+completion usage, tool-schema tax included)

- raw-file arm: **68722**
- hdf-mcp arm (pipeline): **163504** (2.38x vs raw)

**Wall-clock** (sum of arm latencies, seconds)

- raw-file arm: **492.7**
- hdf-mcp arm (pipeline): **431.5** (0.88x vs raw)

**Bookend check** (real arm spend vs the model-free bookends)

- raw arm: **68722** = 3.2% of the whole-file ceiling (2181070)
- hdf arm: **119565** = 20.26x the hand-optimal oracle (5902; over 11 reachable questions)

---

> **Notes / limitations** (read before trusting a number)
>
> Question types are a grading distinction, NOT a ranking. 'objective': one
> answer both arms should reach. 'interpretive': the fair answer depends on the
> question's intent, graded bidirectionally (e.g. distinct rule violations vs raw
> finding volume). 'hdf-only': raw scanners can't natively express it (compliance
> %, effective status) — the raw arm is out of remit. 'raw-only': the HDF view
> lacks the fact (e.g. an SBOM inventory carried as a reference, not embedded)
> — the hdf arm is out of remit.
> Accuracy is scored only over questions an arm can answer. Out-of-remit arms
> report hallucinate-vs-abstain separately, not as a failure.
> Grading parses an 'ANSWER: <value>' line; a correct answer buried in prose
> without that line may read as abstained. Grading favors abstention over false
> credit. No LLM judge is used.
> Cost is real endpoint token usage; the hdf-mcp prompt tokens already include
> the tool-schema tax. Small scans can make HDF cost MORE — expected, and the
> point of measuring rather than assuming.
> A 'failed' arm errored or timed out before answering. The raw arm uses grep +
> paginated reads with NO format hints, so it must discover the schema itself.
> N is small and a single run is noisy; treat as directional, not definitive.
> Bookends are counted in O200k tokens; real usage comes from each model's own
> tokenizer, so bookend ratios are approximate.
> hdf-vs-oracle > 1 even for perfect play: real arms pay the tool-schema tax
> and multi-turn accumulation the oracle excludes — that gap is part of what
> is being measured, not noise.
