package agent

import (
  "context"
  "encoding/json"
  "fmt"
  "os"
  "path/filepath"
  "strings"

  "armin/internal/llm"
)

const (
  AidStuck     Aids = 0b00000001 // step back after repeated failures
  AidProcedure Aids = 0b00000010 // a procedure for the kind of task
  AidVerify    Aids = 0b00000100 // check the work before answering
  AidAll       Aids = 0b00000111
)

// aidNames are the aids as typed (aids = stuck,verify).
var aidNames = map[string]Aids{
  "stuck":     AidStuck,
  "procedure": AidProcedure,
  "verify":    AidVerify,
  "all":       AidAll,
  "none":      0,
}

// ParseAids reads "all", "none" or a comma-separated list.
func ParseAids(s string) (Aids, bool) {
  var a Aids
  for _, part := range strings.Split(s, ",") {
    bit, ok := aidNames[strings.TrimSpace(part)]
    if !ok {
      return 0, false
    }

    a |= bit
  }

  return a, true
}

func (a Aids) Has(b Aids) bool { return a & b != 0 }

const (
  TaskOther Task = iota
  TaskFix
  TaskBuild
  TaskDebug
  TaskExplain
  TaskShell
)

var taskNames = [...]string{
  TaskOther:   "other",
  TaskFix:     "fix",
  TaskBuild:   "build",
  TaskDebug:   "debug",
  TaskExplain: "explain",
  TaskShell:   "shell",
}

func (t Task) String() string { return taskNames[t] }

// feasible opens every procedure: the cheapest wrong answer to
// avoid is doing a task that can't or shouldn't be done as asked.
const feasible = "First check the request against what is actually " +
  "in the project: if it contradicts the code, depends on something " +
  "missing, or can't be done as asked, say so plainly instead of " +
  "forcing it."

// DefaultProcedures are the steps each kind of task gets; files in
// <config>/procedures/<kind>.md replace them (LoadProcedures).
var DefaultProcedures = map[Task]string{
  TaskFix: feasible + " Then: reproduce the failure (run the test " +
    "or program), read the code it points to, make the smallest " +
    "change that fixes the cause, and run the same check again to " +
    "show it passes.",
  TaskBuild: feasible + " Then: find where it belongs and how the " +
    "nearby code does similar things, write it in that style, add " +
    "or run a test that exercises it, and build before reporting.",
  TaskDebug: feasible + " Then: reproduce it, read the error and " +
    "the code around it, list two or three possible causes, test " +
    "the cheapest one first, and only change code once you know the " +
    "cause. Re-run the failing case at the end.",
  TaskExplain: feasible + " Then: find the relevant code with the " +
    "search tools, read it, and answer from what it says, citing " +
    "file:line. Say where you're unsure instead of guessing.",
  TaskShell: feasible + " Then: check the tools and files involved " +
    "exist, try the command on a small case first, and show its real " +
    "output.",
}

// LoadProcedures is DefaultProcedures with any <dir>/<kind>.md
// file in its place.
func LoadProcedures(dir string) map[Task]string {
  out := make(map[Task]string, len(DefaultProcedures))
  for t, text := range DefaultProcedures {
    out[t] = text
    data, err := os.ReadFile(filepath.Join(dir, t.String() + ".md"))
    if err == nil && strings.TrimSpace(string(data)) != "" {
      out[t] = strings.TrimSpace(string(data))
    }
  }

  return out
}

// addProcedure appends the task's procedure to the turn's request.
func (l *Loop) addProcedure(history []llm.Message, t Task) {
  if !l.Aids.Has(AidProcedure) || t == TaskOther {
    return
  }

  text := l.Procedures[t]
  if text == "" {
    text = DefaultProcedures[t]
  }

  last := len(history) - 1
  if last < 0 || history[last].Role != "user" || text == "" {
    return
  }

  history[last].Content += "\n\n(How to approach this " + t.String() +
    " task: " + text + ")"
}

const stuckNote = "Step back: your last attempts failed without " +
  "progress. Before trying again: (1) say what you know for sure " +
  "from the tool output, (2) list two or three hypotheses for the " +
  "cause, (3) pick the cheapest one to test and say what result " +
  "would confirm it. If something outside the code blocks you, stop " +
  "and tell the user."

// stallLimit misses in a row earn a note, at most maxNudges a turn.
const (
  stallLimit = 3
  maxNudges  = 2
)

// observe counts a tool result as a miss (an error, failing tests or
// build, a program exiting non-zero) or as progress (a passing check),
// and reports when the turn should step back.
func (s *stall) observe(id ToolID, result []byte) bool {
  var r struct {
    Error    string `json:"error"`
    Passed   *bool  `json:"passed"`
    ExitCode *int   `json:"exit_code"`
  }

  json.Unmarshal(result, &r)
  checked := id == toolRunTests || id == toolBuildCheck ||
    id == toolLintCheck || id == toolRunProgram
  failed := r.Error != "" || r.Passed != nil && !*r.Passed ||
    id == toolRunProgram && r.ExitCode != nil && *r.ExitCode != 0
  switch {
  case failed:
    s.misses++
  case checked:
    s.misses = 0
  }

  if s.misses < stallLimit || s.nudges >= maxNudges {
    return false
  }

  s.misses = 0
  s.nudges++

  return true
}

