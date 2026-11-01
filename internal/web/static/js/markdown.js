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
const mdLink     = /\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g;
const bold       = /\*\*([^*]+)\*\*/g;
const italic     = /(^|[^*])\*([^*\s][^*]*)\*/g;
const listRow    = /^\s*([-*]|\d+\.)\s+/;
const tableRow   = /^\s*\|.*\|\s*$/;
const tableSep   = /^\s*\|[\s:|-]+\|\s*$/;
const blockStart = /^(```|#{1,6}\s|>|\s*([-*]|\d+\.)\s)/;

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

function inline(s) {
  const codes = [];
  s = s.replace(/`([^`]+)`/g, (_, c) => {
    codes.push(c);

    return "\u0000" + (codes.length - 1) + "\u0000";
  });

  // HTML the file wrote itself: its images kept (rebuilt safe), the
  // tags that only wrap or center them dropped, the rest escaped
  const images = [];
  s = s.replace(htmlImg, (_, attrs) => {
    images.push(htmlImage(attrs));

    return "\u0001" + (images.length - 1) + "\u0001";
  });

  s = esc(s.replace(htmlWrap, ""));
  s = s.replace(mdImg, (_, a, src) => imgTag(src, a));

  s = s.replace(mdLink, (_, t, u) => {
    return `<a href = "${ u }" target = "_blank" rel = "noopener">${ t }</a>`;
  });

  s = s.replace(bold, "<b>$1</b>").replace(italic, "$1<i>$2</i>");
  s = s.replace(fileRef, (m, pre, path, line) => {
    if (!known(path)) {
      return m;
    }

    return pre + refLink(path, line, m.slice(pre.length));
  });

  s = s.replace(/\u0001(\d+)\u0001/g, (_, i) => images[+i]);

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

// markdown renders src; dir is the folder of the file it came from
// ("" for answers), which relative image paths are read against.
export function markdown(src, dir = "") {
  base = dir;
  const lines = src.replace(/\r/g, "").split("\n");
  const out   = [];
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

    const h = l.match(/^(#{1,6})\s+(.*)/);
    if (h) {
      out.push(`<h${ h[1].length }>${ inline(h[2]) }</h${ h[1].length }>`);
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
      const ordered = /^\s*\d+\./.test(l);
      const items   = [];
      for (; i < lines.length && listRow.test(lines[i]); i++) {
        items.push(`<li>${ inline(lines[i].replace(listRow, "")) }</li>`);
      }

      const tag = ordered ? "ol" : "ul";
      out.push(`<${ tag }>${ items.join("") }</${ tag }>`);
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

    const p = [];
    for (; i < lines.length; i++) {
      if (lines[i].trim() === "" || blockStart.test(lines[i])) {
        break;
      }

      p.push(lines[i]);
    }

    out.push(`<p>${ inline(p.join("\n")).replace(/\n/g, "<br>") }</p>`);
  }

  return out.join("\n");
}
