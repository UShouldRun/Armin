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
  "time"
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
  server := serverReturning(t, "/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
    var req owaiChatRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
      t.Fatal(err)
    }
    if req.Stream {
      t.Error("expected Stream=false when onToken is nil")
    }
    json.NewEncoder(w).Encode(owaiChatResponse{
      Choices: []owaiChoice{
        { Message: owaiMessage{ Role: "assistant", Content: "hi there" } },
      },
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

func TestChatSendsToolCallArgumentsAsJSONString(t *testing.T) {
  server := serverReturning(t, "/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
    var raw map[string]any
    if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
      t.Fatal(err)
    }
    msgs := raw["messages"].([]any)
    assistantMsg := msgs[0].(map[string]any)
    toolCalls := assistantMsg["tool_calls"].([]any)
    fn := toolCalls[0].(map[string]any)["function"].(map[string]any)
    args, ok := fn["arguments"].(string)
    if !ok {
      t.Fatalf("arguments = %v (%T), want a JSON-encoded string", fn["arguments"], fn["arguments"])
    }
    if !strings.Contains(args, `"query"`) {
      t.Fatalf("arguments = %q, want it to contain the query key", args)
    }
    json.NewEncoder(w).Encode(owaiChatResponse{
      Choices: []owaiChoice{{ Message: owaiMessage{ Role: "assistant", Content: "ok" } }},
    })
  })

  client := New(server.URL, "fake-model")
  _, err := client.Chat(context.Background(), []Message{
    {
      Role: "assistant",
      ToolCalls: []ToolCall{
        { ID: "call_1", Function: ToolCallFunction{ Name: "search_code", Arguments: map[string]any{ "query": "foo" } } },
      },
    },
  }, nil, nil)
  if err != nil {
    t.Fatalf("Chat: %v", err)
  }
}

func TestChatParsesToolCallIDAndArguments(t *testing.T) {
  server := serverReturning(t, "/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
    json.NewEncoder(w).Encode(owaiChatResponse{
      Choices: []owaiChoice{
        {
          Message: owaiMessage{
            Role: "assistant",
            ToolCalls: []owaiToolCall{
              {
                ID: "call_abc", Type: "function",
                Function: owaiToolCallFunc{ Name: "search_code", Arguments: `{"query":"foo"}` },
              },
            },
          },
        },
      },
    })
  })

  client := New(server.URL, "fake-model")
  msg, err := client.Chat(context.Background(), nil, nil, nil)
  if err != nil {
    t.Fatalf("Chat: %v", err)
  }
  if len(msg.ToolCalls) != 1 {
    t.Fatalf("ToolCalls = %+v, want 1", msg.ToolCalls)
  }
  call := msg.ToolCalls[0]
  if call.ID != "call_abc" {
    t.Fatalf("ID = %q, want %q", call.ID, "call_abc")
  }
  if call.Function.Arguments["query"] != "foo" {
    t.Fatalf("Arguments = %+v", call.Function.Arguments)
  }
}

func TestChatSynthesizesToolCallIDWhenMissing(t *testing.T) {
  server := serverReturning(t, "/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
    json.NewEncoder(w).Encode(owaiChatResponse{
      Choices: []owaiChoice{
        {
          Message: owaiMessage{
            Role: "assistant",
            ToolCalls: []owaiToolCall{
              { Function: owaiToolCallFunc{ Name: "search_code", Arguments: `{}` } },
            },
          },
        },
      },
    })
  })

  client := New(server.URL, "fake-model")
  msg, err := client.Chat(context.Background(), nil, nil, nil)
  if err != nil {
    t.Fatalf("Chat: %v", err)
  }
  if msg.ToolCalls[0].ID == "" {
    t.Fatal("expected a synthesized non-empty ID when the server didn't supply one")
  }
}

