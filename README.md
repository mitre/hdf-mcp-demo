# hdf-mcp-demo

A worked **primer on using the [HDF MCP server](https://github.com/mitre/hdf-libs)**
(`hdf mcp`) well — and a demonstration of *why* its bounded, normalized tool
surface is so much cheaper for an AI agent than dumping raw scan files into
context.

This repository is an **example consumer** (a sibling to `hdf-conmon-demo`): it
drives the shipped `hdf` binary over stdio exactly the way a real MCP client does,
so nothing here reaches into hdf-libs internals. It teaches by showing, and the
token-efficiency numbers fall out of showing the tool used correctly.

> Design rationale: [`dev-docs/adr-0001-hdf-mcp-demo.md`](dev-docs/adr-0001-hdf-mcp-demo.md).

## Status

Under construction, phased (see the ADR):

- **Phase 1 — foundation** (this): a stdio MCP client over `hdf mcp`, offline
  O200k tokenization, and vendored real fixtures.
- **Phase 2 — demo + primer**: runnable tool-sequencing examples and the
  raw-file-vs-HDF token comparison table.
- **Phase 3 — model-driven study** (gated): a two-arm agent study measuring
  accuracy, tokens, and latency on a zero-charge instrument.
- **Phase 4 — publish** (optional): a manual GitHub Actions run.

## Requirements

- Go (matching hdf-libs' toolchain).
- A built `hdf` binary with the `mcp` command. Point the demo at it with
  `HDF_BIN=/path/to/hdf`, or put `hdf` on your `PATH`. Build it from hdf-libs:

  ```bash
  (cd ../hdf-libs/hdf-cli && go build -o hdf ./cmd/hdf)
  export HDF_BIN="$PWD/../hdf-libs/hdf-cli/hdf"
  ```

## Run

```bash
./run.sh          # locates hdf, runs the test suite (and, from Phase 2, the demo)
```

## Fixtures

Real security-tool output, vendored with provenance — see
[`fixtures/PROVENANCE.md`](fixtures/PROVENANCE.md).
