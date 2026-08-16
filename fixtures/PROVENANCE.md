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
