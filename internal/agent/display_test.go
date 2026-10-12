package agent

import "testing"

func TestCallLine(t *testing.T) {
  cases := []struct {
    name string
    args map[string]any
    want string
  }{
    { "run_command", map[string]any{ "binary": "git", "args": []any{ "log", "--oneline", "-5" } }, "run_command  git log --oneline -5" },
    { "run_command", map[string]any{ "binary": "grep", "args": []any{ "a b", "x.go" } }, "run_command  grep 'a b' x.go" },
    { "read_file", map[string]any{ "path": "stats.go", "start_line": 1.0, "end_line": 40.0 }, "read_file  stats.go:1-40" },
    { "read_file", map[string]any{ "path": "stats.go" }, "read_file  stats.go" },
    { "search_code", map[string]any{ "pattern": "func Mean", "path": "internal", "file_type": "go" }, `search_code  "func Mean" in internal (go)` },
    { "web_search", map[string]any{ "query": "MPI_Cart_create docs", "topic": "code" }, `web_search  "MPI_Cart_create docs" · code` },
    { "edit_file", map[string]any{ "path": "s.go", "old_string": "a\nb", "new_string": "c" }, "edit_file  s.go (−2 +1 lines)" },
    { "write_file", map[string]any{ "path": "n.go", "content": "x\ny\nz\n" }, "write_file  n.go (3 lines)" },
    { "git_log", map[string]any{ "path": "main.go", "limit": 5.0 }, "git_log  limit=5 path=main.go" },
    { "run_tests", map[string]any{}, "run_tests" },
  }
  for _, c := range cases {
    got := CallLine(c.name, c.args)
    if got != c.want {
      t.Errorf("CallLine(%s)\n got  %q\n want %q", c.name, got, c.want)
    }
  }
}

func TestResultLine(t *testing.T) {
  cases := []struct {
    name, result, want string
  }{
    { "web_search", `{"results":[{"url":"https://www.mpich.org/a"},{"url":"https://docs.open-mpi.org/b"},{"url":"https://mpich.org/c"}]}`, "3 results — mpich.org, docs.open-mpi.org" },
    { "web_search", `{"results":[]}`, "no results" },
    { "read_file", `{"content":"a\nb\nc","total_lines":120}`, "3 of 120 lines" },
    { "search_code", `{"matches":"x.go:1:a\nx.go:9:b"}`, "2 lines of matches" },
    { "run_command", `{"stdout":"one\ntwo\n","exit_code":0}`, "exit 0 · 2 lines of output" },
    { "run_command", `{"stdout":"one\n","exit_code":0}`, "exit 0 · 1 line of output" },
    { "run_tests", `{"command":"go test ./...","passed":false,"available":true,"summary":"FAIL demo\nok other"}`, "go test ./... · failed · FAIL demo" },
    { "edit_file", `{"path":"s.go","lines_added":1,"lines_removed":1}`, "+1 −1 lines" },
    { "write_file", `{"path":"n.go","created":true,"lines_added":3}`, "created, 3 lines" },
    { "read_file", `{"error":"\n\t... in ReadFile:\n\t opening \"x\": no such file"}`, `error: opening "x": no such file` },
  }
  for _, c := range cases {
    got := ResultLine(c.name, c.result)
    if got != c.want {
      t.Errorf("ResultLine(%s)\n got  %q\n want %q", c.name, got, c.want)
    }
  }
}
