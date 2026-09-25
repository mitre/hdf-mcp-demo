# HDF-MCP benchmark

- **timestamp:** 2026-08-26T14:06:32Z
- **provider:** ollama
- **models:** gpt-oss:20b
- **settings:** concurrency=2, max-tokens=4096, max-iters=6, ad-hoc=false, repeat=1, temperature=0, num-ctx=32768
- **cost:** local Ollama inference — no external API call, zero charge incurred

## Bookends (model-free)

rawCeil = whole raw file(s) tokenized; oracle = the hand-written optimal call's response; idealMult = oracle/rawCeil, directly comparable to each question's real mult.

| question | rawCeil | oracle | idealMult | note |
|---|--:|--:|--:|---|
| gosec-distinct-rules | 1537 | 271 | 0.18x | |
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
| cross-format-high-count | 185656 | 746 | 0.004x | |
| grype-fixed-vulns | 307031 | 2717 | 0.0088x | |
| sbom-vuln-free-packages | 177175 | — | — | oracle unreachable: the join key (package inventory) is carried as a BOM reference, not embedded — no HDF document or read tool holds it |

## ollama:gpt-oss:20b (14 questions)

| question | type | raw | hdf | rawTok | hdfTok | mult | rawSec | hdfSec | vsCeil | vsOracle |
|---|---|---|---|--:|--:|--:|--:|--:|--:|--:|
| cross-format-high-count | objective | failed | failed | 11468 | 9452 | 0.82x | 30.3 | 48.6 | 0.062x | 13x |
| gosec-distinct-rules | interpretive | correct | correct | 7095 | 3373 | 0.48x | 40.1 | 14.0 | 4.6x | 12x |
| gosec-total-findings | interpretive | correct | wrong | 2572 | 5234 | 2.03x | 19.7 | 23.4 | 1.7x | — |
| grype-compliance-rate | hdf-only | failed | correct | 8818 | 3161 | 0.36x | 36.8 | 11.4 | 0.057x | 13x |
| grype-cve-present | objective | correct | wrong | 1118 | 2911 | 2.60x | 7.6 | 15.7 | 0.0072x | 13x |
| grype-fixed-vulns | objective | failed | wrong | 560 | 5742 | 10.25x | 6.8 | 21.1 | 0.0018x | 2.1x |
| grype-has-critical | objective | failed | correct | 984 | 2978 | 3.03x | 12.9 | 13.5 | 0.0063x | 10x |
| grype-match-count | objective | correct | correct | 4362 | 3127 | 0.72x | 32.8 | 13.1 | 0.028x | 11x |
| grype-related-vulns | objective | correct | failed | 3322 | 12265 | 3.69x | 27.0 | 72.6 | 0.021x | — |
| inspec-compliance-rate | objective | failed | correct | 3346 | 3323 | 0.99x | 22.7 | 27.2 | 0.01x | 12x |
| inspec-control-count | objective | failed | correct | 4440 | 5217 | 1.18x | 27.2 | 20.0 | 0.013x | 18x |
| sbom-vuln-free-packages | raw-only | failed | failed | 5534 | 10523 | 1.90x | 22.0 | 38.4 | 0.031x | — |
| zap-alert-count | objective | failed | correct | 6263 | 4874 | 0.78x | 47.8 | 47.8 | 0.22x | 18x |
| zap-high-severity-count | objective | failed | correct | 13006 | 3144 | 0.24x | 38.6 | 17.5 | 0.46x | 12x |

**Accuracy by type** (correct / questions the arm can answer)

| type | raw-file | hdf-mcp |
|---|---|---|
| objective (shared fact) | 3/10 (30%) | 6/10 (60%) |
| interpretive (to intent) | 2/2 (100%) | 1/2 (50%) |
| hdf-only (raw n/a) | n/a | 1/1 (100%) |
| raw-only (hdf n/a) | 0/1 (0%) | n/a |
| **ALL (scored)** | 5/13 (38%) | 8/13 (61%) |

_Raw-file arm on hdf-only questions (out of remit, not scored): 0 hallucinated / 0 abstained of 1._

_HDF-mcp arm on raw-only questions (out of remit, not scored): 0 hallucinated / 0 abstained of 1._

_Failed (errored/over-context/timeout): raw 9, hdf 3._

**Failure reasons** (first error per failed arm)

- raw ×6 — reached max iterations (6) without a final answer
- raw ×1 — ollama error: error parsing tool call: raw='{"name":"grype-alpine311.json","offset":0,"limit":20"}', err=invalid character '"' after object key:value pair
- raw ×1 — ollama error: error parsing tool call: raw='{"name":"grype.json","offset":0,"limit":200"}', err=invalid character '"' after object key:value pair
- raw ×1 — ollama error: error parsing tool call: raw='{"name":"inspec.json","offset":120000,"limit":20"}', err=invalid character '"' after object key:value pair
- hdf ×1 — ollama error: error parsing tool call: raw='{"limit":0,"page":0,"severity":["high","critical"],"source":{"path":"grype.hdf.json"}{"verbosity":"concise"}', err=invalid character '{' after object key:value pair
- hdf ×1 — ollama error: error parsing tool call: raw='{"source":{"path":"grype-alpine312.hdf.json"}","baseline":"alpine:3.12","limit":0,"page":0,"verbosity":"concise"}', err=invalid character '"' after object key:value pair
- hdf ×1 — reached max iterations (6) without a final answer

**Token cost** (real prompt+completion usage, tool-schema tax included)

- raw-file arm: **72888**
- hdf-mcp arm (pipeline): **75324** (1.03x vs raw)

**Wall-clock** (sum of arm latencies, seconds)

- raw-file arm: **372.3**
- hdf-mcp arm (pipeline): **384.2** (1.03x vs raw)

**Bookend check** (real arm spend vs the model-free bookends)

- raw arm: **72888** = 3.3% of the whole-file ceiling (2181070)
- hdf arm: **47302** = 8.02x the hand-optimal oracle (5896; over 11 reachable questions)

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
