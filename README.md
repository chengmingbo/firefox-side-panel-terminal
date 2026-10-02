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
  - `Alt+Shift+D` — restart the shell
- One shell per sidebar: each window's sidebar gets its own helper
  process and shell, so windows never share state. Closing the sidebar
  hangs up its shell.
- Settings apply live (font, size, scrollback, cursor blink, theme).
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

1. Build `helper/firefox_side_panel_terminal_host` if it isn't there
   (requires Go 1.23+, e.g. `brew install go`).
2. Copy `native/firefox_side_panel_terminal_host.json` to the right
   Firefox native-messaging directory (macOS: `~/Library/Application
   Support/Mozilla/NativeMessagingHosts/`, Linux: `~/.mozilla/native-
   messaging-hosts/`, Windows: registry), with the absolute path to
   the helper.

Then load the extension:

```sh
./scripts/build.sh
# Firefox → about:debugging → This Firefox → Load Temporary Add-on…
#   → pick build/side-panel-terminal-0.2.0.xpi
```

Restart Firefox after the first `install.sh` so it picks up the helper
manifest. Press **Alt+Shift+T** to open the sidebar; a shell starts
automatically. After the shell exits, press **Enter** for a new one.

Temporary add-ons are removed when Firefox restarts. To keep it
installed without signing, use Firefox Nightly or Developer Edition: set
`xpinstall.signatures.required` to `false` in `about:config`, then
`about:addons` → gear → **Install Add-on From File…**. Release Firefox
needs a signed build (`npx web-ext sign --channel unlisted` with AMO API
keys).

To uninstall the helper:

```sh
./scripts/install.sh --uninstall
```

## Project layout

```
src/
├── manifest.json             MV3, sidebar_action, nativeMessaging perm (Firefox 140+)
├── _locales/en/messages.json
├── sidebar/                  the terminal UI (HTML/CSS/JS)
├── options/                  settings page (HTML/CSS/JS)
├── background/background.js  event page: routes keyboard commands
├── common/config.js          shared config (browser.storage.sync)
├── styles/common.css         design tokens
├── vendor/xterm/             vendored xterm.js UMD bundles
└── icons/                    PNG icons

helper/                       Go native messaging host
├── main.go                   framing + dispatch
├── pty_unix.go               creack/pty backend (macOS, Linux, BSD)
├── host_test.go              framing, env, shell, ping
└── host_unix_test.go         real PTY session over pipes (resize, exit code, stop)

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

- The sidebar is an HTML page that mounts xterm.js and opens its own
  `browser.runtime.connectNative` port. Firefox starts one helper process
  per port and closes the helper's stdin when the port goes away, at
  which point the helper hangs up its shells and exits. (Keeping the port
  in the MV3 background event page would lose it whenever Firefox
  suspends the idle page.)
- The background page only routes the keyboard shortcuts to the sidebar
  of the window they were pressed in.
- The Go helper speaks Firefox's native messaging stdio protocol
  (length-prefixed JSON, 4-byte little-endian header) and forwards
  bytes to/from a `creack/pty` master file descriptor.
- PTY bytes are base64-encoded in both directions so arbitrary bytes
  (partial UTF-8, ANSI/control sequences) survive JSON encoding. The
  sidebar hands xterm.js raw bytes, which decodes UTF-8 across chunk
  boundaries.
- Shells start as login shells (`-l`) so `PATH` is correct even when
  Firefox was launched from the Dock with a minimal environment.

## Permissions

- `storage` (sync) — settings.
- `nativeMessaging` — talk to the Go helper.
- `clipboardWrite` — the settings page's "Copy install command" buttons.

## Security

The helper runs locally as you. Anything you type into the sidebar is
sent straight to a shell. Don't load this extension if you don't trust
the page that's prompting you to install it.

## Development

```sh
(cd helper && go test -race ./...)   # helper tests, including a real PTY
npm run lint                          # web-ext lint
npm run dev                           # web-ext run with live reload
```

## Limitations

- Windows (ConPTY) is not implemented yet; PRs welcome.
- "Temporary" add-ons are removed on Firefox restart; see Install for
  permanent options.

## License

MIT — see [LICENSE.md](LICENSE.md).
