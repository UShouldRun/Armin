// Syntax highlighting: a small tokenizer per language family
// (comments, strings, numbers, keywords, types, calls), no library.

import { esc } from "./api.js";

// A small tokenizer per language family: comments, strings,
// numbers, keywords, types and calls. Enough to read code by, with
// no library behind it.
const words = (...parts) => new Set(parts.join(" ").split(" "));

const cLike = {
  line:  "//",
  block: [ "/*", "*/" ],
  quote: "\"'`",
};

const langs = {
  go: {
    ...cLike,
    kw:    words(
      "break case chan const continue default defer else",
      "fallthrough for func go goto if import interface map",
      "package range return select struct switch type var",
    ),
    lit:   words("true false nil iota"),
    types: words(
      "bool byte complex64 complex128 error float32 float64 int",
      "int8 int16 int32 int64 rune string uint uint8 uint16",
      "uint32 uint64 uintptr any",
    ),
  },
  js: {
    ...cLike,
    kw:    words(
      "async await break case catch class const continue debugger",
      "default delete do else export extends finally for from",
      "function if import in instanceof let new of return static",
      "super switch this throw try typeof var void while with",
      "yield interface type enum implements",
    ),
    lit:   words("true false null undefined NaN Infinity"),
    types: words("string number boolean any unknown never object void"),
  },
  c: {
    ...cLike,
    quote: "\"'",
    kw:    words(
      "break case const continue default do else enum extern for",
      "goto if inline register restrict return sizeof static",
      "struct switch typedef union volatile while class namespace",
      "template typename public private protected virtual",
      "override constexpr noexcept using new delete auto #include",
      "#define #ifndef #ifdef #endif #if #else #pragma",
    ),
    lit:   words("true false NULL nullptr"),
    types: words(
      "void char short int long float double signed unsigned bool",
      "size_t u8 u16 u32 u64 i8 i16 i32 i64 f32 f64 uint8_t",
      "uint16_t uint32_t uint64_t int8_t int16_t int32_t int64_t",
    ),
  },
  rs: {
    ...cLike,
    quote: "\"",
    kw:    words(
      "as async await break const continue crate dyn else enum",
      "extern fn for if impl in let loop match mod move mut pub",
      "ref return self Self static struct super trait type unsafe",
      "use where while",
    ),
    lit:   words("true false None Some Ok Err"),
    types: words(
      "i8 i16 i32 i64 i128 isize u8 u16 u32 u64 u128 usize f32",
      "f64 bool char str String Vec Option Result Box",
    ),
  },
  java: {
    ...cLike,
    quote: "\"'",
    kw:    words(
      "abstract break case catch class continue default do else",
      "enum extends final finally for if implements import",
      "instanceof interface new package private protected public",
      "return static super switch this throw throws try void",
      "while var record",
    ),
    lit:   words("true false null"),
    types: words("int long short byte char boolean float double String Object"),
  },
  py: {
    line:  "#",
    quote: "\"'",
    kw:    words(
      "and as assert async await break class continue def del",
      "elif else except finally for from global if import in is",
      "lambda nonlocal not or pass raise return try while with",
      "yield match case",
    ),
    lit:   words("True False None self"),
    types: words("int float str bytes bool list dict set tuple object"),
  },
  sh: {
    line:  "#",
    quote: "\"'`",
    kw:    words(
      "if then else elif fi for while until do done case esac in",
      "function return local export readonly set unset shift exit",
      "source alias",
    ),
    lit:   words("true false"),
    types: words(""),
  },
  lua: {
    line:  "--",
    block: [ "--[[", "]]" ],
    quote: "\"'",
    kw:    words(
      "and break do else elseif end for function goto if in local",
      "not or repeat return then until while",
    ),
    lit:   words("true false nil"),
    types: words(""),
  },
  sql: {
    line:  "--",
    block: [ "/*", "*/" ],
    quote: "'\"",
    kw:    words(
      "select from where insert into values update set delete",
      "create table index view drop alter add join left right",
      "inner outer on group by order having limit offset as and",
      "or not null is in exists distinct union all primary key",
      "foreign references default unique check begin commit",
      "rollback with returning case when then else end",
    ),
    lit:   words("true false"),
    types: words(
      "int integer bigint smallint text varchar char boolean date",
      "timestamp timestamptz numeric real serial uuid jsonb json",
      "bytea",
    ),
    fold:  true,
  },
  typ: {
    ...cLike,
    quote: "\"",
    kw:    words(
      "let set show import include if else for in while return",
      "context",
    ),
    lit:   words("true false none auto"),
    types: words(""),
  },
  json: {
    quote: "\"",
    kw:    words(""),
    lit:   words("true false null"),
    types: words(""),
  },
};

