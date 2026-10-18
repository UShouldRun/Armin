// Package mcpserver serves armin's sandboxed tools (the agent's tool
// table, see agent/tools.go) over MCP, so other MCP clients (Claude
// Desktop, etc.) can use them. No SDK: MCP-
// over-stdio is JSON-RPC 2.0 on
// newline-delimited JSON, and the surface armin actually needs
// (initialize, tools/list, tools/call) is small enough to implement
// directly — the same reasoning as internal/lspserver hand-rolling
// its own JSON-RPC transport instead of pulling in an LSP framework.
package mcpserver

import (
  "bufio"
  "context"
  "encoding/json"
  "errors"
  "fmt"
  "io"
  "os"

  "armin/internal/agent"
  "armin/internal/ctags"
  "armin/internal/sandbox"
  "armin/internal/searxng"
  "armin/internal/tools"
)

const serverVersion = "v0.1.0"

// New builds a Server around sb — the same sandbox.Config the REPL
// and agent loop use, so path jail, allowlist, and truncation apply
// identically regardless of which front end is calling the tools.
// ws may be nil (docker not found at startup) and symbols may be nil
// (no universal-ctags binary found — see cmd/armin/main.go); either
// way the affected tools are still listed but report a clean
// "unavailable" error instead of a panic.
func New(
  sb *sandbox.Config, ws *searxng.Manager, symbols *ctags.Index,
) *Server {
  loop := agent.New(nil, sb)
  loop.WebSearch, loop.Symbols = ws, symbols

  return &Server{ sandbox: sb, loop: loop }
}

// Serve is the convenience entry point main.go calls for -mcp: an
// armin Server wired to a fresh sandbox, talking over stdio.
func Serve(
  ctx context.Context, sb *sandbox.Config, ws *searxng.Manager,
  symbols *ctags.Index,
) error {
  return New(sb, ws, symbols).Serve(ctx, os.Stdin, os.Stdout)
}

// Serve reads newline-delimited JSON-RPC from in and writes
// responses to out until in reaches EOF.
func (s *Server) Serve(
  ctx context.Context, in io.Reader, out io.Writer,
) error {
  s.out = out

  scanner := bufio.NewScanner(in)
  scanner.Buffer(make([]byte, 0, 64 * 1024), 8 * 1024 * 1024)

  for scanner.Scan() {
    line := scanner.Bytes()
    if len(line) == 0 {
      continue
    }

    var msg rpcMessage
    err := json.Unmarshal(line, &msg)
    if err != nil {
      continue // malformed frame — nothing sane to respond with
    }

    s.wg.Add(1)
    go func(msg rpcMessage) {
      defer s.wg.Done()
      s.handle(ctx, msg)
    }(msg)
  }

  s.wg.Wait()

  err := scanner.Err()
  if err != nil {
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
    err := json.Unmarshal(msg.Params, &p)
    if err != nil {
      s.respondError(msg.ID, errInvalidParams, err.Error())
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

// toolList is the agent's sandboxed tools that MCP can run: their
// exact schemas, from the same table the model gets.
func toolList() []tool {
  defs := agent.ToolDefinitions()
  list := make([]tool, 0, len(defs))
  for _, d := range defs {
    name := d.Function.Name
    if !agent.Served(name) && !direct[name] {
      continue
    }

    list = append(list, tool{
      Name:        name,
      Description: d.Function.Description,
      InputSchema: d.Function.Parameters,
    })
  }

  return list
}

// direct are the writes MCP does itself: straight to disk, with no
// read-before-write tracking and no approval, which the MCP client
// owns. Everything else runs through the agent's tool table.
var direct = map[string]bool{ "write_file": true, "edit_file": true }

// callTool runs one tool call and shapes its result for MCP.
func (s *Server) callTool(
  ctx context.Context, p callToolParams,
) callToolResult {
  switch p.Name {
  case "write_file":
    var in tools.WriteFileInput
    err := json.Unmarshal(p.Arguments, &in)
    if err != nil {
      return errorResult(err)
    }

    out, err := tools.WriteFile(s.sandbox, nil, in)
    if err != nil {
      return errorResult(err)
    }

    return jsonResult(out)

  case "edit_file":
    var in tools.EditFileInput
    err := json.Unmarshal(p.Arguments, &in)
    if err != nil {
      return errorResult(err)
    }

    out, err := tools.EditFile(s.sandbox, nil, in)
    if err != nil {
      return errorResult(err)
    }

    return jsonResult(out)
  }

  if !agent.Served(p.Name) {
    return errorResult(fmt.Errorf("unknown tool %q", p.Name))
  }

  return toolResult(s.loop.RunTool(ctx, p.Name, p.Arguments))
}

// toolResult turns a tool's result into MCP's: a plain error as
// text, a structured one (run_command's, with the allowlist) as JSON,
// both flagged IsError so the client knows the call failed.
func toolResult(v any) callToolResult {
  switch r := v.(type) {
  case map[string]string:
    msg, failed := r["error"]
    if failed && len(r) == 1 {
      return errorResult(errors.New(msg))
    }

  case *tools.RunCommandError:
    return jsonErrorResult(r)
  }

  return jsonResult(v)
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

  s.writeMu.Lock()
  defer s.writeMu.Unlock()
  _, err = s.out.Write(body)
  if err != nil {
    fmt.Fprintln(os.Stderr, "armin mcp: write failed:", err)
  }
}
