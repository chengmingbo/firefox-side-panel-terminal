import {
  getConfig,
  setConfig,
  DEFAULT_SHELL,
  DEFAULT_CWD,
  DEFAULT_ENV,
  DEFAULT_FONT_SIZE,
  DEFAULT_FONT_FAMILY,
  DEFAULT_SCROLLBACK,
  DEFAULT_CURSOR_BLINK,
  DEFAULT_THEME,
  DEFAULT_NATIVE_HOST_NAME,
} from "../common/config.js";

const form = document.getElementById("options-form");
const fields = {
  shell: form.elements.shell,
  cwd: form.elements.cwd,
  env: form.elements.env,
  fontSize: form.elements.fontSize,
  fontFamily: form.elements.fontFamily,
  scrollback: form.elements.scrollback,
  cursorBlink: form.elements.cursorBlink,
  nativeHostName: form.elements.nativeHostName,
  helperPath: form.elements.helperPath,
  theme: form.elements.theme,
};

const els = {
  install: document.getElementById("install"),
  uninstall: document.getElementById("uninstall"),
  testConn: document.getElementById("test-conn"),
  connResult: document.getElementById("conn-result"),
  manifestPath: document.getElementById("manifest-path"),
  saved: document.getElementById("saved"),
};

(async function init() {
  const cfg = await getConfig();
  fields.shell.value = cfg.shell;
  fields.cwd.value = cfg.cwd;
  fields.env.value = cfg.env;
  fields.fontSize.value = cfg.fontSize;
  fields.fontFamily.value = cfg.fontFamily;
  fields.scrollback.value = cfg.scrollback;
  fields.cursorBlink.checked = cfg.cursorBlink;
  fields.nativeHostName.value = cfg.nativeHostName;
  fields.helperPath.value = cfg.helperPath;
  fields.theme.value = cfg.theme;

  els.manifestPath.textContent = expectedManifestPath(cfg.nativeHostName);
})();

form.addEventListener("submit", async (e) => {
  e.preventDefault();
  await save();
});

els.install.addEventListener("click", async () => {
  await copyInstallHint();
});

els.uninstall.addEventListener("click", async () => {
  if (
    !confirm(
      "Remove the native messaging manifest? You'll need to re-run install.sh to use the terminal again."
    )
  ) {
    return;
  }
  await copyUninstallHint();
});

els.testConn.addEventListener("click", async () => {
  els.connResult.textContent = "Testing…";
  try {
    const resp = await browser.runtime.sendMessage({ type: "spt/ping-helper" });
    if (resp && resp.ok) {
      els.connResult.textContent = "OK — helper reachable.";
    } else {
      els.connResult.textContent = `Failed: ${(resp && resp.error) || "unknown"}`;
    }
  } catch (e) {
    els.connResult.textContent = `Failed: ${e.message || e}`;
  }
});

async function save() {
  const partial = {
    shell: fields.shell.value.trim(),
    cwd: fields.cwd.value.trim(),
    env: fields.env.value,
    fontSize: clamp(parseInt(fields.fontSize.value, 10), 8, 32, DEFAULT_FONT_SIZE),
    fontFamily: fields.fontFamily.value.trim() || DEFAULT_FONT_FAMILY,
    scrollback: clamp(parseInt(fields.scrollback.value, 10), 100, 50000, DEFAULT_SCROLLBACK),
    cursorBlink: fields.cursorBlink.checked,
    nativeHostName:
      fields.nativeHostName.value.trim() || DEFAULT_NATIVE_HOST_NAME,
    helperPath: fields.helperPath.value.trim(),
    theme: fields.theme.value || DEFAULT_THEME,
  };
  await setConfig(partial);
  flash("Saved.");
}

function clamp(v, lo, hi, fallback) {
  const n = Number(v);
  if (!Number.isFinite(n)) return fallback;
  return Math.min(hi, Math.max(lo, n));
}

function flash(text) {
  els.saved.textContent = text;
  setTimeout(() => {
    els.saved.textContent = "";
  }, 1500);
}

function expectedManifestPath(name) {
  // Best-effort: where Firefox will look for the manifest.
  if (navigator.userAgent.includes("Mac")) {
    return `~/Library/Application Support/Mozilla/NativeMessagingHosts/${name}.json`;
  }
  if (navigator.userAgent.includes("Windows")) {
    return `HKCU\\Software\\Mozilla\\NativeMessagingHosts\\${name}`;
  }
  return `~/.mozilla/native-messaging-hosts/${name}.json`;
}

async function copyInstallHint() {
  const hint =
    "Run this in a terminal:\n\n" +
    "  ./scripts/install.sh\n\n" +
    "The script builds the helper (Go 1.21+ required) and writes the manifest.";
  await navigator.clipboard.writeText(hint).catch(() => {});
  flash("Install command copied to clipboard.");
}

async function copyUninstallHint() {
  const hint = "Run this in a terminal:\n\n  ./scripts/install.sh --uninstall";
  await navigator.clipboard.writeText(hint).catch(() => {});
  flash("Uninstall command copied to clipboard.");
}
