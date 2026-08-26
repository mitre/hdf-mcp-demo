# HDF-MCP benchmark

- **timestamp:** 2026-08-26T14:23:42Z
- **provider:** ollama
- **models:** granite4.1:8b, gpt-oss:20b
- **settings:** concurrency=2, max-tokens=4096, max-iters=6, ad-hoc=true, repeat=3, temperature=0, num-ctx=32768
- **cost:** local Ollama inference — no external API call, zero charge incurred

## Bookends (model-free)

rawCeil = whole raw file(s) tokenized; oracle = the hand-written optimal call's response; idealMult = oracle/rawCeil, directly comparable to each question's real mult.

| question | rawCeil | oracle | idealMult | note |
|---|--:|--:|--:|---|
| gosec-distinct-rules | 1537 | 279 | 0.18x | |
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
| cross-format-high-count | 185656 | 754 | 0.0041x | |
| grype-fixed-vulns | 307031 | 2717 | 0.0088x | |
| sbom-vuln-free-packages | 177175 | — | — | oracle unreachable: the join key (package inventory) is carried as a BOM reference, not embedded — no HDF document or read tool holds it |

## ollama:granite4.1:8b (14 questions)

| question | type | raw | hdf | rawTok | hdfTok | mult | rawSec | hdfSec | vsCeil | vsOracle | adhocTok |
|---|---|---|---|--:|--:|--:|--:|--:|--:|--:|--:|
| cross-format-high-count | objective | failed | failed | 6318 | 13221 | 2.09x | 30.3 | 15.3 | 0.034x | 18x | 11569 |
| gosec-distinct-rules | interpretive | correct | correct | 4301 | 5684 | 1.32x | 27.1 | 17.2 | 2.8x | 20x | 12597 |
| gosec-total-findings | interpretive | correct | failed | 5454 | 13983 | 2.56x | 24.5 | 39.7 | 3.5x | — | 11222 |
| grype-compliance-rate | hdf-only | failed | correct | 6684 | 5419 | 0.81x | 24.4 | 16.5 | 0.043x | 23x | 14335 |
| grype-cve-present | objective | correct | correct | 1226 | 5634 | 4.60x | 12.0 | 26.8 | 0.0079x | 24x | 14114 |
| grype-fixed-vulns | objective | failed | wrong | 5577 | 43555 | 7.81x | 14.8 | 145.5 | 0.018x | 16x | 14866 |
| grype-has-critical | objective | correct | abstained | 1887 | 8334 | 4.42x | 12.0 | 19.6 | 0.012x | 28x | 14458 |
| grype-match-count | objective | wrong | correct | 10138 | 15172 | 1.50x | 35.2 | 35.2 | 0.065x | 51x | 13946 |
| grype-related-vulns | objective | failed | wrong | 4339 | 16157 | 3.72x | 18.7 | 34.0 | 0.028x | — | 14681 |
| inspec-compliance-rate | objective | failed | correct | 4398 | 5706 | 1.30x | 21.3 | 14.5 | 0.013x | 21x | 14048 |
| inspec-control-count | objective | abstained | correct | 5013 | 3422 | 0.68x | 25.3 | 4.1 | 0.015x | 12x | 8893 |
| sbom-vuln-free-packages | raw-only | failed | failed | 5490 | 13700 | 2.50x | 54.7 | 12.8 | 0.031x | — | 17558 |
| zap-alert-count | objective | correct | wrong | 5348 | 5676 | 1.06x | 15.2 | 17.0 | 0.19x | 21x | 15329 |
| zap-high-severity-count | objective | wrong | correct | 2372 | 7711 | 3.25x | 12.2 | 15.8 | 0.084x | 29x | 11495 |

**Accuracy by type** (correct / questions the arm can answer)

| type | raw-file | hdf-mcp |
|---|---|---|
| objective (shared fact) | 9/30 (30%) | 15/30 (50%) |
| interpretive (to intent) | 6/6 (100%) | 3/6 (50%) |
| hdf-only (raw n/a) | n/a | 3/3 (100%) |
| raw-only (hdf n/a) | 0/3 (0%) | n/a |
| **ALL (scored)** | 15/39 (38%) | 21/39 (53%) |

_Raw-file arm on hdf-only questions (out of remit, not scored): 0 hallucinated / 0 abstained of 1._

_HDF-mcp arm on raw-only questions (out of remit, not scored): 0 hallucinated / 0 abstained of 1._

_Failed (errored/over-context/timeout): raw 6, hdf 3._

**Failure reasons** (first error per failed arm)

- raw ×6 — reached max iterations (6) without a final answer
- hdf ×3 — reached max iterations (6) without a final answer

**Token cost** (real prompt+completion usage, tool-schema tax included)

- raw-file arm: **68545**
- hdf-mcp arm (pipeline): **163374** (2.38x vs raw)
- hdf-mcp arm (ad-hoc convert): **189111** (2.76x vs raw)

**Wall-clock** (sum of arm latencies, seconds)

- raw-file arm: **327.8**
- hdf-mcp arm (pipeline): **414.1** (1.26x vs raw)
- hdf-mcp arm (ad-hoc convert): **495.4** (1.51x vs raw)

**Bookend check** (real arm spend vs the model-free bookends)

- raw arm: **68545** = 3.1% of the whole-file ceiling (2181070)
- hdf arm: **119534** = 20.22x the hand-optimal oracle (5912; over 11 reachable questions)

## ollama:gpt-oss:20b (14 questions)

