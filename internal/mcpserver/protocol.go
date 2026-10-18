// The JSON-RPC error codes the server replies with.

package mcpserver

// protocolVersion is the MCP spec revision this server speaks.
const protocolVersion = "2025-06-18"

// Error codes actually emitted — the rest of the JSON-RPC space is
// unused here.
const (
  errInvalidRequest = -32600
  errMethodNotFound = -32601
  errInvalidParams  = -32602
)

