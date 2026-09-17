# Fixture provenance

All fixtures are **real** security-tool output, vendored from the hdf-libs
converter test corpus (which sources them from real tool runs / upstream project
test data). They are used verbatim as the demo's input scans. No data is
fabricated or reshaped.

| File | Source format | Origin (hdf-libs converter fixture) |
|------|---------------|-------------------------------------|
| `gosec.json` | gosec (Go SAST) | `hdf-converters/converters/gosec-to-hdf/fixtures/input/real.json` |
| `zap.json` | OWASP ZAP (DAST) | `hdf-converters/converters/zap-to-hdf/fixtures/input/webgoat.json` |
| `grype.json` | Grype (vulnerability) | `hdf-converters/converters/grype-to-hdf/fixtures/input/anchore_grype.json` |
| `grype-prev.json` | Grype (vulnerability) | `hdf-converters/converters/grype-to-hdf/fixtures/input/amazon.json` — a second scan, used as the "previous" side of the temporal-diff example |
| `inspec.json` | InSpec ExecJSON (CCE / CIS compliance) | `hdf-diff/test/fixtures/ubuntu-22-hardened.json` — a hardened Ubuntu 22.04 InSpec scan (InSpec 7.x exec-json); normalized to HDF via `hdf convert` |
| `spdx.json` | SPDX SBOM (inventory) | `hdf-converters/shared/bom-fixtures/spdx-sbom.json` — a real SPDX SBOM; normalized to an HDF System via `hdf system create --from spdx` |

Note: the older `legacy-inspec-exec.json` (InSpec 4.19.2) was rejected as the
InSpec fixture — the shipped `legacyhdf` converter emits empty `descriptions[]`
for that exec-json version, which fails HDF schema validation (tracked as
hdf-libs bead jbli). Newer exec-json (used here) converts cleanly.

## Paired fixtures for the join and temporal-diff questions

Generated locally on 2026-08-19 (hdf-libs-uqhe.12) because the pre-existing
fixtures could not support these questions: `grype-prev.json` scans a different
system than `grype.json` (Amazon Linux + Python vs Alpine — zero vulnerability-id
overlap, so a "temporal diff" was fiction), and `spdx.json` is a Java SBOM sharing
no package with the Alpine scan, so every join answered a degenerate 0.

Both grype scans were run in one sitting against the same grype version and
vulnerability database, so the difference between them reflects the images and not
database drift.

| File | Source format | Origin |
|------|---------------|--------|
| `grype-alpine311.json` | Grype (vulnerability) | `grype alpine:3.11 -o json` — the earlier side of the temporal pair |
| `grype-alpine312.json` | Grype (vulnerability) | `grype alpine:3.12 -o json` — the later side of the temporal pair |
| `spdx-alpine312.json` | SPDX SBOM (inventory) | `syft alpine:3.12 -o spdx-json` — the SBOM of the SAME image as the later grype scan, so package names genuinely join |

**Tooling:** grype 0.117.0, syft 1.42.3, captured 2026-08-19.

**Images:**

- `alpine:3.11` → `alpine@sha256:bcae378eacedab83da66079d9366c8f5df542d7ed9ab23bf487e3e1a8481375d`
- `alpine:3.12` → `alpine@sha256:c75ac27b49326926b803b9ed43bf088bc220d22556de1bc5f72d742c91398f69`

**Overlap sanity check** — all four numbers must be > 0 or the questions are
degenerate:

| Measure | Value |
|---------|------:|
| vulnerability ids in 3.11 | 49 |
| vulnerability ids in 3.12 | 45 |
| unchanged (intersection) | 44 |
| new in 3.12 | 1 |
| fixed since 3.11 | 5 |
| SBOM packages | 15 |
| grype artifacts (3.12) | 7 |
| SBOM ∩ grype artifacts | 7 |

**Choice of tags.** alpine 3.19/3.20 and 3.13/3.14 were both measured and rejected:
each yields new-in-later = 0, failing the overlap criterion. Successive Alpine point
releases overwhelmingly *fix* vulnerabilities rather than introduce them, so
`new = 1` is the best available and a diff question should lean on
*fixed-since* (5) or *unchanged* (44) rather than *new* (1), which is small enough
for a model to hit by guessing.

**Converter acceptance**, verified before vendoring: `hdf convert --from grype`
succeeds and validates on both scans; `hdf system create --from spdx` succeeds on
the SBOM.

**Known limitation for the join question.** `hdf system create --from spdx` does
NOT ingest the package inventory — it emits a single component (`alpine`, type
application) holding a *reference* to the SBOM file, so the 15 package names never
become queryable data. The raw arm can therefore join SBOM packages against grype
artifacts while the HDF arm cannot, making the SBOM×vuln join a genuine raw-only
(Class D) question until inventory ingestion lands (hdf-libs-ixfr).

## Merged pipeline sample

`merged-gosec-zap-grype.hdf.json` is what a pipeline hands the MCP once it has
normalized each scan and combined them with `hdf merge` (hdf-libs ADR-0016): one
HDF results document, one baseline per scanner run, each named
`<tool>/<original name>` and labelled `tool` / `toolVersion` / `sourceDocument`,
with every scan's own root provenance kept under `extensions["hdf-merge"]`.
It is **derived**, not vendored — regenerate it with the same three commands and
it matches byte-for-byte apart from timestamps: gosec output carries no
timestamp, so `hdf convert --from gosec` stamps the conversion time into its
`results[].startTime`, which the merge then carries as the root `timestamp` and
the gosec provenance entry. ZAP and grype keep their own timestamps. The merge
itself is deterministic; the gated test `TestMergedSample_MatchesPipelineOutput`
in `internal/benchmark` regenerates the sample and compares it with timestamps
masked:

```
hdf convert --from gosec fixtures/gosec.json -o gosec.hdf.json
hdf convert --from zap   fixtures/zap.json   -o zap.hdf.json
hdf convert --from grype fixtures/grype.json -o grype.hdf.json
hdf merge gosec.hdf.json zap.hdf.json grype.hdf.json -o merged-gosec-zap-grype.hdf.json
```

| Measure | Value |
|---------|------:|
| baselines | 6 (`gosec/gosec Scan`; `owasp zap/OWASP ZAP Scan: <site>` ×4; `grype/golang:1.12-alpine`) |
| requirements | 120 (3 + 28 + 89) |
| components | 5 |
| distinct CWE ids (gosec ∪ ZAP; grype carries none) | 10 |
| ZAP findings at impact ≥ 0.7 | 3 |
| failed requirements mapped to NIST family SC | 16 |

**Tooling:** `hdf` built from hdf-libs `feat/multi-scanner-merge` at `69799f2c`
(the branch that adds `hdf merge`; ADR-0016), generated 2026-09-16.
sha256 `a8d2dcfed8abff6bfd860f617664fa687454fb0d9c231b90ab4380eeaca781ca`.

The benchmark does **not** read this file: `merged-*` questions merge at staging
time from the raw fixtures so they track the converters. The file exists so the
document a pipeline would produce can be inspected, and pointed at by an MCP
`HDF_MCP_ROOT`, without running anything.
