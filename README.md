# hdf-mcp-demo

A worked **primer on using the [HDF MCP server](https://github.com/mitre/hdf-libs)**
(`hdf mcp`) well — and a demonstration of *why* its bounded, normalized tool
surface is dramatically cheaper for an AI agent than dumping raw scan files into
context.

This repository is an **example consumer** (a sibling to `hdf-conmon-demo`): it
drives the shipped `hdf` binary over stdio exactly the way a real MCP client does,
so nothing here reaches into hdf-libs internals. It teaches by showing — and the
token-efficiency numbers fall out of showing the tool used correctly.

> Design rationale: [`dev-docs/adr-0001-hdf-mcp-demo.md`](dev-docs/adr-0001-hdf-mcp-demo.md).

## Why an HDF MCP at all

Security pipelines produce *unlike* scan output — SAST, DAST, SBOM, vulnerability,
and compliance (CCE) results, each in its own vendor shape. An agent asked to
reason across them can either haul every raw file into its context and reconcile
five schemas by hand, or normalize everything to one HDF shape and ask a small,
typed, token-bounded tool surface. This repo measures the difference.

## Requirements

- Go (matching hdf-libs' toolchain).
- A built `hdf` binary with the `mcp` command. Point the demo at it with
  `HDF_BIN=/path/to/hdf`, or put `hdf` on your `PATH`:

  ```bash
  (cd ../hdf-libs/hdf-cli && go build -o hdf ./cmd/hdf)
  export HDF_BIN="$PWD/../hdf-libs/hdf-cli/hdf"
  ```

## Run

```bash
./run.sh          # locates hdf, runs the tests, then prints the demo table
```

## Using the HDF MCP well — the patterns this demo shows

**1. Talk to it over stdio.** The client launches `hdf mcp` as a subprocess and
speaks JSON-RPC over stdin/stdout. See [`internal/mcpclient`](internal/mcpclient)
for a ~60-line client over the official MCP SDK — that is the whole integration.

**2. Normalize once, then query.** Bring every source into HDF first, then ask
questions against the normalized documents:

- Scan formats (gosec, ZAP, Grype, …) → the **`hdf_convert`** tool.
- SBOMs (SPDX/CycloneDX) → **`hdf system create --from spdx`** (inventory becomes
  an HDF *System* document).
- Legacy InSpec ExecJSON → **`hdf convert`** (auto-detected).

**3. Pass handles, not documents.** Every tool returns a compact **summary plus a
reusable handle** — never a multi-megabyte body. Reuse the handle across a
multi-step workflow instead of re-sending the document.

**4. Sequence the read tools by intent:**

| Want | Call |
|------|------|
| Detect type + validity + a headline summary | `hdf_open` |
| Structure/metadata (counts, components, inventories) | `hdf_inspect` |
| The requirements themselves (filter by status/severity/id/tag) | `hdf_query` |
| Compliance % + status/severity rollups + threshold verdict | `hdf_compliance` |
| What changed between two documents | `hdf_diff` |

**5. Reads degrade, writes refuse.** A structurally-valid but schema-imperfect
document still opens (`valid:false` + best-effort summary); a write tool refuses
to emit a document that does not validate. Writes are gated by
`HDF_MCP_ENABLE_WRITES` (off by default) — a disabled deployment returns a
preview, never a silent no-op.

## The efficiency result

For each question, the demo compares the tokens a **raw-file agent** must ingest
(the whole source file[s]) against the tokens of the **bounded HDF MCP
response(s)** that answer it — counted with the same O200k encoding the server
budgets against. Model-free, offline, reproducible via `./run.sh`.

```
category                              raw tok    HDF tok   raw/HDF
------------------------------------------------------------------
1 single-fact (small src)                1537        274      5.6x
1 single-fact (large src)              155964       2774     56.2x
2 cross-format aggregate               185656        704    263.7x
2 cross-doc join (SBOM x vuln)         156715       3026     51.8x
3 compliance rollup (vuln)             155964        240    649.9x
3 compliance rollup (CCE)              336002        268   1253.7x
4 temporal diff                        185759       2669     69.6x
5 tool-specific field (HDF loses)      155964     158739      1.0x
------------------------------------------------------------------
TOTAL                                 1333561     168694      7.9x
```

HDF is leaner everywhere the question is answerable from normalized fields — by
50–1250× on the realistic large-scan, cross-source, and rollup cases. **Category 5
is deliberately the honest counter-example:** it asks for a tool-specific field
(grype's `matchDetails`) that HDF normalization *drops*, so the agent must fall
back to the raw scan — HDF adds overhead and **loses** (1.0×). Normalization is
lossy by design; this is where that costs you.

### Read the numbers honestly

- **Token *ingest*, not end-to-end agent cost.** This measures what enters
  context. Real agent accuracy and latency are the model-driven study (Phase 3).
- **Amortized (pipeline) case.** The HDF side assumes documents are already
  normalized. A one-shot "convert then ask once" pays the conversion cost too.
- **A whole-system result.** The win is HDF-MCP-as-a-system (normalization + a
  token-bounded surface), not the schema alone.

## Fixtures

Real security-tool output, vendored with provenance — see
[`fixtures/PROVENANCE.md`](fixtures/PROVENANCE.md).

## Status

Phased (see the ADR): Phase 1 (foundation) and Phase 2 (this demo + primer) are
in place. Phase 3 (a model-driven two-arm accuracy/latency study on a zero-charge
instrument) and Phase 4 (an optional manual CI run) are future work.
