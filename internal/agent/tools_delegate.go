package agent

// delegate: the copilot or advisor hands a small, well-defined task to
// the minion (the local model). The minion runs its own short turn in
// a fresh Loop (same sandbox, tools and tree, role minion, its own
// delegation number on everything it writes), then the acceptance
// check runs. A passing check raises the minion's tree writes from
// inferred to checked, with the check attached as evidence; they stay
// minion-authored and unverified until the delegator verifies them.
//
// ChoreClient picks the model for armin's own small helper calls:
// the minion when chat runs on a hosted model (free, and the GPU is
// idle), the chat model otherwise.

import (
  "context"
  "fmt"
  "strings"

  "armin/internal/know"
  "armin/internal/llm"
  "armin/internal/sandbox"
  "armin/internal/tools"
)

// minionRounds caps a delegated turn: a task that needs more is not
// a minion's task.
const minionRounds = 15

// minionPrompt is the delegated turn's system prompt.
const minionPrompt = "You are the minion: a small local model doing one " +
  "task for the copilot. Do exactly the task with your tools, check " +
  "your result, and answer briefly with what you did and found, citing " +
  "files and lines. Don't do more than asked. If the task is unclear or " +
  "too big for you, say so instead of guessing. Record what you " +
  "establish with know_write; it stays marked as yours until checked."

var delegateTools = []toolSpec{
  {
    id:    toolDelegate,
    flags: flagAgent | flagSession | flagMinion,
    roles: RoleCopilot | RoleAdvisor,
    run: func(ctx context.Context, l *Loop, c toolCall) any {
      return delegateRun(ctx, l, c)
    },
    def: toolFunc(
      "delegate",
      "Hand a small, well-defined task to the minion, the local model: a "+
        "search, a summary of some files, a small contained edit. First rule "+
        "out that a script or a deterministic tool does it better "+
        "(search_code, find_references, run_command...). It must be a task "+
        "the minion gets right almost every time, with an acceptance check "+
        "the result has to pass. What it adds to the tree stays marked as "+
        "the minion's until you verify it.",
      map[string]llm.ToolProperty{
        "task": stringProp("What to do, precisely: files, names, the " +
          "expected shape of the answer"),
        "check": {
          Type:        "string",
          Description: "The acceptance check run afterwards: tests, build, " +
            "lint, command (check_args is the command), or none when you'll " +
            "check the answer yourself",
          Enum:        []string{ "tests", "build", "lint", "command", "none" },
        },
        "check_args": {
          Type:        "array",
          Description: "Arguments for the check: test or build args, or the " +
            "command's binary then its arguments",
          Items:       &llm.ToolProperty{ Type: "string" },
        },
        "edits": {
          Type:        "boolean",
          Description: "Let the minion change files (each change is still " +
            "approved); default false",
        },
        "nodes": {
          Type:        "array",
          Description: "Tree nodes the task builds on (ids or paths); their " +
            "lines go in the task",
          Items:       &llm.ToolProperty{ Type: "string" },
        },
      },
      "task", "check",
    ),
  },
}

// delegateRun is runDelegate, set in init: a delegation runs a turn,
// and a turn reads the tool table, so naming runDelegate in the table
// itself would be an initialization cycle.
var delegateRun runFunc

func init() {
  delegateRun = runDelegate
}

// ChoreClient is the model for armin's small helper calls (titles,
// effort, cataloging): the minion when chat runs on a hosted model,
// otherwise the chat model with reasoning off.
func (l *Loop) ChoreClient(ctx context.Context) *llm.Client {
  if l.Minion != nil && l.LLM != nil && l.LLM.APIKey != "" {
    c, err := l.Minion(ctx)
    if err == nil {
      return c.Chore()
    }
  }

  return l.LLM.Chore()
}

