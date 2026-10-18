// Inline completion (ghost text): inlineCompletion cuts a fill-in-
// the-middle window around the cursor (fimWindow), adds nearby open
// files as context (otherChunks), asks the completion model and trims
// the answer to what fits the line. A newer keystroke cancels an older
// request (startCompletion, endCompletion).

package lspserver

import (
  "context"
  "encoding/json"
  "fmt"
  "os"
  "path"
  "sort"
  "strings"

  "armin/internal/llm"
  "armin/internal/share"
)

const (
  // The prefix window starts on a multiple of completionAlign lines,
  // so it moves once every 16 lines instead of on every line: the
  // prompt's start stays byte-stable and llama-server reuses its KV
  // cache, processing only what changed near the cursor.
  completionPrefixLines = 64
  completionSuffixLines = 24
  completionAlign       = 16

  // completionHeadLines of the file (package, imports, types) go in
  // as extra context when they've scrolled out of the prefix window.
  completionHeadLines = 32

  // Up to completionOtherFiles other open files of the same kind
  // contribute completionChunkLines around their last edit.
  completionOtherFiles = 3
  completionChunkLines = 32

  completionMaxTokens    = 128
  completionMaxLines     = 16
  completionMaxPredictMs = 400
  completionTemperature  = 0.2
  completionTopK         = 40
  completionTopP         = 0.95
)

// inlineCompletion answers textDocument/inlineCompletion. Unlike
// hover and ask, this never goes through the agent loop: tool calls
// and chat formatting are far too slow for something meant to
// appear while you're still typing.
func (s *Server) inlineCompletion(
  ctx context.Context, id json.RawMessage, p InlineCompletionParams,
) {
  uri  := p.TextDocument.URI
  text := s.doc(uri)
  if text == "" {
    s.respond(id, []InlineCompletionItem{}, nil)
    return
  }

  w, ok := fimWindow(text, p.Position.Line, p.Position.Character)
  if !ok {
    s.respond(id, []InlineCompletionItem{}, nil)
    return
  }

  extra := s.otherChunks(uri)
  if w.head != "" {
    extra = append(extra, llm.InfillChunk{
      Filename: s.relPath(uri), Text: w.head,
    })
  }

  compCtx, cancel, gen := s.startCompletion(uri, ctx)
  defer cancel()

  raw, err := s.complete(compCtx, share.Fill{
    Key:    uri,
    Prefix: w.prefix,
    Suffix: w.suffix,
    Opts:   llm.CompleteOptions{
      Prompt:       w.prompt,
      Extra:        extra,
      Indent:       w.indent,
      Temperature:  completionTemperature,
      TopK:         completionTopK,
      TopP:         completionTopP,
      NumPredict:   completionMaxTokens,
      MaxPredictMs: completionMaxPredictMs,
      Stop:         []string{ "\n\n\n" },
    },
  })

  s.endCompletion(uri, gen)

  if err != nil {
    if compCtx.Err() != nil {
      // Superseded by a newer completion request for the same file
      // — the client already has something more current in flight.
      s.respondError(
        id, errRequestCancelled, "superseded by a newer request",
      )
      return
    }

    s.warnCompletionFailure(err)
    s.respondError(id, errInternal, firstLine(err))
    return
  }

  cleaned := fitToLine(
    cleanCompletion(raw, completionMaxLines), w.rest, w.next,
  )
  if cleaned == "" {
    s.respond(id, []InlineCompletionItem{}, nil)
    return
  }

  item := InlineCompletionItem{ InsertText: cleaned }
  s.respond(id, []InlineCompletionItem{ item }, nil)
}

// firstLine is err's innermost cause on one line: the wrapped
// "... in X:" chain is noise in an editor notification.
func firstLine(err error) string {
  msg := strings.TrimSpace(err.Error())
  i := strings.LastIndex(msg, "\n")
  if i >= 0 {
    msg = strings.TrimSpace(msg[i + 1:])
  }

  return msg
}

// fimWindow cuts the request around (line, character): prefix is
// whole lines from an aligned start up to the cursor's line, prompt
// is that line up to the cursor, suffix runs from the cursor to an
// aligned end. Character is a byte offset, which is what the nvim
// client sends (nvim_win_get_cursor). Slices only — no allocation.
func fimWindow(text string, line, character int) (fim, bool) {
  var w fim

  lineStart, lineEnd, ok := lineOffset(text, line)
  if !ok {
    return w, false
  }

  character = min(max(character, 0), lineEnd - lineStart)
  cut      := lineStart + character

  from := max(line - completionPrefixLines, 0) /
    completionAlign * completionAlign
  prefixStart, _, _ := lineOffset(text, from)

  to := (line + completionSuffixLines) /
    completionAlign * completionAlign + completionAlign
  _, suffixEnd, ok := lineOffset(text, to)
  if !ok {
    suffixEnd = len(text)
  }

  w.prefix = text[prefixStart:lineStart]
  w.prompt = text[lineStart:cut]
  w.suffix = text[cut:suffixEnd]
  w.rest   = text[cut:lineEnd]

  if from > 0 {
    w.head = firstLines(text, min(completionHeadLines, from))
  }

  for w.indent < lineEnd - lineStart {
    c := text[lineStart + w.indent]
    if c != ' ' && c != '\t' {
      break
    }

    w.indent++
  }

  for i := lineEnd + 1; i < suffixEnd; {
    j := strings.IndexByte(text[i:suffixEnd], '\n')
    if j < 0 {
      j = suffixEnd - i
    }

    l := strings.TrimSpace(text[i:i + j])
    if l != "" {
      w.next = l
      break
    }

    i += j + 1
  }

  return w, true
}

