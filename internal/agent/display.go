package agent

import (
  "encoding/json"
  "fmt"
  "net/url"
  "sort"
  "strings"
)

// CallLine renders a tool call the way a person reads it: the tool,
// then its arguments in full — the command line for run_command, the
// file and range for read_file, the query for web_search — so every
// front end can show exactly what the model is doing.
func CallLine(name string, args map[string]any) string {
  str := func(k string) string {
    v, _ := args[k].(string)
    return oneLine(v)
  }

  num := func(k string) int {
    f, _ := args[k].(float64)
    return int(f)
  }

  switch toolIDs[name] {
  case toolRunCommand:
    parts := []string{ str("binary") }
    list, ok := args["args"].([]any)
    if ok {
      for _, a := range list {
        parts = append(parts, shellWord(fmt.Sprint(a)))
      }
    }

    return name + "  " + strings.Join(parts, " ")

  case toolRunProgram:
    parts := []string{ str("binary") }
    list, ok := args["args"].([]any)
    if ok {
      for _, a := range list {
        parts = append(parts, shellWord(fmt.Sprint(a)))
      }
    }

    line := name + "  " + strings.Join(parts, " ")
    d := str("dir")
    if d != "" && d != "." {
      line += "  (in " + d + ")"
    }

    return line

  case toolReadFile:
    loc := str("path")
    s, e := num("start_line"), num("end_line")
    if s > 0 || e > 0 {
      loc += fmt.Sprintf(":%d-%d", max(s, 1), e)
    }

    return name + "  " + loc

  case toolReadPDF:
    line := name + "  " + str("path")
    f, l := num("first_page"), num("last_page")
    if f > 0 || l > 0 {
      line += fmt.Sprintf(" · pages %d-%d", max(f, 1), l)
    }

    return line

  case toolSearchCode:
    line := name + "  " + quote(str("pattern"))
    p := str("path")
    if p != "" {
      line += " in " + p
    }

    t := str("file_type")
    if t != "" {
      line += " (" + t + ")"
    }

    return line

  case toolWebSearch:
    line := name + "  " + quote(str("query"))
    t := str("topic")
    if t != "" && t != "general" {
      line += " · " + t
    }

    return line

  case toolWriteFile:
    return fmt.Sprintf("%s  %s (%d lines)", name, str("path"), lines(args["content"]))

  case toolEditFile:
    line := fmt.Sprintf(
      "%s  %s (−%d +%d lines)",
      name, str("path"), lines(args["old_string"]), lines(args["new_string"]),
    )
    all, _ := args["replace_all"].(bool)
    if all {
      line += " everywhere"
    }

    return line

  case toolRequestAccess:
    return name + "  " + str("dir") + " — " + str("reason")

  case toolScratchpadWrite:
    verb := "write"
    a, _ := args["append"].(bool)
    if a {
      verb = "append"
    }

    return fmt.Sprintf("%s  %s %s (%d lines)", name, verb, str("name"), lines(args["content"]))
  }

  // Everything else: name(key=value, ...), sorted, values whole.
  keys := make([]string, 0, len(args))
  for k := range args {
    keys = append(keys, k)
  }

  sort.Strings(keys)

  parts := make([]string, 0, len(keys))
  for _, k := range keys {
    v := args[k]
    s, ok := v.(string)
    if ok {
      v = oneLine(s)
    }

    parts = append(parts, fmt.Sprintf("%s=%v", k, v))
  }

  return strings.TrimSpace(name + "  " + strings.Join(parts, " "))
}

