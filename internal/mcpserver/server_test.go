package mcpserver

import (
  "bytes"
  "context"
  "encoding/json"
  "os"
  "path/filepath"
  "strings"
  "testing"

  "armin/internal/ctags"
  "armin/internal/sandbox"
)

func newTestServer(t *testing.T) *Server {
  t.Helper()
  root := t.TempDir()
  err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("hello"), 0o644)
  if err != nil {
    t.Fatal(err)
  }
  cfg, err := sandbox.New(root, sandbox.Options{})
  if err != nil {
    t.Fatal(err)
  }
  return New(cfg, nil, ctags.New(root, ""))
}

// call sends one request line through Serve's handler directly (no
// stdio round trip needed) and returns the decoded response.
func call(t *testing.T, s *Server, id int, method string, params any) rpcResponse {
  t.Helper()

  var paramsRaw json.RawMessage
  if params != nil {
    b, err := json.Marshal(params)
    if err != nil {
      t.Fatal(err)
    }
    paramsRaw = b
  }

  var out bytes.Buffer
  s.out = &out
  s.handle(context.Background(), rpcMessage{
    JSONRPC: "2.0",
    ID:      json.RawMessage(itoa(id)),
    Method:  method,
    Params:  paramsRaw,
  })

  var resp rpcResponse
  err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp)
  if err != nil {
    t.Fatalf("decoding response to %s: %v (raw: %s)", method, err, out.String())
  }
  return resp
}

func itoa(n int) string {
  return string(rune('0' + n)) // test ids are always single digits
}

func TestInitialize(t *testing.T) {
  s := newTestServer(t)
  resp := call(t, s, 1, "initialize", map[string]any{})

  if resp.Error != nil {
    t.Fatalf("initialize returned an error: %+v", resp.Error)
  }

  var result initializeResult
  remarshal(t, resp.Result, &result)
  if result.ProtocolVersion != protocolVersion {
    t.Fatalf("ProtocolVersion = %q, want %q", result.ProtocolVersion, protocolVersion)
  }
  if result.ServerInfo.Name != "armin" {
    t.Fatalf("ServerInfo.Name = %q, want armin", result.ServerInfo.Name)
  }
}

func TestToolsList(t *testing.T) {
  s := newTestServer(t)
  resp := call(t, s, 1, "tools/list", nil)

  var result listToolsResult
  remarshal(t, resp.Result, &result)

  // the agent's 38 sandboxed tools minus run_program, which needs
  // someone to approve each run
  if len(result.Tools) != 37 {
    t.Fatalf("tools/list returned %d tools, want 37", len(result.Tools))
  }

  for _, tl := range result.Tools {
    if tl.Name == "run_program" {
      t.Fatal("run_program is listed, but MCP can't run it")
    }
  }
  // noPropsOK are tools with no meaningful parameters at all
  // (list_directory's only param is optional, the rest take
  // nothing) — every other tool should declare at least one
  // property.
  noPropsOK := map[string]bool{
    "list_directory": true, "env_info": true, "project_manifest": true,
    "list_dependencies": true, "module_graph": true,
  }

  names := map[string]bool{}
  for _, tl := range result.Tools {
    names[tl.Name] = true
    if len(tl.InputSchema.Properties) == 0 && !noPropsOK[tl.Name] {
      t.Errorf("tool %q has no input schema properties", tl.Name)
    }
  }
  for _, want := range []string{
    "search_code", "read_file", "list_directory", "run_command", "web_search",
    "count_lines", "list_languages", "find_files", "file_stat", "find_todo_fixme",
    "env_info", "project_manifest",
    "find_definition", "list_symbols", "outline_file", "find_references",
    "git_log", "git_blame", "git_diff", "git_show", "working_tree_diff",
    "grep_history", "code_owners", "commit_stats",
    "run_tests", "build_check", "lint_check",
    "list_dependencies", "module_graph",
    "read_doc_comment",
    "find_callers", "find_dead_code", "impact_analysis",
    "semantic_search",
  } {
    if !names[want] {
      t.Errorf("tools/list missing %q", want)
    }
  }
}