| question | type | raw | hdf | rawTok | hdfTok | mult | rawSec | hdfSec | vsCeil | vsOracle | adhocTok |
|---|---|---|---|--:|--:|--:|--:|--:|--:|--:|--:|
| cross-format-high-count | objective | failed | failed | 9608 | 9814 | 1.02x | 26.6 | 51.6 | 0.052x | 13x | 10477 |
| gosec-distinct-rules | interpretive | correct | correct | 3259 | 2786 | 0.85x | 15.3 | 20.4 | 2.1x | 10x | 9050 |
| gosec-total-findings | interpretive | correct | wrong | 2613 | 7881 | 3.02x | 14.5 | 46.0 | 1.7x | — | 14772 |
| grype-compliance-rate | hdf-only | failed | correct | 8801 | 3143 | 0.36x | 31.0 | 23.6 | 0.056x | 13x | 12471 |
| grype-cve-present | objective | correct | wrong | 1118 | 2911 | 2.60x | 10.8 | 15.2 | 0.0072x | 13x | 11272 |
| grype-fixed-vulns | objective | failed | wrong | 600 | 5811 | 9.69x | 22.2 | 20.1 | 0.002x | 2.1x | 4669 |
| grype-has-critical | objective | failed | correct | 984 | 3515 | 3.57x | 13.7 | 11.9 | 0.0063x | 12x | 9758 |
| grype-match-count | objective | failed | correct | 5242 | 3163 | 0.60x | 46.3 | 15.1 | 0.034x | 11x | 8858 |
| grype-related-vulns | objective | failed | failed | 7038 | 30052 | 4.27x | 58.6 | 98.2 | 0.045x | — | 13171 |
| inspec-compliance-rate | objective | failed | wrong | 4545 | 3256 | 0.72x | 26.0 | 23.0 | 0.014x | 12x | 8773 |
| inspec-control-count | objective | failed | correct | 4436 | 6663 | 1.50x | 27.9 | 28.8 | 0.013x | 23x | 8639 |
| sbom-vuln-free-packages | raw-only | failed | failed | 5413 | 13433 | 2.48x | 35.8 | 30.1 | 0.031x | — | 20762 |
| zap-alert-count | objective | failed | correct | 6988 | 5164 | 0.74x | 42.6 | 56.9 | 0.25x | 19x | 11637 |
| zap-high-severity-count | objective | failed | correct | 9816 | 3094 | 0.32x | 31.2 | 15.7 | 0.35x | 11x | 10069 |

**Accuracy by type** (correct / questions the arm can answer)

| type | raw-file | hdf-mcp |
|---|---|---|
| objective (shared fact) | 4/30 (13%) | 16/30 (53%) |
| interpretive (to intent) | 6/6 (100%) | 2/6 (33%) |
| hdf-only (raw n/a) | n/a | 3/3 (100%) |
| raw-only (hdf n/a) | 0/3 (0%) | n/a |
| **ALL (scored)** | 10/39 (25%) | 21/39 (53%) |

_Raw-file arm on hdf-only questions (out of remit, not scored): 0 hallucinated / 0 abstained of 1._

_HDF-mcp arm on raw-only questions (out of remit, not scored): 0 hallucinated / 0 abstained of 1._

_Failed (errored/over-context/timeout): raw 11, hdf 3._

**Failure reasons** (first error per failed arm)

- raw ×6 — reached max iterations (6) without a final answer
- raw ×1 — ollama error: error parsing tool call: raw='{"name":"grype-alpine311.json","offset":0,"limit":20"}', err=invalid character '"' after object key:value pair
- raw ×1 — ollama error: error parsing tool call: raw='{"name":"grype.json","offset":0,"limit":20"}', err=invalid character '"' after object key:value pair
- raw ×1 — ollama error: error parsing tool call: raw='{"name":"grype.json","offset":0,"limit":200"}', err=invalid character '"' after object key:value pair
- raw ×1 — ollama error: error parsing tool call: raw='{"name":"zap.json","offset":0,"limit":20"}', err=invalid character '"' after object key:value pair
- raw ×1 — ollama error: error parsing tool call: raw='{"name":"zap.json","offset":0,"limit":200"}', err=invalid character '"' after object key:value pair
- hdf ×2 — reached max iterations (6) without a final answer
- hdf ×1 — ollama error: error parsing tool call: raw='{"severity":["high","critical"],"source":{"path":"grype.hdf.json"}.."}', err=invalid character '.' after object key:value pair

**Token cost** (real prompt+completion usage, tool-schema tax included)

- raw-file arm: **70461**
- hdf-mcp arm (pipeline): **100686** (1.43x vs raw)
- hdf-mcp arm (ad-hoc convert): **154378** (2.19x vs raw)

**Wall-clock** (sum of arm latencies, seconds)

- raw-file arm: **402.4**
- hdf-mcp arm (pipeline): **456.5** (1.13x vs raw)
- hdf-mcp arm (ad-hoc convert): **896.4** (2.23x vs raw)

**Bookend check** (real arm spend vs the model-free bookends)

- raw arm: **70461** = 3.2% of the whole-file ceiling (2181070)
- hdf arm: **49320** = 8.34x the hand-optimal oracle (5912; over 11 reachable questions)

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
> Pipeline view assumes conversion happened out-of-band (cost ~0/query); ad-hoc
> view charges the agent's on-demand hdf_convert round-trip.
> Bookends are counted in O200k tokens; real usage comes from each model's own
> tokenizer, so bookend ratios are approximate.
> hdf-vs-oracle > 1 even for perfect play: real arms pay the tool-schema tax
> and multi-turn accumulation the oracle excludes — that gap is part of what
> is being measured, not noise.