const byExt = {
  go: "go", js: "js", mjs: "js", cjs: "js", jsx: "js", ts: "js", tsx: "js",
  c: "c", h: "c", cc: "c", cpp: "c", cxx: "c", hpp: "c", hh: "c", inl: "c",
  rs: "rs", java: "java", kt: "java", py: "py", sh: "sh", bash: "sh",
  zsh: "sh", lua: "lua", sql: "sql", typ: "typ", json: "json",
};

// number is a numeric literal: hex, binary, decimal with a fraction
// and an exponent.
const number =
  /^(0x[0-9a-fA-F_]+|0b[01_]+|[0-9][0-9_]*(\.[0-9_]+)?([eE][+-]?[0-9]+)?)/;

// highlightMax: bigger files show plain, fast.
const highlightMax = 300000;

// highlight returns the text as HTML lines, spans closed and
// reopened at each line break so a block comment or string spanning
// lines stays colored in every table row.
export function highlight(text, ext) {
  const lang = langs[byExt[ext]];
  if (!lang || text.length > highlightMax) {
    return text.split("\n").map(esc);
  }

  const out  = [];
  let   line = "";
  const put  = (cls, tok) => {
    const parts = tok.split("\n");
    parts.forEach((p, i) => {
      if (i > 0) {
        out.push(line);
        line = "";
      }

      if (p !== "") {
        line += cls ? `<span class = "${ cls }">${ esc(p) }</span>` :
          esc(p);
      }
    });
  };

  let i = 0;
  const n = text.length;
  while (i < n) {
    const ch = text[i];

    if (lang.block && text.startsWith(lang.block[0], i)) {
      let end = text.indexOf(lang.block[1], i + lang.block[0].length);
      end = end < 0 ? n : end + lang.block[1].length;
      put("c", text.slice(i, end));
      i = end;
      continue;
    }

    if (lang.line && text.startsWith(lang.line, i)) {
      let end = text.indexOf("\n", i);
      end = end < 0 ? n : end;
      put("c", text.slice(i, end));
      i = end;
      continue;
    }

    if (lang.quote && lang.quote.includes(ch)) {
      const triple = ch.repeat(3);
      const long   = byExt[ext] === "py" && text.startsWith(triple, i);
      const close  = long ? triple : ch;
      let   j      = i + close.length;
      while (j < n && !text.startsWith(close, j)) {
        if (text[j] === "\\" && ch !== "`") {
          j++;
        }

        if (text[j] === "\n" && !long && ch !== "`") {
          break;
        }

        j++;
      }

      j = Math.min(n, j + close.length);
      put("s", text.slice(i, j));
      i = j;
      continue;
    }

    if (/[0-9]/.test(ch) && !/[\w]/.test(text[i - 1] || "")) {
      const m = number.exec(text.slice(i, i + 40));
      put("num", m[0]);
      i += m[0].length;
      continue;
    }

    const directive = ch === "#" && /[A-Za-z]/.test(text[i + 1] || "");
    if (/[A-Za-z_]/.test(ch) || directive) {
      const m    = /^#?[A-Za-z_][\w]*/.exec(text.slice(i, i + 80));
      const word = m[0];
      const key  = lang.fold ? word.toLowerCase() : word;
      let   cls  = "";
      const after  = text.slice(i + word.length, i + word.length + 4);
      const goType = byExt[ext] === "go" && /^[A-Z]/.test(word) &&
        text[i - 1] !== ".";
      if (lang.kw.has(key)) {
        cls = "k";

      } else if (lang.lit.has(key)) {
        cls = "num";

      } else if (lang.types.has(key)) {
        cls = "t";

      } else if (/^\s*\(/.test(after)) {
        cls = "f";

      } else if (goType) {
        cls = "t";
      }

      put(cls, word);
      i += word.length;
      continue;
    }

    put("", ch);
    i++;
  }

  out.push(line);

  return out;
}
