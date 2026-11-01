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
    if (!state.current) {
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
  const list = state.files
    .filter((f) => f.toLowerCase().includes(q))
    .slice(0, 800);

  // a filtered list stays flat: matches may live anywhere in the tree,
  // so collapsing would hide them.
  if (q) {
    $("files").innerHTML = list.map(row).join("");
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

  const render = (dir, node) => {
    let html = dirHeader(dir, node.open);
    if (!node.open) {
      return html;
    }

    for (const [ name, sub ] of node.dirs) {
      html += render(sub, root.get(sub));
    }

    for (const f of node.files) {
      html += row(f);
    }

    return html;
  };

  $("files").innerHTML = render("", root.get(""));
}

function row(f) {
  const cut  = f.lastIndexOf("/") + 1;
  const dir  = `<span class = "dir">${ esc(f.slice(0, cut)) }</span>`;
  const name = dir + esc(f.slice(cut));
  const on   = f === state.current ? "on" : "";

  return `<div data-path = "${ esc(f) }" class = "${ on }" ` +
    `title = "${ esc(f) }">${ name }</div>`;
}

// A directory header: a twistie, the directory path, a count of the
// files under it. Clicking toggles it; the state survives re-renders.
function dirHeader(dir, open) {
  if (!dir) {
    return "";
  }

  const name  = esc(dir.slice(0, -1));
  const twist = open ? "▾" : "▸";
  const shut  = open ? "" : " shut";

  return `<div class = "dirhead${ shut }" data-dir = "${ name }" ` +
    `title = "${ name }"><span class = "twist">${ twist }</span>` +
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

export async function preview(path, line, keepScroll) {
  showViewer(true);

  let full = path;
  if (!state.files.includes(path)) {
    full = state.files.find((f) => f.endsWith("/" + path)) || path;
  }

  const top = keepScroll ? $("preview").scrollTop : 0;
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
// new ones in green right after, scrolled into view.
export async function showChange(ch) {
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