func TestChatStreamingAssemblesDeltasAndToolCallFragments(t *testing.T) {
  server := serverReturning(t, "/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
    var req owaiChatRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
      t.Fatal(err)
    }
    if !req.Stream {
      t.Error("expected Stream=true when onToken is non-nil")
    }

    flusher, _ := w.(http.Flusher)
    write := func(chunk owaiStreamChunk) {
      b, _ := json.Marshal(chunk)
      io.WriteString(w, "data: " + string(b) + "\n\n")
      if flusher != nil {
        flusher.Flush()
      }
    }

    write(owaiStreamChunk{Choices: []owaiStreamChoice{{Delta: owaiStreamDelta{Content: "Hel"}}}})
    write(owaiStreamChunk{Choices: []owaiStreamChoice{{Delta: owaiStreamDelta{Content: "lo"}}}})
    // Tool call arguments split across two fragments, matched by index.
    write(owaiStreamChunk{Choices: []owaiStreamChoice{{Delta: owaiStreamDelta{
      ToolCalls: []owaiStreamToolCall{
        {Index: 0, ID: "call_1", Function: owaiToolCallFunc{Name: "search_code", Arguments: `{"que`}},
      },
    }}}})
    write(owaiStreamChunk{Choices: []owaiStreamChoice{{Delta: owaiStreamDelta{
      ToolCalls: []owaiStreamToolCall{
        {Index: 0, Function: owaiToolCallFunc{Arguments: `ry":"foo"}`}},
      },
    }}}})
    io.WriteString(w, "data: [DONE]\n\n")
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
    t.Fatalf("final Content = %q, want \"Hello\"", msg.Content)
  }
  if len(msg.ToolCalls) != 1 {
    t.Fatalf("ToolCalls = %+v, want 1 assembled from two fragments", msg.ToolCalls)
  }
  call := msg.ToolCalls[0]
  if call.ID != "call_1" || call.Function.Name != "search_code" {
    t.Fatalf("call = %+v", call)
  }
  if call.Function.Arguments["query"] != "foo" {
    t.Fatalf("Arguments = %+v, want the two fragments joined into {\"query\":\"foo\"}", call.Function.Arguments)
  }
}

func TestChatNonOKStatus(t *testing.T) {
  server := serverReturning(t, "/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusInternalServerError)
    io.WriteString(w, "boom")
  })

  client := New(server.URL, "fake-model")
  if _, err := client.Chat(context.Background(), nil, nil, nil); err == nil {
    t.Fatal("expected an error for a non-200 response")
  }
}

func TestChatToolsUnsupportedIsDetected(t *testing.T) {
  server := serverReturning(t, "/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusInternalServerError)
    io.WriteString(w, `{"error":{"message":"this chat template does not support tool calls","type":"server_error"}}`)
  })

  client := New(server.URL, "fake-model")
  _, err := client.Chat(context.Background(), nil, nil, nil)
  if !errors.Is(err, ErrToolsUnsupported) {
    t.Fatalf("Chat error = %v, want errors.Is(_, ErrToolsUnsupported)", err)
  }
}

func TestComplete(t *testing.T) {
  var gotPrefix, gotSuffix string

  server := serverReturning(t, "/infill", func(w http.ResponseWriter, r *http.Request) {
    var req infillRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
      t.Fatal(err)
    }
    gotPrefix, gotSuffix = req.InputPrefix, req.InputSuffix
    json.NewEncoder(w).Encode(infillResponse{Content: "return 42"})
  })

  client := New(server.URL, "chat-model")
  out, err := client.Complete(context.Background(), "func f() {", "}", CompleteOptions{
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
}

func TestCompleteNonOKStatus(t *testing.T) {
  server := serverReturning(t, "/infill", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusInternalServerError)
    io.WriteString(w, "boom")
  })

  client := New(server.URL, "chat-model")
  if _, err := client.Complete(context.Background(), "x", "", CompleteOptions{}); err == nil {
    t.Fatal("expected an error for a non-200 response")
  }
}

func TestHealthOK(t *testing.T) {
  server := serverReturning(t, "/health", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
  })
  client := New(server.URL, "m")
  if err := client.Health(context.Background()); err != nil {
    t.Fatalf("Health: %v", err)
  }
}

func TestHealthNotReady(t *testing.T) {
  server := serverReturning(t, "/health", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusServiceUnavailable)
    io.WriteString(w, `{"status":"loading model"}`)
  })
  client := New(server.URL, "m")
  if err := client.Health(context.Background()); err == nil {
    t.Fatal("expected an error while the model is still loading")
  }
}

func TestRequestStartEndBracketEveryCall(t *testing.T) {
  server := serverReturning(t, "/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
    json.NewEncoder(w).Encode(owaiChatResponse{
      Choices: []owaiChoice{{Message: owaiMessage{Role: "assistant", Content: "ok"}}},
    })
  })

  client := New(server.URL, "m")
  var starts, ends int
  client.OnRequestStart = func() { starts++ }
  client.OnRequestEnd = func() { ends++ }

  if _, err := client.Chat(context.Background(), nil, nil, nil); err != nil {
    t.Fatal(err)
  }
  if starts != 1 || ends != 1 {
    t.Fatalf("starts=%d ends=%d, want 1 and 1", starts, ends)
  }
}

