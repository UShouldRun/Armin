// What frontends display: Lines turns a history into chat lines (the
// questions, tool calls and answers, not the raw messages), Question
// marks a question asked from a file, Suggest fills in model picks.

package share

import (
  "strings"

  "armin/internal/agent"
  "armin/internal/catalog"
  "armin/internal/llm"
)

// Lines is a conversation as the frontends display it: questions
// without the "Currently editing" line, answers, and one line per
// tool call.
func Lines(msgs []llm.Message) []ChatLine {
  out := []ChatLine{}
  for _, m := range msgs {
    switch m.Role {
    case "user":
      text := m.Content
      if strings.HasPrefix(text, EditingPrefix) {
        i := strings.Index(text, "\n\n")
        if i >= 0 {
          text = text[i + 2:]
        }
      }

      out = append(out, ChatLine{ Role: "user", Text: text })

    case "assistant":
      for _, call := range m.ToolCalls {
        out = append(out, ChatLine{
          Role: "tool",
          Text: agent.CallLine(call.Function.Name, call.Function.Arguments),
        })
      }

      t := strings.TrimSpace(m.Content)
      if t != "" {
        out = append(out, ChatLine{ Role: "assistant", Text: t })
      }
    }
  }

  return out
}

// Question is what a host puts in its history for a question asked
// from file (empty when none).
func Question(question, file string) string {
  if file == "" {
    return question
  }

  return EditingPrefix + file + "\n\n" + question
}

// Suggest fills in, when hosted names a configured OpenRouter model:
// the models worth trying, each hosted model's price, and what the
// account has left. url and key are hosted's (see Manager.Endpoint).
func Suggest(info *ModelInfo, hosted, url, key string) {
  if hosted == "" {
    return
  }

  info.Hosted = hosted
  info.Suggested = catalog.Suggestions()
  for i, c := range info.Configured {
    cost, ok := catalog.Price(c.Model)
    if ok && !c.Managed {
      info.Configured[i].Cost = &cost
    }
  }

  info.Balance = catalog.CachedCredits(url, key)
}