const verifyPrompt = `You check a coding assistant's work before it reports back. You get the user's request, the actions it took with their outcome, and its final answer.

Reply OK if the answer addresses the whole request and its claims are backed by the actions: code that was changed was then built or tested and passed, facts come from files it read or commands it ran.

Otherwise reply ISSUE: and one sentence naming what is missing or wrong: an unverified claim, a part of the request not done, an edit never tested, a contradiction with what the tools showed.

Reply with OK or ISSUE: only.`

// verifyNote sends the model back to work on what the check found.
const verifyNote = "Before you finish, a check of your work found: %s " +
  "Verify it with the tools and fix what's needed, then give your " +
  "final answer. If it's actually fine, say why in one line."

// maxVerifyInput bounds what the check reads.
const maxVerifyInput = 6000

// verify asks the model, thinking off, whether this turn's answer
// holds up. Empty means it does (or the check couldn't run).
func (l *Loop) verify(
  ctx context.Context, history []llm.Message, start int,
) string {
  var request string
  var b strings.Builder
  for _, m := range history[start:] {
    switch m.Role {
    case "user":
      if request == "" {
        request = m.Content
      }

    case "assistant":
      for _, c := range m.ToolCalls {
        args, _ := json.Marshal(c.Function.Arguments)
        fmt.Fprintf(&b, "- %s %s", c.Function.Name, truncate(string(args), 120))
      }

    case "tool":
      fmt.Fprintf(&b, " → %s\n", outcome(m.Content))
    }
  }

  answer := history[len(history) - 1].Content
  input := fmt.Sprintf("Request:\n%s\n\nActions:\n%s\nFinal answer:\n%s",
    truncate(request, 1500), b.String(), truncate(answer, 2000))

  quick := *l.LLM
  quick.Effort = "none"
  reply, err := quick.Chat(ctx, []llm.Message{
    { Role: "system", Content: verifyPrompt },
    { Role: "user", Content: truncate(input, maxVerifyInput) },
  }, nil, nil)
  if err != nil {
    return ""
  }

  text := strings.TrimSpace(reply.Content)
  issue, ok := strings.CutPrefix(text, "ISSUE:")
  if !ok {
    return ""
  }

  return strings.TrimSpace(issue)
}

// outcome summarizes a tool result for the check: its error, a
// test or build verdict, an exit code, or "ok".
func outcome(result string) string {
  var r struct {
    Error    string `json:"error"`
    Passed   *bool  `json:"passed"`
    ExitCode *int   `json:"exit_code"`
  }

  json.Unmarshal([]byte(result), &r)
  switch {
  case r.Error != "":
    return "error: " + truncate(r.Error, 120)
  case r.Passed != nil && *r.Passed:
    return "passed"
  case r.Passed != nil:
    return "FAILED"
  case r.ExitCode != nil && *r.ExitCode != 0:
    return fmt.Sprintf("exit %d", *r.ExitCode)
  }

  return "ok"
}

// changedOrRan reports whether the turn wrote files or ran code:
// the turns worth a check. Pure reading and chat aren't.
func changedOrRan(history []llm.Message, start int) bool {
  for _, m := range history[start:] {
    for _, c := range m.ToolCalls {
      id := toolIDs[c.Function.Name]
      if id.isWrite() || id == toolRunTests || id == toolBuildCheck {
        return true
      }
    }
  }

  return false
}

func truncate(s string, n int) string {
  if len(s) <= n {
    return s
  }

  return s[:n] + "…"
}

// The lines Qwen3.8's chat template puts at the top of the system
// prompt for its xhigh and low reasoning efforts (medium has none).
// It's trained on them; to other models they're plain instructions.
const (
  effortHighLine = "Reasoning effort is set to xhigh. Please think " +
    "carefully through the task, validate key assumptions, consider " +
    "plausible alternatives, and prioritize correctness, consistency, " +
    "and clarity in the final answer."
  effortLowLine = "Reasoning effort is set to low. Keep your thinking " +
    "brief and focused, moving directly to the conclusion without " +
    "unnecessary elaboration."
)

// setEffortLine puts the line for this turn's effort (a Wire value)
// at the top of the system prompt, or takes it off. The prompt only
// changes when the effort does, so the server's cache holds.
func setEffortLine(history []llm.Message, wire string) {
  if len(history) == 0 || history[0].Role != "system" {
    return
  }

  base := history[0].Content
  for _, line := range []string{ effortHighLine, effortLowLine } {
    base = strings.TrimPrefix(base, line + "\n\n")
  }

  switch wire {
  case "high":
    base = effortHighLine + "\n\n" + base
  case "low":
    base = effortLowLine + "\n\n" + base
  }

  history[0].Content = base
}

// note tells the frontend what an aid did, if it listens.
func note(hooks Hooks, msg string) {
  if hooks.Aside != nil {
    hooks.Aside(msg)
  }
}
