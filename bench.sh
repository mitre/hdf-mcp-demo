#!/usr/bin/env bash
# Run the graded two-arm study against local models via Ollama — the
# zero-cost path that needs no gateway, no API key, and no token leaving
# your machine. For the model-free token bookends, run
#   go run ./cmd/benchmark -bookends
#
#   ./bench.sh                      # every tool-capable model you have pulled
#   ./bench.sh gpt-oss:20b          # just these models
#   ./bench.sh -- -repeat 3         # pass extra flags through to cmd/benchmark
#
set -euo pipefail

OLLAMA_URL="${OLLAMA_HOST:-${OLLAMA_BASE_URL:-http://127.0.0.1:11434}}"

# --- hdf binary -------------------------------------------------------------
HDF="${HDF_BIN:-$(command -v hdf || true)}"
if [ -z "${HDF}" ]; then
  echo "error: no hdf binary found." >&2
  echo "  Set HDF_BIN=/path/to/hdf, or put hdf on your PATH. Build it with:" >&2
  echo "    (cd ../hdf-libs/hdf-cli && go build -o hdf ./cmd/hdf)" >&2
  echo "    export HDF_BIN=\"\$PWD/../hdf-libs/hdf-cli/hdf\"" >&2
  exit 1
fi
export HDF_BIN="${HDF}"

# --- ollama -----------------------------------------------------------------
if ! curl -sf -m 5 "${OLLAMA_URL}/api/tags" >/dev/null 2>&1; then
  echo "error: no Ollama server at ${OLLAMA_URL}" >&2
  echo "  Start one with:  ollama serve" >&2
  echo "  (install: https://ollama.com/download)" >&2
  exit 1
fi

# Split "./bench.sh model1 model2 -- -flag" into models and pass-through flags.
MODELS=()
EXTRA=()
seen_sep=0
for arg in "$@"; do
  if [ "${arg}" = "--" ]; then seen_sep=1; continue; fi
  if [ "${seen_sep}" -eq 1 ]; then EXTRA+=("${arg}"); else MODELS+=("${arg}"); fi
done

# Default to every pulled model that can actually call tools — a model without
# tool support cannot drive either arm, so including it would only produce a
# confusing row of failures.
if [ "${#MODELS[@]}" -eq 0 ]; then
  while IFS= read -r m; do
    [ -n "${m}" ] && MODELS+=("${m}")
  done < <(curl -sf "${OLLAMA_URL}/api/tags" |
    python3 -c 'import json,sys; print("\n".join(m["name"] for m in json.load(sys.stdin)["models"] if "tools" in m.get("capabilities",[])))')
fi

if [ "${#MODELS[@]}" -eq 0 ]; then
  echo "error: no tool-capable models are pulled." >&2
  echo "  Pull one, e.g.:  ollama pull gpt-oss:20b" >&2
  echo "  Browse the rest: https://ollama.com/search?c=tools" >&2
  exit 1
fi

JOINED=$(IFS=,; echo "${MODELS[*]}")
mkdir -p results
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
OUT="results/bench-${STAMP}.md"

echo "hdf:     ${HDF_BIN}"
echo "ollama:  ${OLLAMA_URL}"
echo "models:  ${JOINED}"
echo "output:  ${OUT}"
echo
echo "Local inference is slow — a 30B model can take ~30min for the bank alone."
echo

go run ./cmd/benchmark \
  -provider ollama \
  -models "${JOINED}" \
  -format markdown \
  -out "${OUT}" \
  "${EXTRA[@]+"${EXTRA[@]}"}"

echo
echo "wrote ${OUT}"
