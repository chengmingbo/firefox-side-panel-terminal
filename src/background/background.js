// Background service worker. Owns the single native messaging port to the
// PTY helper, and multiplexes per-session PTYs across the sidebar.
//
// Protocol with the Go helper (one JSON object per line, newline-delimited):
//   browser → helper:
//     {"cmd":"start","id":"<uuid>","cols":N,"rows":N,"shell":"...","cwd":"...","env":{...}}
//     {"cmd":"input","id":"<uuid>","data":"<base64 raw bytes>"}
//     {"cmd":"resize","id":"<uuid>","cols":N,"rows":N}
//     {"cmd":"stop","id":"<uuid>"}
//     {"cmd":"shutdown"}
//
//   helper → browser:
//     {"evt":"started","id":"<uuid>","title":"zsh - 100x28"}
//     {"evt":"output","id":"<uuid>","data":"<base64 raw bytes>"}
//     {"evt":"closed","id":"<uuid>","code":0}
//     {"evt":"error","id":"<uuid>","message":"..."}
//
// Base64 is used because PTY output is arbitrary bytes (UTF-8, ANSI escapes,
// control chars, etc.) and JSON strings must be valid UTF-16.

import {
  getConfig,
  parseEnv,
  DEFAULT_NATIVE_HOST_NAME,
} from "../common/config.js";

const sessions = new Map(); // sessionId → { alive, closed }
let nativePort = null;
let nextLocalId = 1;

browser.runtime.onInstalled.addListener(() => {
  // No-op — sessions are opened on demand from the sidebar.
});

browser.commands.onCommand.addListener(async (command) => {
  if (command === "toggle-sidebar") {
    await toggleSidebar();
  } else if (command === "new-session") {
    await notifyAll({ type: "spt/new-session" });
    await toggleSidebar();
  } else if (command === "disconnect") {
    await notifyAll({ type: "spt/reconnect" });
  }
});

browser.runtime.onMessage.addListener((msg, sender) => {
  if (!msg || !msg.type) return false;
  switch (msg.type) {
    case "spt/open-session":
      return handleOpenSession(msg.payload);
    case "spt/input":
      forwardToHelper("input", msg.payload);
      return false;
    case "spt/close-session":
      forwardToHelper("stop", { id: msg.payload.sessionId });
      return false;
    case "spt/list-sessions":
      return Promise.resolve({
        sessions: Array.from(sessions.keys()).map((id) => ({ id })),
      });
    case "spt/ping-helper":
      return pingHelper();
    default:
      return false;
  }
});

async function handleOpenSession(opts) {
  const cfg = await getConfig();
  try {
    const port = await ensurePort(cfg.nativeHostName || DEFAULT_NATIVE_HOST_NAME);
    const id = nextSessionId();
    sessions.set(id, { alive: true, closed: false });
    sendCmd(port, {
      cmd: "start",
      id,
      cols: opts && opts.cols ? opts.cols : 100,
      rows: opts && opts.rows ? opts.rows : 28,
      shell: (opts && opts.shell) || "",
      cwd: (opts && opts.cwd) || "",
      env: parseEnv(opts && opts.env) || {},
    });
    return { ok: true, sessionId: id };
  } catch (e) {
    return { ok: false, error: String(e && e.message ? e.message : e) };
  }
}

function ensurePort(hostName) {
  return new Promise((resolve, reject) => {
    if (nativePort) {
      resolve(nativePort);
      return;
    }
    try {
      const port = browser.runtime.connectNative(hostName);
      let resolved = false;
      port.onMessage.addListener((m) => onHelperMessage(m));
      port.onDisconnect.addListener((p) => {
        const err =
          p && p.error ? p.error : { message: "native host disconnected" };
        nativePort = null;
        // Mark all sessions as dead; sidebar will see a disconnect message.
        for (const id of sessions.keys()) {
          sessions.set(id, { alive: false, closed: true });
          notifyAll({
            type: "spt/error",
            payload: { sessionId: id, message: err.message },
          });
          notifyAll({
            type: "spt/closed",
            payload: { sessionId: id, code: -1 },
          });
        }
        if (!resolved) reject(new Error(err.message));
      });
      nativePort = port;
      resolved = true;
      resolve(port);
    } catch (e) {
      reject(e);
    }
  });
}

async function pingHelper() {
  try {
    await ensurePort(
      (await getConfig()).nativeHostName || DEFAULT_NATIVE_HOST_NAME
    );
    return { ok: true };
  } catch (e) {
    return { ok: false, error: String(e && e.message ? e.message : e) };
  }
}

function sendCmd(port, obj) {
  port.postMessage(obj);
}

function forwardToHelper(cmd, payload) {
  if (!nativePort) return;
  sendCmd(nativePort, { cmd, ...payload });
}

function onHelperMessage(m) {
  if (!m || !m.evt) return;
  switch (m.evt) {
    case "started":
      notifyAll({
        type: "spt/title",
        payload: { sessionId: m.id, title: m.title || "" },
      });
      break;
    case "output":
      notifyAll({
        type: "spt/output",
        payload: { sessionId: m.id, data: m.data || "" },
      });
      break;
    case "closed": {
      const s = sessions.get(m.id);
      if (s) s.closed = true;
      notifyAll({
        type: "spt/closed",
        payload: { sessionId: m.id, code: m.code ?? 0 },
      });
      break;
    }
    case "error":
      notifyAll({
        type: "spt/error",
        payload: { sessionId: m.id, message: m.message || "" },
      });
      break;
  }
}

async function notifyAll(message) {
  try {
    await browser.runtime.sendMessage(message);
  } catch (e) {
    // No listeners (sidebar closed). Drop the message — the sidebar will
    // reconnect when it next opens.
  }
}

function nextSessionId() {
  // 8 hex chars — readable in logs, collision space is fine for a single
  // user's sessions.
  const n = (nextLocalId++).toString(16).padStart(4, "0");
  return `${Date.now().toString(36)}-${n}`;
}

async function toggleSidebar() {
  try {
    if (browser.sidebarAction.toggle) {
      await browser.sidebarAction.toggle();
      return;
    }
  } catch (e) {
    // fall through
  }
  try {
    await browser.sidebarAction.open();
  } catch (e) {
    await browser.sidebarAction.close();
  }
}
