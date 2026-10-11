// Package mcpserver exposes the same sandboxed tools the REPL uses
// directly — search_code, read_file, list_directory, run_command,
// web_search, and 29 others across repo stats, symbol navigation,
// git history, build/test, dependencies, docs, call graph, and
// semantic search (see agent.ToolDefinitions) — over MCP, so other
// MCP clients (Claude Desktop, etc.) can reuse them. No SDK: MCP-
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
  "fmt"
  "io"
  "os"
  "sync"

  "armin/internal/agent"
  "armin/internal/ctags"
  "armin/internal/sandbox"
  "armin/internal/searxng"
  "armin/internal/tools"
)

const serverVersion = "v0.1.0"

// Server is a minimal MCP server. Each request is handled in its own
// goroutine — most tools here are fast, local, sandboxed I/O, but a
// few (build_check, run_tests, web_search's first-call Docker
// startup) can run long enough to block the stdio read loop; without
// this, one slow call would head-of-line-block every other pending
// request, including a trivial ping. Responses race to the writer in
// whatever order they finish, which JSON-RPC allows since callers
// match responses to requests by id, not by arrival order — writeMu
// only keeps two responses from interleaving mid-write, and wg lets
// Serve drain in-flight goroutines before returning at EOF.
type Server struct {
  sandbox   *sandbox.Config
  webSearch *searxng.Manager
  symbols   *ctags.Index
  out       io.Writer
  writeMu   sync.Mutex
  wg        sync.WaitGroup
}

// New builds a Server around sb — the same sandbox.Config the REPL
// and agent loop use, so path jail, allowlist, and truncation apply
// identically regardless of which front end is calling the tools.
// ws may be nil (docker not found at startup) and symbols may be nil
// (no universal-ctags binary found — see cmd/armin/main.go); either
// way the affected tools are still listed but report a clean
// "unavailable" error instead of a panic.
func New(sb *sandbox.Config, ws *searxng.Manager, symbols *ctags.Index) *Server {
  return &Server{ sandbox: sb, webSearch: ws, symbols: symbols }
}

// Serve is the convenience entry point main.go calls for -mcp: an
// armin Server wired to a fresh sandbox, talking over stdio.
func Serve(
  ctx context.Context, sb *sandbox.Config, ws *searxng.Manager, symbols *ctags.Index,
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
    if err := json.Unmarshal(line, &msg); err != nil {
      continue // malformed frame — nothing sane to respond with
    }

    s.wg.Add(1)
    go func(msg rpcMessage) {
      defer s.wg.Done()
      s.handle(ctx, msg)
    }(msg)
  }

  s.wg.Wait()

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

// toolList builds MCP's tool list straight from
// agent.ToolDefinitions — the exact schema the model gets for tool
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
// (llm.Message's tool_calls shape vs. MCP's CallToolResult) and each
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

  case "write_file":
    var in tools.WriteFileInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    // No tracker: the MCP client owns read-before-write and approval.
    out, err := tools.WriteFile(s.sandbox, nil, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "edit_file":
    var in tools.EditFileInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.EditFile(s.sandbox, nil, in)
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

  case "web_search":
    var in tools.WebSearchInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.WebSearch(ctx, s.webSearch, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "count_lines":
    var in tools.CountLinesInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.CountLines(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "list_languages":
    var in tools.ListLanguagesInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.ListLanguages(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "find_files":
    var in tools.FindFilesInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.FindFiles(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "file_stat":
    var in tools.FileStatInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.FileStat(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "find_todo_fixme":
    var in tools.FindTodoFixmeInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.FindTodoFixme(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "env_info":
    var in tools.EnvInfoInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.EnvInfo(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "project_manifest":
    var in tools.ProjectManifestInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.ProjectManifest(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "find_definition":
    var in tools.FindDefinitionInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.FindDefinition(ctx, s.symbols, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "list_symbols":
    var in tools.ListSymbolsInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.ListSymbols(ctx, s.symbols, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "outline_file":
    var in tools.OutlineFileInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.OutlineFile(ctx, s.symbols, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "find_references":
    var in tools.FindReferencesInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.FindReferences(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "git_log":
    var in tools.GitLogInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.GitLog(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "git_blame":
    var in tools.GitBlameInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.GitBlame(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "git_diff":
    var in tools.GitDiffInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.GitDiff(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "git_show":
    var in tools.GitShowInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.GitShow(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "working_tree_diff":
    var in tools.WorkingTreeDiffInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.WorkingTreeDiff(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "grep_history":
    var in tools.GrepHistoryInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.GrepHistory(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "code_owners":
    var in tools.CodeOwnersInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.CodeOwners(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "commit_stats":
    var in tools.CommitStatsInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.CommitStats(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "run_tests":
    var in tools.RunTestsInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.RunTests(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "build_check":
    var in tools.BuildCheckInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.BuildCheck(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "lint_check":
    var in tools.LintCheckInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.LintCheck(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "list_dependencies":
    var in tools.ListDependenciesInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.ListDependencies(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "module_graph":
    var in tools.ModuleGraphInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.ModuleGraph(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "read_doc_comment":
    var in tools.ReadDocCommentInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.ReadDocComment(ctx, s.symbols, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "find_callers":
    var in tools.FindCallersInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.FindCallers(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "find_dead_code":
    var in tools.FindDeadCodeInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.FindDeadCode(ctx, s.symbols, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "impact_analysis":
    var in tools.ImpactAnalysisInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.ImpactAnalysis(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }
    return jsonResult(out)

  case "read_pdf":
    var in tools.ReadPDFInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }

    out, err := tools.ReadPDF(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
    }

    return jsonResult(out)

  case "semantic_search":
    var in tools.SemanticSearchInput
    if err := json.Unmarshal(p.Arguments, &in); err != nil {
      return errorResult(err)
    }
    out, err := tools.SemanticSearch(ctx, s.sandbox, in)
    if err != nil {
      return errorResult(err)
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

  s.writeMu.Lock()
  defer s.writeMu.Unlock()
  if _, err := s.out.Write(body); err != nil {
    fmt.Fprintln(os.Stderr, "armin mcp: write failed:", err)
  }
}
