package agent

// Tools that run programs: read-only commands, the project's
// tests, build and linter, and programs the user approves.

import (
  "context"
  "fmt"
  "os"
  "path/filepath"
  "strings"

  "armin/internal/llm"
  "armin/internal/sandbox"
  "armin/internal/tools"
)

var runTools = []toolSpec{
  {
    id:    toolRunCommand,
    flags: flagRun,
    run:   runRunCommand,
    paths: commandPaths,
    def: llm.ToolFunction{
      Name: "run_command",
      Description: "Run an allowlisted, read-only binary " +
        "directly (no shell). args is always an array of " +
        "separate arguments, never one shell string.",
      Parameters: llm.ToolParameters{
        Type: "object",
        Properties: map[string]llm.ToolProperty{
          "binary": {
            Type:        "string",
            Description: "Bare binary name, e.g. git, wc, find",
          },
          "args": {
            Type:        "array",
            Description: `Arguments as separate array entries, e.g. ` +
              `["log", "--oneline", "-5"]`,
            Items:       &llm.ToolProperty{ Type: "string" },
          },
        },
        Required: []string{ "binary" },
      },
    },
  },
  {
    id:    toolRunProgram,
    flags: flagWrite | flagRun | flagAgent,
    run:   runRunProgram,
    def: llm.ToolFunction{
      Name: "run_program",
      Description: "Build, compile, test or run code: make, cmake, gcc, " +
        "g++, clang, mpicc, mpirun, go, cargo, python3, node, npm... " +
        "or a program the project built (\"./build/app\"). Binary plus " +
        "an args array, no shell: feed a file as input with stdin, " +
        "save the output with stdout (then diff it). For pipes, use " +
        "binary bash with args [\"-c\", \"...\"] — always asked. The " +
        "user approves each run. Prefer run_tests / build_check when " +
        "they fit.",
      Parameters: llm.ToolParameters{
        Type: "object",
        Properties: map[string]llm.ToolProperty{
          "binary": {
            Type:        "string",
            Description: "Program: a bare name (make, gcc, mpirun) or a " +
              "relative path inside the project (./fox)",
          },
          "args": {
            Type:        "array",
            Description: `Arguments as separate entries, e.g. ["-O2", ` +
              `"-o", "fox", "src/fox.c"]`,
            Items:       &llm.ToolProperty{ Type: "string" },
          },
          "dir": {
            Type:        "string",
            Description: "Working directory inside the project (default: " +
              "project root), e.g. \"build\"",
          },
          "stdin": {
            Type:        "string",
            Description: "Project file to feed as the program's input, " +
              "like < file, e.g. \"tests/input/input6\"",
          },
          "stdout": {
            Type:        "string",
            Description: "Project file to write the program's output to, " +
              "like > file, e.g. \"build/out6.txt\"",
          },
        },
        Required: []string{ "binary" },
      },
    },
  },
  {
    id:    toolRunTests,
    flags: flagRun,
    run:   inSandbox(tools.RunTests),
    def: projectRunFunc(
      "run_tests",
      "Run the project's test command (detected from Makefile, "+
        "go.mod, Cargo.toml, package.json, or Python config; or "+
        "user-configured). Reports pass/fail, a failure summary, "+
        "and the end of the output.",
      `Extra arguments replacing the default target, e.g. `+
        `["./internal/tools", "-run", "TestX"] or ["-k", "parser"]`,
    ),
  },
  {
    id:    toolBuildCheck,
    flags: flagRun,
    run:   inSandbox(tools.BuildCheck),
    def: projectRunFunc(
      "build_check",
      "Run the project's build or type-check command and report "+
        "pass/fail plus errors. Run it after editing code.",
      `Extra arguments replacing the default target, e.g. `+
        `["./cmd/..."]`,
    ),
  },
  {
    id:    toolLintCheck,
    flags: flagRun,
    run:   inSandbox(tools.LintCheck),
    def: projectRunFunc(
      "lint_check",
      "Run the project's linter; reports available:false if the "+
        "project has none.",
      "Extra arguments replacing the default target",
    ),
  },
}

