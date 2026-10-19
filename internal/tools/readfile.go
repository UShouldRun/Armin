// ReadFile: a line range of a text file; long files need a range.

package tools

import (
  "bufio"
  "context"
  "fmt"
  "os"
  "strings"

  "armin/internal/sandbox"
)

// maxWholeFileLines is the threshold above which a line range is
// required. Below it, read_file returns the whole file so small
// files don't force the model into a pointless round trip.
const maxWholeFileLines = 400

// maxReadBytes caps one read, range or not: about 4–5k tokens. A
// longer read ends early and says where to go on (NextLine), so one
// log or minified file can't fill a small model's window.
const maxReadBytes = 16 * 1024

// ReadFile reads a line range from a sandboxed file. For files longer
// than maxWholeFileLines, a range is required — this forces the
// model to search first, then read narrowly, rather than dumping
// whole files into context.
func ReadFile(
  ctx context.Context, cfg *sandbox.Config, in ReadFileInput,
) (*ReadFileOutput, error) {
  resolved, err := cfg.ResolvePath(in.Path)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in ReadFile:\n\t%w", err)
  }

  f, err := os.Open(resolved)
  if err != nil {
    return nil, fmt.Errorf(
      "\n\t... in ReadFile:\n\t opening %q: %w", in.Path, err,
    )
  }
  defer f.Close()

  info, err := f.Stat()
  if err != nil {
    return nil, fmt.Errorf("\n\t... in ReadFile:\n\t%w", err)
  }
  if info.IsDir() {
    return nil, fmt.Errorf(
      "\n\t... in ReadFile:\n\t %q is a directory, not a file — use " +
        "list_directory",
      in.Path,
    )
  }

  lines, err := readLines(f)
  if err != nil {
    return nil, fmt.Errorf(
      "\n\t... in ReadFile:\n\t reading %q: %w", in.Path, err,
    )
  }
  s_lines := len(lines)

  if in.StartLine == 0 && in.EndLine == 0 {
    if s_lines > maxWholeFileLines {
      return nil, fmt.Errorf(
        "\n\t... in ReadFile:\n\t %q has %d lines, above the "+
          "%d-line threshold — search first (search_code), then "+
          "re-read with start_line/end_line",
        in.Path, s_lines, maxWholeFileLines,
      )
    }
    return capRead(lines, 1, s_lines), nil
  }

  start, end := in.StartLine, in.EndLine
  if start < 1 {
    start = 1
  }
  if end == 0 || end > s_lines {
    end = s_lines
  }
  if start > s_lines {
    return nil, fmt.Errorf(
      "\n\t... in ReadFile:\n\t start_line %d is beyond end of file (%d lines)",
      start, s_lines,
    )
  }
  if start > end {
    return nil, fmt.Errorf(
      "\n\t... in ReadFile:\n\t start_line %d is after end_line %d",
      start, end,
    )
  }

  return capRead(lines[start - 1 : end], start, s_lines), nil
}

// capRead joins the selected lines, from line first on, stopping at
// maxWholeFileLines lines or maxReadBytes bytes, whichever comes
// first. A cut read is Truncated, with NextLine where to resume.
func capRead(selected []string, first, total int) *ReadFileOutput {
  out := &ReadFileOutput{ TotalLines: total }
  n, size := 0, 0
  for n < len(selected) && n < maxWholeFileLines {
    size += len(selected[n]) + 1
    if size > maxReadBytes && n > 0 {
      break
    }

    n++
  }

  out.Content = strings.Join(selected[:n], "\n")
  if n < len(selected) {
    out.Truncated, out.NextLine = true, first + n
  }

  return out
}

func readLines(f *os.File) ([]string, error) {
  var lines []string

  scanner := bufio.NewScanner(f)
  scanner.Buffer(make([]byte, 0, 64 * 1024), 1024 * 1024)
  for scanner.Scan() {
    lines = append(lines, scanner.Text())
  }

  return lines, scanner.Err()
}
