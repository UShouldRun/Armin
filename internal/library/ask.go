package library

import (
  "context"
  "fmt"
  "strings"

  "armin/internal/llm"
)

// choreTokens caps one reply: a merge of ~150 topics is about 3k
// tokens, a book's catalog far less.
const choreTokens = 8192

// AskWith adapts a chat client to Ask. Cataloging is list work that
// thinking doesn't improve, so every model answers with it off, local
// ones too (Chore leaves those as they are), and with a cap: a small
// model thinking unbounded over 150 topics ran past 10k tokens.
func AskWith(c *llm.Client) Ask {
  // one copy for the whole run, so what it learns (a regional
  // endpoint refusing it) sticks
  chore := *c.Chore()
  chore.Effort, chore.MaxTokens = "none", choreTokens

  return func(ctx context.Context, system, user string) (string, error) {
    msg, err := chore.Chat(ctx, []llm.Message{
      { Role: "system", Content: system },
      { Role: "user", Content: user },
    }, nil, nil)
    if err != nil {
      return "", fmt.Errorf("\n\t... in AskWith:\n\t%w", err)
    }

    return strings.TrimSpace(msg.Content), nil
  }
}
