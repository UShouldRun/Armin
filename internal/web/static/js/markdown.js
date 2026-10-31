// Markdown for answers and previews: small and safe (escape first,
// then build), with file:line references made into links the viewer
// opens.

import { esc }       from "./api.js";
import { highlight } from "./highlight.js";
import { state }     from "./state.js";

// fenceLangs maps a code fence's language to highlight's file
// extension names; others are tried as they are.
const fenceLangs = {
  python: "py", rust: "rs", bash: "sh", shell: "sh", zsh: "sh",
  javascript: "js", typescript: "ts", "c++": "cpp", golang: "go",
  typst: "typ", kotlin: "kt",
};

const fileRef = /(^|[\s(`\[])((?:[\w.-]+\/)*[\w.-]+\.[A-Za-z0-9]{1,8}):(\d+)(?:-(\d+))?/g;

function inline(s) {
  const codes = [];
  s = s.replace(/`([^`]+)`/g, (_, c) => {
    codes.push(c);
    return "\u0000" + (codes.length - 1) + "\u0000";
  });
  s = esc(s);
  // raw HTML images from the file itself (&lt;img&gt; after esc):
  // unescape just the tag, and route repo paths through /api/raw
  s = s.replace(/&lt;img([^&]*?)src=(&quot;|")([^"&]+)(&quot;|")([^&]*?)&gt;/g,
    (_, pre, _q1, src, _q2, post) => {
      const img = src.startsWith("http") ? src
                : src.startsWith("/") ? src
                : `/api/raw?path=${ encodeURIComponent(src) }`;
      return `<img class = "md-img" src = "${ img }"${ pre }${ post }>`;
    });
  s = s.replace(/!\[([^\]]*)\]\(([^\s)]+)\)/g, (_, a, src) => {
    const img = src.startsWith("http") ? esc(src)
              : src.startsWith("/") ? esc(src)
              : `/api/raw?path=${ encodeURIComponent(src) }`;
    return `<img class = "md-img" src = "${ img }" alt = "${ esc(a) }" loading = "lazy">`;
  });
  s = s.replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, (_, t, u) => `<a href = "${ u }" target = "_blank" rel = "noopener">${ t }</a>`);
  s = s.replace(/\*\*([^*]+)\*\*/g, "<b>$1</b>").replace(/(^|[^*])\*([^*\s][^*]*)\*/g, "$1<i>$2</i>");
  s = s.replace(fileRef, (m, pre, path, line) => state.files.includes(path) || state.files.some((f) => f.endsWith("/" + path))
    ? `${ pre }<a class = "ref" data-path = "${ path }" data-line = "${ line }">${ m.slice(pre.length) }</a>`
    : m);

  return s.replace(/\u0000(\d+)\u0000/g, (_, i) => {
    const c = codes[+i];
    const ref = c.match(/^((?:[\w.-]+\/)*[\w.-]+\.[A-Za-z0-9]{1,8}):(\d+)/);

    return ref ? `<a class = "ref" data-path = "${ esc(ref[1]) }" data-line = "${ ref[2] }"><code>${ esc(c) }</code></a>`
               : `<code>${ esc(c) }</code>`;
  });
}

export function markdown(src) {
  const lines = src.replace(/\r/g, "").split("\n");
  const out   = [];
  let i = 0;
  while (i < lines.length) {
    const l = lines[i];
    const fence = l.match(/^```(\S*)/);
    if (fence) {
      const body = [];
      for (i++; i < lines.length && !lines[i].startsWith("```"); i++) {
        body.push(lines[i]);
      }

      i++;
      const lang = fenceLangs[fence[1].toLowerCase()] || fence[1].toLowerCase();
      out.push(`<pre><code>${ highlight(body.join("\n"), lang).join("\n") }</code></pre>`);
      continue;
    }

    const h = l.match(/^(#{1,6})\s+(.*)/);
    if (h) {
      out.push(`<h${ h[1].length }>${ inline(h[2]) }</h${ h[1].length }>`);
      i++;
      continue;
    }

    if (/^\s*\|.*\|\s*$/.test(l) && i + 1 < lines.length && /^\s*\|[\s:|-]+\|\s*$/.test(lines[i + 1])) {
      const row = (r) => r.trim().replace(/^\||\|$/g, "").split("|").map((c) => c.trim());
      let t = "<table><tr>" + row(l).map((c) => `<th>${ inline(c) }</th>`).join("") + "</tr>";
      for (i += 2; i < lines.length && /^\s*\|.*\|\s*$/.test(lines[i]); i++) {
        t += "<tr>" + row(lines[i]).map((c) => `<td>${ inline(c) }</td>`).join("") + "</tr>";
      }

      out.push(t + "</table>");
      continue;
    }

    if (/^\s*([-*]|\d+\.)\s+/.test(l)) {
      const ordered = /^\s*\d+\./.test(l);
      const items   = [];
      for (; i < lines.length && /^\s*([-*]|\d+\.)\s+/.test(lines[i]); i++) {
        items.push(`<li>${ inline(lines[i].replace(/^\s*([-*]|\d+\.)\s+/, "")) }</li>`);
      }

      out.push(ordered ? `<ol>${ items.join("") }</ol>` : `<ul>${ items.join("") }</ul>`);
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
    for (; i < lines.length && lines[i].trim() !== "" && !/^(```|#{1,6}\s|>|\s*([-*]|\d+\.)\s)/.test(lines[i]); i++) {
      p.push(lines[i]);
    }
    out.push(`<p>${ inline(p.join("\n")).replace(/\n/g, "<br>") }</p>`);
  }
  return out.join("\n");
}