// fitToLine drops what would duplicate the code around the cursor.
// With text after the cursor only the first line is kept, minus the
// tail that already follows ("foo(|)" + "a)" → "a"); a completion
// that just repeats the next line is no suggestion.
func fitToLine(text, rest, next string) string {
  if text == "" {
    return ""
  }

  tail := strings.TrimSpace(rest)
  if tail != "" {
    i := strings.IndexByte(text, '\n')
    if i >= 0 {
      text = text[:i]
    }

    text = strings.TrimRight(text, " \t")
    text = strings.TrimRight(strings.TrimSuffix(text, tail), " \t")
    if strings.TrimSpace(text) == "" {
      return ""
    }

    return text
  }

  first := text
  i := strings.IndexByte(text, '\n')
  if i >= 0 {
    first = text[:i]
  }

  if next != "" && strings.TrimSpace(first) == next {
    return ""
  }

  return text
}

// otherChunks is input_extra from the other open files of uri's
// kind that were edited this session, most recent last (nearest the
// prefix): completionChunkLines around each one's last edit. The
// order only changes when those files are edited, so the chunks stay
// cached while you work in this one.
func (s *Server) otherChunks(uri string) []llm.InfillChunk {
  ext := path.Ext(uri)

  type pick struct {
    uri, text string
    line      int
    seq       uint64
  }

  var picks [completionOtherFiles]pick
  n := 0

  s.docsMu.Lock()
  for u, e := range s.edits {
    if u == uri || path.Ext(u) != ext {
      continue
    }

    p := pick{ uri: u, text: s.docs[u], line: e.line, seq: e.seq }
    if n < len(picks) {
      picks[n] = p
      n++
      continue
    }

    oldest := 0
    for i := 1; i < n; i++ {
      if picks[i].seq < picks[oldest].seq {
        oldest = i
      }
    }

    if p.seq > picks[oldest].seq {
      picks[oldest] = p
    }
  }
  s.docsMu.Unlock()

  if n == 0 {
    return nil
  }

  sort.Slice(picks[:n], func(i, j int) bool {
    return picks[i].seq < picks[j].seq
  })

  out := make([]llm.InfillChunk, 0, n + 1)
  for _, p := range picks[:n] {
    from := max(p.line - completionChunkLines / 2, 0) / 8 * 8
    start, _, ok := lineOffset(p.text, from)
    if !ok {
      continue
    }

    out = append(out, llm.InfillChunk{
      Filename: s.relPath(p.uri),
      Text:     firstLines(p.text[start:], completionChunkLines),
    })
  }

  return out
}

// startCompletion cancels whatever completion was already running
// for uri (its result is stale the moment a newer one is requested)
// and registers this one under a fresh generation number, so
// endCompletion can tell whether it's still the current occupant
// before clearing it.
func (s *Server) startCompletion(
  uri string, parent context.Context,
) (context.Context, context.CancelFunc, int64) {
  s.inflightMu.Lock()
  defer s.inflightMu.Unlock()

  prev, ok := s.inflight[uri]
  if ok {
    prev.cancel()
  }

  s.nextGen++
  gen := s.nextGen
  ctx, cancel := context.WithCancel(parent)
  s.inflight[uri] = inflightEntry{ gen: gen, cancel: cancel }
  return ctx, cancel, gen
}

func (s *Server) endCompletion(uri string, gen int64) {
  s.inflightMu.Lock()
  defer s.inflightMu.Unlock()
  cur, ok := s.inflight[uri]
  if ok && cur.gen == gen {
    delete(s.inflight, uri)
  }
}

func (s *Server) warnCompletionFailure(err error) {
  s.warnOnce.Do(func() {
    fmt.Fprintf(
      os.Stderr,
      "armin lsp: completion model call failed, will keep trying silently: %s\n",
      err,
    )
  })
}

// firstLines returns the first n lines of s: scans forward and
// stops at the nth newline, so a small n on a huge document never
// scans the whole thing.
func firstLines(s string, n int) string {
  count := 0
  for i := 0; i < len(s); i++ {
    if s[i] != '\n' {
      continue
    }
    count++
    if count == n {
      return s[:i]
    }
  }
  return s
}

// cleanCompletion strips a markdown fence a chat-leaning model may
// add, trims trailing whitespace and caps the line count. Leading
// whitespace is the completion's own: "x :=|" continues " foo()".
func cleanCompletion(raw string, maxLines int) string {
  text := raw
  t := strings.TrimSpace(raw)
  if strings.HasPrefix(t, "```") {
    text = t[3:]
    idx := strings.Index(text, "\n")
    if idx >= 0 {
      text = text[idx + 1:]
    }

    text = strings.TrimSuffix(strings.TrimRight(text, "\n \t"), "```")
  }

  text = strings.TrimRight(text, "\n \t")
  if strings.TrimSpace(text) == "" {
    return ""
  }

  n := 0
  for i := 0; i < len(text); i++ {
    if text[i] != '\n' {
      continue
    }

    n++
    if n == maxLines {
      return text[:i]
    }
  }

  return text
}
