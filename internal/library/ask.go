package library

import (
  "context"
  "fmt"
  "strings"

  "armin/internal/llm"
)

// AskWith adapts a chat client to Ask. Cataloging is chore work: a
// hosted reasoning model answers with its thinking off.
func AskWith(c *llm.Client) Ask {
  // one copy for the whole run, so what it learns (a regional
  // endpoint refusing it) sticks
  chore := c.Chore()

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
