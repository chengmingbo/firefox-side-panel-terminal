#!/usr/bin/env bash
# Run the extension with live reload during development.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

WEB_EXT="$(command -v web-ext || true)"
[ -z "$WEB_EXT" ] && [ -x "$ROOT/node_modules/.bin/web-ext" ] && WEB_EXT="$ROOT/node_modules/.bin/web-ext"

if [ -n "$WEB_EXT" ]; then
  cd "$ROOT"
  exec "$WEB_EXT" run --source-dir "$ROOT/src" \
    --browser-console \
    --pref "extensions.webextensions.keepStorageOnUninstall=true" \
    "$@"
fi

cat <<EOF
web-ext not found. Install it with:

    npm install          # from the repo root

Or do a one-off load:

    ./scripts/build.sh
    # then in Firefox: about:debugging → This Firefox → Load Temporary Add-on…
    #   → pick build/side-panel-terminal-<version>.xpi
EOF