// ResultLine summarizes a tool result (its JSON) in one line: what
// came back, not the whole of it.
func ResultLine(name, result string) string {
  var r map[string]any
  err := json.Unmarshal([]byte(result), &r)
  if err != nil {
    return sizeOf(result)
  }

  msg, ok := r["error"].(string)
  if ok && msg != "" {
    return "error: " + lastErrorLine(msg)
  }

  ref, ok := r["archived"].(string)
  if ok {
    return "archived: " + ref
  }

  dir, ok := r["granted"].(string)
  if ok {
    return "read access granted: " + dir
  }

  num := func(k string) int {
    f, _ := r[k].(float64)
    return int(f)
  }

  str := func(k string) string {
    s, _ := r[k].(string)
    return s
  }

  switch toolIDs[name] {
  case toolWebSearch:
    results, _ := r["results"].([]any)
    if len(results) == 0 {
      return "no results"
    }

    return fmt.Sprintf("%d results — %s", len(results), domains(results, 3))

  case toolReadFile:
    shown := strings.Count(str("content"), "\n") + 1
    total := num("total_lines")
    if total > shown {
      return fmt.Sprintf("%d of %d lines", shown, total)
    }

    return plural(shown, "line")

  case toolReadPDF:
    line := fmt.Sprintf("pages %d-%d", num("first_page"), num("last_page"))
    total := num("total_pages")
    if total > 0 {
      line += fmt.Sprintf(" of %d", total)
    }

    return line + " · " + sizeOf(str("text"))

  case toolSearchCode:
    out := strings.TrimSpace(str("matches"))
    if out == "" {
      return "no matches"
    }

    return plural(strings.Count(out, "\n") + 1, "line") + " of matches"

  case toolRunCommand:
    out := strings.TrimSpace(str("stdout") + str("stderr"))
    line := fmt.Sprintf("exit %d", num("exit_code"))
    if out != "" {
      line += " · " + plural(strings.Count(out, "\n") + 1, "line") + " of output"
    }

    return line

  case toolRunProgram:
    out := strings.TrimSpace(str("output"))
    line := fmt.Sprintf("exit %d", num("exit_code"))
    if out != "" {
      line += " · " + plural(strings.Count(out, "\n") + 1, "line") + " of output"
    }

    if num("exit_code") != 0 {
      last := lastLine(out)
      if last != "" {
        line += " · " + last
      }
    }

    return line

  case toolRunTests, toolBuildCheck, toolLintCheck:
    avail, ok := r["available"].(bool)
    if ok && !avail {
      return "unavailable: " + str("hint")
    }

    verdict := "passed"
    passed, _ := r["passed"].(bool)
    if !passed {
      verdict = "failed"
    }

    line := str("command") + " · " + verdict
    if verdict == "failed" {
      first := firstLine(str("summary"))
      if first != "" {
        line += " · " + first
      }
    }

    return line

  case toolWriteFile, toolEditFile:
    dry, _ := r["dry_run"].(bool)
    if dry {
      return "dry run, nothing written"
    }

    created, _ := r["created"].(bool)
    if created {
      return fmt.Sprintf("created, %d lines", num("lines_added"))
    }

    return fmt.Sprintf("+%d −%d lines", num("lines_added"), num("lines_removed"))
  }

  return sizeOf(result)
}

// plural is "1 line", "2 lines".
func plural(n int, word string) string {
  if n == 1 {
    return "1 " + word
  }

  return fmt.Sprintf("%d %ss", n, word)
}

func oneLine(s string) string {
  return strings.ReplaceAll(strings.TrimSpace(s), "\n", "⏎")
}

func quote(s string) string {
  return `"` + s + `"`
}

// shellWord quotes an argument the way a shell would need it.
func shellWord(s string) string {
  if s == "" || strings.ContainsAny(s, " \t\"'$`\\|&;<>()*?") {
    return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
  }

  return s
}

func lines(v any) int {
  s, _ := v.(string)
  if s == "" {
    return 0
  }

  return strings.Count(strings.TrimSuffix(s, "\n"), "\n") + 1
}

func lastLine(s string) string {
  s = strings.TrimSpace(s)
  i := strings.LastIndexByte(s, '\n')
  if i >= 0 {
    return strings.TrimSpace(s[i + 1:])
  }

  return s
}

func firstLine(s string) string {
  s = strings.TrimSpace(s)
  i := strings.IndexByte(s, '\n')
  if i >= 0 {
    return s[:i]
  }

  return s
}

// lastErrorLine is the useful part of armin's wrapped error chains:
// the innermost message, at the end.
func lastErrorLine(msg string) string {
  parts := strings.Split(strings.TrimSpace(msg), "\n")
  for i := len(parts) - 1; i >= 0; i-- {
    p := strings.TrimSpace(parts[i])
    if p != "" && !strings.HasPrefix(p, "... in ") {
      return p
    }
  }

  return strings.TrimSpace(msg)
}

func domains(results []any, n int) string {
  var out []string
  seen := map[string]bool{}
  for _, item := range results {
    m, _ := item.(map[string]any)
    raw, _ := m["url"].(string)
    u, err := url.Parse(raw)
    if err != nil || u.Host == "" {
      continue
    }

    host := strings.TrimPrefix(u.Host, "www.")
    if !seen[host] {
      seen[host] = true
      out = append(out, host)
    }
    if len(out) == n {
      break
    }
  }

  return strings.Join(out, ", ")
}

func sizeOf(s string) string {
  if len(s) < 1024 {
    return fmt.Sprintf("%d bytes", len(s))
  }

  return fmt.Sprintf("%.1f KB", float64(len(s)) / 1024)
}
