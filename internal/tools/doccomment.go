package tools

import (
  "bufio"
  "context"
  "fmt"
  "os"
  "strings"

  "armin/internal/ctags"
  "armin/internal/sandbox"
)

type ReadDocCommentInput struct {
  Name     string `json:"name"`
  FromFile string `json:"from_file,omitempty"`
}

type ReadDocCommentOutput struct {
  File    string `json:"file"`
  Line    int    `json:"line"`
  Comment string `json:"comment"` // empty if no doc comment precedes the definition
}

// ReadDocComment finds Name's definition via ctags, then reads
// backward from it for a contiguous line-comment (//) or block
// comment (/* */) immediately preceding it — Go/C-style doc comment
// convention. Not every symbol has one; an empty Comment (not an
// error) means none was found directly above the definition.
func ReadDocComment(
  ctx context.Context, idx *ctags.Index, cfg *sandbox.Config,
  in ReadDocCommentInput,
) (*ReadDocCommentOutput, error) {
  if idx == nil {
    return nil, ctagsUnavailable("ReadDocComment")
  }
  if in.Name == "" {
    return nil, fmt.Errorf("\n\t... in ReadDocComment:\n\t name is required")
  }

  matches := idx.Lookup(ctx, in.Name, in.FromFile)
  if len(matches) == 0 {
    return nil, fmt.Errorf(
      "\n\t... in ReadDocComment:\n\t no definition found for %q", in.Name,
    )
  }
  sym := matches[0] // Lookup already ranks the most relevant match first

  resolved, err := cfg.ResolvePath(sym.File)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in ReadDocComment:\n\t%w", err)
  }
  lines, err := readAllLines(resolved)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in ReadDocComment:\n\t%w", err)
  }

  comment := commentAbove(lines, sym.Line)
  return &ReadDocCommentOutput{ File: sym.File, Line: sym.Line, Comment: comment }, nil
}

func readAllLines(path string) ([]string, error) {
  f, err := os.Open(path)
  if err != nil {
    return nil, err
  }
  defer f.Close()

  var lines []string
  scanner := bufio.NewScanner(f)
  scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
  for scanner.Scan() {
    lines = append(lines, scanner.Text())
  }
  return lines, scanner.Err()
}

// commentAbove walks upward from defLine (1-indexed) collecting a
// contiguous run of "//"-prefixed lines, or a single /* ... */
// block, immediately preceding it — a blank line breaks the run,
// matching how Go/C-style doc comments must directly precede their
// symbol with nothing in between.
func commentAbove(lines []string, defLine int) string {
  i := defLine - 2 // 0-indexed line just above defLine
  if i < 0 || i >= len(lines) {
    return ""
  }

  if strings.HasSuffix(strings.TrimSpace(lines[i]), "*/") {
    return blockCommentAbove(lines, i)
  }

  var collected []string
  for i >= 0 {
    trimmed := strings.TrimSpace(lines[i])
    if !strings.HasPrefix(trimmed, "//") {
      break
    }
    collected = append([]string{ trimmed }, collected...)
    i--
  }
  return strings.Join(collected, "\n")
}

func blockCommentAbove(lines []string, endIdx int) string {
  start := endIdx
  for start >= 0 && !strings.Contains(lines[start], "/*") {
    start--
  }
  if start < 0 {
    return ""
  }
  return strings.Join(lines[start:endIdx+1], "\n")
}
