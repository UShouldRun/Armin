package tools

import (
  "context"
  "fmt"
  "os/exec"
  "strconv"
  "strings"

  "armin/internal/sandbox"
)

const (
  // pdfPagesPerCall is the default page window.
  pdfPagesPerCall = 10
  // maxPDFText caps one call's text; past it the model is told to
  // continue from the next page.
  maxPDFText = 24 << 10
)

// ReadPDF extracts the text of a page range with poppler's
// pdftotext, keeping the layout (slides and tables stay readable).
// Like search_code's ripgrep, it's a fixed, trusted binary this tool
// controls, not run_command's allowlist.
func ReadPDF(
  ctx context.Context, cfg *sandbox.Config, in ReadPDFInput,
) (*ReadPDFOutput, error) {
  _, err := exec.LookPath("pdftotext")
  if err != nil {
    return nil, fmt.Errorf(
      "\n\t... in ReadPDF:\n\t pdftotext isn't installed — the user can " +
        "install poppler-utils (e.g. sudo apt install poppler-utils)",
    )
  }

  resolved, err := cfg.ResolvePath(in.Path)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in ReadPDF:\n\t%w", err)
  }

  total := pdfPages(ctx, cfg, resolved)
  first := max(in.FirstPage, 1)
  last := in.LastPage
  if last < first {
    last = first + pdfPagesPerCall - 1
  }
  if total > 0 {
    if first > total {
      return nil, fmt.Errorf(
        "\n\t... in ReadPDF:\n\t first_page %d is past the last page (%d)",
        first, total,
      )
    }

    last = min(last, total)
  }

  res, err := cfg.RunForceFull(ctx, "pdftotext", []string{
    "-layout", "-f", strconv.Itoa(first), "-l", strconv.Itoa(last),
    resolved, "-",
  })
  if err != nil {
    return nil, fmt.Errorf("\n\t... in ReadPDF:\n\t%w", err)
  }

  if res.ExitCode != 0 {
    return nil, fmt.Errorf(
      "\n\t... in ReadPDF:\n\t pdftotext: %s", strings.TrimSpace(res.Stderr),
    )
  }

  out := &ReadPDFOutput{
    Text:       markPages(res.Stdout, first),
    FirstPage:  first,
    LastPage:   last,
    TotalPages: total,
  }

  if len(out.Text) > maxPDFText {
    cut := strings.LastIndex(out.Text[:maxPDFText], "\n--- page ")
    if cut <= 0 {
      cut = maxPDFText
    }

    reached := first + strings.Count(out.Text[:cut], "\n--- page ")
    out.Text, out.Truncated = out.Text[:cut], true
    out.LastPage = max(reached - 1, first)
    out.Next = fmt.Sprintf("call again with first_page=%d", out.LastPage + 1)
  } else if total > 0 && last < total {
    out.Next = fmt.Sprintf(
      "pages %d-%d of %d shown; call again with first_page=%d for more",
      first, last, total, last + 1,
    )
  }

  return out, nil
}

// pdfPages asks pdfinfo for the page count; 0 if unknown.
func pdfPages(ctx context.Context, cfg *sandbox.Config, path string) int {
  _, err := exec.LookPath("pdfinfo")
  if err != nil {
    return 0
  }

  res, err := cfg.RunForceFull(ctx, "pdfinfo", []string{ path })
  if err != nil || res.ExitCode != 0 {
    return 0
  }

  for _, line := range strings.Split(res.Stdout, "\n") {
    v, ok := strings.CutPrefix(line, "Pages:")
    if ok {
      n, _ := strconv.Atoi(strings.TrimSpace(v))
      return n
    }
  }

  return 0
}

// markPages turns pdftotext's form feeds into "--- page N ---"
// lines, so the model can cite and navigate pages.
func markPages(text string, first int) string {
  pages := strings.Split(strings.TrimRight(text, "\f\n"), "\f")
  var b strings.Builder
  for i, p := range pages {
    fmt.Fprintf(&b, "--- page %d ---\n%s\n", first + i, strings.TrimRight(p, "\n"))
  }

  return b.String()
}
