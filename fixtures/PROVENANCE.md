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

SPDX (SBOM → HDF System) and InSpec (CCE via the legacy ExecJSON path) fixtures
are added in Phase 2 (bead uqhe.4), each with provenance recorded here.
