// Package mcpserver exposes the same four sandboxed tools the REPL
// uses directly — search_code, read_file, list_directory,
// run_command — over MCP, so other MCP clients (Claude Desktop,
// etc.) can reuse them. No SDK: MCP-over-stdio is JSON-RPC 2.0 on
// newline-delimited JSON, and the surface armin actually needs
// (initialize, tools/list, tools/call) is small enough to implement
// directly — the same reasoning as internal/lspserver hand-rolling
// its own JSON-RPC transport instead of pulling in an LSP framework.
package mcpserver

import (
  "bufio"
  "context"
  "encoding/json"
  "fmt"
  "io"
  "os"

  "armin/internal/agent"
  "armin/internal/sandbox"
  "armin/internal/tools"
)

const serverVersion = "v0.1.0"

// Server is a minimal MCP server. All requests are handled
// synchronously — every tool here is fast, local, sandboxed I/O,
// no LLM call in the loop, so there's no need for the per-request
// goroutines and cancellation bookkeeping internal/lspserver needs.
type Server struct {
  sandbox *sandbox.Config
  out     io.Writer
}

// New builds a Server around sb — the same sandbox.Config the REPL
// and agent loop use, so path jail, allowlist, and truncation apply
// identically regardless of which front end is calling the tools.
func New(sb *sandbox.Config) *Server {
  return &Server{ sandbox: sb }
}

// Serve is the convenience entry point main.go calls for -mcp: an
// armin Server wired to a fresh sandbox, talking over stdio.
func Serve(ctx context.Context, sb *sandbox.Config) error {
  return New(sb).Serve(ctx, os.Stdin, os.Stdout)
}

// Serve reads newline-delimited JSON-RPC from in and writes
// responses to out until in reaches EOF.
func (s *Server) Serve(
  ctx context.Context, in io.Reader, out io.Writer,
) error {
  s.out = out

  scanner := bufio.NewScanner(in)
  scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

  for scanner.Scan() {
    line := scanner.Bytes()
    if len(line) == 0 {
      continue
    }

    var msg rpcMessage
    if err := json.Unmarshal(line, &msg); err != nil {
      continue // malformed frame — nothing sane to respond with
    }
    s.handle(ctx, msg)
  }

  if err := scanner.Err(); err != nil {
    return fmt.Errorf("\n\t... in Serve:\n\t%w", err)
  }
  return nil
}

func (s *Server) handle(ctx context.Context, msg rpcMessage) {
  switch msg.Method {
  case "initialize":
    s.respond(msg.ID, initializeResult{
      ProtocolVersion: protocolVersion,
      Capabilities: serverCapabilities{
        Tools: toolCapabilities{},
      },
      ServerInfo: implementation{
        Name: "armin", Version: serverVersion,
      },
    }, nil)

  case "notifications/initialized":
    // Notification — nothing to do.

  case "ping":
    s.respond(msg.ID, struct{}{}, nil)

  case "tools/list":
    s.respond(msg.ID, listToolsResult{ Tools: toolList() }, nil)

  case "tools/call":
    var p callToolParams
    if err := json.Unmarshal(msg.Params, &p); err != nil {
      s.respondError(msg.ID, errInvalidRequest, err.Error())
      return
    }
    s.respond(msg.ID, s.callTool(ctx, p), nil)

  default:
    if len(msg.ID) > 0 {
      s.respondError(
        msg.ID, errMethodNotFound,
        fmt.Sprintf("method not supported: %s", msg.Method),
      )
    }
    // Unhandled notifications are silently ignored, per spec.
  }
}

// toolList builds MCP's tool list straight from
// agent.ToolDefinitions — the exact schema Ollama gets for tool
// calling, reused rather than redefined.
func toolList() []tool {
  defs := agent.ToolDefinitions()
  list := make([]tool, 0, len(defs))
  for _, d := range defs {
    list = append(list, tool{
      Name:        d.Function.Name,
      Description: d.Function.Description,
      InputSchema: d.Function.Parameters,
    })
  }
  return list
}

// callTool dispatches to the same tools.* functions
// agent.Loop.execute calls for the REPL — duplicated rather than
// shared, since the two callers need different result shapes
// (Ollama's tool_calls JSON vs. MCP's CallToolResult) and each
// dispatch is small enough on its own that sharing it would cost
// more in indirection than it saves.
func (s *Server) callTool(
  ctx context.Context, p callToolParams,
) callToolResult {
  switch p.Name {
  case "search_code":
    var in tools.SearchCodeInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.SearchCode(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "read_file":
    var in tools.ReadFileInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.ReadFile(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "list_directory":
    var in tools.ListDirectoryInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.ListDirectory(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "run_command":
    var in tools.RunCommandInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, cmdErr := tools.RunCommand(ctx, s.sandbox, in)
    if cmdErr != nil {
      return jsonErrorResult(cmdErr)
    }
    return jsonResult(out)

  default:
    return errorResult(fmt.Errorf("unknown tool %q", p.Name))
  }
}

func jsonResult(v any) callToolResult {
  encoded, err := json.Marshal(v)
  if err != nil {
    return errorResult(err)
  }
  return callToolResult{
    Content: []textContent{
      { Type: "text", Text: string(encoded) },
    },
  }
}

func errorResult(err error) callToolResult {
  return callToolResult{
    Content: []textContent{ { Type: "text", Text: err.Error() } },
    IsError: true,
  }
}

// jsonErrorResult is jsonResult with IsError forced true — for
// structured (not just plain-string) tool failures like
// RunCommandError, which still carries useful detail (e.g. the
// allowlist) worth keeping as JSON rather than flattening to text.
func jsonErrorResult(v any) callToolResult {
  r := jsonResult(v)
  r.IsError = true
  return r
}

func (s *Server) respond(id json.RawMessage, result any, errObj *rpcError) {
  if len(id) == 0 {
    return // notification — nothing to respond to
  }
  s.write(rpcResponse{
    JSONRPC: "2.0", ID: id, Result: result, Error: errObj,
  })
}

func (s *Server) respondError(id json.RawMessage, code int, message string) {
  s.respond(id, nil, &rpcError{ Code: code, Message: message })
}

func (s *Server) write(msg rpcResponse) {
  body, err := json.Marshal(msg)
  if err != nil {
    fmt.Fprintln(os.Stderr, "armin mcp: encode failed:", err)
    return
  }
  body = append(body, '\n')
  if _, err := s.out.Write(body); err != nil {
    fmt.Fprintln(os.Stderr, "armin mcp: write failed:", err)
  }
}
