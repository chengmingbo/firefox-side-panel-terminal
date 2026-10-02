#!/usr/bin/env bash
# Build the Go helper binary.
#
# Usage:   ./scripts/build-helper.sh                # outputs to helper/firefox_side_panel_terminal_host
#          ./scripts/build-helper.sh /path/to/out
#
# Honours $GOPROXY (defaults to https://proxy.golang.org,direct).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${1:-$ROOT/helper/firefox_side_panel_terminal_host}"
mkdir -p "$(dirname "$OUT")"

if ! command -v go >/dev/null 2>&1; then
  echo "error: Go toolchain not found on PATH." >&2
  echo "       Install Go 1.23+ (e.g. `brew install go`) from https://go.dev/dl/ and re-run." >&2
  exit 1
fi

cd "$ROOT/helper"

if [ ! -f go.sum ] || [ -z "$(ls go.sum 2>/dev/null)" ]; then
  echo "→ resolving Go modules"
  go mod tidy
fi

echo "→ building helper"
go build -trimpath -ldflags="-s -w" -o "$OUT" .

echo "→ wrote $OUT ($(du -h "$OUT" | cut -f1))"
