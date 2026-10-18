// Project facts: line counts, languages, finding files, file stat,
// TODO/FIXME markers, the environment (OS, toolchains) and the
// project manifest that orients a new session.

package tools

import (
  "bufio"
  "context"
  "fmt"
  "io"
  "io/fs"
  "os"
  "os/exec"
  "path/filepath"
  "runtime"
  "sort"
  "strings"
  "time"
  "unicode/utf8"

  "armin/internal/sandbox"
)

// --- count_lines / list_languages ---

func walkStats(cfg *sandbox.Config, path string) (*repoStats, error) {
  resolved, err := cfg.ResolvePath(path)
  if err != nil {
    return nil, err
  }

  stats := &repoStats{
    byTopDir: make(map[string]dirCount), byExt: make(map[string]dirCount),
  }

  err = filepath.WalkDir(resolved, func(p string, d fs.DirEntry, err error) error {
    if err != nil {
      return err
    }
    if d.IsDir() {
      if d.Name() != "." && skipDirs[d.Name()] {
        return filepath.SkipDir
      }
      return nil
    }
    if strings.HasPrefix(d.Name(), ".") {
      return nil
    }

    lines, ok := countFileLines(p)
    if !ok {
      return nil // binary or unreadable — skip silently, not fatal
    }

    stats.totalLines += lines
    stats.totalFiles++

    rel, relErr := filepath.Rel(resolved, p)
    if relErr != nil {
      rel = p
    }
    top := topDir(rel)
    tc := stats.byTopDir[top]
    tc.files++
    tc.lines += lines
    stats.byTopDir[top] = tc

    ext := strings.ToLower(filepath.Ext(p))
    if ext == "" {
      ext = "(none)"
    }
    ec := stats.byExt[ext]
    ec.files++
    ec.lines += lines
    stats.byExt[ext] = ec

    return nil
  })
  if err != nil {
    return nil, err
  }
  return stats, nil
}

// topDir returns rel's first path segment, or "." for a file
// directly at the walked root.
func topDir(rel string) string {
  parts := strings.SplitN(filepath.ToSlash(rel), "/", 2)
  if len(parts) == 1 {
    return "."
  }
  return parts[0]
}

// isBinary is the same cheap NUL-byte heuristic file(1) and git use
// — good enough to skip binary blobs without a real content-type
// sniff, and cheap enough to run on every file in a full-tree walk.
func isBinary(data []byte) bool {
  for _, b := range data {
    if b == 0 {
      return true
    }
  }
  return false
}

// countFileLines reports (0, false) for anything that looks binary
// or can't be opened, so callers can skip it silently rather than
// erroring out a whole-tree walk over one bad file.
func countFileLines(path string) (int, bool) {
  f, err := os.Open(path)
  if err != nil {
    return 0, false
  }
  defer f.Close()

  head := make([]byte, 512)
  n, _ := f.Read(head)
  if isBinary(head[:n]) {
    return 0, false
  }
  _, err = f.Seek(0, io.SeekStart)
  if err != nil {
    return 0, false
  }

  scanner := bufio.NewScanner(f)
  scanner.Buffer(make([]byte, 0, 64 * 1024), 1024 * 1024)
  lines := 0
  for scanner.Scan() {
    lines++
  }
  return lines, true
}

// CountLines walks Path and sums line counts across every
// non-binary file — a one-call answer to "how big is this
// codebase," where reaching the same total via search_code/
// list_directory/run_command would cost the model more tool-call
// rounds than maxToolRounds allows on anything but a tiny repo.
func CountLines(
  ctx context.Context, cfg *sandbox.Config, in CountLinesInput,
) (*CountLinesOutput, error) {
  stats, err := walkStats(cfg, in.Path)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in CountLines:\n\t%w", err)
  }

  out := &CountLinesOutput{
    TotalFiles: stats.totalFiles, TotalLines: stats.totalLines,
  }

  dirs := make([]string, 0, len(stats.byTopDir))
  for d := range stats.byTopDir {
    dirs = append(dirs, d)
  }
  sort.Strings(dirs)
  for _, d := range dirs {
    dc := stats.byTopDir[d]
    out.ByTopDir = append(
      out.ByTopDir, DirLineCount{ Dir: d, Files: dc.files, Lines: dc.lines },
    )
  }
  return out, nil
}

