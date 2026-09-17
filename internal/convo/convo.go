// Package convo tracks and manages conversation state that isn't
// the LLM/tool round trip itself: rough context-usage accounting
// and compaction — the "/context" and "/compact" half of the
// Claude-Code-style UX the REPL offers (plan Phase 4 usability
// polish).
package convo

import (
  "context"
  "fmt"
  "strings"

  "armin/internal/llm"
)

// charsPerToken is a rough, model-agnostic estimate (English text
// averages ~4 chars/token). Good enough for a budget indicator; not
// a real tokenizer, and we say so wherever this number is shown.
const charsPerToken = 4

// Stats summarizes a history slice for the /context command.
type Stats struct {
  Messages, ToolCalls, ToolResults, ApproxTokens int
}

// Analyze walks history once and returns a Stats summary.
func Analyze(history []llm.Message) Stats {
  var s Stats

  s.Messages = len(history)
  for _, m := range history {
    s.ToolCalls += len(m.ToolCalls)
    if m.Role == "tool" {
      s.ToolResults++
    }
    s.ApproxTokens += (len(m.Content) + len(m.ToolName)) / charsPerToken
  }

  return s
}

// compactPrompt asks the model to reduce everything before the kept
// tail into a short brief the conversation can continue from.
const compactPrompt = `Summarize the conversation above into a short brief for yourself to continue from: what the user is trying to accomplish, what you've already found out about the codebase (files, structure, relevant facts), and anything still open. Be concise — bullet points, no preamble.`

// Compact replaces every message before the last keepLast with a
// single model-generated summary, mirroring Claude Code's
// /compact: it trades detail for context headroom once a session
// runs long. The system prompt (history[0], assumed role:"system")
// is always preserved.
func Compact(
  ctx context.Context, client *llm.Client,
  history *[]llm.Message, keepLast int,
) error {
  h := *history
  if len(h) <= keepLast+1 {
    return fmt.Errorf(
      "\n\t... in Compact:\n\t nothing to compact yet — only %d "+
        "messages, keeping the last %d",
      len(h), keepLast,
    )
  }

  system := h[0]
  head := h[1 : len(h)-keepLast]
  tail := h[len(h)-keepLast:]

  summarized := append(
    append([]llm.Message{}, head...),
    llm.Message{ Role: "user", Content: compactPrompt },
  )

  reply, err := client.Chat(ctx, summarized, nil, nil)
  if err != nil {
    return fmt.Errorf("\n\t... in Compact:\n\t%w", err)
  }

  summaryNote := "Summary of earlier conversation " +
    "(compacted to save context):\n" + reply.Content

  compacted := make([]llm.Message, 0, 2+len(tail))
  compacted = append(compacted, system)
  compacted = append(compacted, llm.Message{
    Role: "system", Content: summaryNote,
  })
  compacted = append(compacted, tail...)

  *history = compacted
  return nil
}

// maxSuggestions caps how many follow-up questions Suggest returns.
const maxSuggestions = 3

// suggestPrompt asks for short follow-ups, one per line, so parsing
// is just a split on newlines plus stripping any list marker a
// model adds despite being asked not to.
const suggestPrompt = `Based on the conversation so far, suggest exactly 3 short, specific follow-up questions the user might want to ask next about this codebase. One per line, no numbering, no preamble, no extra commentary.`

// Suggest asks the model for a short list of follow-up questions
// the user might want to ask next, grounded in the conversation so
// far — the "suggested next steps" half of the Claude-Code-style
// UX, alongside Compact's /compact. A malformed or oddly formatted
// reply just yields fewer (or zero) suggestions rather than an
// error: this is a nice-to-have, not worth interrupting the REPL
// over.
func Suggest(
  ctx context.Context, client *llm.Client, history []llm.Message,
) ([]string, error) {
  ask := append(
    append([]llm.Message{}, history...),
    llm.Message{ Role: "user", Content: suggestPrompt },
  )

  reply, err := client.Chat(ctx, ask, nil, nil)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Suggest:\n\t%w", err)
  }

  return parseSuggestions(reply.Content), nil
}

func parseSuggestions(raw string) []string {
  var out []string
  for _, line := range strings.Split(raw, "\n") {
    line = stripListMarker(strings.TrimSpace(line))
    if line == "" {
      continue
    }
    out = append(out, line)
    if len(out) == maxSuggestions {
      break
    }
  }
  return out
}

// stripListMarker removes a leading "1.", "2)", "-", or "*" a model
// might add despite being asked for one plain line per suggestion.
// A digit prefix is only treated as a list marker when followed by
// "." or ")" — "3D rendering..." must not lose its "3".
func stripListMarker(line string) string {
  i := 0
  for i < len(line) && line[i] >= '0' && line[i] <= '9' {
    i++
  }
  switch {
  case i > 0 && i < len(line) && (line[i] == '.' || line[i] == ')'):
    line = line[i+1:]
  case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* "):
    line = line[2:]
  }
  return strings.TrimSpace(line)
}
