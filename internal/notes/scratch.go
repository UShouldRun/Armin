package notes

import (
  "fmt"
  "os"
  "path/filepath"
  "sort"
  "strings"
  "time"
)

// maxScratchBytes caps one scratch file, so an append loop can't
// grow it without bound.
const maxScratchBytes = 256 * 1024

func (p *Scratchpad) path(name string) (string, string, error) {
  safe, err := SafeName(name)
  if err != nil {
    return "", "", err
  }

  return safe, filepath.Join(p.Dir, safe + ".md"), nil
}

// Write replaces a scratch file, or appends to it.
func (p *Scratchpad) Write(
  name, content string, appendTo bool,
) (ScratchFile, error) {
  safe, path, err := p.path(name)
  if err != nil {
    return ScratchFile{}, fmt.Errorf("\n\t... in Write:\n\t%w", err)
  }

  data := content
  if appendTo {
    old, err := os.ReadFile(path)
    if err != nil && !os.IsNotExist(err) {
      return ScratchFile{}, fmt.Errorf("\n\t... in Write:\n\t%w", err)
    }

    if len(old) > 0 && !strings.HasSuffix(string(old), "\n") {
      old = append(old, '\n')
    }

    data = string(old) + content
  }

  if len(data) > maxScratchBytes {
    return ScratchFile{}, fmt.Errorf(
      "\n\t... in Write:\n\t %q would be %d bytes, over the %d cap — "+
        "rewrite it shorter or split it",
      safe, len(data), maxScratchBytes,
    )
  }

  if !strings.HasSuffix(data, "\n") {
    data += "\n"
  }

  err = writeAtomic(path, []byte(data))
  if err != nil {
    return ScratchFile{}, fmt.Errorf("\n\t... in Write:\n\t%w", err)
  }

  return ScratchFile{
    Name:     safe,
    Bytes:    int64(len(data)),
    Lines:    strings.Count(data, "\n"),
    Modified: time.Now(),
  }, nil
}

// Read returns a line range of a scratch file; zero bounds mean the
// whole file.
func (p *Scratchpad) Read(
  name string, start, end int,
) (string, int, error) {
  _, path, err := p.path(name)
  if err != nil {
    return "", 0, fmt.Errorf("\n\t... in Read:\n\t%w", err)
  }

  raw, err := os.ReadFile(path)
  if err != nil {
    return "", 0, fmt.Errorf("\n\t... in Read:\n\t%w", err)
  }

  lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
  total := len(lines)
  if start == 0 && end == 0 {
    return string(raw), total, nil
  }

  if start < 1 {
    start = 1
  }

  if end == 0 || end > total {
    end = total
  }

  if start > end {
    return "", total, fmt.Errorf(
      "\n\t... in Read:\n\t start_line %d is past end_line %d",
      start, end,
    )
  }

  return strings.Join(lines[start - 1:end], "\n"), total, nil
}

// List returns every scratch file, most recently modified first.
func (p *Scratchpad) List() ([]ScratchFile, error) {
  entries, err := os.ReadDir(p.Dir)
  if os.IsNotExist(err) {
    return nil, nil
  }

  if err != nil {
    return nil, fmt.Errorf("\n\t... in List:\n\t%w", err)
  }

  var out []ScratchFile
  for _, e := range entries {
    if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
      continue
    }

    info, err := e.Info()
    if err != nil {
      continue
    }

    raw, _ := os.ReadFile(filepath.Join(p.Dir, e.Name()))
    out = append(out, ScratchFile{
      Name:     strings.TrimSuffix(e.Name(), ".md"),
      Bytes:    info.Size(),
      Lines:    strings.Count(string(raw), "\n"),
      Modified: info.ModTime(),
    })
  }

  sort.Slice(out, func(i, j int) bool {
    return out[i].Modified.After(out[j].Modified)
  })

  return out, nil
}

// archivePrefix names the files Archive rotates through.
const archivePrefix = "archive-"

// Archive appends content to the newest archive file (starting a new
// one when it would pass maxScratchBytes) and returns where it went,
// so a trimmed tool result can be pointed at instead of re-run.
func (p *Scratchpad) Archive(
  label, content string,
) (name string, start, end int, err error) {
  entry := "## " + label + "\n" + strings.TrimRight(content, "\n") + "\n\n"
  if len(entry) > maxScratchBytes {
    entry = entry[:maxScratchBytes - 64] + "\n[... cut to fit the archive]\n\n"
  }

  for n := 1; ; n++ {
    name = fmt.Sprintf("%s%d", archivePrefix, n)
    path := filepath.Join(p.Dir, name + ".md")
    old, rerr := os.ReadFile(path)
    if rerr != nil && !os.IsNotExist(rerr) {
      return "", 0, 0, fmt.Errorf("\n\t... in Archive:\n\t%w", rerr)
    }

    if len(old) + len(entry) > maxScratchBytes {
      continue
    }

    start = strings.Count(string(old), "\n") + 1
    err := writeAtomic(path, append(old, entry...))
    if err != nil {
      return "", 0, 0, fmt.Errorf("\n\t... in Archive:\n\t%w", err)
    }

    // The entry ends with a blank separator line; point past it.
    end = start + strings.Count(entry, "\n") - 2

    return name, start, end, nil
  }
}

// Clear deletes every scratch file.
func (p *Scratchpad) Clear() (int, error) {
  list, err := p.List()
  if err != nil {
    return 0, fmt.Errorf("\n\t... in Clear:\n\t%w", err)
  }

  for _, f := range list {
    os.Remove(filepath.Join(p.Dir, f.Name + ".md"))
  }

  return len(list), nil
}

// Summary is a one-line-per-file pointer list, for compaction notes:
// what exists and where, not the content.
func (p *Scratchpad) Summary() string {
  list, err := p.List()
  if err != nil || len(list) == 0 {
    return ""
  }

  var b strings.Builder
  for _, f := range list {
    fmt.Fprintf(&b, "- %s (%d lines)\n", f.Name, f.Lines)
  }

  return b.String()
}
