# Side Panel Terminal

A native Firefox sidebar that runs a **real shell** in a PTY on your
machine. xterm.js renders the output; a tiny Go helper bridges Firefox's
native messaging protocol to `forkpty`/`ConPTY`. ANSI colors, resize,
Ctrl-C, and cwd persistence all work — it's a terminal, not a web REPL.

## Features

- Native Firefox sidebar (`View → Sidebar → Side Panel Terminal`, or
  **Alt+Shift+T**).
- Real PTY on macOS, Linux (and a stub for Windows ConPTY).
- ANSI colors, line drawing, truecolor, alternate screen buffer
  (`vim`, `htop`, `less` work).
- Customisable shell, cwd, extra env, font, scrollback, cursor blink.
- Keyboard shortcuts:
  - `Alt+Shift+T` — toggle sidebar
  - `Alt+Shift+N` — new session
  - `Alt+Shift+D` — disconnect / reconnect helper
- Per-session reconnect. Sessions are independent shells, so the cwd and
  env of one don't pollute another.
- Settings page with a "Test connection" button.

## Install

The extension is two parts: the WebExtension itself, and a small Go
helper that lives on your machine.

```sh
git clone https://github.com/chengmingbo/firefox-side-panel-terminal.git
cd firefox-side-panel-terminal
./scripts/install.sh         # builds helper + writes Firefox manifest
```

`install.sh` will:

1. Build `helper/firefox_side_panel_terminal_host` (requires Go 1.21+).
2. Copy `native/firefox_side_panel_terminal_host.json` to the right
   Firefox native-messaging directory (macOS: `~/Library/Application
   Support/Mozilla/NativeMessagingHosts/`, Linux: `~/.mozilla/native-
   messaging-hosts/`, Windows: registry), with the absolute path to
   the helper.

Then load the extension:

```sh
./scripts/build.sh
# Firefox → about:debugging → This Firefox → Load Temporary Add-on…
#   → pick build/side-panel-terminal-0.1.0.xpi
```

Restart Firefox. Press **Alt+Shift+T** to open the sidebar. Press
**Enter** in the terminal pane to spawn a session.

To uninstall the helper:

```sh
./scripts/install.sh --uninstall
```

## Project layout

```
src/
├── manifest.json             MV3, sidebar_action, nativeMessaging perm
├── _locales/en/messages.json
├── sidebar/                  the terminal UI (HTML/CSS/JS)
├── options/                  settings page (HTML/CSS/JS)
├── background/background.js  event page: owns the native messaging port
├── common/config.js          shared config (browser.storage.sync)
├── styles/common.css         design tokens
├── vendor/xterm/             vendored xterm.js UMD bundles
└── icons/                    PNG icons

helper/                       Go native messaging host
├── main.go                   framing + dispatch
├── pty_unix.go               creack/pty backend (macOS, Linux, BSD)
├── smoke_test.go             unit tests (framing, base64, env)
└── integration_test.go       PTY round-trip (skipped in CI without pty)

native/
└── firefox_side_panel_terminal_host.json  template manifest

scripts/
├── build.sh                  zip the extension as an unsigned .xpi
├── build-helper.sh           go build the helper
├── install.sh                build helper + write Firefox manifest
├── dev.sh                    web-ext run with live reload
└── make-icons.py             regenerate icons
```

## How it works

- The sidebar is an HTML page that mounts xterm.js. Keystrokes are sent
  to the background worker via `runtime.sendMessage`.
- The background worker is the single `browser.runtime.connectNative`
  endpoint. Firefox only allows one native-messaging port per extension
  per process, so the worker multiplexes per-session PTYs by `id`.
- The Go helper speaks Firefox's native messaging stdio protocol
  (length-prefixed JSON, 4-byte little-endian header) and forwards
  bytes to/from a `creack/pty` master file descriptor.
- All output is base64-encoded so PTY output (UTF-8 plus arbitrary
  ANSI/control bytes) survives JSON encoding.

## Permissions

- `storage` (sync) — settings.
- `nativeMessaging` — talk to the Go helper.
- `clipboardRead`, `clipboardWrite` — copy from the terminal.
- `notifications` — reserved for future errors.

## Security

The helper runs locally as you. Anything you type into the sidebar is
sent straight to a shell. Don't load this extension if you don't trust
the page that's prompting you to install it.

## Limitations

- Windows uses ConPTY via a stub; PRs welcome.
- Only one persistent background-worker connection per Firefox process
  — that's a Firefox limit, not ours.
- "Temporary" add-ons are removed on Firefox restart. For a permanent
  install, submit to AMO.

## License

MIT — see [LICENSE.md](LICENSE.md).
