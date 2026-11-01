// Syntax highlighting for the terminal: a small tokenizer per language
// family (comments, strings, numbers, keywords, types, calls), the
// same one the web UI uses (static/js/highlight.js), in the colors the
// user's nvim draws code with (everblush, by tree-sitter capture), as
// 24-bit color. A whole file is tokenized at once, so a block comment
// or a string spanning lines stays colored on every line.
//
//   Highlight   a file's lines, colored, by its extension

package ui

import (
  "path/filepath"
  "strings"
)

// highlightMax: bigger files come back plain, fast.
const highlightMax = 300000

// The everblush colors nvim gives each tree-sitter capture.
const (
  synKeyword = "\x1b[38;2;196;127;213m"  // #c47fd5 @keyword
  synFuncKw  = "\x1b[38;2;103;176;232m"  // #67b0e8 func, return, nil, escapes
  synString  = "\x1b[38;2;140;207;126m"  // #8ccf7e @string
  synComment = "\x1b[3;38;2;64;71;73m"   // #404749 @comment, italic
  synNumber  = "\x1b[38;2;229;199;107m"  // #e5c76b @number, @type
  synFunc    = "\x1b[38;2;229;116;116m"  // #e57474 @function, after a dot
  synBool    = "\x1b[38;2;108;191;191m"  // #6cbfbf @boolean
)

// funcWords are the keywords everblush colors apart from the rest
// (@keyword.function, @keyword.return), by language family.
var funcWords = map[string]map[string]bool{
  "go":   words("func return"),
  "js":   words("function return"),
  "c":    words("return"),
  "rs":   words("fn return"),
  "java": words("return"),
  "py":   words("def return lambda"),
  "sh":   words("function return"),
  "lua":  words("function return"),
  "typ":  words("return"),
}

// booleans get their own color; the other literals share func's.
var booleans = words("true false True False TRUE FALSE")

func words(s string) map[string]bool {
  m := map[string]bool{}
  for _, w := range strings.Fields(s) {
    m[w] = true
  }

  return m
}

var cLike = lang{ line: "//", block: [2]string{ "/*", "*/" }, quote: "\"'`" }

func withWords(base lang, quote, kw, lit, types string) lang {
  base.kw, base.lit, base.types = words(kw), words(lit), words(types)
  if quote != "" {
    base.quote = quote
  }

  return base
}

var langs = map[string]lang{
  "go": withWords(
    cLike, "",
    "break case chan const continue default defer else fallthrough for " +
      "func go goto if import interface map package range return select " +
      "struct switch type var",
    "true false nil iota",
    "bool byte complex64 complex128 error float32 float64 int int8 int16 " +
      "int32 int64 rune string uint uint8 uint16 uint32 uint64 uintptr any",
  ),
  "js": withWords(
    cLike, "",
    "async await break case catch class const continue debugger default " +
      "delete do else export extends finally for from function if import " +
      "in instanceof let new of return static super switch this throw try " +
      "typeof var void while with yield interface type enum implements",
    "true false null undefined NaN Infinity",
    "string number boolean any unknown never object void",
  ),
  "c": withWords(
    cLike, "\"'",
    "break case const continue default do else enum extern for goto if " +
      "inline register restrict return sizeof static struct switch " +
      "typedef union volatile while class namespace template typename " +
      "public private protected virtual override constexpr noexcept using " +
      "new delete auto #include #define #ifndef #ifdef #endif #if #else " +
      "#pragma",
    "true false NULL nullptr",
    "void char short int long float double signed unsigned bool size_t " +
      "u8 u16 u32 u64 i8 i16 i32 i64 f32 f64 uint8_t uint16_t uint32_t " +
      "uint64_t int8_t int16_t int32_t int64_t",
  ),
  "rs": withWords(
    cLike, "\"",
    "as async await break const continue crate dyn else enum extern fn " +
      "for if impl in let loop match mod move mut pub ref return self Self " +
      "static struct super trait type unsafe use where while",
    "true false None Some Ok Err",
    "i8 i16 i32 i64 i128 isize u8 u16 u32 u64 u128 usize f32 f64 bool " +
      "char str String Vec Option Result Box",
  ),
  "py": withWords(
    lang{ line: "#", quote: "\"'" },
    "",
    "and as assert async await break class continue def del elif else " +
      "except finally for from global if import in is lambda nonlocal not " +
      "or pass raise return try while with yield match case",
    "True False None self",
    "int float str bytes bool list dict set tuple object",
  ),
  "sh": withWords(
    lang{ line: "#", quote: "\"'`" },
    "",
    "if then else elif fi for while until do done case esac in function " +
      "return local export readonly set unset shift exit source alias",
    "true false", "",
  ),
  "lua": withWords(
    lang{ line: "--", block: [2]string{ "--[[", "]]" }, quote: "\"'" },
    "",
    "and break do else elseif end for function goto if in local not or " +
      "repeat return then until while",
    "true false nil", "",
  ),
  "typ": withWords(
    cLike, "\"",
    "let set show import include if else for in while return context",
    "true false none auto", "",
  ),
  "json": withWords(lang{ quote: "\"" }, "", "", "true false null", ""),
}

