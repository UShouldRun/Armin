// One turn of the agent loop: Turn runs a user request from the first
// model call to the final answer; a turn value carries what that takes
// across rounds (the repeat guard's memory, the reasoning aids'
// counters, the web sources to cite).
//
// A round: what the user typed meanwhile goes in (steer), the context
// is kept within the window (manageContext, context.go), the model is
// called with the tools (Definitions, tools.go), and either it
// answered, so the work is checked (verify, aids.go) and the answer
// finished with its sources, or it called tools, so each call runs
// through the guard (call) and its result goes back into history.

package agent

import (
  "context"
  "encoding/json"
  "fmt"

  "armin/internal/llm"
)

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
  "already failed, so tools are off for the rest of this turn. " +
  "Don't call any more tools. Tell the user " +
  "what you were trying to do, what failed, and — if they gave " +
  "feedback — how you'll follow it, then ask how to continue."

// Turn runs one user turn to completion: it sends history, executes
// any tool calls the model makes, feeds results back, and returns
// once the model answers in plain text. history is mutated in place
// so callers can persist it across turns.
func (l *Loop) Turn(
  ctx context.Context, history *[]llm.Message, hooks Hooks,
) (string, error) {
  t := l.newTurn(history, hooks)
  defer func() { l.endTurn(history, t.start, hooks) }()

  defs := l.Definitions()
  stream := llm.StreamHandlers{
    Token: hooks.Token, Reasoning: hooks.Reasoning, Progress: hooks.Progress,
    Drafting: hooks.Drafting,
  }

  var last llm.Usage
  stopNote := roundLimitNote
  l.hintCompact(history, hooks)

  for round := 0; round < l.maxRounds(); round++ {
    // Messages the user sent while the turn ran go in before the
    // model's next step.
    if round > 0 {
      l.steer(history, hooks)
    }

    if l.manageContext(ctx, history, last, hooks) {
      t.start = 0 // indices moved; the end-of-turn pass rescans
      last = llm.Usage{}
    }

    reply, err := t.client.ChatStream(ctx, *history, defs, stream)
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
      if t.recheck(ctx) {
        continue
      }

      answer := l.finish(history, t.sources, hooks)

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

      stepBack = t.call(ctx, call) || stepBack
    }

    if stepBack {
      note(hooks, "stepping back after repeated failures")
      *history = append(*history, llm.Message{
        Role: "user", Content: stuckNote,
      })
    }

    if t.blocked >= maxBlocked {
      stopNote = repeatNote
      break
    }
  }

  // Out of rounds, or stopped for repeating itself: one last call,
  // without tools, so the user gets a report instead of an error.
  *history = append(*history, llm.Message{
    Role: "user", Content: stopNote,
  })
  reply, err := t.client.ChatStream(ctx, *history, nil, stream)
  if err != nil {
    return "", fmt.Errorf(
      "\n\t... in Turn:\n\t stopping the turn (tool-round limit %d or "+
        "repeated calls), and the closing summary call failed: %w",
      l.maxRounds(), err,
    )
  }

  *history = append(*history, *reply)

  return l.finish(history, t.sources, hooks), nil
}

// newTurn starts a turn: the client at the turn's effort (a copy:
// lspserver runs turns concurrently on the same client), the effort
// line in the system prompt and the task's procedure on the request.
func (l *Loop) newTurn(history *[]llm.Message, hooks Hooks) *turn {
  client := l.LLM
  if hooks.Effort != "" {
    c := *l.LLM
    c.Effort = hooks.Effort
    client = &c
  }

  setEffortLine(*history, hooks.Effort)
  l.addProcedure(*history, hooks.Task)

  return &turn{
    l:        l,
    hooks:    hooks,
    client:   client,
    history:  history,
    start:    len(*history),
    sources:  &turnSources{},
    failed:   map[string]string{},
    declined: map[string]bool{},
    ran:      map[string]string{},
  }
}

