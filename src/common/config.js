// Shared configuration for Side Panel Terminal.
// Stored in browser.storage.sync so it roams between signed-in profiles.

export const STORAGE_KEYS = Object.freeze({
  shell: "spt.shell",
  cwd: "spt.cwd",
  env: "spt.env",
  cols: "spt.cols",
  rows: "spt.rows",
  fontSize: "spt.fontSize",
  fontFamily: "spt.fontFamily",
  theme: "spt.theme",
  scrollback: "spt.scrollback",
  cursorBlink: "spt.cursorBlink",
  helperPath: "spt.helperPath",
  nativeHostName: "spt.nativeHostName",
});

export const DEFAULT_SHELL = ""; // empty => helper uses $SHELL / /bin/bash
export const DEFAULT_CWD = ""; // empty => $HOME
export const DEFAULT_ENV = ""; // newline-separated KEY=VALUE
export const DEFAULT_COLS = 100;
export const DEFAULT_ROWS = 28;
export const DEFAULT_FONT_SIZE = 13;
export const DEFAULT_FONT_FAMILY =
  'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace';
export const DEFAULT_THEME = "auto";
export const DEFAULT_SCROLLBACK = 5000;
export const DEFAULT_CURSOR_BLINK = true;
export const DEFAULT_HELPER_PATH = ""; // discovered at install time
export const DEFAULT_NATIVE_HOST_NAME = "firefox_side_panel_terminal_host";

export async function getConfig() {
  const stored = await browser.storage.sync.get(null);
  return {
    shell: stored[STORAGE_KEYS.shell] ?? DEFAULT_SHELL,
    cwd: stored[STORAGE_KEYS.cwd] ?? DEFAULT_CWD,
    env: stored[STORAGE_KEYS.env] ?? DEFAULT_ENV,
    cols: intOr(stored[STORAGE_KEYS.cols], DEFAULT_COLS),
    rows: intOr(stored[STORAGE_KEYS.rows], DEFAULT_ROWS),
    fontSize: intOr(stored[STORAGE_KEYS.fontSize], DEFAULT_FONT_SIZE),
    fontFamily: stored[STORAGE_KEYS.fontFamily] ?? DEFAULT_FONT_FAMILY,
    theme: stored[STORAGE_KEYS.theme] ?? DEFAULT_THEME,
    scrollback: intOr(stored[STORAGE_KEYS.scrollback], DEFAULT_SCROLLBACK),
    cursorBlink: stored[STORAGE_KEYS.cursorBlink] !== false,
    helperPath: stored[STORAGE_KEYS.helperPath] ?? DEFAULT_HELPER_PATH,
    nativeHostName:
      stored[STORAGE_KEYS.nativeHostName] ?? DEFAULT_NATIVE_HOST_NAME,
  };
}

export async function setConfig(partial) {
  const map = {};
  for (const [k, v] of Object.entries(partial)) {
    map[STORAGE_KEYS[k] ?? k] = v;
  }
  return browser.storage.sync.set(map);
}

export function parseEnv(text) {
  const out = {};
  if (!text) return out;
  for (const line of String(text).split(/\r?\n/)) {
    const t = line.trim();
    if (!t || t.startsWith("#")) continue;
    const eq = t.indexOf("=");
    if (eq < 0) continue;
    const k = t.slice(0, eq).trim();
    let v = t.slice(eq + 1).trim();
    if (
      (v.startsWith('"') && v.endsWith('"')) ||
      (v.startsWith("'") && v.endsWith("'"))
    ) {
      v = v.slice(1, -1);
    }
    if (k) out[k] = v;
  }
  return out;
}

function intOr(v, fallback) {
  const n = parseInt(v, 10);
  return Number.isFinite(n) ? n : fallback;
}
