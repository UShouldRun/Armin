// Writing the installed model into config.conf: a models.<name>
// block appended at the end, with the plan's flags, so it's there to
// read and edit; and ram = …, once the user has said what RAM it is.

package install

import (
  "fmt"
  "os"
  "path/filepath"
  "sort"
  "strings"
  "time"

  "armin/internal/config"
)

// modelName turns a file name into a config name: lower case, no
// quantization, no dots (config keys split on them).
func modelName(file, quant string, taken map[string]Existing) string {
  base := strings.TrimSuffix(splitSuffix.ReplaceAllString(filepath.Base(file), ".gguf"), ".gguf")
  if quant != "" {
    i := strings.LastIndex(strings.ToUpper(base), strings.ToUpper(quant))
    if i > 0 {
      base = base[:i]
    }
  }

  base = strings.ToLower(strings.Trim(base, "-_. "))
  base = strings.NewReplacer(".", "", " ", "-", "_", "-").Replace(base)
  base = strings.TrimSuffix(base, "-gguf")
  if base == "" {
    base = "model"
  }

  name := base
  for i := 2; ; i++ {
    _, used := taken[name]
    if !used {
      return name
    }

    name = fmt.Sprintf("%s-%d", base, i)
  }
}

// validName: a config name can't hold dots, spaces or "=".
func validName(s string) bool {
  return s != "" && !strings.ContainsAny(s, ". =\t#")
}

// nextPort is one past the highest port a managed model uses.
func nextPort(models map[string]Existing) int {
  port := 8080
  for _, m := range models {
    if m.Port > port {
      port = m.Port
    }
  }

  return port + 1
}

// usualBinary is the llama-server most models already use, or plain
// llama-server for PATH to find.
func usualBinary(models map[string]Existing) string {
  count := map[string]int{}
  for _, m := range models {
    if m.Binary != "" && !m.Remote {
      count[m.Binary]++
    }
  }

  best := "llama-server"
  n := 0
  for b, c := range count {
    if c > n || c == n && b < best {
      best, n = b, c
    }
  }

  return best
}

// usualDir is the folder most models already live in, or ~/models.
func usualDir(models map[string]Existing) string {
  count := map[string]int{}
  for _, m := range models {
    if m.ModelPath != "" && !m.Remote {
      count[filepath.Dir(m.ModelPath)]++
    }
  }

  dirs := make([]string, 0, len(count))
  for d := range count {
    dirs = append(dirs, d)
  }

  sort.Slice(dirs, func(i, j int) bool {
    if count[dirs[i]] != count[dirs[j]] {
      return count[dirs[i]] > count[dirs[j]]
    }

    return dirs[i] < dirs[j]
  })

  if len(dirs) > 0 {
    return dirs[0]
  }

  home, err := os.UserHomeDir()
  if err != nil {
    return "models"
  }

  return filepath.Join(home, "models")
}

// block is the config text for one model.
func block(name, binary, modelPath, origin string, port int, p *Plan) string {
  pad := len("models." + name + ".model_path")
  line := func(field, val string) string {
    key := "models." + name + "." + field
    return key + strings.Repeat(" ", pad - len(key)) + " = " + val + "\n"
  }

  var b strings.Builder
  fmt.Fprintf(&b, "\n# %s, added by armin -install on %s\n", origin, time.Now().Format("2006-01-02"))
  b.WriteString(line("binary", binary))
  b.WriteString(line("model_path", modelPath))
  b.WriteString(line("port", fmt.Sprint(port)))
  b.WriteString(line("args", strings.Join(p.Args, " ")))

  return b.String()
}

// appendConfig adds text to the end of config.conf, creating it (and
// its folder) if needed.
func appendConfig(text string) (string, error) {
  path, err := config.Path()
  if err != nil {
    return "", fmt.Errorf("\n\t... in appendConfig:\n\t%w", err)
  }

  err = os.MkdirAll(filepath.Dir(path), 0o755)
  if err != nil {
    return "", fmt.Errorf("\n\t... in appendConfig:\n\t%w", err)
  }

  f, err := os.OpenFile(path, os.O_APPEND | os.O_CREATE | os.O_WRONLY, 0o644)
  if err != nil {
    return "", fmt.Errorf("\n\t... in appendConfig:\n\t%w", err)
  }

  _, err = f.WriteString(text)
  if err != nil {
    f.Close()
    return "", fmt.Errorf("\n\t... in appendConfig:\n\t%w", err)
  }

  return path, f.Close()
}
