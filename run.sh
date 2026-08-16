#!/usr/bin/env bash
# Locate a built `hdf` binary and run the demo's test suite against it.
# Phase 2 extends this to also run the token-efficiency demo (cmd/demo).
set -euo pipefail

HDF="${HDF_BIN:-$(command -v hdf || true)}"
if [ -z "${HDF}" ]; then
  echo "error: no hdf binary found." >&2
  echo "  Set HDF_BIN=/path/to/hdf, or put hdf on your PATH." >&2
  echo "  Build it from hdf-libs:" >&2
  echo "    (cd ../hdf-libs/hdf-cli && go build -o hdf ./cmd/hdf)" >&2
  echo "    export HDF_BIN=\"\$PWD/../hdf-libs/hdf-cli/hdf\"" >&2
  exit 1
fi
export HDF_BIN="${HDF}"
echo "Using hdf: ${HDF_BIN}"

go test ./... "$@"

echo
echo "=== token-efficiency demo ==="
go run ./cmd/demo
