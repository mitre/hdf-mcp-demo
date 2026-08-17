# truth/testdata provenance

`gosec.hdf.json` is the HDF Results conversion of `fixtures/gosec.json`, produced
by the shipped converter:

```
hdf convert fixtures/gosec.json --from gosec -o gosec.hdf.json
```

- Source binary: `hdf` built from `github.com/mitre/hdf-libs` at commit `d8aa51b3`
  (Go 1.26).
- Why vendored: it is only ~8 KB and lets the classifier's headline pins
  (7-vs-3 Class B, rule-existence Class A, compliance-rate Class C) run fully
  offline with no `hdf` binary at test time. The larger grype/inspec conversions
  (~1 MB each) are NOT vendored — those pins live in `truth_convert_test.go`,
  gated on `HDF_BIN`.
- `fixtures/gosec.json` provenance: see `fixtures/PROVENANCE.md` (vendored
  byte-identical from hdf-libs).
