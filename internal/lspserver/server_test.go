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
  "armin/internal/llamacpp"
  "armin/internal/sandbox"
)

// fakeChatServer serves a canned /v1/chat/completions response so
// armin.model/armin.ask can be tested without a real llama-server.
func fakeChatServer(t *testing.T, reply string) *httptest.Server {
  t.Helper()
  return httptest.NewServer(http.HandlerFunc(
    func(w http.ResponseWriter, r *http.Request) {
      json.NewEncoder(w).Encode(map[string]any{
        "choices": []map[string]any{
          { "message": map[string]string{ "role": "assistant", "content": reply } },
        },
      })
    },
  ))
}

// newTestServer builds a Server backed by a llamacpp.Manager whose
// every name in modelNames is configured as an external, unmanaged
// spec pointed at chatURL — no process spawning involved, matching
// how a "just point armin at a llama-server you already run"
// zero-config setup works. modelNames[0] is the initial chat model;
// the completion Client reuses the same one.
func newTestServer(t *testing.T, chatURL string, modelNames ...string) *Server {
  t.Helper()
  root := t.TempDir()
  sb, err := sandbox.New(root, sandbox.Options{})
  if err != nil {
    t.Fatal(err)
  }

  specs := make(map[string]llamacpp.ModelSpec, len(modelNames))
  for _, name := range modelNames {
    specs[name] = llamacpp.ModelSpec{ URL: chatURL }
  }
  models := llamacpp.New(specs)
  t.Cleanup(func() { models.Shutdown(context.Background()) })

  ctx := context.Background()
  client, err := models.Client(ctx, modelNames[0])
  if err != nil {
    t.Fatal(err)
  }
  completionClient, err := models.Client(ctx, modelNames[0])
  if err != nil {
    t.Fatal(err)
  }

  loop := agent.New(client, sb)
  return New(loop, sb, "system prompt", completionClient, models, ctags.New(root, ""))
}

// callExecuteCommand invokes s.executeCommand directly rather than
// through s.handle — handle dispatches executeCommand in a
// goroutine (so a slow model call never blocks the read loop), which
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
  server := fakeChatServer(t, "ok")
  defer server.Close()
  s := newTestServer(t, server.URL, "chat", "coder")

  resp := callExecuteCommand(t, s, ExecuteCommandParams{
    Command:   "armin.model",
    Arguments: []json.RawMessage{ json.RawMessage(`{"name":"coder"}`) },
  })
  if resp.Error != nil {
    t.Fatalf("armin.model (set): %+v", resp.Error)
  }

  var info ModelInfo
  remarshalLSP(t, resp.Result, &info)
  if info.Current != "coder" {
    t.Fatalf("Current = %q, want coder", info.Current)
  }
  if s.loop.LLM.Model != "coder" {
    t.Fatalf("the shared client's Model wasn't actually updated: %q", s.loop.LLM.Model)
  }
}

func TestExecuteModelRejectsUnconfiguredName(t *testing.T) {
  server := fakeChatServer(t, "ok")
  defer server.Close()
  s := newTestServer(t, server.URL, "chat")

  resp := callExecuteCommand(t, s, ExecuteCommandParams{
    Command:   "armin.model",
    Arguments: []json.RawMessage{ json.RawMessage(`{"name":"nonexistent"}`) },
  })
  if resp.Error == nil {
    t.Fatal("expected an error for a name with no matching models.<name> entry")
  }
  if s.loop.LLM.Model != "chat" {
    t.Fatalf("a rejected switch must not mutate the client: Model = %q", s.loop.LLM.Model)
  }
}

func TestExecuteModelReportsCurrentAndConfigured(t *testing.T) {
  server := fakeChatServer(t, "ok")
  defer server.Close()
  s := newTestServer(t, server.URL, "chat", "coder")

  resp := callExecuteCommand(t, s, ExecuteCommandParams{ Command: "armin.model" })
  if resp.Error != nil {
    t.Fatalf("armin.model (get): %+v", resp.Error)
  }

  var info ModelInfo
  remarshalLSP(t, resp.Result, &info)
  if info.Current != "chat" {
    t.Fatalf("Current = %q, want chat", info.Current)
  }
  if len(info.Configured) != 2 {
    t.Fatalf("Configured = %v, want 2 entries", info.Configured)
  }
  for _, c := range info.Configured {
    if c.Managed {
      t.Fatalf("%+v: an external (URL-only) spec must report Managed=false", c)
    }
  }
}

// fakeChatServerRejectsTools serves /v1/chat/completions with the
// raw error body llama-server returns for a model/template with no
// tool-calling support — executeAsk/hover are expected to turn this
// into a clean, one-line message instead of passing it through.
func fakeChatServerRejectsTools(t *testing.T, modelName string) *httptest.Server {
  t.Helper()
  return httptest.NewServer(http.HandlerFunc(
    func(w http.ResponseWriter, r *http.Request) {
      w.WriteHeader(http.StatusBadRequest)
      json.NewEncoder(w).Encode(map[string]any{
        "error": map[string]string{
          "message": modelName + " does not support tools",
        },
      })
    },
  ))
}

func TestExecuteAskToolsUnsupportedShowsCleanMessage(t *testing.T) {
  server := fakeChatServerRejectsTools(t, "fake-model")
  defer server.Close()
  s := newTestServer(t, server.URL, "fake-model")

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
