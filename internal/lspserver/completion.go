package lspserver

import (
  "context"
  "encoding/json"
  "fmt"
  "os"
  "strings"

  "armin/internal/llm"
)

const (
  // completionPrefixLines/completionSuffixLines bound how much of
  // the file goes to the model — a completion needs to be fast, not
  // exhaustively grounded, so this is deliberately narrow compared
  // to what hover/ask are willing to read.
  completionPrefixLines = 60
  completionSuffixLines = 20

  completionMaxTokens   = 96
  completionMaxLines    = 20
  completionTemperature = 0.2
)

// inlineCompletion answers textDocument/inlineCompletion. Unlike
// hover and ask, this never goes through the agent loop: tool calls
// and chat formatting are far too slow for something meant to
// appear while you're still typing.
func (s *Server) inlineCompletion(
  ctx context.Context, id json.RawMessage, p InlineCompletionParams,
) {
  text := s.doc(p.TextDocument.URI)
  if text == "" {
    s.respond(id, []InlineCompletionItem{}, nil)
    return
  }

  prefix, suffix, ok := splitAtPosition(
    text, p.Position.Line, p.Position.Character,
  )
  if !ok {
    s.respond(id, []InlineCompletionItem{}, nil)
    return
  }
  prefix = lastLines(prefix, completionPrefixLines)
  suffix = firstLines(suffix, completionSuffixLines)

  compCtx, cancel, gen := s.startCompletion(p.TextDocument.URI, ctx)
  defer cancel()

  raw, err := s.completionLLM.Complete(
    compCtx, prefix, suffix, llm.CompleteOptions{
      Temperature: completionTemperature,
      NumPredict:  completionMaxTokens,
      Stop:        []string{ "\n\n\n" },
    },
  )

  s.endCompletion(p.TextDocument.URI, gen)

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
    s.respond(id, []InlineCompletionItem{}, nil)
    return
  }

  cleaned := cleanCompletion(raw, completionMaxLines)
  if cleaned == "" {
    s.respond(id, []InlineCompletionItem{}, nil)
    return
  }

  item := InlineCompletionItem{ InsertText: cleaned }
  s.respond(id, []InlineCompletionItem{ item }, nil)
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

  if prev, ok := s.inflight[uri]; ok {
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
  if cur, ok := s.inflight[uri]; ok && cur.gen == gen {
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

// splitAtPosition cuts text into everything before and after a
// position, via lineOffset + a slice — no per-line allocation,
// unlike the strings.Split-then-Join round trip this replaced
// (which also allocated a fresh joined string on top of the
// split). Position.Character is treated as a byte offset into the
// line, not a UTF-16 code unit count as the LSP spec technically
// requires — fine for ASCII source, which is most code; a line with
// multi-byte characters before the cursor can misplace the cut
// point.
func splitAtPosition(
  text string, line, character int,
) (prefix, suffix string, ok bool) {
  start, end, ok := lineOffset(text, line)
  if !ok {
    return "", "", false
  }

  lineLen := end - start
  if character < 0 {
    character = 0
  }
  if character > lineLen {
    character = lineLen
  }

  cut := start + character
  return text[:cut], text[cut:], true
}

// lastLines returns the final n lines of s (or all of s, if it has
// n or fewer) by scanning backward and slicing — zero per-line
// allocations, versus strings.Split(s, "\n") materializing every
// line in the whole document just to keep the last handful.
func lastLines(s string, n int) string {
  if n <= 0 {
    return ""
  }

  count := 0
  for i := len(s) - 1; i >= 0; i-- {
    if s[i] != '\n' {
      continue
    }
    count++
    if count == n {
      return s[i+1:]
    }
  }
  return s // fewer than n newlines total: the whole string qualifies
}

// firstLines is lastLines' mirror: scans forward and stops the
// moment it's seen n newlines, so a small n on a huge document
// doesn't even need to scan the whole thing (strings.Split always
// would).
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
// add despite instructions not to, trims trailing blank lines, and
// caps length — enforced here, not left to the model's judgment,
// same philosophy as the sandbox's output truncation.
func cleanCompletion(raw string, maxLines int) string {
  text := strings.TrimSpace(raw)

  if strings.HasPrefix(text, "```") {
    text = strings.TrimPrefix(text, "```")
    if idx := strings.Index(text, "\n"); idx >= 0 {
      text = text[idx+1:]
    }
    text = strings.TrimSuffix(strings.TrimRight(text, "\n \t"), "```")
  }

  text = strings.TrimRight(text, "\n")
  if text == "" {
    return ""
  }

  lines := strings.Split(text, "\n")
  if len(lines) > maxLines {
    lines = lines[:maxLines]
  }
  return strings.Join(lines, "\n")
}
