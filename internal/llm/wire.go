// The OpenAI-compatible wire format: armin's Message to and from the
// JSON llama-server and hosted APIs speak, and how an effort reaches
// a local model's chat template.

package llm

import (
  "encoding/json"
  "fmt"
)

// toWireMessages translates outgoing history to the OpenAI shape —
// each ToolCall's map[string]any Arguments becomes a JSON-encoded
// string, and ToolCallID is carried straight through so a
// role:"tool" message stays matched to the call that produced it.
func toWireMessages(msgs []Message) []owaiMessage {
  out := make([]owaiMessage, len(msgs))
  for i, m := range msgs {
    wm := owaiMessage{
      Role:             m.Role,
      Content:          m.Content,
      ReasoningContent: m.Reasoning,
      ToolCallID:       m.ToolCallID,
    }

    for _, tc := range m.ToolCalls {
      args, err := json.Marshal(tc.Function.Arguments)
      if err != nil {
        args = []byte("{}")
      }

      wm.ToolCalls = append(wm.ToolCalls, owaiToolCall{
        ID: tc.ID, Type: "function",
        Function: owaiToolCallFunc{
          Name: tc.Function.Name, Arguments: string(args),
        },
      })
    }

    out[i] = wm
  }

  return out
}

// fromWireMessage translates one incoming OpenAI-shaped message
// back to armin's own Message — parsing each tool call's
// JSON-encoded arguments string back into a map, and synthesizing a
// positional ID ("call_0", "call_1", ...) for any tool call the
// model didn't supply one for.
func fromWireMessage(wm owaiMessage) (Message, error) {
  role := wm.Role
  if role == "" {
    role = "assistant"
  }

  m := Message{
    Role:      role,
    Content:   wm.Content,
    Reasoning: wm.ReasoningContent,
  }

  for i, tc := range wm.ToolCalls {
    var args map[string]any
    if tc.Function.Arguments != "" {
      err := json.Unmarshal([]byte(tc.Function.Arguments), &args)
      if err != nil {
        return Message{}, fmt.Errorf(
          "parsing tool call arguments for %q: %w",
          tc.Function.Name, err,
        )
      }
    }

    id := tc.ID
    if id == "" {
      id = fmt.Sprintf("call_%d", i)
    }

    m.ToolCalls = append(m.ToolCalls, ToolCall{
      ID: id,
      Function: ToolCallFunction{
        Name: tc.Function.Name, Arguments: args,
      },
    })
  }

  return m, nil
}

// thinkBudgets cap thinking tokens per effort on a local model; high
// is unbounded.
var thinkBudgets = map[string]int{
  "none":    0,
  "minimal": 0,
  "low":     1024,
  "medium":  4096,
}

// localEffort is how an effort reaches llama-server: thinking on or
// off for templates that switch it (Qwen's enable_thinking), and a
// budget of thinking tokens, which works whatever the template.
//
// Not the template's own reasoning_effort: K2 Horizon's opens a
// different thinking tag per level (<ifm|think_fast> for medium), and
// llama.cpp only knows the high one's, so the reply came back empty.
func localEffort(effort string) (map[string]any, *int) {
  kwargs := map[string]any{
    "enable_thinking": effort != "none" && effort != "minimal",
  }

  budget, ok := thinkBudgets[effort]
  if !ok {
    return kwargs, nil
  }

  return kwargs, &budget
}
