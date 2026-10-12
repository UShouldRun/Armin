package repl

import (
  "os"
  "path/filepath"
  "strings"
  "testing"
  "time"

  "armin/internal/agent"
  "armin/internal/catalog"
  "armin/internal/share"
  "armin/internal/ui"
)

func TestShowProposal(t *testing.T) {
  ui.Enabled = false
  defer func() { ui.Enabled = true }()

  r := &REPL{ Root: "/p" }
  update := capture(t, func() {
    r.showProposal(agent.Proposal{
      Tool: "edit_file", Path: "main.c", Start: 2,
      OldLines: []string{ "a", "b", "old", "c", "d" },
      Gone:     []string{ "old" },
      Came:     []string{ "new1", "new2" },
    })
  })

  for _, want := range []string{
    "● Update(main.c)", "└ 2 additions, 1 removal",
    "1   a", "3 - old", "3 + new1", "4 + new2", "5   c",
  } {
    if !strings.Contains(update, want) {
      t.Fatalf("update display lacks %q:\n%s", want, update)
    }
  }

  run := capture(t, func() {
    r.showProposal(agent.Proposal{ Tool: "run_program", Command: "make -j4", Dir: "/p/src" })
  })

  if !strings.Contains(run, "● Run(make -j4)") || !strings.Contains(run, "└ in src") {
    t.Fatalf("run display:\n%s", run)
  }
}

// TestViewOpensNvim: [v]iew starts the configured terminal with nvim
// read-only: a diff for a change, the script for a run.
func TestViewOpensNvim(t *testing.T) {
  dir := t.TempDir()
  bin := filepath.Join(dir, "bin")
  os.Mkdir(bin, 0o755)
  os.WriteFile(filepath.Join(bin, "nvim"), []byte("#!/bin/sh\n"), 0o755)
  log := filepath.Join(dir, "args")
  term := filepath.Join(bin, "fake-term")
  os.WriteFile(term, []byte("#!/bin/sh\necho \"$@\" > " + log + "\n"), 0o755)
  t.Setenv("PATH", bin + ":" + os.Getenv("PATH"))

  r := &REPL{ Root: dir, Terminal: term + " -e" }
  err := r.view(agent.Proposal{
    Tool: "edit_file", Path: "src/main.c", Resolved: "/p/src/main.c",
    OldLines: []string{ "a", "old", "b" }, Start: 1,
    Gone: []string{ "old" }, Came: []string{ "new" },
  })
  if err != nil {
    t.Fatal(err)
  }

  args := waitFile(t, log)
  if strings.Contains(args, " -d ") || !strings.Contains(args, "filetype=diff +5 ") || !strings.HasSuffix(strings.TrimSpace(args), "main.c.diff") {
    t.Fatalf("change opened as %q", args)
  }

  diff := readSmall(strings.Fields(args)[len(strings.Fields(args)) - 1])
  if !strings.Contains(diff, " a\n-old\n+new\n b\n") {
    t.Fatalf("diff = %q", diff)
  }

  os.Remove(log)
  err = r.view(agent.Proposal{ Tool: "run_program", Command: "python3 gen.py", Script: "/p/gen.py" })
  if err != nil {
    t.Fatal(err)
  }

  if args := waitFile(t, log); !strings.HasSuffix(strings.TrimSpace(args), "nvim -R /p/gen.py") {
    t.Fatalf("script opened as %q", args)
  }
}

func waitFile(t *testing.T, path string) string {
  t.Helper()
  for i := 0; i < 100; i++ {
    data, err := os.ReadFile(path)
    if err == nil && len(data) > 0 {
      return string(data)
    }

    time.Sleep(20 * time.Millisecond)
  }

  t.Fatalf("%s never written", path)

  return ""
}

func TestTerminalCmd(t *testing.T) {
  got, _ := terminalCmd("ghostty -e")
  if strings.Join(got, " ") != "ghostty -e" {
    t.Fatalf("configured = %v", got)
  }

  t.Setenv("TMUX", "/tmp/tmux-1/default,1,0")
  got, _ = terminalCmd("")
  if strings.Join(got, " ") != "tmux new-window" {
    t.Fatalf("in tmux = %v", got)
  }

  t.Setenv("TMUX", "")
  t.Setenv("DISPLAY", "")
  t.Setenv("WAYLAND_DISPLAY", "")
  _, err := terminalCmd("")
  if err == nil {
    t.Fatal("no window system should be an error")
  }
}

func TestChooseHosted(t *testing.T) {
  ui.Enabled = false
  defer func() { ui.Enabled = true }()

  info := share.ModelInfo{
    Configured: []share.ModelChoice{ { Name: "planner", Model: "qwen/qwen3.8-27b" } },
    Suggested:  []catalog.Suggestion{ { ID: "z-ai/glm-5.3" }, { ID: "deepseek/deepseek-v4-pro-0813" } },
  }

  cases := []struct {
    answers []string
    want    string
  }{
    { []string{ "" }, "planner" },
    { []string{ "2" }, "planner:deepseek/deepseek-v4-pro-0813" },
    { []string{ "anthropic/claude-sonnet-5.5" }, "planner:anthropic/claude-sonnet-5.5" },
    { []string{ "oops", "" }, "planner" },
  }

  for _, c := range cases {
    answers := c.answers
    var printed string
    printed = capture(t, func() {
      got := ChooseHosted(info, "planner", func(string) (string, error) {
        a := answers[0]
        answers = answers[1:]

        return a, nil
      })
      if got != c.want {
        t.Errorf("answers %q → %q, want %q", c.answers, got, c.want)
      }
    })

    if !strings.Contains(printed, "→ qwen/qwen3.8-27b") || !strings.Contains(printed, "(default)") || !strings.Contains(printed, "2 deepseek") {
      t.Fatalf("list shown:\n%s", printed)
    }
  }
}
