// The file list and the viewer: filtering, previews (code, Markdown,
// PDFs, images), proposed changes shown in place, and reloading what a
// tool changed.

import { $, esc, api } from "./api.js";
import { state }       from "./state.js";
import { markdown }    from "./markdown.js";
import { highlight }   from "./highlight.js";

export async function loadFiles() {
  try {
    state.files = await api("/api/files");
    renderFiles();

  } catch (e) {
    console.error("[ERROR]: loadFiles - ", e);
  }
}

// (The tree's root path bar is a .path[data-root] element in the
// aside; refreshInfo fills it from info.root along with the header's.)

// The tree watches the disk: while the tab shows, the list refetches
// every few seconds and re-renders only when it changed — a file
// added or removed outside this page (another armin, git, an
// editor) shows up without a reload.
let watching = false;

export function watchFiles() {
  if (watching) {
    return;
  }

  watching = true;
  let seen = state.files.join("\n");

  setInterval(async () => {
    if (document.hidden) {
      return;
    }

    try {
      const files = await api("/api/files");
      const now   = files.join("\n");
      if (now === seen) {
        return;
      }

      seen        = now;
      state.files = files;
      renderFiles();

    } catch (e) {
      // the server blinked: the next tick retries
    }
  }, 4000);
}

// Tools that create or change files: after one succeeds, the file
// list reloads, and so does the preview when it shows what changed.
export const changers = [ "write_file", "edit_file", "run_program" ];
let   reloadTimer     = null;

// samePath says whether a tool's path is the file the viewer shows.
const samePath = (path, current) => {
  return path === current || path.endsWith("/" + current) ||
    current.endsWith("/" + path);
};

export function filesChanged(call) {
  clearTimeout(reloadTimer);
  reloadTimer = setTimeout(async () => {
    await loadFiles();

    // only a viewer already open showing the file is refreshed: a tool
    // ending never opens it
    if (!state.current || !viewerOpen()) {
      return;
    }

    const path = call.args?.path || "";
    const same = path && samePath(path, state.current);
    if (same || call.name === "run_program") {
      preview(state.current, null, true);
    }
  }, 150);
}

export function renderFiles() {
  const q    = $("filter").value.toLowerCase();
  const list = state.files.filter((f) => f.toLowerCase().includes(q));

  // a filtered list stays flat: matches may live anywhere in the tree,
  // so collapsing would hide them; its first 800 rows are drawn. The
  // tree takes every file: it only draws the open folders' rows.
  if (q) {
    $("files").innerHTML = list.slice(0, 800).map((f) => row(f)).join("");
    return;
  }

  // group the paths into a tree: a directory node holds its files and
  // subdirectories; each renders under a collapsible header. Dirs
  // start closed (depth > 0) and come before the files.
  const root = new Map();   // dir name -> node
  const node = (dir) => {
    if (!root.has(dir)) {
      root.set(dir, {
        files: [],
        dirs:  new Map(),
        open:  dir === "" ? true : expanded.has(dir),
      });
    }

    return root.get(dir);
  };

  for (const f of list) {
    const parts = f.split("/");
    let   dir   = "";
    for (let i = 0; i < parts.length - 1; i++) {
      const name   = parts[i];
      const parent = node(dir);
      const child  = dir + name + "/";
      node(child);                       // exists even if only dirs inside
      parent.dirs.set(name, child);
      dir = child;
    }

    node(dir).files.push(f);
  }

  // depth indents a row under its directory; the root's own rows
  // (depth 0) sit at the edge
  const render = (dir, node, depth) => {
    let html = dirHeader(dir, node.open, depth - 1);
    if (!node.open) {
      return html;
    }

    for (const [ name, sub ] of node.dirs) {
      html += render(sub, root.get(sub), depth + 1);
    }

    for (const f of node.files) {
      html += row(f, depth);
    }

    return html;
  };

  $("files").innerHTML = render("", root.get(""), 0);
}

// indent is a row's left padding at a depth in the tree.
const indent = (depth) => `style = "padding-left: ${ 8 + depth * 14 }px"`;

// row is one file: its whole path in a flat (filtered) list, its name
// alone under its directory in the tree.
function row(f, depth) {
  const cut  = f.lastIndexOf("/") + 1;
  const on   = f === state.current ? "on" : "";
  let   name = esc(f.slice(cut));
  let   pad  = "";
  if (depth === undefined) {
    name = `<span class = "dir">${ esc(f.slice(0, cut)) }</span>` + name;

  } else {
    pad = indent(depth + 1) + " ";
  }

  return `<div data-path = "${ esc(f) }" class = "${ on }" ${ pad }` +
    `title = "${ esc(f) }">${ name }</div>`;
}

// A directory header: a twistie and the directory's own name.
// Clicking toggles it; the state survives re-renders.
function dirHeader(dir, open, depth) {
  if (!dir) {
    return "";
  }

  const path  = esc(dir.slice(0, -1));
  const name  = esc(dir.slice(0, -1).split("/").pop());
  const twist = open ? "▼" : "▶";
  const shut  = open ? "" : " shut";

  return `<div class = "dirhead${ shut }" data-dir = "${ path }" ` +
    `${ indent(depth) } title = "${ path }">` +
    `<span class = "twist">${ twist }</span>` +
    `<span class = "dirname">${ name }</span></div>`;
}

// Directories the user expanded: kept across renders and reloads.
// Empty by default, so every dir starts collapsed.
const expanded = new Set();

