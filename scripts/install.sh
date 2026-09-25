#!/usr/bin/env bash
# Install the native messaging host manifest for Firefox.
#
# Firefox looks for native messaging manifests at:
#   macOS:   ~/Library/Application Support/Mozilla/NativeMessagingHosts/
#   Linux:   ~/.mozilla/native-messaging-hosts/
#   Windows: HKEY_CURRENT_USER\Software\Mozilla\NativeMessagingHosts\<name>
#
# Usage:
#   ./scripts/install.sh                          # builds helper if missing, then installs to default profile
#   ./scripts/install.sh --uninstall              # removes the manifest (helper binary stays on disk)
#   HELPER_PATH=/custom/path ./scripts/install.sh # override the helper binary path
#
# After install, restart Firefox and open the sidebar (Alt+Shift+T).

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
NAME="firefox_side_panel_terminal_host"
MANIFEST_TEMPLATE="$ROOT/native/${NAME}.json"

# Locate the OS-appropriate Firefox native messaging directory.
native_dir() {
  case "$(uname -s)" in
    Darwin)
      echo "$HOME/Library/Application Support/Mozilla/NativeMessagingHosts"
      ;;
    Linux)
      echo "$HOME/.mozilla/native-messaging-hosts"
      ;;
    MINGW*|MSYS*|CYGWIN*|Windows*)
      echo "$USERPROFILE\\Mozilla\\NativeMessagingHosts"
      ;;
    *)
      echo "error: unsupported OS $(uname -s)" >&2
      exit 1
      ;;
  esac
}

if [ "${1:-}" = "--uninstall" ]; then
  TARGET="$(native_dir)/${NAME}.json"
  if [ -f "$TARGET" ]; then
    rm -f "$TARGET"
    echo "→ removed $TARGET"
  else
    echo "→ no manifest to remove at $TARGET"
  fi
  exit 0
fi

HELPER="${HELPER_PATH:-$ROOT/helper/${NAME}}"

# Build helper if missing.
if [ ! -x "$HELPER" ]; then
  echo "→ helper not found, building"
  "$ROOT/scripts/build-helper.sh" "$HELPER"
fi

HELPER_ABS="$(cd "$(dirname "$HELPER")" && pwd)/$(basename "$HELPER")"
echo "→ helper binary: $HELPER_ABS"

TARGET_DIR="$(native_dir)"
mkdir -p "$TARGET_DIR"
TARGET="$TARGET_DIR/${NAME}.json"

# Materialise the manifest with the resolved helper path.
sed "s|/ABSOLUTE/PATH/TO/firefox-side-panel-terminal-host|$HELPER_ABS|" \
  "$MANIFEST_TEMPLATE" > "$TARGET"

echo "→ wrote manifest: $TARGET"
echo
echo "Restart Firefox, then open the sidebar (Alt+Shift+T)."
echo "The sidebar will show a 'connecting…' overlay that goes away once"
echo "the extension finds the helper."
