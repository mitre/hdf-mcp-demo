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