// ListLanguages breaks Path down by file extension — the same walk
// CountLines does, grouped differently.
func ListLanguages(
  ctx context.Context, cfg *sandbox.Config, in ListLanguagesInput,
) (*ListLanguagesOutput, error) {
  stats, err := walkStats(cfg, in.Path)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in ListLanguages:\n\t%w", err)
  }

  exts := make([]string, 0, len(stats.byExt))
  for e := range stats.byExt {
    exts = append(exts, e)
  }
  sort.Slice(exts, func(i, j int) bool {
    return stats.byExt[exts[i]].lines > stats.byExt[exts[j]].lines
  })

  out := &ListLanguagesOutput{}
  for _, e := range exts {
    c := stats.byExt[e]
    out.Languages = append(
      out.Languages, LanguageCount{ Extension: e, Files: c.files, Lines: c.lines },
    )
  }
  return out, nil
}

// --- find_files ---

const maxFindFilesResults = 200

// FindFiles matches Pattern against each file's base name under
// Path — search_code's name-only complement, for "which files are
// called X" instead of "which files contain X."
func FindFiles(
  ctx context.Context, cfg *sandbox.Config, in FindFilesInput,
) (*FindFilesOutput, error) {
  if in.Pattern == "" {
    return nil, fmt.Errorf("\n\t... in FindFiles:\n\t pattern is required")
  }
  resolved, err := cfg.ResolvePath(in.Path)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in FindFiles:\n\t%w", err)
  }

  out := &FindFilesOutput{}
  walkErr := filepath.WalkDir(resolved, func(p string, d fs.DirEntry, err error) error {
    if err != nil {
      return err
    }
    if d.IsDir() {
      if d.Name() != "." && skipDirs[d.Name()] {
        return filepath.SkipDir
      }
      return nil
    }

    matched, matchErr := filepath.Match(in.Pattern, d.Name())
    if matchErr != nil {
      return fmt.Errorf("bad pattern %q: %w", in.Pattern, matchErr)
    }
    if !matched {
      return nil
    }
    if len(out.Files) >= maxFindFilesResults {
      out.Truncated = true
      return filepath.SkipAll
    }

    rel, relErr := filepath.Rel(cfg.Root, p)
    if relErr != nil {
      rel = p
    }
    out.Files = append(out.Files, filepath.ToSlash(rel))
    return nil
  })
  if walkErr != nil {
    return nil, fmt.Errorf("\n\t... in FindFiles:\n\t%w", walkErr)
  }
  return out, nil
}

// --- file_stat ---

// FileStat reports one file's size, line count, and mtime without
// returning its content — cheaper than read_file for deciding what
// range is worth actually reading.
func FileStat(
  ctx context.Context, cfg *sandbox.Config, in FileStatInput,
) (*FileStatOutput, error) {
  resolved, err := cfg.ResolvePath(in.Path)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in FileStat:\n\t%w", err)
  }

  info, err := os.Stat(resolved)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in FileStat:\n\t%w", err)
  }
  if info.IsDir() {
    return nil, fmt.Errorf(
      "\n\t... in FileStat:\n\t %q is a directory, not a file — use list_directory",
      in.Path,
    )
  }

  out := &FileStatOutput{
    SizeBytes: info.Size(), ModTime: info.ModTime().Format(time.RFC3339),
  }
  lines, ok := countFileLines(resolved)
  if !ok {
    out.IsBinary = true
  } else {
    out.Lines = lines
  }
  return out, nil
}

