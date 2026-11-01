// Wiring only: the page's events to the modules, and the first loads.

import { $, esc, api }                   from "./api.js";
import { state }                         from "./state.js";
import { refreshInfo }                   from "./header.js";
import { add, hold, send, loadHistory }  from "./chat.js";
import {
  loadFiles, renderFiles, preview, showViewer, viewerOpen, watchFiles,
} from "./viewer.js";

$("form").addEventListener("submit", (e) => {
  e.preventDefault();
  const text = $("input").value.trim();
  if (!text) {
    return;
  }

  $("input").value = "";
  grow();
  if (state.busy) {
    hold(text);   // mid-run: sent when the turn ends

  } else {
    send(text);   // plain send otherwise
  }
});

// the message box grows with what's typed, up to its max-height
const grow = () => {
  const box = $("input");
  box.style.height = "auto";
  box.style.height = box.scrollHeight + "px";
};

$("input").addEventListener("input", grow);

$("input").addEventListener("keydown", (e) => {
  if (e.key === "Enter" && !e.shiftKey) {
    e.preventDefault();
    $("form").requestSubmit();
  }
});

$("filter").addEventListener("input", renderFiles);

// the composer's ⋯ dropup: opens on click, shuts on a click anywhere
// else or on Escape, and shuts after any action taken inside it.
const menu   = $("menu");
const dropup = menu.closest(".dropup");
const shut   = () => dropup.classList.remove("open");
menu.addEventListener("click", () => dropup.classList.toggle("open"));
for (const name of [ "compact", "clear", "approve", "viewer" ]) {
  $(name).addEventListener("click", shut);
}

document.addEventListener("click", (e) => {
  if (!e.target.closest(".dropup")) {
    shut();
  }
});

document.addEventListener("keydown", (e) => {
  if (e.key === "Escape") {
    shut();
  }
});

$("files").addEventListener("click", (e) => {
  const row = e.target.closest("[data-path]");
  if (row) {
    preview(row.dataset.path);
  }
});

$("log").addEventListener("click", (e) => {
  const ref = e.target.closest("a.ref");
  if (ref) {
    preview(ref.dataset.path, ref.dataset.line);
  }
});

$("askfile").addEventListener("click", () => {
  if (!state.current) {
    return;
  }

  $("input").value = `About \`${ state.current }\`: ` + $("input").value;
  $("input").focus();
});

$("model").addEventListener("change", async (e) => {
  try {
    const r = await api(
      "/api/command",
      { name: "model", arg: e.target.value },
    );
    add("notice", esc(r.result));

  } catch (err) {
    console.error("[ERROR]: switchModel - ", err);
    add("msg err", esc(err.message));
  }

  refreshInfo();
});

$("approve").addEventListener("click", async () => {
  try {
    const r  = await api("/api/command", { name: "approve" });
    const on = r.result.startsWith("approvals on");
    add("notice", esc(r.result));
    $("approve").textContent = on ? "Approve all: on" : "Approve all: off";

  } catch (err) {
    console.error("[ERROR]: approveAll - ", err);
    add("msg err", esc(err.message));
  }
});

for (const name of [ "clear", "compact" ]) {
  $(name).addEventListener("click", async () => {
    if (state.busy) {
      add("notice", `${ name } waits: a turn is running`);
      return;
    }

    try {
      const r = await api("/api/command", { name });
      if (name === "clear") {
        $("log").innerHTML = "";
      }

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
window.addEventListener("focus", () => {
  if (!state.busy) {
    loadFiles();
  }
});

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
watchFiles();
