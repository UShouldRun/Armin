// Effort: how hard the model thinks on a task. ParseEffort and Wire
// translate levels; Assess asks the model to judge a request (effort
// and kind of task) and PlanTurn decides when to ask.

package agent

import (
  "context"
  "strings"
  "time"

  "armin/internal/llm"
)

// Effort levels. The user picks one, or EffortAuto lets the model
// judge each request first. EffortDefault leaves the model alone.
const (
  EffortDefault Effort = iota
  EffortAuto
  EffortOff
  EffortLow
  EffortMedium
  EffortHigh
)

// effortNames are the levels as typed (-effort, /effort, effort =).
var effortNames = [...]string{
  EffortDefault: "default",
  EffortAuto:    "auto",
  EffortOff:     "off",
  EffortLow:     "low",
  EffortMedium:  "medium",
  EffortHigh:    "high",
}

// EffortNames lists the levels a user can pick.
const EffortNames = "auto, off, low, medium, high"

// chooseTimeout bounds auto's question; past it the turn goes on at
// medium.
const chooseTimeout = 30 * time.Second

const effortPrompt = `Classify the user's request to a coding assistant. Reply with exactly two words: the effort, then the kind.

Effort, how much step-by-step thinking it needs before acting:
low: a greeting, a lookup, a one-line answer, a small mechanical edit, running a command
medium: a typical bug fix, feature or explanation that needs reading some code
high: a hard or open-ended problem: an unknown root cause, a subtle algorithm, concurrency, a design decision, a proof, many files at once

Kind: fix (make failing code work), build (write something new), debug (find why something misbehaves), explain (answer a question about code or a concept), shell (commands, files, the system), other.`

// ParseEffort reads a level by name.
func ParseEffort(s string) (Effort, bool) {
  for e, name := range effortNames {
    if e != int(EffortDefault) && name == s {
      return Effort(e), true
    }
  }

  return EffortDefault, false
}

func (e Effort) String() string { return effortNames[e] }

// Wire is the client effort for a level: "" keeps the model's own
// default, off becomes "none".
func (e Effort) Wire() string {
  switch e {
  case EffortOff:
    return "none"
  case EffortLow, EffortMedium, EffortHigh:
    return e.String()
  }

  return ""
}

// Assess asks the model, with its thinking off, how much a request
// needs (low, medium or high) and what kind of task it is. Anything
// unexpected is medium and other.
func Assess(
  ctx context.Context, c *llm.Client, request string,
) (Effort, Task) {
  if len(strings.Fields(request)) <= 3 {
    return EffortLow, TaskOther // "thanks", "go on", "run the tests"
  }

  if len(request) > 4000 {
    request = request[:4000]
  }

  ctx, cancel := context.WithTimeout(ctx, chooseTimeout)
  defer cancel()

  quick := *c
  quick.Effort = "none"
  reply, err := quick.Chat(ctx, []llm.Message{
    { Role: "system", Content: effortPrompt },
    { Role: "user", Content: request },
  }, nil, nil)
  if err != nil {
    return EffortMedium, TaskOther
  }

  return parseAssessment(reply.Content)
}

// parseAssessment reads "high debug", forgiving case and punctuation.
func parseAssessment(reply string) (Effort, Task) {
  effort, task := EffortMedium, TaskOther
  words := strings.FieldsFunc(strings.ToLower(reply), func(r rune) bool {
    return r < 'a' || r > 'z'
  })

  for _, w := range words {
    for _, e := range []Effort{ EffortLow, EffortMedium, EffortHigh } {
      if w == e.String() {
        effort = e
      }
    }

    for t := TaskFix; t <= TaskShell; t++ {
      if w == t.String() {
        task = t
      }
    }
  }

  return effort, task
}

// PlanTurn settles a turn's effort and kind of task. The model is
// asked (Assess) when effort is auto, or when the procedure aid needs
// the kind; otherwise effort stays as set and the kind is other.
func PlanTurn(
  ctx context.Context, c *llm.Client, aids Aids, effort Effort,
  request string,
) (Effort, Task) {
  auto := effort == EffortAuto
  if !auto && !aids.Has(AidProcedure) {
    return effort, TaskOther
  }

  judged, task := Assess(ctx, c, request)
  if auto {
    effort = judged
  }

  return effort, task
}
