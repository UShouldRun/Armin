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
    if list, ok := args["args"].([]any); ok {
      for _, a := range list {
        parts = append(parts, shellWord(fmt.Sprint(a)))
      }
    }

    return name + "  " + strings.Join(parts, " ")

  case toolReadFile:
    loc := str("path")
    if s, e := num("start_line"), num("end_line"); s > 0 || e > 0 {
      loc += fmt.Sprintf(":%d-%d", max(s, 1), e)
    }

    return name + "  " + loc

  case toolReadPDF:
    line := name + "  " + str("path")
    if f, l := num("first_page"), num("last_page"); f > 0 || l > 0 {
      line += fmt.Sprintf(" · pages %d-%d", max(f, 1), l)
    }

    return line

  case toolSearchCode:
    line := name + "  " + quote(str("pattern"))
    if p := str("path"); p != "" {
      line += " in " + p
    }

    if t := str("file_type"); t != "" {
      line += " (" + t + ")"
    }

    return line

  case toolWebSearch:
    line := name + "  " + quote(str("query"))
    if t := str("topic"); t != "" && t != "general" {
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
    if all, _ := args["replace_all"].(bool); all {
      line += " everywhere"
    }

    return line

  case toolScratchpadWrite:
    verb := "write"
    if a, _ := args["append"].(bool); a {
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
    if s, ok := v.(string); ok {
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
  if err := json.Unmarshal([]byte(result), &r); err != nil {
    return sizeOf(result)
  }

  if msg, ok := r["error"].(string); ok && msg != "" {
    return "error: " + lastErrorLine(msg)
  }

  if ref, ok := r["archived"].(string); ok {
    return "archived: " + ref
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
    if total := num("total_lines"); total > shown {
      return fmt.Sprintf("%d of %d lines", shown, total)
    }

    return plural(shown, "line")

  case toolReadPDF:
    line := fmt.Sprintf("pages %d-%d", num("first_page"), num("last_page"))
    if total := num("total_pages"); total > 0 {
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

  case toolRunTests, toolBuildCheck, toolLintCheck:
    if avail, ok := r["available"].(bool); ok && !avail {
      return "unavailable: " + str("hint")
    }

    verdict := "passed"
    if passed, _ := r["passed"].(bool); !passed {
      verdict = "failed"
    }

    line := str("command") + " · " + verdict
    if verdict == "failed" {
      if first := firstLine(str("summary")); first != "" {
        line += " · " + first
      }
    }

    return line

  case toolWriteFile, toolEditFile:
    if dry, _ := r["dry_run"].(bool); dry {
      return "dry run, nothing written"
    }

    if created, _ := r["created"].(bool); created {
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

func firstLine(s string) string {
  s = strings.TrimSpace(s)
  if i := strings.IndexByte(s, '\n'); i >= 0 {
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
