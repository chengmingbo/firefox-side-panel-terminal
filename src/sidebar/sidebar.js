// Sidebar controller. Owns the xterm.js Terminal and a native-messaging port
// to the PTY helper. Firefox starts one helper process per port, and closing
// the sidebar closes the port, which makes the helper hang up its shells.
//
// Protocol: see helper/main.go. PTY bytes travel as base64 in both
// directions because they are arbitrary bytes, not valid UTF-16 strings.

import {
  getConfig,
  parseEnv,
  STORAGE_KEYS,
  DEFAULT_THEME,
} from "../common/config.js";

const els = {
  host: document.getElementById("terminal-host"),
  term: document.getElementById("terminal"),
  overlay: document.getElementById("overlay"),
  overlayMsg: document.getElementById("overlay-msg"),
  overlayAction: document.getElementById("overlay-action"),
  status: document.getElementById("status"),
  meta: document.getElementById("meta"),
  newSession: document.getElementById("new-session"),
  reconnect: document.getElementById("reconnect"),
  openOptions: document.getElementById("open-options"),
};

// xterm.js core spreads its exports onto globalThis; each addon UMD bundle
// exposes a module object, so the class lives one level down.
const { Terminal } = window;
const { FitAddon } = window.FitAddon;
const WebLinksAddon = window.WebLinksAddon?.WebLinksAddon;

const encoder = new TextEncoder();

const state = {
  config: null,
  term: null,
  fit: null,
  port: null,
  sessionId: null,
  pendingId: null, // id sent in "start", until "started" arrives
  starting: false,
  windowId: null,
};

let nextId = 1;

(async function init() {
  state.config = await getConfig();
  applyTheme(state.config.theme);

  state.term = new Terminal({
    fontFamily: state.config.fontFamily,
    fontSize: state.config.fontSize,
    cursorBlink: state.config.cursorBlink,
    scrollback: state.config.scrollback,
    theme: xtermTheme(),
  });

  state.fit = new FitAddon();
  state.term.loadAddon(state.fit);
  if (WebLinksAddon) {
    state.term.loadAddon(
      new WebLinksAddon((event, uri) => {
        event.preventDefault();
        browser.tabs.create({ url: uri });
      })
    );
  }

  state.term.open(els.term);
  fitNow();
  state.term.onData((data) => {
    if (state.sessionId) {
      sendInput(data);
    } else if (data === "\r" || data === "\n") {
      startSession();
    }
  });
  state.term.onResize(({ cols, rows }) => {
    if (state.sessionId) send({ cmd: "resize", id: state.sessionId, cols, rows });
  });

  new ResizeObserver(() => fitNow()).observe(els.host);

  els.newSession.addEventListener("click", () => restartSession());
  els.reconnect.addEventListener("click", () => restartSession());
  els.openOptions.addEventListener("click", () =>
    browser.runtime.openOptionsPage()
  );
  els.overlayAction.addEventListener("click", onOverlayAction);

  browser.runtime.onMessage.addListener(onRuntimeMessage);
  browser.storage.onChanged.addListener(onStorageChanged);
  browser.windows
    .getCurrent()
    .then((w) => (state.windowId = w.id))
    .catch(() => {});

  startSession();
})();

function applyTheme(theme) {
  document.body.setAttribute("data-theme", theme || DEFAULT_THEME);
}

function xtermTheme() {
  // Terminals read best dark regardless of the chrome theme; only the cursor
  // follows the sidebar's accent colour.
  const cs = getComputedStyle(document.body);
  const accent = cs.getPropertyValue("--spt-accent").trim() || "#ff6a00";
  return {
    background: "#101113",
    foreground: "#e7e8ea",
    cursor: accent,
    cursorAccent: "#101113",
    selectionBackground: "#3a3b40",
    black: "#1f2024",
    red: "#e06c75",
    green: "#98c379",
    yellow: "#e5c07b",
    blue: "#61afef",
    magenta: "#c678dd",
    cyan: "#56b6c2",
    white: "#d8dadd",
    brightBlack: "#5b5d63",
    brightRed: "#ff7a85",
    brightGreen: "#aed487",
    brightYellow: "#f0cd91",
    brightBlue: "#7cc1f5",
    brightMagenta: "#d68fe5",
    brightCyan: "#6cc6d1",
    brightWhite: "#ffffff",
  };
}

function fitNow() {
  try {
    state.fit?.fit();
  } catch (e) {
    // fit throws while the sidebar has no layout yet (collapsed, hidden)
  }
}

// --- native port ---------------------------------------------------------

function ensurePort() {
  if (state.port) return state.port;
  const port = browser.runtime.connectNative(state.config.nativeHostName);
  port.onMessage.addListener(onHelperMessage);
  port.onDisconnect.addListener((p) => {
    if (state.port !== port) return;
    state.port = null;
    const hadSession = !!state.sessionId;
    state.sessionId = null;
    state.pendingId = null;
    state.starting = false;
    const message = p.error?.message || "Native helper exited.";
    if (hadSession) {
      state.term.write(`\r\n\x1b[31m[helper disconnected] ${message}\x1b[0m\r\n`);
      setStatus("disconnected", "disconnected");
    } else {
      showOverlay(friendlyError(message), "options");
      setStatus("error", "error");
    }
  });
  state.port = port;
  return port;
}

