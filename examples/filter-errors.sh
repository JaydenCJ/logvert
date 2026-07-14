#!/usr/bin/env bash
# Normalize examples/mixed.log and keep only error-and-worse events.
# Shows logvert as a pipe stage: mixed formats in, one grep-able JSONL out.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="${LOGVERT:-$ROOT/logvert}"
if [ ! -x "$BIN" ]; then
  echo "building logvert…" >&2
  (cd "$ROOT" && go build -o "$ROOT/logvert" ./cmd/logvert)
fi

# --assume-year pins the BSD syslog line's missing year for reproducibility.
"$BIN" --assume-year 2026 "$ROOT/examples/mixed.log" \
  | grep -E '"level":"(error|fatal)"'