func TestRequestEndFiresEvenOnError(t *testing.T) {
  server := serverReturning(t, "/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusInternalServerError)
  })

  client := New(server.URL, "m")
  var ends int
  client.OnRequestEnd = func() { ends++ }

  client.Chat(context.Background(), nil, nil, nil)
  if ends != 1 {
    t.Fatalf("ends=%d, want 1 even though the call errored", ends)
  }
}

func TestChatSendsAuthorizationHeaderWhenAPIKeySet(t *testing.T) {
  var gotAuth string
  server := serverReturning(t, "/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
    gotAuth = r.Header.Get("Authorization")
    json.NewEncoder(w).Encode(owaiChatResponse{
      Choices: []owaiChoice{{Message: owaiMessage{Role: "assistant", Content: "ok"}}},
    })
  })

  client := New(server.URL, "claude")
  client.APIKey = "sk-ant-test"
  if _, err := client.Chat(context.Background(), nil, nil, nil); err != nil {
    t.Fatalf("Chat: %v", err)
  }
  if gotAuth != "Bearer sk-ant-test" {
    t.Fatalf("Authorization header = %q, want %q", gotAuth, "Bearer sk-ant-test")
  }
}

func TestChatOmitsAuthorizationHeaderWhenNoAPIKey(t *testing.T) {
  var sawHeader bool
  server := serverReturning(t, "/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
    sawHeader = r.Header.Get("Authorization") != ""
    json.NewEncoder(w).Encode(owaiChatResponse{
      Choices: []owaiChoice{{Message: owaiMessage{Role: "assistant", Content: "ok"}}},
    })
  })

  client := New(server.URL, "local")
  if _, err := client.Chat(context.Background(), nil, nil, nil); err != nil {
    t.Fatalf("Chat: %v", err)
  }
  if sawHeader {
    t.Fatal("no Authorization header should be sent when APIKey is unset (e.g. a local llama-server)")
  }
}

func TestCompleteSendsAuthorizationHeaderWhenAPIKeySet(t *testing.T) {
  var gotAuth string
  server := serverReturning(t, "/infill", func(w http.ResponseWriter, r *http.Request) {
    gotAuth = r.Header.Get("Authorization")
    json.NewEncoder(w).Encode(infillResponse{Content: "x"})
  })

  client := New(server.URL, "m")
  client.APIKey = "sk-ant-test"
  if _, err := client.Complete(context.Background(), "x", "", CompleteOptions{}); err != nil {
    t.Fatalf("Complete: %v", err)
  }
  if gotAuth != "Bearer sk-ant-test" {
    t.Fatalf("Authorization header = %q, want %q", gotAuth, "Bearer sk-ant-test")
  }
}

func TestHealthSendsAuthorizationHeaderWhenAPIKeySet(t *testing.T) {
  var gotAuth string
  server := serverReturning(t, "/health", func(w http.ResponseWriter, r *http.Request) {
    gotAuth = r.Header.Get("Authorization")
    w.WriteHeader(http.StatusOK)
  })

  client := New(server.URL, "m")
  client.APIKey = "sk-ant-test"
  if err := client.Health(context.Background()); err != nil {
    t.Fatalf("Health: %v", err)
  }
  if gotAuth != "Bearer sk-ant-test" {
    t.Fatalf("Authorization header = %q, want %q", gotAuth, "Bearer sk-ant-test")
  }
}

func TestNewDefaultsBaseURL(t *testing.T) {
  client := New("", "m")
  if client.BaseURL != "http://localhost:8080" {
    t.Fatalf("BaseURL = %q, want the llama-server default", client.BaseURL)
  }
}

