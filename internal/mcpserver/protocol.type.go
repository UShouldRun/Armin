// JSON-RPC and MCP message shapes, only the fields armin uses.

package mcpserver

import (
  "encoding/json"

  "armin/internal/llm"
)

// rpcMessage covers every shape an incoming JSON-RPC frame can take —
// request or notification — since both share the same wire format
// here, distinguished only by whether ID is present.
type rpcMessage struct {
  JSONRPC string          `json:"jsonrpc"`
  ID      json.RawMessage `json:"id,omitempty"`
  Method  string          `json:"method,omitempty"`
  Params  json.RawMessage `json:"params,omitempty"`
}

// rpcResponse is what the server sends back. Result has no
// "omitempty" — a nil result must still marshal as an explicit
// "result": null, not vanish from the object.
type rpcResponse struct {
  JSONRPC string          `json:"jsonrpc"`
  ID      json.RawMessage `json:"id"`
  Result  any             `json:"result"`
  Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
  Code    int    `json:"code"`
  Message string `json:"message"`
}

type implementation struct {
  Name    string `json:"name"`
  Version string `json:"version"`
}

type toolCapabilities struct {
  ListChanged bool `json:"listChanged"`
}

type serverCapabilities struct {
  Tools toolCapabilities `json:"tools"`
}

type initializeResult struct {
  ProtocolVersion string             `json:"protocolVersion"`
  Capabilities    serverCapabilities `json:"capabilities"`
  ServerInfo      implementation     `json:"serverInfo"`
}

// tool mirrors MCP's Tool shape. InputSchema reuses
// llm.ToolParameters directly: its JSON tags
// (type/properties/required) already form a valid JSON Schema
// object — the very same one sent to the model for tool calling — so
// each tool's parameter shape is defined exactly once, in
// agent.ToolDefinitions, not duplicated here.
type tool struct {
  Name        string             `json:"name"`
  Description string             `json:"description"`
  InputSchema llm.ToolParameters `json:"inputSchema"`
}

type listToolsResult struct {
  Tools []tool `json:"tools"`
}

type callToolParams struct {
  Name      string          `json:"name"`
  Arguments json.RawMessage `json:"arguments"`
}

type textContent struct {
  Type string `json:"type"`
  Text string `json:"text"`
}

type callToolResult struct {
  Content []textContent `json:"content"`
  IsError bool          `json:"isError,omitempty"`
}
