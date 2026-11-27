"use strict";
const host = document.querySelector("#terminal");
const dark = {background:"#17191c", foreground:"#ecedf0", cursor:"#b1cef7", selectionBackground:"#385e89"};
const light = {background:"#f6f8fa", foreground:"#262c34", cursor:"#375884", selectionBackground:"#c4dbfc"};
const terminal = new Terminal({
  theme: dark,
  fontFamily: '"Cascadia Mono",Consolas,"Microsoft YaHei UI",monospace',
  fontSize: 14,
  cursorBlink: true,
  scrollback: 2500,
});
const fit = new FitAddon.FitAddon();
terminal.loadAddon(fit);
terminal.open(host);
const send = (kind, details = {}) => window.viva?.postMessage(JSON.stringify({kind, ...details}));
const sendSize = (kind) => send(kind, {cols: terminal.cols, rows: terminal.rows});
let toggleShortcut = {};
window.vivaTerminal = {
  write(base64) {
    const raw = atob(base64);
    const bytes = Uint8Array.from(raw, (part) => part.charCodeAt(0));
    terminal.write(bytes);
  },
  paste(text) { terminal.paste(text); },
  setToggleShortcut(key, label, ctrl, alt, shift) { toggleShortcut = {key, label, ctrl, alt, shift}; },
};
new MutationObserver(() => {
  terminal.options.theme = document.documentElement.classList.contains("light") ? light : dark;
}).observe(document.documentElement, {attributes:true, attributeFilter:["class"]});
terminal.onData(text => send("input", {text}));
terminal.attachCustomKeyEventHandler(event => {
  if (event.type !== "keydown") return true;
  const key = event.key.toLowerCase();
  if (key === toggleShortcut.key && !event.metaKey && !event.isComposing && !event.getModifierState("AltGraph") &&
      event.ctrlKey === toggleShortcut.ctrl && event.altKey === toggleShortcut.alt && event.shiftKey === toggleShortcut.shift) {
    if (!event.repeat) send("toggle", {text:toggleShortcut.label});
    return false;
  }
  if (event.ctrlKey && event.shiftKey && key === "c") {
    send("copy", {text:terminal.getSelection()});
    return false;
  }
  if (event.ctrlKey && event.shiftKey && key === "v") {
    send("paste");
    return false;
  }
  return true;
});
fit.fit();
sendSize("ready");
new ResizeObserver(() => {
  fit.fit();
  sendSize("resize");
}).observe(host);
terminal.focus();