// byExt maps a file extension to its language family.
var byExt = map[string]string{
  "go": "go", "js": "js", "mjs": "js", "cjs": "js", "jsx": "js",
  "ts": "js", "tsx": "js", "c": "c", "h": "c", "cc": "c", "cpp": "c",
  "cxx": "c", "hpp": "c", "hh": "c", "inl": "c", "rs": "rs",
  "java": "c", "py": "py", "sh": "sh", "bash": "sh", "zsh": "sh",
  "lua": "lua", "typ": "typ", "json": "json",
}

// Highlight returns lines (a whole file, or a run of it) colored for
// the language of path's extension; plain when the language is unknown,
// the text too big, or color is off.
func Highlight(path string, lines []string) []string {
  ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
  lg, ok := langs[byExt[ext]]
  text := strings.Join(lines, "\n")
  if !ok || !Enabled || len(text) > highlightMax {
    return lines
  }

  out := make([]string, 0, len(lines))
  var line strings.Builder
  put := func(code, tok string) {
    for i, part := range strings.Split(tok, "\n") {
      if i > 0 {
        out = append(out, line.String())
        line.Reset()
      }

      if part == "" {
        continue
      }

      if code == "" {
        line.WriteString(part)
        continue
      }

      line.WriteString(code + part + reset)
    }
  }

  py := byExt[ext] == "py"
  n := len(text)
  for i := 0; i < n; {
    c := text[i]
    switch {
    case lg.block[0] != "" && strings.HasPrefix(text[i:], lg.block[0]):
      end := strings.Index(text[i + len(lg.block[0]):], lg.block[1])
      j := n
      if end >= 0 {
        j = i + len(lg.block[0]) + end + len(lg.block[1])
      }

      put(synComment, text[i:j])
      i = j

    case lg.line != "" && strings.HasPrefix(text[i:], lg.line):
      j := strings.IndexByte(text[i:], '\n')
      if j < 0 {
        j = n - i
      }

      put(synComment, text[i:i + j])
      i += j

    case strings.IndexByte(lg.quote, c) >= 0:
      j := endOfString(text, i, py)
      putString(put, text[i:j], c == '`' && byExt[ext] == "go")
      i = j

    case c >= '0' && c <= '9' && (i == 0 || !isWord(text[i - 1])):
      j := i
      for j < n && (isWord(text[j]) || text[j] == '.') {
        j++
      }

      put(synNumber, text[i:j])
      i = j

    case isWordStart(c) || c == '#' && i + 1 < n && isWordStart(text[i + 1]):
      j := i + 1
      for j < n && isWord(text[j]) {
        j++
      }

      fw := funcWords[byExt[ext]]
      put(wordColor(lg, fw, text, i, j, byExt[ext] == "go"), text[i:j])
      i = j

    default:
      put("", text[i:i + 1])
      i++
    }
  }

  out = append(out, line.String())

  return out
}

// putString puts a string, its escapes (\n, \") in their own color;
// a Go raw string has none.
func putString(put func(code, tok string), s string, raw bool) {
  start := 0
  for i := 0; i < len(s) - 1; i++ {
    if raw || s[i] != '\\' {
      continue
    }

    put(synString, s[start:i])
    put(synFuncKw, s[i:i + 2])
    start = i + 2
    i++
  }

  put(synString, s[start:])
}

// endOfString finds where the string opened at i ends: its closing
// quote, or the line's end for a quote that may not span lines.
func endOfString(text string, i int, py bool) int {
  q := text[i]
  closer := string(q)
  long := py && strings.HasPrefix(text[i:], strings.Repeat(closer, 3))
  if long {
    closer = strings.Repeat(closer, 3)
  }

  n := len(text)
  j := i + len(closer)
  for j < n && !strings.HasPrefix(text[j:], closer) {
    if text[j] == '\\' && q != '`' {
      j++
    } else if text[j] == '\n' && !long && q != '`' {
      return j
    }

    j++
  }

  return min(n, j + len(closer))
}

// wordColor colors a word: a function keyword, keyword, literal,
// type, a field or method after a dot, a call (followed by "("), or
// in Go an exported name used as a type.
func wordColor(
  lg lang, fw map[string]bool, text string, i, j int, goLang bool,
) string {
  w := text[i:j]
  key := w
  if lg.fold {
    key = strings.ToLower(w)
  }

  switch {
  case fw[key]:
    return synFuncKw
  case lg.kw[key]:
    return synKeyword
  case lg.lit[key] && booleans[key]:
    return synBool
  case lg.lit[key]:
    return synFuncKw
  case lg.types[key]:
    return synNumber
  case i > 0 && text[i - 1] == '.':
    return synFunc
  case strings.HasPrefix(
    strings.TrimLeft(text[j:min(len(text), j + 4)], " "), "(",
  ):
    return synFunc
  case goLang && w[0] >= 'A' && w[0] <= 'Z':
    return synNumber
  }

  return ""
}

func isWordStart(c byte) bool {
  return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isWord(c byte) bool {
  return isWordStart(c) || c >= '0' && c <= '9'
}