func TestChatStreamSeparatesReasoningAndReadsUsage(t *testing.T) {
  server := serverReturning(t, "/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
    var req owaiChatRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
      t.Fatal(err)
    }
    if req.StreamOptions == nil || !req.StreamOptions.IncludeUsage {
      t.Error("expected stream_options.include_usage on a streamed request")
    }
    write := func(chunk owaiStreamChunk) {
      b, _ := json.Marshal(chunk)
      io.WriteString(w, "data: " + string(b) + "\n\n")
    }
    write(owaiStreamChunk{ Choices: []owaiStreamChoice{ { Delta: owaiStreamDelta{ ReasoningContent: "hmm " } } } })
    write(owaiStreamChunk{ Choices: []owaiStreamChoice{ { Delta: owaiStreamDelta{ ReasoningContent: "ok" } } } })
    write(owaiStreamChunk{ Choices: []owaiStreamChoice{ { Delta: owaiStreamDelta{ Content: "answer" } } } })
    write(owaiStreamChunk{ Usage: &Usage{ PromptTokens: 100, CompletionTokens: 20 } })
    io.WriteString(w, "data: [DONE]\n\n")
  })

  client := New(server.URL, "fake-model")

  var tokens, thoughts strings.Builder
  msg, err := client.ChatStream(context.Background(), nil, nil, StreamHandlers{
    Token:     func(d string) { tokens.WriteString(d) },
    Reasoning: func(d string) { thoughts.WriteString(d) },
  })
  if err != nil {
    t.Fatalf("ChatStream: %v", err)
  }
  if tokens.String() != "answer" || msg.Content != "answer" {
    t.Fatalf("content = %q / %q, want \"answer\"", tokens.String(), msg.Content)
  }
  if thoughts.String() != "hmm ok" || msg.Reasoning != "hmm ok" {
    t.Fatalf("reasoning = %q / %q, want \"hmm ok\"", thoughts.String(), msg.Reasoning)
  }
  if msg.Usage.Total() != 120 {
    t.Fatalf("Usage = %+v, want 100+20", msg.Usage)
  }
}

func TestChatSendsReasoningBack(t *testing.T) {
  server := serverReturning(t, "/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
    var req owaiChatRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
      t.Fatal(err)
    }
    if req.Messages[0].ReasoningContent != "plan" {
      t.Errorf("reasoning_content = %q, want \"plan\"", req.Messages[0].ReasoningContent)
    }
    json.NewEncoder(w).Encode(owaiChatResponse{
      Choices: []owaiChoice{ { Message: owaiMessage{ Role: "assistant", Content: "ok" } } },
      Usage:   &Usage{ PromptTokens: 7 },
    })
  })

  client := New(server.URL, "fake-model")
  msg, err := client.Chat(context.Background(), []Message{
    { Role: "assistant", Content: "x", Reasoning: "plan" },
  }, nil, nil)
  if err != nil {
    t.Fatalf("Chat: %v", err)
  }
  if msg.Usage.PromptTokens != 7 {
    t.Fatalf("Usage = %+v, want prompt_tokens 7", msg.Usage)
  }
}

func TestChatReadTimeoutFiresOnSilence(t *testing.T) {
  release := make(chan struct{})
  server := serverReturning(t, "/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
    select {
    case <-release:
    case <-r.Context().Done():
    }
  })
  defer close(release)

  client := New(server.URL, "fake-model")
  client.ReadTimeout = 50 * time.Millisecond

  _, err := client.Chat(context.Background(), nil, nil, nil)
  if err == nil || !strings.Contains(err.Error(), "read timeout") {
    t.Fatalf("err = %v, want a read timeout", err)
  }
}

func TestChatReadTimeoutResetsWhileStreaming(t *testing.T) {
  server := serverReturning(t, "/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
    flusher := w.(http.Flusher)
    for i := 0; i < 6; i++ {
      b, _ := json.Marshal(owaiStreamChunk{ Choices: []owaiStreamChoice{ { Delta: owaiStreamDelta{ Content: "x" } } } })
      io.WriteString(w, "data: " + string(b) + "\n\n")
      flusher.Flush()
      time.Sleep(30 * time.Millisecond)
    }
    io.WriteString(w, "data: [DONE]\n\n")
  })

  client := New(server.URL, "fake-model")
  client.ReadTimeout = 80 * time.Millisecond

  msg, err := client.Chat(context.Background(), nil, nil, func(string) {})
  if err != nil {
    t.Fatalf("Chat: %v (total time exceeds ReadTimeout, but it never went idle)", err)
  }
  if msg.Content != "xxxxxx" {
    t.Fatalf("Content = %q", msg.Content)
  }
}

func TestContextSizeFromProps(t *testing.T) {
  server := serverReturning(t, "/props", func(w http.ResponseWriter, r *http.Request) {
    io.WriteString(w, `{"default_generation_settings":{"n_ctx":65536}}`)
  })
  n, err := New(server.URL, "m").ContextSize(context.Background())
  if err != nil || n != 65536 {
    t.Fatalf("ContextSize = %d, %v; want 65536", n, err)
  }
}