function send(msg) {
  try {
    state.port?.postMessage(msg);
  } catch (e) {
    // port already gone; onDisconnect reports it
  }
}

function startSession() {
  if (state.starting || state.sessionId) return;
  state.starting = true;
  hideOverlay();
  setStatus("connecting", "connecting…");
  state.term.reset();
  fitNow();
  try {
    ensurePort();
    const id = `s${nextId++}`;
    state.pendingId = id;
    send({
      cmd: "start",
      id,
      cols: state.term.cols,
      rows: state.term.rows,
      shell: state.config.shell,
      cwd: state.config.cwd,
      env: parseEnv(state.config.env),
    });
  } catch (e) {
    state.starting = false;
    showOverlay(friendlyError(e.message || String(e)), "options");
    setStatus("error", "error");
  }
}

function restartSession() {
  if (state.sessionId) {
    // Ignore the old session's trailing output and "closed" event.
    send({ cmd: "stop", id: state.sessionId });
    state.sessionId = null;
  }
  state.starting = false;
  startSession();
}

function sendInput(data) {
  send({ cmd: "input", id: state.sessionId, data: bytesToBase64(encoder.encode(data)) });
}

function onHelperMessage(m) {
  if (!m || !m.evt) return;
  if (m.id && m.id !== state.sessionId && m.id !== state.pendingId) return;
  switch (m.evt) {
    case "started":
      state.sessionId = m.id;
      state.pendingId = null;
      state.starting = false;
      setStatus("connected", "connected");
      els.meta.textContent = m.title || "";
      state.term.focus();
      // The sidebar may have been resized while the shell was starting.
      send({ cmd: "resize", id: m.id, cols: state.term.cols, rows: state.term.rows });
      break;
    case "output":
      if (m.id === state.sessionId) state.term.write(base64ToBytes(m.data || ""));
      break;
    case "closed":
      if (m.id !== state.sessionId) return;
      state.sessionId = null;
      setStatus("disconnected", "exited");
      els.meta.textContent = "";
      state.term.write(
        `\r\n\x1b[2m[process exited with code ${m.code ?? 0} — press Enter to start a new shell]\x1b[0m\r\n`
      );
      break;
    case "error":
      if (m.id && m.id === state.pendingId) {
        // The session never started (bad shell/cwd).
        state.pendingId = null;
        state.starting = false;
        showOverlay(m.message || "Could not start the shell.", "options");
        setStatus("error", "error");
      } else {
        state.term.write(`\r\n\x1b[31m[helper] ${m.message || "error"}\x1b[0m\r\n`);
      }
      break;
  }
}

// --- messages, settings ---------------------------------------------------

function onRuntimeMessage(msg) {
  if (!msg || !msg.type) return;
  if (msg.windowId != null && state.windowId != null && msg.windowId !== state.windowId) {
    return;
  }
  if (msg.type === "spt/new-session" || msg.type === "spt/reconnect") {
    restartSession();
  }
}

async function onStorageChanged(changes, area) {
  if (area !== "sync") return;
  state.config = await getConfig();
  const t = state.term;
  if (changes[STORAGE_KEYS.theme]) applyTheme(state.config.theme);
  t.options.fontFamily = state.config.fontFamily;
  t.options.fontSize = state.config.fontSize;
  t.options.cursorBlink = state.config.cursorBlink;
  t.options.scrollback = state.config.scrollback;
  t.options.theme = xtermTheme();
  fitNow();
}

// --- UI helpers -----------------------------------------------------------

function setStatus(stateName, label) {
  els.status.dataset.state = stateName;
  els.status.textContent = label || stateName;
}

function showOverlay(message, action) {
  els.overlay.hidden = false;
  els.overlayMsg.textContent = message;
  els.overlayAction.hidden = false;
  els.overlayAction.dataset.action = action || "retry";
  els.overlayAction.textContent = action === "options" ? "Open settings" : "Retry";
}

function hideOverlay() {
  els.overlay.hidden = true;
}

function onOverlayAction(e) {
  if (e.currentTarget.dataset.action === "options") {
    browser.runtime.openOptionsPage();
  }
  hideOverlay();
  startSession();
}

function friendlyError(msg) {
  if (/native messaging host not found|No such native application/i.test(msg)) {
    return "The native helper isn't registered with Firefox. Run ./scripts/install.sh from the extension's folder, then press Retry.";
  }
  return msg;
}

// --- encoding ------------------------------------------------------------

function bytesToBase64(bytes) {
  if (bytes.toBase64) return bytes.toBase64();
  let bin = "";
  for (let i = 0; i < bytes.length; i += 0x8000) {
    bin += String.fromCharCode.apply(null, bytes.subarray(i, i + 0x8000));
  }
  return btoa(bin);
}

function base64ToBytes(b64) {
  if (Uint8Array.fromBase64) return Uint8Array.fromBase64(b64);
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}
