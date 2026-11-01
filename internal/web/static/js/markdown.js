// Markdown for answers and previews: small and safe (escape first,
// then build), with file:line references made into links the viewer
// opens.

import { esc }       from "./api.js";
import { highlight } from "./highlight.js";
import { state }     from "./state.js";

// fenceLangs maps a code fence's language to highlight's file
// extension names; others are tried as they are.
const fenceLangs = {
  python:     "py",
  rust:       "rs",
  bash:       "sh",
  shell:      "sh",
  zsh:        "sh",
  javascript: "js",
  typescript: "ts",
  "c++":      "cpp",
  golang:     "go",
  typst:      "typ",
  kotlin:     "kt",
};

// A path with an extension, a colon and a line: internal/a/b.go:42.
const pathLine = /((?:[\w.-]+\/)*[\w.-]+\.[A-Za-z0-9]{1,8}):(\d+)/;
const fileRef  = new RegExp(
  /(^|[\s(`\[])/.source + pathLine.source + /(?:-(\d+))?/.source,
  "g",
);
const codeRef  = new RegExp("^" + pathLine.source);

// The patterns inline rewrites, in order.
const htmlImg    = /<img\b([^>]*)>/gi;
const htmlWrap   = /<\/?(p|div|center|picture|source|span|br)\b[^>]*>/gi;
const htmlAttr   = /([\w-]+)\s*=\s*(?:"([^"]*)"|'([^']*)')/g;
const mdImg      = /!\[([^\]]*)\]\(([^\s)]+)\)/g;
const mdLink     = /\[([^\]]+)\]\(([^\s)]+)(?:\s+&quot;[^&]*&quot;)?\)/g;
const bold       = /\*\*([^*]+)\*\*/g;
const boldU      = /(^|[^\w])__([^_]+)__(?=[^\w]|$)/g;
const italic     = /(^|[^*])\*([^*\s][^*]*)\*/g;
const italicU    = /(^|[^\w])_([^_\s][^_]*)_(?=[^\w]|$)/g;
const strike     = /~~([^~]+)~~/g;
const rule       = /^ {0,3}([-*_])( *\1){2,} *$/;
const listRow    = /^\s*([-*+]|\d+\.)\s+/;
const taskBox    = /^\[([ xX])\]\s+/;
const tableRow   = /^\s*\|.*\|\s*$/;
const tableSep   = /^\s*\|[\s:|-]+\|\s*$/;
const blockStart = /^(```|#{1,6}\s|>|\s*([-*+]|\d+\.)\s)/;

// base is the folder of the file being rendered, so an image's
// relative path resolves the way the file's own viewer would.
let base = "";

// resolve joins a relative path onto base, folding "." and "..".
const resolve = (path) => {
  const parts = [];
  for (const p of (base + "/" + path).split("/")) {
    if (p === "..") {
      parts.pop();

    } else if (p !== "" && p !== ".") {
      parts.push(p);
    }
  }

  return parts.join("/");
};

// imgSrc is where an image loads from: a URL as written, a project
// path (relative to the file, or to the root with a leading "/")
// through /api/raw.
const imgSrc = (src) => {
  if (/^(https?:|data:image\/)/.test(src)) {
    return src;
  }

  const path = src.startsWith("/") ? src.slice(1) : resolve(src);

  return `/api/raw?path=${ encodeURIComponent(path) }`;
};

// imgTag is a safe <img>: the source resolved, the alt text and a
// plain width kept, every other attribute dropped.
const imgTag = (src, alt, width) => {
  let tag = `<img class = "md-img" src = "${ esc(imgSrc(src)) }" ` +
    `alt = "${ esc(alt || "") }" loading = "lazy"`;
  if (/^\d+%?$/.test(width || "")) {
    tag += ` width = "${ width }"`;
  }

  return tag + ">";
};

// htmlImage reads an <img> a Markdown file wrote as HTML.
const htmlImage = (attrs) => {
  const a = {};
  for (const m of attrs.matchAll(htmlAttr)) {
    a[m[1].toLowerCase()] = m[2] ?? m[3];
  }

  return a.src ? imgTag(a.src, a.alt, a.width) : "";
};

// known says whether a path is one of the project's files.
const known = (path) => {
  return state.files.includes(path) ||
    state.files.some((f) => f.endsWith("/" + path));
};

// refLink is a file:line reference the viewer opens on click.
const refLink = (path, line, html) => {
  return `<a class = "ref" data-path = "${ path }" ` +
    `data-line = "${ line }">${ html }</a>`;
};

// emphasis is bold, italic and struck text, with * or _.
const emphasis = (s) => {
  return s.replace(bold, "<b>$1</b>").replace(boldU, "$1<b>$2</b>")
    .replace(italic, "$1<i>$2</i>").replace(italicU, "$1<i>$2</i>")
    .replace(strike, "<del>$1</del>");
};

// docLink is a Markdown link: a web address opens in a new tab; a
// project path (relative to the file, or to the root with a leading
// "/") opens in the viewer, at its #section if it names one; a bare
// #section scrolls this document.
const docLink = (text, target) => {
  const href = target.replace(/&amp;/g, "&");
  if (/^(https?:|mailto:)/.test(href)) {
    return `<a href = "${ esc(href) }" target = "_blank" ` +
      `rel = "noopener">${ text }</a>`;
  }

  const [ path, anchor ] = href.split("#");
  const at = anchor ? ` data-anchor = "${ esc(anchor) }"` : "";
  if (path === "") {
    return `<a class = "anchor"${ at }>${ text }</a>`;
  }

  // relative to the file, as GitHub reads it; a link written relative
  // to the root instead still lands when that's the file there is
  let full = path.startsWith("/") ? path.slice(1) : resolve(path);
  if (!state.files.includes(full) && state.files.includes(path)) {
    full = path;
  }

  return `<a class = "ref" data-path = "${ esc(full) }"${ at }>${ text }</a>`;
};

function inline(s) {
  const codes = [];
  s = s.replace(/`([^`]+)`/g, (_, c) => {
    codes.push(c);

    return "\u0000" + (codes.length - 1) + "\u0000";
  });

  // what's rendered early is held aside, so the emphasis rules below
  // can't reach into its attributes (a path with underscores in it)
  const held = [];
  const hold = (html) => {
    held.push(html);

    return "\u0001" + (held.length - 1) + "\u0001";
  };

  // HTML the file wrote itself: its images kept (rebuilt safe), the
  // tags that only wrap or center them dropped, the rest escaped
  s = s.replace(htmlImg, (_, attrs) => hold(htmlImage(attrs)));
  s = esc(s.replace(htmlWrap, ""));
  s = s.replace(mdImg, (_, a, src) => hold(imgTag(src, a)));
  s = s.replace(mdLink, (_, t, u) => hold(docLink(emphasis(t), u)));
  s = emphasis(s);
  s = s.replace(fileRef, (m, pre, path, line) => {
    if (!known(path)) {
      return m;
    }

    return pre + refLink(path, line, m.slice(pre.length));
  });

  s = s.replace(/\u0001(\d+)\u0001/g, (_, i) => held[+i]);

  return s.replace(/\u0000(\d+)\u0000/g, (_, i) => {
    const c    = codes[+i];
    const ref  = c.match(codeRef);
    const code = `<code>${ esc(c) }</code>`;

    return ref ? refLink(esc(ref[1]), ref[2], code) : code;
  });
}

// table renders a Markdown table starting at lines[i], and returns
// the HTML and the line after it.
const table = (lines, i) => {
  const row   = (r) => {
    return r.trim().replace(/^\||\|$/g, "").split("|").map((c) => c.trim());
  };

  const cells = (r, tag) => {
    return row(r).map((c) => `<${ tag }>${ inline(c) }</${ tag }>`).join("");
  };

  let t = "<table><tr>" + cells(lines[i], "th") + "</tr>";
  for (i += 2; i < lines.length && tableRow.test(lines[i]); i++) {
    t += "<tr>" + cells(lines[i], "td") + "</tr>";
  }

  return [ t + "</table>", i ];
};

// slug is a heading's id as GitHub makes it: lowercase, punctuation
// dropped, spaces to hyphens; a repeat gets -1, -2 after it.
const slug = (text, seen) => {
  const id = text.toLowerCase().replace(/<[^>]*>/g, "")
    .replace(/&[a-z]+;/g, "").replace(/[^\w\- ]/g, "").replace(/ /g, "-");
  const n = seen.get(id) || 0;
  seen.set(id, n + 1);

  return n ? `${ id }-${ n }` : id;
};

// list renders the list starting at lines[i], nested by indentation,
// with [ ] and [x] items as checkboxes; returns the HTML and the line
// after it.
const list = (lines, i) => {
  const items = [];
  for (; i < lines.length; i++) {
    const l = lines[i];
    if (listRow.test(l)) {
      const marker = l.match(listRow)[1];
      items.push({
        indent: l.length - l.trimStart().length,
        tag:    /\d/.test(marker) ? "ol" : "ul",
        text:   l.replace(listRow, ""),
      });

    } else if (items.length && l.trim() !== "" && /^\s+/.test(l)) {
      // a wrapped item continues on an indented line
      items[items.length - 1].text += " " + l.trim();

    } else {
      break;
    }
  }

  let html = "";
  const open = [];
  for (const it of items) {
    while (open.length && it.indent < open[open.length - 1].indent) {
      html += `</li></${ open.pop().tag }>`;
    }

    // a numbered list right after a bulleted one (or back) starts anew
    let top = open[open.length - 1];
    if (top && it.indent === top.indent && it.tag !== top.tag) {
      html += `</li></${ open.pop().tag }>`;
      top = open[open.length - 1];
    }

    if (!top || it.indent > top.indent) {
      html += `<${ it.tag }>`;
      open.push(it);

    } else {
      html += "</li>";
    }

    const box = it.text.match(taskBox);
    if (box) {
      const done = box[1] === " " ? "" : " checked";
      const check = `<input type = "checkbox" disabled${ done }>`;
      html += `<li class = "task">${ check } ` +
        inline(it.text.replace(taskBox, ""));

    } else {
      html += "<li>" + inline(it.text);
    }
  }

  while (open.length) {
    html += `</li></${ open.pop().tag }>`;
  }

  return [ html, i ];
};

// markdown renders src; dir is the folder of the file it came from
// ("" for answers), which relative links and images are read against.
export function markdown(src, dir = "") {
  base = dir;
  const lines = src.replace(/\r/g, "").split("\n");
  const out   = [];
  const seen  = new Map();
  let i       = 0;
  while (i < lines.length) {
    const l     = lines[i];
    const fence = l.match(/^```(\S*)/);
    if (fence) {
      const body = [];
      for (i++; i < lines.length && !lines[i].startsWith("```"); i++) {
        body.push(lines[i]);
      }

      i++;
      const name = fence[1].toLowerCase();
      const code = highlight(body.join("\n"), fenceLangs[name] || name);
      out.push(`<pre><code>${ code.join("\n") }</code></pre>`);
      continue;
    }

    const h = l.match(/^(#{1,6})\s+(.*?)\s*#*\s*$/);
    if (h) {
      const n    = h[1].length;
      const text = inline(h[2]);
      out.push(`<h${ n } id = "${ slug(text, seen) }">${ text }</h${ n }>`);
      i++;
      continue;
    }

    if (rule.test(l)) {
      out.push("<hr>");
      i++;
      continue;
    }

    const next = lines[i + 1];
    if (tableRow.test(l) && next !== undefined && tableSep.test(next)) {
      const [ t, after ] = table(lines, i);
      out.push(t);
      i = after;
      continue;
    }

    if (listRow.test(l)) {
      const [ html, after ] = list(lines, i);
      out.push(html);
      i = after;
      continue;
    }

    if (/^>\s?/.test(l)) {
      const q = [];
      for (; i < lines.length && /^>\s?/.test(lines[i]); i++) {
        q.push(lines[i].replace(/^>\s?/, ""));
      }

      out.push(`<blockquote>${ inline(q.join(" ")) }</blockquote>`);
      continue;
    }

    if (l.trim() === "") {
      i++;
      continue;
    }

    // a paragraph: its lines joined with spaces, as Markdown reads a
    // wrapped line; two spaces or a backslash at a line's end break it
    const p = [];
    for (; i < lines.length; i++) {
      if (lines[i].trim() === "" || blockStart.test(lines[i]) ||
        rule.test(lines[i])) {
        break;
      }

      const hard = / {2,}$|\\$/.test(lines[i]);
      p.push(lines[i].replace(/ +$|\\$/, "") + (hard ? "\u0002" : ""));
    }

    out.push(`<p>${ inline(p.join(" ")).replace(/\u0002 ?/g, "<br>") }</p>`);
  }

  return out.join("\n");
}
