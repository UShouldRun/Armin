// armin -uninstall <name>: the mirror of -install. It removes a
// model's models.<name> lines from config.conf (and the comment
// -install wrote above them) and deletes its .gguf, every part of a
// split one and any half-finished .part, unless another model still
// uses the file. A hosted or cloud model loses only its lines.

package install

import (
  "bufio"
  "errors"
  "fmt"
  "os"
  "path/filepath"
  "sort"
  "strings"

  "armin/internal/config"
  "armin/internal/ui"
)

// Uninstall is armin -uninstall: req.Model names the model, or is
// "list".
func Uninstall(req Request) error {
  if req.Model == "list" {
    printInstalled(req)
    return nil
  }

  name := req.Model
  e, ok := req.Models[name]
  if !ok {
    return fmt.Errorf(
      "\n\t... in Uninstall:\n\t no models.%s in config.conf (armin -uninstall list shows them)",
      name,
    )
  }

  for _, use := range []struct{ key, value string }{
    { "model", req.Default }, { "completion_model", req.Completion },
  } {
    if use.value == name {
      return fmt.Errorf(
        "\n\t... in Uninstall:\n\t %s is config.conf's %s = — point %s at "+
          "another model first, or armin would start with none",
        name, use.key, use.key,
      )
    }
  }

  path, err := config.Path()
  if err != nil {
    return fmt.Errorf("\n\t... in Uninstall:\n\t%w", err)
  }

  data, err := os.ReadFile(path)
  if err != nil {
    return fmt.Errorf("\n\t... in Uninstall:\n\t%w", err)
  }

  kept, removed := withoutModel(string(data), name)
  files, shared := modelFiles(name, e, req.Models)

  fmt.Printf("%s %s\n", ui.Bold("uninstall"), name)
  fmt.Printf("  from %s:\n", path)
  for _, l := range removed {
    fmt.Println(ui.Dim("    " + l))
  }

  var total int64
  for _, f := range files {
    total += f.size
    fmt.Printf("  delete %s (%s)\n", f.path, gb(f.size))
  }

  for _, s := range shared {
    fmt.Printf("  keep %s — %s\n", s, ui.Dim("another model uses it"))
  }

  if e.ModelPath != "" && (e.Remote || e.External) {
    fmt.Println(ui.Dim("  its weights live on another machine: left there"))
  }

  in := bufio.NewReader(os.Stdin)
  if !asker(in, req.Yes)("remove it?", false) {
    return nil
  }

  err = writeAtomic(path, kept)
  if err != nil {
    return fmt.Errorf("\n\t... in Uninstall:\n\t%w", err)
  }

  var failed []string
  for _, f := range files {
    err := os.Remove(f.path)
    if err != nil && !errors.Is(err, os.ErrNotExist) {
      failed = append(failed, err.Error())
    }
  }

  fmt.Printf("removed models.%s; %s freed\n", name, gb(total))
  if len(failed) > 0 {
    return fmt.Errorf("\n\t... in Uninstall:\n\t%s", strings.Join(failed, "\n\t"))
  }

  return nil
}

// withoutModel is config text with every models.<name>.* line taken
// out, the "added by armin -install" comment just above them too,
// and the blank line that leaves doubled. removed is what went.
func withoutModel(text, name string) (kept string, removed []string) {
  prefix := "models." + name + "."
  lines := strings.Split(text, "\n")
  drop := make([]bool, len(lines))
  for i, l := range lines {
    key, _, _ := strings.Cut(strings.TrimSpace(l), "=")
    if !strings.HasPrefix(strings.TrimSpace(key), prefix) {
      continue
    }

    drop[i] = true
    j := i - 1
    for j >= 0 && drop[j] {
      j--
    }

    if j >= 0 && strings.HasPrefix(strings.TrimSpace(lines[j]), "#") &&
      strings.Contains(lines[j], "added by armin -install") {
      drop[j] = true
    }
  }

  var out []string
  for i, l := range lines {
    if drop[i] {
      removed = append(removed, l)
      continue
    }

    blank := strings.TrimSpace(l) == ""
    if blank && len(out) > 0 && strings.TrimSpace(out[len(out) - 1]) == "" && i > 0 && drop[i - 1] {
      continue
    }

    out = append(out, l)
  }

  return strings.Join(out, "\n"), removed
}

// modelFiles is what deleting name frees: its .gguf, the other parts
// of a split one, and leftover .part downloads. A file another model
// also points at is returned in shared instead.
func modelFiles(name string, e Existing, models map[string]Existing) (files []sizedFile, shared []string) {
  if e.ModelPath == "" || e.Remote || e.External {
    return nil, nil
  }

  path := expandHome(e.ModelPath)
  paths := []string{ path }
  m := splitSuffix.FindStringSubmatchIndex(path)
  if m != nil {
    // name-00001-of-00003.gguf: every part shares what comes before.
    found, _ := filepath.Glob(path[:m[0]] + "-?????-of-" + path[m[2]:m[3]] + ".gguf")
    paths = found
  }

  used := map[string]bool{}
  for other, o := range models {
    if other != name && o.ModelPath != "" && !o.Remote && !o.External {
      used[expandHome(o.ModelPath)] = true
    }
  }

  // A split model's other parts are in use when its first part is.
  for _, p := range paths {
    if used[p] || used[path] {
      shared = append(shared, p)
      continue
    }

    for _, f := range []string{ p, p + ".part" } {
      info, err := os.Stat(f)
      if err == nil {
        files = append(files, sizedFile{ f, info.Size() })
      }
    }
  }

  return files, shared
}

func expandHome(p string) string {
  if p != "~" && !strings.HasPrefix(p, "~/") {
    return p
  }

  home, err := os.UserHomeDir()
  if err != nil {
    return p
  }

  return filepath.Join(home, p[1:])
}

// writeAtomic replaces path with text, keeping its permissions, so a
// crash never leaves config.conf half-written.
func writeAtomic(path, text string) error {
  mode := os.FileMode(0o644)
  info, err := os.Stat(path)
  if err == nil {
    mode = info.Mode().Perm()
  }

  tmp := path + ".tmp"
  err = os.WriteFile(tmp, []byte(text), mode)
  if err != nil {
    return err
  }

  return os.Rename(tmp, path)
}

// printInstalled is armin -uninstall list: every configured model,
// where it runs and what its file takes on disk.
func printInstalled(req Request) {
  names := make([]string, 0, len(req.Models))
  width := 0
  for n := range req.Models {
    names = append(names, n)
    width = max(width, len(n))
  }

  sort.Strings(names)
  fmt.Println(ui.Bold("models in config.conf"))
  var total int64
  for _, n := range names {
    e := req.Models[n]
    where := ""
    switch {
    case e.External:
      where = "hosted / external server"
    case e.Remote:
      where = "cloud machine: " + filepath.Base(e.ModelPath)
    default:
      files, _ := modelFiles(n, e, nil)
      var size int64
      for _, f := range files {
        size += f.size
      }

      total += size
      where = fmt.Sprintf("%8s  %s", gb(size), e.ModelPath)
      if len(files) == 0 {
        where = fmt.Sprintf("%8s  %s", "missing", e.ModelPath)
      }
    }

    mark := ""
    switch n {
    case req.Default:
      mark = ui.Cyan("  (model)")
    case req.Completion:
      mark = ui.Cyan("  (completion_model)")
    }

    fmt.Printf("  %-*s  %s%s\n", width, n, where, mark)
  }

  fmt.Printf("%s on disk; armin -uninstall <name> removes one\n", gb(total))
}
