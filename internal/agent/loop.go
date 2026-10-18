// Package agent is the tool-calling round trip at armin's center:
// send the conversation to the model, run the tools it calls, feed the
// results back, and repeat until it answers in plain text. Every
// frontend (REPL, web, nvim) runs its turns through one Loop.
//
//   loop.go        the Loop: New, the per-call checks shared by all
//                  tools (unknown arguments, access outside the
//                  project), finishing an answer
//   turn.go        Turn: one user request, round by round, and the
//                  guard against repeated calls
//   tools.go       the tool table: lookup, definitions, execute,
//   tools_*.go     adapters; one spec per tool, grouped by topic
//   toolid.go      the tool IDs, in the order the model sees them
//   context.go     keeping a long session inside the model's window:
//   policy.go      trimming old results, compacting, the stream tier
//   aids.go        reasoning aids: step back, procedures, verify
//   effort.go      effort levels and auto's judgment (Assess)
//   sources.go     numbering web results for citations
//   display.go     how a tool call and its result read on screen
//
// think builds the Loop (think.buildLoop) on the chat model; Hooks
// are how a frontend watches and steers a turn.
package agent

import (
  "bytes"
  "context"
  "encoding/json"
  "errors"
  "fmt"
  "sort"
  "strings"

  "armin/internal/convo"
  "armin/internal/llm"
  "armin/internal/sandbox"
  "armin/internal/tools"
)

// DefaultMaxRounds bounds how many tool-call round trips one user
// turn may take. Real edit-build-fix work needs dozens; the cap only
// stops a model that's looping.
const DefaultMaxRounds = 50

// New returns a ready-to-use Loop.
func New(client *llm.Client, sb *sandbox.Config) *Loop {
  return &Loop{
    LLM:     client,
    Sandbox: sb,
    Tracker: tools.NewFileTracker(),
  }
}

// Estimate is the rough size of the next request: history plus the
// tool definitions that go with every request.
func (l *Loop) Estimate(history []llm.Message) int {
  l.Definitions()
  return convo.Analyze(history).ApproxTokens + l.defsTokens
}

func (l *Loop) maxRounds() int {
  if l.MaxRounds > 0 {
    return l.MaxRounds
  }

  return DefaultMaxRounds
}

// unknownParams names arguments the tool doesn't have. Decoding
// would silently drop them — "arguments" for "args" ran a bare
// find — so they're an error the model can correct.
func (l *Loop) unknownParams(name string, args map[string]any) string {
  l.Definitions()
  known, ok := l.params[name]
  if !ok {
    return "" // unknown tools are reported by the dispatch
  }

  var bad []string
  for k := range args {
    if !known[k] {
      bad = append(bad, k)
    }
  }

  if len(bad) == 0 {
    return ""
  }

  valid := make([]string, 0, len(known))
  for k := range known {
    valid = append(valid, k)
  }

  sort.Strings(bad)
  sort.Strings(valid)

  return fmt.Sprintf(
    "%s has no parameter %s; its parameters are: %s. Nothing was run.",
    name, strings.Join(bad, ", "), strings.Join(valid, ", "),
  )
}

// argAliases are names small models use for run_command's and
// run_program's "args".
var argAliases = []string{
  "arguments", "argv", "arg", "cmd_args", "params", "flags",
}

// normalizeArgs fixes the shapes small models give run_command and
// run_program in place, before the call is shown or run: an alias
// for "args", args as one string, or the whole command line in
// "binary". The model's history then holds the fixed call, so it
// sees the right form.
func normalizeArgs(call *llm.ToolCall) {
  id := toolIDs[call.Function.Name]
  if id != toolRunCommand && id != toolRunProgram {
    return
  }

  a := call.Function.Arguments
  if a == nil {
    return
  }

  _, ok := a["args"]
  if !ok {
    for _, alias := range argAliases {
      v, ok := a[alias]
      if ok {
        a["args"] = v
        delete(a, alias)
        break
      }
    }
  }

  s, ok := a["args"].(string)
  if ok {
    a["args"] = toAny(splitWords(s))
  }

  bin, _ := a["binary"].(string)
  words := splitWords(bin)
  if len(words) > 1 {
    list, _ := a["args"].([]any)
    a["binary"] = words[0]
    a["args"] = append(toAny(words[1:]), list...)
  }
}

func toAny(words []string) []any {
  out := make([]any, len(words))
  for i, w := range words {
    out[i] = w
  }

  return out
}

