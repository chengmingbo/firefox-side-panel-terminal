// Background event page. Only routes keyboard commands to the sidebar.
//
// The sidebar owns its native-messaging port to the PTY helper directly:
// an MV3 event page can be suspended while idle, which would drop a port
// held here and kill every shell. Each sidebar (one per window) gets its own
// helper process, so windows are isolated from each other.
//
// Alt+Shift+T is the built-in _execute_sidebar_action command and needs no
// handler.

browser.commands.onCommand.addListener(async (command, tab) => {
  const type =
    command === "new-session"
      ? "spt/new-session"
      : command === "disconnect"
        ? "spt/reconnect"
        : null;
  if (!type) return;
  // open() must run synchronously inside the command handler to count as a
  // user action. A sidebar that is opening fresh starts a session itself, so
  // a dropped message in that case is harmless.
  const opening = browser.sidebarAction.open().catch(() => {});
  // Target only the sidebar in the window the shortcut was pressed in.
  const windowId =
    tab?.windowId ?? (await browser.windows.getLastFocused()).id;
  await opening;
  browser.runtime.sendMessage({ type, windowId }).catch(() => {});
});