func runRunCommand(ctx context.Context, l *Loop, c toolCall) any {
  in, bad := decode[tools.RunCommandInput](c)
  if bad != nil {
    return bad
  }

  out, cmdErr := tools.RunCommand(ctx, l.Sandbox, in)
  if cmdErr != nil {
    return cmdErr
  }

  return out
}

// commandPaths are the files a read-only command's arguments name.
func commandPaths(args map[string]any) []string {
  bin, _ := args["binary"].(string)
  list, _ := args["args"].([]any)
  strs := make([]string, 0, len(list))
  for _, a := range list {
    strs = append(strs, fmt.Sprint(a))
  }

  return tools.PathArgs(bin, strs)
}

func runRunProgram(ctx context.Context, l *Loop, c toolCall) any {
  in, bad := decode[tools.RunProgramInput](c)
  if bad != nil {
    return bad
  }

  return l.runProgram(ctx, in, c.hooks)
}

// runProgram checks a run_program call, asks the user (with nobody
// to ask, it doesn't run), then runs it.
func (l *Loop) runProgram(
  ctx context.Context, in tools.RunProgramInput, hooks Hooks,
) any {
  dir, _, err := tools.PlanProgram(l.Sandbox, in)
  if err != nil {
    return map[string]string{ "error": err.Error() }
  }

  if hooks.Approve == nil {
    return map[string]string{
      "error": "run_program needs the user's approval and nobody can " +
        "approve here; use run_tests / build_check, or ask the user to run it",
    }
  }

  preview := "$ " + tools.Describe(in)
  if dir != l.Sandbox.Root {
    preview += "\nin " + dir
  }

  tool := "run_program"
  if sandbox.Privileged(in.Binary) {
    tool = ApprovePrivileged
    preview += "\n(" + sandbox.PrivilegedWhy(in.Binary) + " — always asked)"
  }

  if hooks.Propose != nil {
    hooks.Propose(Proposal{
      Tool:    tool,
      Command: tools.Describe(in),
      Dir:     dir,
      Script:  l.scriptOf(in, dir),
    })
  }

  ok, feedback := hooks.Approve(tool, preview)
  if !ok {
    return declinedResult("running this", feedback)
  }

  out, err := tools.RunProgram(ctx, l.Sandbox, in)
  if err != nil {
    return map[string]string{ "error": err.Error() }
  }

  return out
}

// interpreters run the script file they're given, so it's what's
// about to run: the user should be able to read it.
var interpreters = map[string]bool{
  "python3": true, "python": true, "bash": true, "sh": true,
  "node": true, "deno": true, "bun": true, "ruby": true, "perl": true,
  "lua": true, "php": true, "Rscript": true, "julia": true,
}

// scriptOf is the script an interpreter runs (python3 tools/gen.py,
// bash run.sh): its first non-flag argument, when that's a text file
// in the project. Other programs' file arguments are data, not code
// to show, and a binary is never a script.
func (l *Loop) scriptOf(in tools.RunProgramInput, dir string) string {
  if !interpreters[filepath.Base(in.Binary)] {
    return ""
  }

  for _, a := range in.Args {
    if a == "-c" || a == "-e" {
      return "" // inline code, already in the command line
    }

    if strings.HasPrefix(a, "-") {
      continue
    }

    p := a
    if !filepath.IsAbs(p) {
      p = filepath.Join(dir, p)
    }

    resolved, err := l.Sandbox.ResolvePath(p)
    if err != nil {
      return ""
    }

    info, err := os.Stat(resolved)
    if err != nil || !info.Mode().IsRegular() || info.Size() > 1 << 20 {
      return ""
    }

    if !tools.IsText(resolved) {
      return ""
    }

    return resolved
  }

  return ""
}
