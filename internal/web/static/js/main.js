// Wiring only: the page's events to the modules, and the first loads.

import { $, esc, api }                                             from "./api.js";
import { state }                                                   from "./state.js";
import { refreshInfo }                                             from "./header.js";
import { loadFiles, renderFiles, preview, showViewer, viewerOpen } from "./viewer.js";
import { add, send, loadHistory }                                  from "./chat.js";

$("form").addEventListener("submit", (e) => {
  e.preventDefault();
  const text = $("input").value.trim();
  if (!text || state.busy) return;
  $("input").value = "";
  send(text);
});
$("input").addEventListener("keydown", (e) => {
  if (e.key === "Enter" && !e.shiftKey) {
    e.preventDefault();
    $("form").requestSubmit();
  }
});
$("filter").addEventListener("input", renderFiles);
$("files").addEventListener("click", (e) => {
  const row = e.target.closest("[data-path]");
  if (row) preview(row.dataset.path);
});
$("log").addEventListener("click", (e) => {
  const ref = e.target.closest("a.ref");
  if (ref) preview(ref.dataset.path, ref.dataset.line);
});
$("askfile").addEventListener("click", () => {
  if (!state.current) return;
  $("input").value = `About \`${ state.current }\`: ` + $("input").value;
  $("input").focus();
});
$("model").addEventListener("change", async (e) => {
  try {
    const r = await api("/api/command", { name: "model", arg: e.target.value });
    add("notice", esc(r.result));

  } catch (err) {
    console.error("[ERROR]: switchModel - ", err);
    add("msg err", esc(err.message));
  }
  refreshInfo();
});
for (const name of [ "clear", "compact" ]) {
  $(name).addEventListener("click", async () => {
    if (state.busy) return;
    try {
      const r = await api("/api/command", { name });
      if (name === "clear") $("log").innerHTML = "";
      add("notice", esc(r.result));

    } catch (err) {
      console.error(`[ERROR]: ${ name } - `, err);
      add("msg err", esc(err.message));
    }
    refreshInfo();
  });
}

// Files changed outside armin (an editor, a terminal) show up when
// you come back to the tab.
window.addEventListener("focus", () => { if (!state.busy) loadFiles(); });

$("viewer").addEventListener("click", () => showViewer(!viewerOpen()));
$("closeviewer").addEventListener("click", () => showViewer(false));

try {
  if (localStorage.getItem("armin.viewer") === "0") {
    showViewer(false);
  }

} catch (e) {
  // storage blocked: the viewer starts open
}

loadFiles();
loadHistory();
refreshInfo();
