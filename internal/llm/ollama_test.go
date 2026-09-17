package llm

import (
  "context"
  "encoding/json"
  "errors"
  "io"
  "net/http"
  "net/http/httptest"
  "strings"
  "testing"
)

func serverReturning(t *testing.T, path string, handler http.HandlerFunc) *httptest.Server {
  t.Helper()
  mux := http.NewServeMux()
  mux.HandleFunc(path, handler)
  server := httptest.NewServer(mux)
  t.Cleanup(server.Close)
  return server
}

func TestChatNonStreaming(t *testing.T) {
  server := serverReturning(t, "/api/chat", func(w http.ResponseWriter, r *http.Request) {
    var req chatRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
      t.Fatal(err)
    }
    if req.Stream {
      t.Error("expected Stream=false when onToken is nil")
    }
    json.NewEncoder(w).Encode(chatChunk{
      Message: Message{Role: "assistant", Content: "hi there"},
      Done:    true,
    })
  })

  client := New(server.URL, "fake-model")
  msg, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "hey"}}, nil, nil)
  if err != nil {
    t.Fatalf("Chat: %v", err)
  }
  if msg.Content != "hi there" {
    t.Fatalf("Content = %q, want %q", msg.Content, "hi there")
  }
}

func TestChatStreamingAssemblesDeltasAndToolCalls(t *testing.T) {
  server := serverReturning(t, "/api/chat", func(w http.ResponseWriter, r *http.Request) {
    var req chatRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
      t.Fatal(err)
    }
    if !req.Stream {
      t.Error("expected Stream=true when onToken is non-nil")
    }

    enc := json.NewEncoder(w)
    enc.Encode(chatChunk{Message: Message{Content: "Hel"}})
    enc.Encode(chatChunk{Message: Message{Content: "lo"}})
    enc.Encode(chatChunk{
      Message: Message{ToolCalls: []ToolCall{{Function: ToolCallFunction{Name: "search_code"}}}},
      Done:    true,
    })
  })

  client := New(server.URL, "fake-model")

  var streamed strings.Builder
  msg, err := client.Chat(context.Background(), nil, nil, func(delta string) {
    streamed.WriteString(delta)
  })
  if err != nil {
    t.Fatalf("Chat: %v", err)
  }

  if streamed.String() != "Hello" {
    t.Fatalf("streamed deltas = %q, want \"Hello\"", streamed.String())
  }
  if msg.Content != "Hello" {
    t.Fatalf("final Content = %q, want \"Hello\" (accumulated from deltas)", msg.Content)
  }
  if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Name != "search_code" {
    t.Fatalf("ToolCalls = %+v, want one search_code call", msg.ToolCalls)
  }
}

func TestChatNonOKStatus(t *testing.T) {
  server := serverReturning(t, "/api/chat", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusInternalServerError)
    io.WriteString(w, "boom")
  })

  client := New(server.URL, "fake-model")
  if _, err := client.Chat(context.Background(), nil, nil, nil); err == nil {
    t.Fatal("expected an error for a non-200 response")
  }
}

func TestChatToolsUnsupportedIsDetected(t *testing.T) {
  server := serverReturning(t, "/api/chat", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusBadRequest)
    io.WriteString(w, `{"error":"registry.ollama.ai/library/fake-model does not support tools"}`)
  })

  client := New(server.URL, "fake-model")
  _, err := client.Chat(context.Background(), nil, nil, nil)
  if !errors.Is(err, ErrToolsUnsupported) {
    t.Fatalf("Chat error = %v, want errors.Is(_, ErrToolsUnsupported)", err)
  }
}

func TestComplete(t *testing.T) {
  var gotPrefix, gotSuffix, gotModel string

  server := serverReturning(t, "/api/generate", func(w http.ResponseWriter, r *http.Request) {
    var req generateRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
      t.Fatal(err)
    }
    gotPrefix, gotSuffix, gotModel = req.Prompt, req.Suffix, req.Model
    json.NewEncoder(w).Encode(generateResponse{Response: "return 42"})
  })

  client := New(server.URL, "chat-model")
  out, err := client.Complete(context.Background(), "func f() {", "}", CompleteOptions{
    Model:      "coder-model",
    NumPredict: 10,
  })
  if err != nil {
    t.Fatalf("Complete: %v", err)
  }
  if out != "return 42" {
    t.Fatalf("Complete result = %q", out)
  }
  if gotPrefix != "func f() {" || gotSuffix != "}" {
    t.Fatalf("sent prefix/suffix = %q / %q", gotPrefix, gotSuffix)
  }
  if gotModel != "coder-model" {
    t.Fatalf("model = %q, want the CompleteOptions override, not the chat model", gotModel)
  }
}

func TestCompleteDefaultsToClientModel(t *testing.T) {
  var gotModel string
  server := serverReturning(t, "/api/generate", func(w http.ResponseWriter, r *http.Request) {
    var req generateRequest
    json.NewDecoder(r.Body).Decode(&req)
    gotModel = req.Model
    json.NewEncoder(w).Encode(generateResponse{})
  })

  client := New(server.URL, "chat-model")
  if _, err := client.Complete(context.Background(), "x", "", CompleteOptions{}); err != nil {
    t.Fatal(err)
  }
  if gotModel != "chat-model" {
    t.Fatalf("model = %q, want it to fall back to the client's Model", gotModel)
  }
}

func TestListModels(t *testing.T) {
  server := serverReturning(t, "/api/tags", func(w http.ResponseWriter, r *http.Request) {
    json.NewEncoder(w).Encode(tagsResponse{Models: []TagsModel{
      {Name: "qwen3:4b", Size: 2_500_000_000},
      {Name: "gemma3:latest", Size: 3_300_000_000},
    }})
  })

  client := New(server.URL, "qwen3:4b")
  models, err := client.ListModels(context.Background())
  if err != nil {
    t.Fatalf("ListModels: %v", err)
  }
  if len(models) != 2 || models[0].Name != "qwen3:4b" {
    t.Fatalf("ListModels = %+v", models)
  }
  if models[0].Size != 2_500_000_000 {
    t.Fatalf("models[0].Size = %d, want 2500000000", models[0].Size)
  }
}

func TestNewDefaultsBaseURL(t *testing.T) {
  client := New("", "m")
  if client.BaseURL != "http://localhost:11434" {
    t.Fatalf("BaseURL = %q, want the default", client.BaseURL)
  }
}
