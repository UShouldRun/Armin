// The types of convo.go.

package convo

// Stats summarizes a history slice for the /context command.
type Stats struct {
  Messages, ToolCalls, ToolResults, ApproxTokens int
}
