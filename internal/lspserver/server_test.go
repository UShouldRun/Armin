package lspserver

import (
  "bufio"
  "bytes"
  "context"
  "encoding/json"
  "net/http"
  "net/http/httptest"
  "strings"
  "testing"

  "armin/internal/agent"
  "armin/internal/ctags"
  "armin/internal/llm"
  "armin/internal/sandbox"
)

// fakeOllama serves /api/tags (for ListModels) so armin.model's
// "list what's installed" path can be tested without a real Ollama.
func fakeOllama(t *testing.T, models ...llm.TagsModel) *httptest.Server {
  t.Helper()
  return httptest.NewServer(http.HandlerFunc(
    func(w http.ResponseWriter, r *http.Request) {
      json.NewEncoder(w).Encode(map[string]any{ "models": models })
    },
  ))
}

func model(name string, size int64) llm.TagsModel {
  return llm.TagsModel{ Name: name, Size: size }
}

func newTestServerWithOllama(t *testing.T, ollamaURL, model string) *Server {
  t.Helper()
  root := t.TempDir()
  sb, err := sandbox.New(root, sandbox.Options{})
  if err != nil {
    t.Fatal(err)
  }
  client := llm.New(ollamaURL, model)
  loop := agent.New(client, sb)
  return New(loop, sb, "system prompt", "", ctags.New(root, ""))
}

// callExecuteCommand invokes s.executeCommand directly rather than
// through s.handle — handle dispatches executeCommand in a
// goroutine (so a slow LLM call never blocks the read loop), which
// would make a synchronous test racy against s.out.
func callExecuteCommand(t *testing.T, s *Server, p ExecuteCommandParams) rpcResponse {
  t.Helper()

  var out bytes.Buffer
  s.out = &out
  s.executeCommand(context.Background(), json.RawMessage("1"), p)

  // Server writes Content-Length-framed JSON-RPC (see transport.go),
  // not bare newline-delimited JSON — decode it the same way a real
  // client would.
  body, err := readMessage(bufio.NewReader(&out))
  if err != nil {
    t.Fatalf("reading framed executeCommand response: %v", err)
  }
  var resp rpcResponse
  if err := json.Unmarshal(body, &resp); err != nil {
    t.Fatalf("decoding executeCommand response: %v (raw: %s)", err, body)
  }
  return resp
}

func TestExecuteModelSwitchesLive(t *testing.T) {
  server := fakeOllama(t, model("qwen3:4b", 2_500_000_000))
  defer server.Close()
  s := newTestServerWithOllama(t, server.URL, "qwen3:4b")

  resp := callExecuteCommand(t, s, ExecuteCommandParams{
    Command:   "armin.model",
    Arguments: []json.RawMessage{ json.RawMessage(`{"name":"qwen2.5-coder:1.5b-base"}`) },
  })
  if resp.Error != nil {
    t.Fatalf("armin.model (set): %+v", resp.Error)
  }

  var info ModelInfo
  remarshalLSP(t, resp.Result, &info)
  if info.Current != "qwen2.5-coder:1.5b-base" {
    t.Fatalf("Current = %q, want qwen2.5-coder:1.5b-base", info.Current)
  }
  if s.loop.LLM.Model != "qwen2.5-coder:1.5b-base" {
    t.Fatalf("the shared client's Model wasn't actually updated: %q", s.loop.LLM.Model)
  }
}

func TestExecuteModelReportsCurrentAndInstalled(t *testing.T) {
  server := fakeOllama(t,
    model("qwen3:4b", 2_500_000_000),
    model("qwen2.5-coder:1.5b-base", 986_000_000),
  )
  defer server.Close()
  s := newTestServerWithOllama(t, server.URL, "qwen3:4b")

  resp := callExecuteCommand(t, s, ExecuteCommandParams{ Command: "armin.model" })
  if resp.Error != nil {
    t.Fatalf("armin.model (get): %+v", resp.Error)
  }

  var info ModelInfo
  remarshalLSP(t, resp.Result, &info)
  if info.Current != "qwen3:4b" {
    t.Fatalf("Current = %q, want qwen3:4b", info.Current)
  }
  if len(info.Installed) != 2 {
    t.Fatalf("Installed = %v, want 2 entries", info.Installed)
  }
  if info.Installed[1].Size != 986_000_000 {
    t.Fatalf("Installed[1].Size = %d, want 986000000", info.Installed[1].Size)
  }
}

// fakeOllamaChatRejectsTools serves /api/chat with the raw 400 body
// Ollama actually returns for a model with no tool-calling support —
// executeAsk/hover are expected to turn this into a clean, one-line
// message instead of passing it through.
func fakeOllamaChatRejectsTools(t *testing.T, modelName string) *httptest.Server {
  t.Helper()
  return httptest.NewServer(http.HandlerFunc(
    func(w http.ResponseWriter, r *http.Request) {
      w.WriteHeader(http.StatusBadRequest)
      json.NewEncoder(w).Encode(map[string]string{
        "error": modelName + " does not support tools",
      })
    },
  ))
}

func TestExecuteAskToolsUnsupportedShowsCleanMessage(t *testing.T) {
  server := fakeOllamaChatRejectsTools(t, "fake-model")
  defer server.Close()
  s := newTestServerWithOllama(t, server.URL, "fake-model")

  resp := callExecuteCommand(t, s, ExecuteCommandParams{
    Command:   "armin.ask",
    Arguments: []json.RawMessage{ json.RawMessage(`{"question":"what does this do?"}`) },
  })
  if resp.Error == nil {
    t.Fatal("expected an error response")
  }
  if strings.Contains(resp.Error.Message, "\t") || strings.Contains(resp.Error.Message, "\n") {
    t.Fatalf("error message wasn't cleaned up: %q", resp.Error.Message)
  }
  if !strings.Contains(resp.Error.Message, "fake-model") ||
    !strings.Contains(resp.Error.Message, ":ArminModel") {
    t.Fatalf("error message = %q, want it to name the model and mention :ArminModel", resp.Error.Message)
  }
}

func remarshalLSP(t *testing.T, from any, to any) {
  t.Helper()
  b, err := json.Marshal(from)
  if err != nil {
    t.Fatal(err)
  }
  if err := json.Unmarshal(b, to); err != nil {
    t.Fatal(err)
  }
}
