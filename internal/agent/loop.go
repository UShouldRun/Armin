// Package agent implements the tool-calling round trip: send
// messages to the model, execute any tool calls it asks for, feed
// results back, and repeat until it answers in plain text.
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

// roundLimitNote is sent when a turn runs out of tool rounds: one
// last call, with no tools, so the user gets a report instead of an
// error.
const roundLimitNote = "You have reached the tool-call limit for " +
  "this turn. Don't call any more tools. Report what you did, what " +
  "is done and verified, and what is left, so the user can decide " +
  "how to continue."

// maxBlocked is how many repeats the guard refuses before the turn
// stops: a model that keeps retrying a blocked call won't stop on
// its own.
const maxBlocked = 2

const repeatNote = "[armin] You kept repeating a call that had " +
  "already failed, so tools are off for the rest of this turn. Don't call any more tools. Tell the user " +
  "what you were trying to do, what failed, and — if they gave " +
  "feedback — how you'll follow it, then ask how to continue."

// Turn runs one user turn to completion: it sends history, executes
// any tool calls the model makes, feeds results back, and returns
// once the model answers in plain text. history is mutated in place
// so callers can persist it across turns.
func (l *Loop) Turn(
  ctx context.Context, history *[]llm.Message, hooks Hooks,
) (string, error) {
  defs := l.Definitions()
  stream := llm.StreamHandlers{
    Token: hooks.Token, Reasoning: hooks.Reasoning, Progress: hooks.Progress,
    Drafting: hooks.Drafting,
  }

  // The turn's effort on a copy: lspserver runs turns concurrently
  // on the same client.
  client := l.LLM
  if hooks.Effort != "" {
    c := *l.LLM
    c.Effort = hooks.Effort
    client = &c
  }

  setEffortLine(*history, hooks.Effort)
  l.addProcedure(*history, hooks.Task)

  turnStart := len(*history)
  var (
    last     llm.Usage
    stuck    stall
    verified bool
  )

  sources := &turnSources{}
  failed := map[string]string{} // name+args → error since the last success
  declined := map[string]bool{}  // directories the user refused this turn
  blocked := 0                   // repeats the guard refused this turn
  ran := map[string]string{}     // run tool call → its result, this turn
  lastCall := ""                 // the call before this one

  defer func() { l.endTurn(history, turnStart, hooks) }()

  stopNote := roundLimitNote
  l.hintCompact(history, hooks)

  for round := 0; round < l.maxRounds(); round++ {
    // Messages the user sent while the turn ran go in before the
    // model's next step.
    if round > 0 {
      l.steer(history, hooks)
    }

    if l.manageContext(ctx, history, last, hooks) {
      turnStart = 0 // indices moved; the end-of-turn pass rescans
      last = llm.Usage{}
    }

    reply, err := client.ChatStream(ctx, *history, defs, stream)
    if err != nil {
      return "", fmt.Errorf(
        "\n\t... in Turn:\n\t model call failed: %w", err,
      )
    }

    last = reply.Usage
    if hooks.Usage != nil && last.Total() > 0 {
      hooks.Usage(last)
    }

    *history = append(*history, *reply)

    if len(reply.ToolCalls) == 0 {
      // One check of work that changed or ran something; an issue
      // sends the model back for another round.
      check := l.Aids.Has(AidVerify) && !verified &&
        changedOrRan(*history, turnStart)
      if check {
        verified = true
        issue := l.verify(ctx, *history, turnStart)
        if issue != "" {
          note(hooks, "checking the work: " + issue)
          *history = append(*history, llm.Message{
            Role: "user", Content: fmt.Sprintf(verifyNote, issue),
          })

          continue
        }
      }

      answer := l.finish(history, sources, hooks)

      // Done — unless the user said more meanwhile: then the turn
      // goes on, and the model answers that too.
      if l.steer(history, hooks) {
        continue
      }

      return answer, nil
    }

    stepBack := false
    for _, call := range reply.ToolCalls {
      if ctx.Err() != nil {
        return "", fmt.Errorf("\n\t... in Turn:\n\t%w", ctx.Err())
      }

      normalizeArgs(&call)
      if hooks.ToolCall != nil {
        hooks.ToolCall(call.Function.Name, call.Function.Arguments)
      }

      key := callKey(call)
      var resultJSON []byte
      prev, ok := failed[key]
      if ok {
        blocked++
        resultJSON, _ = json.Marshal(map[string]string{
          "error": "you already made this exact call this turn and it " +
            "failed: " + prev + " Don't repeat it: change the " +
            "arguments, use another tool, or stop and tell the user " +
            "what is blocking you.",
        })
      } else if blocked >= maxBlocked {
        // Once the turn is being wound down, the rest of this reply's
        // calls get a result (every call needs one) but don't run.
        resultJSON, _ = json.Marshal(map[string]string{
          "error": "not run: this turn is stopping",
        })
      } else {
        result := l.executeTurn(ctx, call, hooks, sources, declined)
        resultJSON, err = json.Marshal(result)
        if err != nil {
          resultJSON = []byte(fmt.Sprintf(
            `{"error":"failed to encode tool result: %s"}`, err,
          ))
        }

        // A success in between may have changed what a failed call
        // depends on (read_file before an edit), so only a repeat
        // with nothing working in between is blocked.
        raw := string(resultJSON) // compared across runs, before any note
        msg := errorOf(resultJSON)
        if msg != "" {
          failed[key] = msg
        } else {
          prev, ok := ran[key]
          same := ok && prev == string(resultJSON) && runTool(call.Function.Name)
          switch {
          case same && lastCall == key:
            // Back to back, nothing in between: a loop.
            blocked++
            resultJSON, _ = json.Marshal(map[string]any{
              "error": "you just ran this exact call and it gave exactly " +
                "the same result (below); running it again won't change " +
                "anything: do something else, or stop and tell the user.",
              "result": json.RawMessage(resultJSON),
            })
          case same:
            // Re-running after a rebuild or an edit is legitimate:
            // pointed out, in case the change didn't take effect.
            resultJSON, _ = json.Marshal(map[string]any{
              "note": "same output as the last time you ran this, this " +
                "turn: if you changed something in between, it didn't " +
                "change this result",
              "result": json.RawMessage(resultJSON),
            })
          }

          clear(failed)
        }

        if runTool(call.Function.Name) && msg == "" {
          ran[key] = raw
        }

        lastCall = key
      }

      if hooks.ToolResult != nil {
        hooks.ToolResult(call.Function.Name, string(resultJSON))
      }

      id := toolIDs[call.Function.Name]
      if l.Aids.Has(AidStuck) && stuck.observe(id, resultJSON) {
        stepBack = true
      }

      *history = append(*history, llm.Message{
        Role:       "tool",
        ToolName:   call.Function.Name,
        ToolCallID: call.ID,
        Content:    string(resultJSON),
      })
    }

    if stepBack {
      note(hooks, "stepping back after repeated failures")
      *history = append(*history, llm.Message{
        Role: "user", Content: stuckNote,
      })
    }

    if blocked >= maxBlocked {
      stopNote = repeatNote
      break
    }
  }

  *history = append(*history, llm.Message{
    Role: "user", Content: stopNote,
  })
  reply, err := client.ChatStream(ctx, *history, nil, stream)
  if err != nil {
    return "", fmt.Errorf(
      "\n\t... in Turn:\n\t stopping the turn (tool-round limit %d or "+
        "repeated calls), and the closing summary call failed: %w",
      l.maxRounds(), err,
    )
  }

  *history = append(*history, *reply)

  return l.finish(history, sources, hooks), nil
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
var argAliases = []string{ "arguments", "argv", "arg", "cmd_args", "params", "flags" }

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
        "error": "the user declined read access to " + dir + " this turn; don't ask again",
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
