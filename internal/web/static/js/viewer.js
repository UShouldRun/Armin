// The file list and the viewer: filtering, previews (code, Markdown,
// PDFs, images), proposed changes shown in place, and reloading what a
// tool changed.

import { $, esc, api } from "./api.js";
import { state } from "./state.js";
import { markdown } from "./markdown.js";
import { highlight } from "./highlight.js";

export async function loadFiles() {
  try {
    state.files = await api("/api/files");
    renderFiles();

  } catch (e) {
    console.error("[ERROR]: loadFiles - ", e);
  }
}

// Tools that create or change files: after one succeeds, the file
// list reloads, and so does the preview when it shows what changed.
export const changers = [ "write_file", "edit_file", "run_program" ];
let   reloadTimer = null;

export function filesChanged(call) {
  clearTimeout(reloadTimer);
  reloadTimer = setTimeout(async () => {
    await loadFiles();
    if (!state.current) return;

    const path = call.args?.path || "";
    const same = path && (path === state.current || path.endsWith("/" + state.current) || state.current.endsWith("/" + path));
    if (same || call.name === "run_program") preview(state.current, null, true);
  }, 150);
}

export function renderFiles() {
  const q    = $("filter").value.toLowerCase();
  const list = state.files.filter((f) => f.toLowerCase().includes(q)).slice(0, 800);
  $("files").innerHTML = list.map((f) => `<div data-path = "${ esc(f) }" class = "${ f === state.current ? "on" : "" }" title = "${ esc(f) }">${ esc(f) }</div>`).join("");
}

// ---------- file viewer ----------

// The viewer opens with a file and closes on × or "viewer"; the
// choice is remembered per browser.
export function showViewer(on) {
  document.body.classList.toggle("noviewer", !on);
  try {
    localStorage.setItem("armin.viewer", on ? "1" : "0");

  } catch (e) {
    // storage blocked: the toggle still works for this page
  }
}

export function viewerOpen() { return !document.body.classList.contains("noviewer"); }

// ---------- preview ----------

const images = [ "png", "jpg", "jpeg", "gif", "webp", "svg" ];

export async function preview(path, line, keepScroll) {
  showViewer(true);
  const full = state.files.includes(path) ? path : (state.files.find((f) => f.endsWith("/" + path)) || path);
  const top  = keepScroll ? $("preview").scrollTop : 0;
  state.current = full;
  renderFiles();
  $("ptitle").textContent = full + (line ? `:${ line }` : "");
  const ext = full.split(".").pop().toLowerCase();
  const url = "/api/raw?path=" + encodeURIComponent(full);
  const box = $("preview");

  if (ext === "pdf") {
    box.innerHTML = `<iframe src = "${ url }"></iframe>`;
    return;
  }
  if (images.includes(ext)) {
    box.innerHTML = `<img src = "${ url }" alt = "${ esc(full) }">`;
    return;
  }
  try {
    const text = await api(url);
    if ((ext === "md" || ext === "markdown") && !line) {
      box.innerHTML = `<div class = "md">${ markdown(text) }</div>`;
      return;
    }
    const rows = highlight(text, ext).map((l, n) => `<tr id = "L${ n + 1 }"${ line && n + 1 === +line ? " class = \"hl\"" : "" }><td class = "n">${ n + 1 }</td><td>${ l || " " }</td></tr>`);
    box.innerHTML = `<table class = "code">${ rows.join("") }</table>`;
    if (line) {
      document.getElementById("L" + line)?.scrollIntoView({ block: "center" });
    } else {
      box.scrollTop = top;
    }

  } catch (e) {
    box.innerHTML = `<div class = "empty err">${ esc(e.message) }</div>`;
  }
}

// parseChange reads an edit's preview (see tools.Change.Preview):
// "path @ line N" or "new file path (N lines)", then "- ", "+ " and
// "  " context lines. Null for anything else (a command, a
// directory), and for a capped preview, which the viewer can't place.
export function parseChange(preview) {
  const lines  = preview.replace(/\n$/, "").split("\n");
  const at     = /^(.+) @ line (\d+)$/.exec(lines[0]);
  const fresh  = /^new file (.+) \(\d+ lines\)$/.exec(lines[0]);
  if (!at && !fresh) return null;

  const ch = {
    path: (at || fresh)[1], start: at ? +at[2] : 1, created: !!fresh,
    before: 0, gone: [], came: [],
  };
  for (const l of lines.slice(1)) {
    if (l.startsWith("… (")) return null;
    if (l.startsWith("- ")) ch.gone.push(l.slice(2));
    else if (l.startsWith("+ ")) ch.came.push(l.slice(2));
    else if (!ch.gone.length && !ch.came.length) ch.before++;
  }

  return ch;
}

// showChange shows a proposed edit in the file viewer: the whole
// file, highlighted, the removed lines in red where they are and the
// new ones in green right after, scrolled into view.
export async function showChange(ch) {
  showViewer(true);
  state.current = ch.path;
  renderFiles();
  $("ptitle").textContent = ch.path + " — proposed change";

  const ext  = ch.path.split(".").pop().toLowerCase();
  const box  = $("preview");
  const row  = (cls, n, html) => `<tr${ cls ? ` class = "${ cls }"` : "" }><td class = "n">${ n }</td><td>${ html || " " }</td></tr>`;
  const came = highlight(ch.came.join("\n"), ext);

  if (ch.created) {
    box.innerHTML = `<table class = "code">${ came.map((l, i) => row("add", i + 1, l)).join("") }</table>`;
    return;
  }

  let text = "";
  try {
    text = await api("/api/raw?path=" + encodeURIComponent(ch.path));

  } catch (e) {
    box.innerHTML = `<div class = "empty err">${ esc(e.message) }</div>`;
    return;
  }

  const file  = highlight(text, ext);
  const first = ch.start - 1; // the first changed line, 0-based
  const rows  = [];
  for (let i = 0; i < first && i < file.length; i++) rows.push(row("", i + 1, file[i]));
  for (let i = 0; i < ch.gone.length; i++) rows.push(row("del", first + i + 1, file[first + i]));
  came.forEach((l, i) => rows.push(row("add", "+", l)));
  for (let i = first + ch.gone.length; i < file.length; i++) rows.push(row("", i + 1, file[i]));

  box.innerHTML = `<table class = "code">${ rows.join("") }</table>`;
  box.querySelector("tr.del, tr.add")?.scrollIntoView({ block: "center" });
}