// recheck is the verify aid: one check of a turn that changed or ran
// something, before its answer stands. True sends the model back for
// another round with what the check found.
func (t *turn) recheck(ctx context.Context) bool {
  h := *t.history
  check := t.l.Aids.Has(AidVerify) && !t.verified && changedOrRan(h, t.start)
  if !check {
    return false
  }

  t.verified = true
  issue := t.l.verify(ctx, h, t.start)
  if issue == "" {
    return false
  }

  note(t.hooks, "checking the work: " + issue)
  *t.history = append(*t.history, llm.Message{
    Role: "user", Content: fmt.Sprintf(verifyNote, issue),
  })

  return true
}

// call runs one tool call through the guard (run), tells the frontend,
// and puts the result in history. True when the stuck aid says the
// model should step back.
func (t *turn) call(ctx context.Context, call llm.ToolCall) bool {
  normalizeArgs(&call)
  if t.hooks.ToolCall != nil {
    t.hooks.ToolCall(call.Function.Name, call.Function.Arguments)
  }

  result := t.run(ctx, call)
  if t.hooks.ToolResult != nil {
    t.hooks.ToolResult(call.Function.Name, string(result))
  }

  *t.history = append(*t.history, llm.Message{
    Role:       "tool",
    ToolName:   call.Function.Name,
    ToolCallID: call.ID,
    Content:    string(result),
  })

  id := toolIDs[call.Function.Name]

  return t.l.Aids.Has(AidStuck) && t.stuck.observe(id, result)
}

// run is the repeat guard around executing a call, returning its
// result as JSON. Small models retry what failed, unchanged; so:
//
//   - a call that failed and is made again, with nothing working in
//     between, is refused (a success in between may have fixed what
//     it depended on, so then it runs)
//   - a program run giving exactly the same output as last time,
//     right after it, is refused as a loop; with other calls in
//     between it runs, with a note that nothing changed
//   - after maxBlocked refusals the turn winds down: the rest of the
//     reply's calls get a result (every call needs one) but don't run
func (t *turn) run(ctx context.Context, call llm.ToolCall) []byte {
  key := callKey(call)
  prev, failedBefore := t.failed[key]
  if failedBefore {
    t.blocked++
    out, _ := json.Marshal(map[string]string{
      "error": "you already made this exact call this turn and it " +
        "failed: " + prev + " Don't repeat it: change the " +
        "arguments, use another tool, or stop and tell the user " +
        "what is blocking you.",
    })

    return out
  }

  if t.blocked >= maxBlocked {
    out, _ := json.Marshal(map[string]string{
      "error": "not run: this turn is stopping",
    })

    return out
  }

  result := t.l.executeTurn(ctx, call, t.hooks, t.sources, t.declined)
  out, err := json.Marshal(result)
  if err != nil {
    out = []byte(fmt.Sprintf(
      `{"error":"failed to encode tool result: %s"}`, err,
    ))
  }

  raw := string(out) // compared across runs, before any note
  msg := errorOf(out)
  isRun := runTool(call.Function.Name)
  if msg != "" {
    t.failed[key] = msg
  } else {
    last, ranBefore := t.ran[key]
    same := ranBefore && last == raw && isRun
    switch {
    case same && t.lastCall == key:
      // Back to back, nothing in between: a loop.
      t.blocked++
      out, _ = json.Marshal(map[string]any{
        "error": "you just ran this exact call and it gave exactly " +
          "the same result (below); running it again won't change " +
          "anything: do something else, or stop and tell the user.",
        "result": json.RawMessage(out),
      })
    case same:
      // Re-running after a rebuild or an edit is legitimate:
      // pointed out, in case the change didn't take effect.
      out, _ = json.Marshal(map[string]any{
        "note": "same output as the last time you ran this, this " +
          "turn: if you changed something in between, it didn't " +
          "change this result",
        "result": json.RawMessage(out),
      })
    }

    clear(t.failed)
  }

  if isRun && msg == "" {
    t.ran[key] = raw
  }

  t.lastCall = key

  return out
}