$("files").addEventListener("click", (e) => {
  const head = e.target.closest(".dirhead");
  if (!head) {
    return;
  }

  const dir = head.dataset.dir + "/";
  if (expanded.has(dir)) {
    expanded.delete(dir);

  } else {
    expanded.add(dir);
  }

  renderFiles();
});

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

export function viewerOpen() {
  return !document.body.classList.contains("noviewer");
}

// ---------- preview ----------

const images = [ "png", "jpg", "jpeg", "gif", "webp", "svg" ];

// codeRow is one numbered line of the code view, marked when it's the
// line asked for.
const codeRow = (l, n, line) => {
  const hl = line && n + 1 === +line ? " class = \"hl\"" : "";

  return `<tr id = "L${ n + 1 }"${ hl }><td class = "n">${ n + 1 }</td>` +
    `<td>${ l || " " }</td></tr>`;
};

// reveal opens the tree down to path, every folder above it, and
// scrolls the tree to it: the file shown, or a folder a link named.
const reveal = (path) => {
  const parts = path.replace(/\/$/, "").split("/");
  for (let i = 1; i < parts.length; i++) {
    expanded.add(parts.slice(0, i).join("/") + "/");
  }

  renderFiles();
  $("files").querySelector(".on")?.scrollIntoView({ block: "nearest" });
};

// openRef follows a link from an answer or a preview: a file opens in
// the viewer (at a line, or at a #section of a Markdown file), a
// folder opens in the tree.
export function openRef(path, line, anchor) {
  const dir = path.replace(/\/$/, "") + "/";
  const isFile = state.files.includes(path) ||
    state.files.some((f) => f.endsWith("/" + path));
  if (!isFile && state.files.some((f) => f.startsWith(dir))) {
    expanded.add(dir);
    reveal(dir);
    $("files").querySelector(`[data-dir = "${ CSS.escape(dir.slice(0, -1)) }"]`)
      ?.scrollIntoView({ block: "nearest" });
    return;
  }

  preview(path, line, false, anchor);
}

export async function preview(path, line, keepScroll, anchor) {
  showViewer(true);

  let full = path;
  if (!state.files.includes(path)) {
    full = state.files.find((f) => f.endsWith("/" + path)) || path;
  }

  const top = keepScroll ? $("preview").scrollTop : 0;
  state.current = full;
  if (keepScroll) {
    renderFiles();

  } else {
    reveal(full);
  }

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
      const dir = full.slice(0, Math.max(0, full.lastIndexOf("/")));
      box.innerHTML = `<div class = "md">${ markdown(text, dir) }</div>`;
      box.scrollTop = keepScroll ? top : 0;
      if (anchor) {
        box.querySelector(`[id = "${ CSS.escape(anchor) }"]`)
          ?.scrollIntoView({ block: "start" });
      }

      return;
    }

    const rows = highlight(text, ext).map((l, n) => codeRow(l, n, line));
    box.innerHTML = `<table class = "code">${ rows.join("") }</table>`;
    if (line) {
      const at = document.getElementById("L" + line);
      at?.scrollIntoView({ block: "center" });

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
  const lines = preview.replace(/\n$/, "").split("\n");
  const at    = /^(.+) @ line (\d+)$/.exec(lines[0]);
  const fresh = /^new file (.+) \(\d+ lines\)$/.exec(lines[0]);
  if (!at && !fresh) {
    return null;
  }

  const ch = {
    path:    (at || fresh)[1],
    start:   at ? +at[2] : 1,
    created: !!fresh,
    before:  0,
    gone:    [],
    came:    [],
  };
  for (const l of lines.slice(1)) {
    if (l.startsWith("… (")) {
      return null;
    }

    if (l.startsWith("- ")) {
      ch.gone.push(l.slice(2));

    } else if (l.startsWith("+ ")) {
      ch.came.push(l.slice(2));

    } else if (!ch.gone.length && !ch.came.length) {
      ch.before++;
    }
  }

  return ch;
}

// diffRow is one row of a proposed change: a class, a number, a line.
const diffRow = (cls, n, html) => {
  const at = cls ? ` class = "${ cls }"` : "";

  return `<tr${ at }><td class = "n">${ n }</td>` +
    `<td>${ html || " " }</td></tr>`;
};

// showChange shows a proposed edit in the file viewer: the whole
// file, highlighted, the removed lines in red where they are and the
// new ones in green right after, scrolled into view. A closed viewer
// stays closed unless open asks for it (a click on the approval).
export async function showChange(ch, open) {
  if (!open && !viewerOpen()) {
    return;
  }

  showViewer(true);
  state.current = ch.path;
  renderFiles();
  $("ptitle").textContent = ch.path + " — proposed change";

  const ext  = ch.path.split(".").pop().toLowerCase();
  const box  = $("preview");
  const came = highlight(ch.came.join("\n"), ext);
  if (ch.created) {
    const rows = came.map((l, i) => diffRow("add", i + 1, l));
    box.innerHTML = `<table class = "code">${ rows.join("") }</table>`;
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
  const after = first + ch.gone.length;
  const rows  = [];
  for (let i = 0; i < first && i < file.length; i++) {
    rows.push(diffRow("", i + 1, file[i]));
  }

  for (let i = first; i < after; i++) {
    rows.push(diffRow("del", i + 1, file[i]));
  }

  for (const l of came) {
    rows.push(diffRow("add", "+", l));
  }

  for (let i = after; i < file.length; i++) {
    rows.push(diffRow("", i + 1, file[i]));
  }

  box.innerHTML = `<table class = "code">${ rows.join("") }</table>`;
  box.querySelector("tr.del, tr.add")?.scrollIntoView({ block: "center" });
}
