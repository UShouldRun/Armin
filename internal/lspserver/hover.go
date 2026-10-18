// Hover: an answer about the symbol under the cursor, from the
// agent loop in read-only mode, grounded in the code around it
// (contextWindow) and cached per file, line and snippet.

package lspserver

import (
  "context"
  "crypto/sha256"
  "encoding/hex"
  "encoding/json"
  "fmt"
  "strings"
)

func (s *Server) hover(
  ctx context.Context, id json.RawMessage, p HoverParams,
) {
  text := s.doc(p.TextDocument.URI)
  if text == "" {
    s.respond(id, nil, nil)
    return
  }

  snippet, lineText := contextWindow(
    text, p.Position.Line, hoverContextLines,
  )
  if strings.TrimSpace(lineText) == "" {
    s.respond(id, nil, nil)
    return
  }

  // Ground the answer in a deterministic fact from the ctags index
  // when the cursor sits on a known symbol, instead of leaving the
  // model to guess where something is defined.
  word := wordAt(lineText, p.Position.Character)
  fact := s.symbolFact(ctx, word, p.TextDocument.URI)

  key := cacheKey(p.TextDocument.URI, p.Position.Line, snippet + "|" + word)
  cached, ok := s.cachedHover(key)
  if ok {
    s.respond(id, hoverResult(cached), nil)
    return
  }

  question := fmt.Sprintf(
    "Explain concisely (max one short paragraph, no preamble) "+
      "what this code does. File: %s, near line %d%s:\n\n```\n%s\n```",
    s.relPath(p.TextDocument.URI), p.Position.Line + 1, fact, snippet,
  )

  answer, err := s.explain(ctx, question)
  if err != nil {
    s.respondAskError(id, err)
    return
  }

  answer = capLength(answer, maxHoverChars)
  s.storeHover(key, answer)
  s.respond(id, hoverResult(answer), nil)
}

func contextWindow(
  text string, line, radius int,
) (snippet, lineText string) {
  start, end, ok := lineOffset(text, line)
  if !ok {
    return "", ""
  }
  lineText = text[start:end]

  winStart, _, ok := lineOffset(text, line - radius)
  if !ok {
    winStart = 0
  }
  _, winEnd, ok := lineOffset(text, line + radius)
  if !ok {
    winEnd = len(text)
  }

  return text[winStart:winEnd], lineText
}

func cacheKey(uri string, line int, snippet string) string {
  sum := sha256.Sum256([]byte(snippet))
  return fmt.Sprintf("%s:%d:%s", uri, line, hex.EncodeToString(sum[:8]))
}

func (s *Server) cachedHover(key string) (string, bool) {
  s.hoverMu.Lock()
  defer s.hoverMu.Unlock()
  v, ok := s.hoverCache[key]
  return v, ok
}

func (s *Server) storeHover(key, value string) {
  s.hoverMu.Lock()
  defer s.hoverMu.Unlock()
  s.hoverCache[key] = value
}

func capLength(s string, max int) string {
  if len(s) <= max {
    return s
  }
  return s[:max] + "\n\n…(truncated)"
}
