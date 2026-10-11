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

// ApproxTokens is a rough token count for one message: content,
// reasoning and tool-call arguments.
func ApproxTokens(m llm.Message) int {
  n := len(m.Content) + len(m.Reasoning) + len(m.ToolName)
  for _, c := range m.ToolCalls {
    n += len(c.Function.Name)
    for k, v := range c.Function.Arguments {
      n += len(k)
      if s, ok := v.(string); ok {
        n += len(s)
      } else {
        n += 8
      }
    }
  }

  return n / charsPerToken
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

    s.ApproxTokens += ApproxTokens(m)
  }

  return s
}

// compactPrompt asks the model to reduce everything before the kept
// tail into a short brief the conversation can continue from.
const compactPrompt = `Summarize the conversation above into a brief for yourself to continue from:
- what the user wants, in their words where it matters
- decisions made and why
- files changed or created, and their state (done / half-done / verified)
- key facts found about the codebase, as pointers (file:line, symbol names), not content
- what is still open or next
Point to scratchpad files and memories by name instead of restating them. Bullet points, no preamble.`

// summaryHeader marks the message that replaces compacted history.
const summaryHeader = "[Summary of the earlier conversation, " +
  "compacted to save context]\n"

// maxSummaryInputBytes caps each tool result fed to the summary
// call, so summarizing an over-full history can't overflow too.
const maxSummaryInputBytes = 1500

// Compact replaces every message before the last keepLast with a
// single model-written summary. The system prompt (history[0]) is
// kept; the summary goes in as a user message, since chat templates
// like Qwen's reject a system message anywhere but first. The cut
// never lands between a tool call and its result, and if the
// user's latest request falls inside the compacted part it's
// carried over verbatim. extra (e.g. a scratchpad listing) is
// appended to the summary as-is.
func Compact(
  ctx context.Context, client *llm.Client,
  history *[]llm.Message, keepLast int, extra string,
) error {
  h := *history
  cut := len(h) - keepLast
  for cut > 1 && h[cut].Role == "tool" {
    cut--
  }

  if cut <= 1 || len(h) <= keepLast + 1 {
    return fmt.Errorf(
      "\n\t... in Compact:\n\t nothing to compact yet — only %d "+
        "messages, keeping the last %d",
      len(h), keepLast,
    )
  }

  system := h[0]
  head := h[1:cut]
  tail := h[cut:]

  ask := make([]llm.Message, 0, len(head) + 2)
  ask = append(ask, system)
  for _, m := range head {
    m.Reasoning = ""
    if m.Role == "tool" && len(m.Content) > maxSummaryInputBytes {
      m.Content = m.Content[:maxSummaryInputBytes] + "…"
    }

    ask = append(ask, m)
  }

  ask = append(ask, llm.Message{ Role: "user", Content: compactPrompt })

  reply, err := client.Chat(ctx, ask, nil, nil)
  if err != nil {
    return fmt.Errorf("\n\t... in Compact:\n\t%w", err)
  }

  var note strings.Builder
  note.WriteString(summaryHeader)
  note.WriteString(strings.TrimSpace(reply.Content))
  if req := lastUserIn(head); req != "" && lastUserIn(tail) == "" {
    note.WriteString("\n\nThe user's current request, verbatim:\n")
    note.WriteString(req)
  }

  if extra != "" {
    note.WriteString("\n\n")
    note.WriteString(extra)
  }

  compacted := make([]llm.Message, 0, 2 + len(tail))
  compacted = append(compacted, system)
  compacted = append(compacted, llm.Message{
    Role: "user", Content: note.String(),
  })
  compacted = append(compacted, tail...)

  *history = compacted

  return nil
}

// lastUserIn returns the content of the last real user message in
// msgs (not an earlier summary).
func lastUserIn(msgs []llm.Message) string {
  for i := len(msgs) - 1; i >= 0; i-- {
    m := msgs[i]
    if m.Role == "user" && !strings.HasPrefix(m.Content, summaryHeader) {
      return m.Content
    }
  }

  return ""
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
    line = line[i + 1:]
  case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* "):
    line = line[2:]
  }

  return strings.TrimSpace(line)
}

// titlePrompt asks for a session name for the resume menu.
const titlePrompt = "Give this conversation a short title, 3 to 6 words, " +
  "naming the task (e.g. \"Fix off-by-one in Mean\"). Reply with the " +
  "title only: no quotes, no punctuation at the end."

// maxTitle caps a title's length.
const maxTitle = 60

// Title asks the model to name the session. It runs on a copy, so
// the exchange never enters history, and falls back to the first
// words of the first request when the reply is unusable (a thinking
// model can answer only in its reasoning).
func Title(ctx context.Context, client *llm.Client, history []llm.Message) string {
  ask := make([]llm.Message, len(history), len(history) + 1)
  copy(ask, history)
  ask = append(ask, llm.Message{ Role: "user", Content: titlePrompt })

  reply, err := client.Chat(ctx, ask, nil, nil)
  if err == nil {
    if t := cleanTitle(reply.Content); t != "" {
      return t
    }
  }

  return fallbackTitle(history)
}

func cleanTitle(s string) string {
  s = strings.TrimSpace(s)
  if i := strings.IndexByte(s, '\n'); i >= 0 {
    s = s[:i]
  }

  s = strings.Trim(s, " \"'`*#.:")
  s = strings.TrimPrefix(s, "Title: ")
  if len(s) > maxTitle {
    s = strings.TrimSpace(s[:maxTitle]) + "…"
  }

  return s
}

func fallbackTitle(history []llm.Message) string {
  for _, m := range history {
    if m.Role != "user" {
      continue
    }

    words := strings.Fields(m.Content)
    if len(words) > 6 {
      words = words[:6]
    }

    return cleanTitle(strings.Join(words, " "))
  }

  return "untitled"
}