func TestToolsCallSuccess(t *testing.T) {
  s := newTestServer(t)
  resp := call(t, s, 1, "tools/call", callToolParams{
    Name:      "read_file",
    Arguments: json.RawMessage(`{"path":"f.txt"}`),
  })

  var result callToolResult
  remarshal(t, resp.Result, &result)

  if result.IsError {
    t.Fatalf("expected success, got an error result: %+v", result)
  }
  if len(result.Content) != 1 || !strings.Contains(result.Content[0].Text, "hello") {
    t.Fatalf("Content = %+v, want it to contain the file's text", result.Content)
  }
}

func TestToolsCallDisallowedBinaryIsError(t *testing.T) {
  s := newTestServer(t)
  resp := call(t, s, 1, "tools/call", callToolParams{
    Name:      "run_command",
    Arguments: json.RawMessage(`{"binary":"not-allowed"}`),
  })

  var result callToolResult
  remarshal(t, resp.Result, &result)

  if !result.IsError {
    t.Fatal("expected IsError=true for a disallowed binary — MCP clients rely on this to know the call failed")
  }
}

func TestToolsCallUnknownTool(t *testing.T) {
  s := newTestServer(t)
  resp := call(t, s, 1, "tools/call", callToolParams{Name: "delete_everything"})

  var result callToolResult
  remarshal(t, resp.Result, &result)
  if !result.IsError {
    t.Fatal("expected IsError=true for an unknown tool")
  }
}

func TestUnknownMethodReturnsProtocolError(t *testing.T) {
  s := newTestServer(t)
  resp := call(t, s, 1, "nonexistent/method", nil)

  if resp.Error == nil || resp.Error.Code != errMethodNotFound {
    t.Fatalf("resp.Error = %+v, want errMethodNotFound", resp.Error)
  }
}

func TestNotificationGetsNoResponse(t *testing.T) {
  s := newTestServer(t)
  var out bytes.Buffer
  s.out = &out

  s.handle(context.Background(), rpcMessage{
    JSONRPC: "2.0",
    Method:  "notifications/initialized",
  })

  if out.Len() != 0 {
    t.Fatalf("a notification produced a response: %s", out.String())
  }
}

func TestPing(t *testing.T) {
  s := newTestServer(t)
  resp := call(t, s, 1, "ping", nil)
  if resp.Error != nil {
    t.Fatalf("ping returned an error: %+v", resp.Error)
  }
}

// TestServeEndToEnd drives the full newline-delimited-JSON transport,
// not just the handler directly — catches framing bugs the direct
// s.handle() calls above wouldn't.
func TestServeEndToEnd(t *testing.T) {
  s := newTestServer(t)

  var in bytes.Buffer
  in.WriteString(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")
  in.WriteString(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")

  var out bytes.Buffer
  err := s.Serve(context.Background(), &in, &out)
  if err != nil {
    t.Fatalf("Serve: %v", err)
  }

  lines := strings.Split(strings.TrimSpace(out.String()), "\n")
  if len(lines) != 2 {
    t.Fatalf("got %d response lines, want 2:\n%s", len(lines), out.String())
  }

  // Requests are now handled concurrently, so responses can arrive in
  // either order — match each by id instead of assuming line order.
  byID := map[string]string{}
  for _, line := range lines {
    var resp struct {
      ID json.RawMessage `json:"id"`
    }
    err := json.Unmarshal([]byte(line), &resp)
    if err != nil {
      t.Fatalf("decoding response line %q: %v", line, err)
    }
    byID[string(resp.ID)] = line
  }

  if !strings.Contains(byID["1"], `"protocolVersion"`) {
    t.Fatalf("response to id=1 = %s, want the initialize result", byID["1"])
  }
  if !strings.Contains(byID["2"], `"tools"`) {
    t.Fatalf("response to id=2 = %s, want the tools/list result", byID["2"])
  }
}

func remarshal(t *testing.T, from any, to any) {
  t.Helper()
  b, err := json.Marshal(from)
  if err != nil {
    t.Fatal(err)
  }
  err = json.Unmarshal(b, to)
  if err != nil {
    t.Fatal(err)
  }
}