func runDelegate(ctx context.Context, l *Loop, c toolCall) any {
  in, bad := decode[delegateArgs](c)
  if bad != nil {
    return bad
  }

  if strings.TrimSpace(in.Task) == "" {
    return errResult(fmt.Errorf("the task is empty"))
  }

  // the advisor doesn't change files, so neither does its minion
  edits := in.Edits && l.Role != RoleAdvisor
  client, err := l.Minion(ctx)
  if err != nil {
    return errResult(fmt.Errorf("the minion isn't available: %w", err))
  }

  l.delegations++
  id := l.delegations
  child := &Loop{
    LLM:        client,
    Sandbox:    l.Sandbox,
    WebSearch:  l.WebSearch,
    Symbols:    l.Symbols,
    Tracker:    l.Tracker,
    Library:    l.Library,
    Know:       l.Know,
    Session:    l.Session,
    Delegation: id,
    Role:       RoleMinion,
    ReadOnly:   !edits,
    MaxRounds:  minionRounds,
    Aids:       AidStuck,
  }

  task := in.Task
  if l.Know != nil && len(in.Nodes) > 0 {
    l.knowMu.Lock()
    var ids []know.NodeID
    for _, ref := range in.Nodes {
      n, err := l.node(ref)
      if err != nil {
        l.knowMu.Unlock()
        return errResult(err)
      }

      ids = append(ids, n)
    }

    task += "\n\nWhat is known already:\n" + l.lines(ids, true)
    l.knowMu.Unlock()
  }

  history := []llm.Message{
    { Role: "system", Content: minionPrompt },
    { Role: "user", Content: task },
  }

  // the user still approves the minion's writes and sees its calls;
  // its tokens don't stream into the delegator's answer
  hooks := Hooks{
    ToolCall:   c.hooks.ToolCall,
    ToolResult: c.hooks.ToolResult,
    Approve:    c.hooks.Approve,
    Propose:    c.hooks.Propose,
    Notice:     c.hooks.Notice,
    Aside:      c.hooks.Aside,
  }

  note(c.hooks, fmt.Sprintf("minion %d: %s", id, cut(in.Task, 80)))
  answer, err := child.Turn(ctx, &history, hooks)
  if err != nil {
    return errResult(fmt.Errorf("minion %d failed: %w", id, err))
  }

  out := map[string]string{
    "delegation": fmt.Sprint(id),
    "answer":     cut(answer, 6000),
  }

  passed, report := l.acceptance(ctx, in)
  out["check"] = report
  out["confidence"] = "inferred"
  if passed {
    out["confidence"] = "checked"
  }

  if l.Know != nil {
    l.knowMu.Lock()
    lines, err := l.settleDelegation(id, passed, report)
    l.knowMu.Unlock()
    if err != nil {
      return knowErr(err)
    }

    if lines != "" {
      out["nodes"] = lines
      out["next"] = "these are the minion's, unverified: check the ones you " +
        "rely on, then know_write them with id and verified: true"
    }
  }

  return out
}

// acceptance runs the delegation's check and reports it.
func (l *Loop) acceptance(ctx context.Context, in delegateArgs) (bool, string) {
  var run func(
    context.Context, *sandbox.Config, tools.ProjectRunInput,
  ) (*tools.ProjectRunOutput, error)
  switch in.Check {
  case "tests":
    run = tools.RunTests
  case "build":
    run = tools.BuildCheck
  case "lint":
    run = tools.LintCheck
  case "command":
    if len(in.CheckArgs) == 0 {
      return false, "not run: command needs check_args"
    }

    res, cerr := tools.RunCommand(ctx, l.Sandbox, tools.RunCommandInput{
      Binary: in.CheckArgs[0], Args: in.CheckArgs[1:],
    })
    if cerr != nil {
      return false, "failed to run: " + cerr.Error
    }

    cmd := strings.Join(in.CheckArgs, " ")
    if res.ExitCode != 0 {
      return false, fmt.Sprintf("failed: %s exited %d: %s", cmd, res.ExitCode,
        cut(strings.TrimSpace(res.Stderr + "\n" + res.Stdout), 1500))
    }

    return true, "passed: " + cmd
  default:
    return false, "not run: check the answer yourself before relying on it"
  }

  res, err := run(ctx, l.Sandbox, tools.ProjectRunInput{ Args: in.CheckArgs })
  if err != nil {
    return false, "failed to run: " + err.Error()
  }

  if !res.Available {
    return false, "not run: no " + in.Check + " command for this project"
  }

  if !res.Passed {
    return false, fmt.Sprintf("failed: %s: %s", res.Command,
      cut(strings.TrimSpace(res.Summary + "\n" + res.Output), 1500))
  }

  return true, "passed: " + res.Command
}

// settleDelegation finds what the minion wrote in delegation id and,
// when its check passed, raises those nodes to checked with the check
// as evidence. It returns their lines.
func (l *Loop) settleDelegation(
  id uint32, passed bool, report string,
) (string, error) {
  var ids []know.NodeID
  for n := 1; n < l.Know.Len(); n++ {
    nid := know.NodeID(n)
    if !l.Know.Live(nid) {
      continue
    }

    a := l.Know.Author(l.Know.Node(nid).Author)
    if a.Role == know.RoleMinion && a.Delegation == id &&
      l.Know.Str(a.Session) == l.Session {
      ids = append(ids, nid)
    }
  }

  if passed {
    for _, n := range ids {
      node := l.Know.Node(n)
      if node.Conf() < know.ConfChecked {
        err := l.Know.SetKind(n, node.Kind(), know.ConfChecked)
        if err != nil {
          return "", err
        }
      }

      _, err := l.Know.AddEvidence(n, know.RefCheck,
        fmt.Sprintf("delegation %d: %s", id, report), 0)
      if err != nil {
        return "", err
      }
    }
  }

  return l.lines(ids, false), nil
}

// cut shortens s to n bytes at most, marking the cut.
func cut(s string, n int) string {
  if len(s) <= n {
    return s
  }

  return s[:n] + "…"
}