// --- find_todo_fixme ---

// FindTodoFixme is a fixed search_code preset for TODO/FIXME/XXX
// markers — common enough to deserve one call instead of the model
// re-writing the same regex every time.
func FindTodoFixme(
  ctx context.Context, cfg *sandbox.Config, in FindTodoFixmeInput,
) (*FindTodoFixmeOutput, error) {
  res, err := SearchCode(ctx, cfg, SearchCodeInput{
    Pattern: `\b(TODO|FIXME|XXX)\b`, Path: in.Path,
  })
  if err != nil {
    return nil, fmt.Errorf("\n\t... in FindTodoFixme:\n\t%w", err)
  }
  return &FindTodoFixmeOutput{ Matches: res.Matches, Truncated: res.Truncated }, nil
}

// --- env_info ---

// EnvInfo reports the toolchain armin can actually see — Go
// version, OS/arch, and whether git/ripgrep/docker are on PATH —
// without leaking PATH's contents itself.
func EnvInfo(
  ctx context.Context, cfg *sandbox.Config, in EnvInfoInput,
) (*EnvInfoOutput, error) {
  out := &EnvInfoOutput{ OS: runtime.GOOS, Arch: runtime.GOARCH }

  res, err := cfg.Exec(ctx, sandbox.Command{ Binary: "go", Args: []string{ "version" }, Mode: sandbox.ExecForce })
  if err == nil && res.ExitCode == 0 {
    out.GoVersion = strings.TrimSpace(res.Stdout)
  }
  out.HasGit = onPath("git")
  out.HasRg = onPath("rg")
  out.HasDocker = onPath("docker")
  return out, nil
}

func onPath(bin string) bool {
  _, err := exec.LookPath(bin)
  return err == nil
}

// --- project_manifest ---

// manifestFiles is checked in order for a first-pass project
// manifest/readme; the first one found is included, the rest
// skipped — one is plenty of orientation.
var manifestFiles = []string{
  "README.md", "readme.md", "go.mod",
  "package.json", "Cargo.toml", "pyproject.toml",
}

// ProjectManifest builds the directory-tree-plus-manifest note the
// REPL loads once automatically at session start (see
// repl.REPL.LoadAutoContext, which calls this same function) —
// exposed here as a re-invocable tool so a mid-conversation question
// or the LSP ask flow (which never runs the REPL's auto-context at
// all) can ask for it again instead of reconstructing it by hand
// from list_directory + read_file.
func ProjectManifest(
  ctx context.Context, cfg *sandbox.Config, in ProjectManifestInput,
) (*ProjectManifestOutput, error) {
  var b strings.Builder

  listing, err := ListDirectory(ctx, cfg, ListDirectoryInput{ Depth: 2 })
  if err == nil {
    b.WriteString("Project structure:\n")
    b.WriteString(listing.Tree)
  }

  for _, name := range manifestFiles {
    out, err := ReadFile(ctx, cfg, ReadFileInput{ Path: name, EndLine: 60 })
    if err != nil {
      continue
    }
    fmt.Fprintf(
      &b, "\n\n%s (first %d lines):\n%s", name, out.TotalLines, out.Content,
    )
    break
  }

  return &ProjectManifestOutput{ Note: b.String() }, nil
}

// IsText reports whether the file at path reads as text: no NUL in
// its first 8 KB, and valid UTF-8 there. For deciding what's safe to
// print in a terminal.
func IsText(path string) bool {
  f, err := os.Open(path)
  if err != nil {
    return false
  }

  defer f.Close()

  buf := make([]byte, 8192)
  n, _ := io.ReadFull(f, buf)
  head := buf[:n]

  // A cut at the end of the buffer can split one UTF-8 rune.
  for i := 0; i < 3 && len(head) > 0 && !utf8.Valid(head); i++ {
    head = head[:len(head) - 1]
  }

  return !isBinary(buf[:n]) && utf8.Valid(head)
}
