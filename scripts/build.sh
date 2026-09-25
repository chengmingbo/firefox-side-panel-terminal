#!/usr/bin/env bash
# Build an unsigned .xpi ready to load into Firefox via about:debugging.
#
# Usage:  ./scripts/build.sh [version]
#
# Firefox needs manifest.json at the zip root, and the .xpi extension
# ignored when loaded as a temporary add-on.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERSION="${1:-$(node -p "require('$ROOT/src/manifest.json').version" 2>/dev/null || jq -r .version "$ROOT/src/manifest.json")}"
STAGE="$ROOT/build/stage"
OUT="$ROOT/build/side-panel-terminal-${VERSION}.xpi"

rm -rf "$STAGE"
mkdir -p "$STAGE" "$ROOT/build"

cp -R "$ROOT/src/." "$STAGE/"

# (No stripping needed — vendor/ is part of the extension.)

# Validate JSON.
for f in "$STAGE/manifest.json" "$STAGE/_locales/en/messages.json"; do
  if command -v jq >/dev/null 2>&1; then
    jq -e . "$f" >/dev/null
  else
    python3 -c "import json,sys; json.load(open(sys.argv[1]))" "$f"
  fi
done

# Vendored xterm.css / *.js are not validated as JSON, but we sanity-check
# that they exist and look like xterm.
test -s "$STAGE/vendor/xterm/xterm.js"
test -s "$STAGE/vendor/xterm/xterm.css"

( cd "$STAGE" && zip -X -q -0 -r "$OUT" . )

echo "wrote $OUT ($(du -h "$OUT" | cut -f1))"
echo
echo "To load: about:debugging → This Firefox → Load Temporary Add-on…"
echo "         → pick $OUT"
echo
echo "Then run ./scripts/install.sh (or open the sidebar's settings page)"
echo "to register the native helper with Firefox."
