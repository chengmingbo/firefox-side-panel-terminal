#!/usr/bin/env bash
# Run the extension with live reload during development.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

if command -v web-ext >/dev/null 2>&1; then
  cd "$ROOT"
  exec web-ext run --source-dir "$ROOT/src" \
    --browser-console \
    --pref "extensions.webextensions.keepStorageOnUninstall=true" \
    "$@"
fi

cat <<EOF
web-ext not found on PATH. Install it with:

    npm install --global web-ext

Or do a one-off load:

    ./scripts/build.sh
    # then in Firefox: about:debugging → This Firefox → Load Temporary Add-on…
    #   → pick build/side-panel-terminal-<version>.xpi
EOF