// splitWords splits a command line the way a shell would split
// plain words and quotes — no variables, globs or escapes beyond
// quotes.
func splitWords(s string) []string {
  var (
    words []string
    cur   strings.Builder
    quote rune
    in    bool
  )

  for _, r := range s {
    switch {
    case quote != 0:
      if r == quote {
        quote = 0
      } else {
        cur.WriteRune(r)
      }

    case r == '\'' || r == '"':
      quote, in = r, true

    case r == ' ' || r == '\t' || r == '\n':
      if in {
        words = append(words, cur.String())
        cur.Reset()
        in = false
      }

    default:
      cur.WriteRune(r)
      in = true
    }
  }

  if in {
    words = append(words, cur.String())
  }

  return words
}

// callKey identifies a call by tool and arguments; json.Marshal
// sorts map keys, so equal arguments give equal keys.
func callKey(call llm.ToolCall) string {
  args, _ := json.Marshal(call.Function.Arguments)
  return call.Function.Name + string(args)
}

// errorOf returns a result's top-level "error", or "". The cheap
// substring test first: inside a JSON string every quote is escaped,
// so `"error":"` only appears as a real key.
func errorOf(result []byte) string {
  if !bytes.Contains(result, []byte(`"error":"`)) {
    return ""
  }

  var r struct {
    Error string `json:"error"`
  }

  json.Unmarshal(result, &r)

  return lastErrorLine(r.Error)
}

// finish appends a Sources list to the final answer when the turn
// searched the web and the model didn't list its sources itself,
// streaming the addition like the rest of the answer.
func (l *Loop) finish(
  history *[]llm.Message, sources *turnSources, hooks Hooks,
) string {
  h := *history
  final := &h[len(h) - 1]
  add := sources.appendix(final.Content)
  if add != "" {
    final.Content += add
    if hooks.Token != nil {
      hooks.Token(add)
    }
  }

  return final.Content
}

// executeTurn is execute plus the turn's citation numbering and the
// access check for reads outside the project.
func (l *Loop) executeTurn(
  ctx context.Context, call llm.ToolCall, hooks Hooks,
  sources *turnSources, declined map[string]bool,
) any {
  out := l.askAccess(call, hooks, declined)
  if out != nil {
    return out
  }

  out = l.execute(ctx, call, hooks)
  ws, ok := out.(*tools.WebSearchOutput)
  if ok {
    sources.number(ws)
  }

  return out
}

// askAccess handles a read pointed outside the project. Small models
// rarely think to call request_access first, so armin asks the user
// itself, before the call runs, and grants the directory on a yes.
// A directory declined once isn't asked about again this turn.
func (l *Loop) askAccess(
  call llm.ToolCall, hooks Hooks, declined map[string]bool,
) any {
  if hooks.Approve == nil {
    return nil // nobody to ask: the call fails with a pointer to -add-dir
  }

  for _, p := range readPaths(call) {
    var outside *sandbox.OutsideError
    _, err := l.Sandbox.ResolvePath(p)
    if !errors.As(err, &outside) {
      continue
    }

    dir := outside.Dir()
    if declined[dir] {
      return map[string]string{
        "error": "the user declined read access to " + dir +
          " this turn; don't ask again",
      }
    }

    ok, feedback := hooks.Approve(
      "request_access",
      "read access to " + dir + "\nneeded for: " +
        CallLine(call.Function.Name, call.Function.Arguments),
    )
    if !ok {
      declined[dir] = true
      return declinedResult("read access to " + dir, feedback)
    }

    _, err = l.Sandbox.AddReadRoot(dir)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
  }

  return nil
}

// ApprovePrivileged is the tool name Approve gets for a privileged
// run_program (rm, cp, mv, scp, rsync).
const ApprovePrivileged = "run_program (privileged)"

// AlwaysAsk reports whether an approval must reach the user even
// under -yes or "all this session": reads outside the project and
// privileged programs.
func AlwaysAsk(tool string) bool {
  return tool == "request_access" || tool == ApprovePrivileged
}

// steer adds what the user typed while the turn ran (hooks.Steer)
// as user messages; true when there was any.
func (l *Loop) steer(history *[]llm.Message, hooks Hooks) bool {
  if hooks.Steer == nil {
    return false
  }

  msgs := hooks.Steer()
  for _, m := range msgs {
    *history = append(*history, llm.Message{ Role: "user", Content: m })
  }

  return len(msgs) > 0
}
