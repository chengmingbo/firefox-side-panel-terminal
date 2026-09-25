// Sidebar controller. Owns the xterm.js Terminal and proxies keystrokes
// to the background worker via runtime messages. The background worker is
// the single native messaging client — every Firefox sidebar document and
// popup would otherwise spawn its own, which native messaging forbids.

import { getConfig, DEFAULT_THEME } from "../common/config.js";

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

// xterm.js is loaded as a UMD script that exports window.Terminal / window.FitAddon.
const Term = window.Terminal;
const FitAddon = window.FitAddon;
const WebLinksAddon = window.WebLinksAddon;

const state = {
  config: null,
  term: null,
  fit: null,
  webLinks: null,
  sessionId: null,
  sessionCount: 0,
  connecting: false,
};

(async function init() {
  state.config = await getConfig();
  applyTheme(state.config.theme);

  state.term = new Term({
    fontFamily: state.config.fontFamily,
    fontSize: state.config.fontSize,
    cursorBlink: state.config.cursorBlink,
    scrollback: state.config.scrollback,
    theme: xtermTheme(),
    allowProposedApi: true,
    convertEol: false,
  });

  state.fit = new FitAddon();
  state.term.loadAddon(state.fit);

  if (WebLinksAddon) {
    state.webLinks = new WebLinksAddon();
    state.term.loadAddon(state.webLinks);
  }

  state.term.open(els.term);
  fitNow();
  state.term.writeln("\x1b[2mSide Panel Terminal — press Enter to connect.\x1b[0m");
  state.term.onData((data) => {
    if (!state.sessionId) {
      // Buffer input until a session is alive.
      if (data === "\r" || data === "\n") startSession();
      return;
    }
    sendInput(data);
  });
  state.term.focus();

  const ro = new ResizeObserver(() => fitNow());
  ro.observe(els.host);

  window.addEventListener("resize", fitNow);

  els.newSession.addEventListener("click", () => startSession());
  els.reconnect.addEventListener("click", () => reconnect());
  els.openOptions.addEventListener("click", () =>
    browser.runtime.openOptionsPage()
  );
  els.overlayAction.addEventListener("click", onOverlayAction);

  browser.runtime.onMessage.addListener(onRuntimeMessage);
})();

function applyTheme(theme) {
  const t = theme || DEFAULT_THEME;
  document.body.setAttribute("data-theme", t);
}

function xtermTheme() {
  // Pull a couple of CSS variables so the terminal picks up the sidebar's
  // chrome. Falls back to a sensible dark scheme when the document is in
  // auto/light mode (terminals look better dark by default).
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
    if (state.fit) state.fit.fit();
  } catch (e) {
    // ignore — fit may throw during initial layout
  }
}

async function startSession() {
  if (state.connecting) return;
  state.connecting = true;
  showOverlay("Connecting to native helper…", null, false);
  setStatus("connecting", "Connecting…");
  state.term.clear();
  try {
    const resp = await browser.runtime.sendMessage({
      type: "spt/open-session",
      payload: await sessionOptions(),
    });
    if (!resp || !resp.ok) {
      throw new Error((resp && resp.error) || "Failed to open session.");
    }
    state.sessionId = resp.sessionId;
    state.sessionCount++;
    hideOverlay();
    setStatus("connected", "connected");
    fitNow();
  } catch (e) {
    showOverlay(
      friendlyError(e && e.message ? e.message : String(e)),
      "install-helper",
      true
    );
    setStatus("error", "error");
  } finally {
    state.connecting = false;
  }
}

async function sessionOptions() {
  const cfg = await getConfig();
  return {
    cols: state.term.cols,
    rows: state.term.rows,
    shell: cfg.shell,
    cwd: cfg.cwd,
    env: cfg.env,
  };
}

function sendInput(data) {
  if (!state.sessionId) return;
  browser.runtime.sendMessage({
    type: "spt/input",
    payload: { sessionId: state.sessionId, data },
  });
}

async function onRuntimeMessage(msg) {
  if (!msg || !msg.type) return;
  switch (msg.type) {
    case "spt/output":
      if (
        state.sessionId &&
        msg.payload &&
        msg.payload.sessionId === state.sessionId
      ) {
        state.term.write(msg.payload.data);
      }
      break;
    case "spt/closed":
      if (msg.payload && msg.payload.sessionId === state.sessionId) {
        state.sessionId = null;
        setStatus("disconnected", "disconnected");
        state.term.writeln(
          `\r\n\x1b[2m[connection closed — press Enter to reconnect]\x1b[0m`
        );
      }
      break;
    case "spt/error":
      if (msg.payload && msg.payload.sessionId === state.sessionId) {
        state.term.write(`\r\n\x1b[31m[helper error] ${msg.payload.message}\x1b[0m\r\n`);
      }
      break;
    case "spt/title":
      if (msg.payload && msg.payload.title) {
        els.meta.textContent = msg.payload.title;
      }
      break;
  }
}

async function reconnect() {
  if (state.sessionId) {
    await browser.runtime.sendMessage({
      type: "spt/close-session",
      payload: { sessionId: state.sessionId },
    });
    state.sessionId = null;
  }
  startSession();
}

function setStatus(stateName, label) {
  els.status.dataset.state = stateName;
  els.status.textContent = label || stateName;
}

function showOverlay(message, action, showAction) {
  els.overlay.hidden = false;
  els.overlayMsg.textContent = message;
  els.overlayAction.hidden = !showAction;
  els.overlayAction.textContent =
    action === "install-helper" ? "Open install instructions" : "Retry";
  els.overlayAction.dataset.action = action || "";
}

function hideOverlay() {
  els.overlay.hidden = true;
}

async function onOverlayAction(e) {
  const action = e.currentTarget.dataset.action;
  if (action === "install-helper") {
    browser.runtime.openOptionsPage();
  } else {
    hideOverlay();
    startSession();
  }
}

function friendlyError(msg) {
  if (/Specified native messaging host not found/i.test(msg)) {
    return "Native messaging host not found. Open Settings → Install helper to register it with Firefox.";
  }
  if (/No such file/i.test(msg) || /not found/i.test(msg)) {
    return msg + " — open Settings to set the helper path.";
  }
  return msg;
}
