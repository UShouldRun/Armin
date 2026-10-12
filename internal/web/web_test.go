package web

import (
  "bufio"
  "context"
  "encoding/json"
  "io"
  "net/http"
  "net/http/httptest"
  "os"
  "path/filepath"
  "strings"
  "testing"
  "time"

  "armin/internal/agent"
  "armin/internal/llm"
  "armin/internal/sandbox"
  "armin/internal/session"
)

// fakeModel answers the first request with a write_file call and the
// second with plain text.
func fakeModel(t *testing.T) *httptest.Server {
  round := 0
  return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    round++
    w.Header().Set("Content-Type", "text/event-stream")
    var chunk string
    if round == 1 {
      chunk = `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"write_file","arguments":"{\"path\":\"new.txt\",\"content\":\"hi\\n\"}"}}]}}]}`
    } else {
      chunk = `{"choices":[{"delta":{"content":"Created it, see new.txt:1"}}]}`
    }

    w.Write([]byte("data: " + chunk + "\n\ndata: [DONE]\n\n"))
  }))
}

func newServer(t *testing.T) (*Server, string) {
  t.Helper()
  root := t.TempDir()
  os.WriteFile(filepath.Join(root, "page.html"), []byte("<script>alert(1)</script>"), 0o644)
  sb, err := sandbox.New(root, sandbox.Options{})
  if err != nil {
    t.Fatal(err)
  }

  model := fakeModel(t)
  t.Cleanup(model.Close)
  client := llm.New(model.URL, "fake")
  s := New(context.Background(), Config{
    Addr: "127.0.0.1:0", Loop: agent.New(client, sb), Client: client,
    Sandbox: sb, SystemPrompt: "sys",
  })

  return s, root
}

func TestLocalOnlyRejectsForeignHostAndOrigin(t *testing.T) {
  s, _ := newServer(t)
  h := s.routes()

  req := httptest.NewRequest("GET", "/api/info", nil)
  req.Host = "evil.example:7777"
  rec := httptest.NewRecorder()
  h.ServeHTTP(rec, req)
  if rec.Code != http.StatusForbidden {
    t.Errorf("foreign Host: %d, want 403", rec.Code)
  }

  req = httptest.NewRequest("POST", "/api/command", strings.NewReader(`{"name":"clear"}`))
  req.Host = "127.0.0.1:7777"
  req.Header.Set("Origin", "https://evil.example")
  rec = httptest.NewRecorder()
  h.ServeHTTP(rec, req)
  if rec.Code != http.StatusForbidden {
    t.Errorf("cross-site POST: %d, want 403", rec.Code)
  }

  req = httptest.NewRequest("GET", "/api/info", nil)
  req.Host = "localhost:7777"
  rec = httptest.NewRecorder()
  h.ServeHTTP(rec, req)
  if rec.Code != http.StatusOK {
    t.Errorf("local request: %d, want 200", rec.Code)
  }
}

func TestRawIsJailedAndNeverServesHTML(t *testing.T) {
  s, _ := newServer(t)
  h := s.routes()

  req := httptest.NewRequest("GET", "/api/raw?path=../../etc/passwd", nil)
  req.Host = "127.0.0.1"
  rec := httptest.NewRecorder()
  h.ServeHTTP(rec, req)
  if rec.Code != http.StatusForbidden {
    t.Errorf("escape: %d, want 403", rec.Code)
  }

  req = httptest.NewRequest("GET", "/api/raw?path=page.html", nil)
  req.Host = "127.0.0.1"
  rec = httptest.NewRecorder()
  h.ServeHTTP(rec, req)
  ct := rec.Header().Get("Content-Type")
  if !strings.HasPrefix(ct, "text/plain") {
    t.Errorf("project HTML served as %q, want text/plain", ct)
  }
}

func TestServeRefusesNonLoopback(t *testing.T) {
  err := Serve(context.Background(), Config{ Addr: "0.0.0.0:7777" })
  if err == nil || !strings.Contains(err.Error(), "only binds to localhost") {
    t.Fatalf("err = %v", err)
  }
}

func TestChatStreamsAndWaitsForApproval(t *testing.T) {
  s, root := newServer(t)
  srv := httptest.NewServer(s.routes())
  defer srv.Close()

  resp, err := http.Post(srv.URL + "/api/chat", "application/json", strings.NewReader(`{"message":"make new.txt"}`))
  if err != nil {
    t.Fatal(err)
  }

  defer resp.Body.Close()

  events := map[string][]string{}
  sc := bufio.NewScanner(resp.Body)
  kind := ""
  deadline := time.After(10 * time.Second)
  for sc.Scan() {
    line := sc.Text()
    switch {
    case strings.HasPrefix(line, "event: "):
      kind = strings.TrimPrefix(line, "event: ")
    case strings.HasPrefix(line, "data: "):
      data := strings.TrimPrefix(line, "data: ")
      events[kind] = append(events[kind], data)
      if kind == "approve" {
        var a map[string]string
        json.Unmarshal([]byte(data), &a)
        if !strings.Contains(a["preview"], "+ hi") {
          t.Errorf("approval preview = %q", a["preview"])
        }

        body := `{"id":"` + a["id"] + `","ok":true}`
        r, err := http.Post(srv.URL + "/api/approve", "application/json", strings.NewReader(body))
        if err != nil || r.StatusCode != 200 {
          t.Fatalf("approve: %v %v", err, r.Status)
        }
      }
    }

    select {
    case <-deadline:
      t.Fatal("timed out reading the stream")
    default:
    }

    if kind == "done" && len(events["done"]) > 0 {
      break
    }
  }

  if len(events["tool"]) != 1 || len(events["approve"]) != 1 || len(events["done"]) != 1 {
    t.Fatalf("events = %v", events)
  }

  got, _ := os.ReadFile(filepath.Join(root, "new.txt"))
  if string(got) != "hi\n" {
    t.Fatalf("new.txt = %q (approval didn't apply the write)", got)
  }
}

// TestChatSavesAndResumes: a web turn is saved like a REPL turn, and
// a server started with that session shows it in /api/history.
func TestChatSavesAndResumes(t *testing.T) {
  t.Setenv("XDG_DATA_HOME", t.TempDir())
  root := t.TempDir()
  sb, _ := sandbox.New(root, sandbox.Options{})
  model := fakeModel(t)
  defer model.Close()

  client := llm.New(model.URL, "fake")
  cfg := Config{
    Loop: agent.New(client, sb), Client: client, Sandbox: sb,
    SystemPrompt: "sys", AutoApprove: true, Autosave: true,
  }

  srv := httptest.NewServer(New(context.Background(), cfg).routes())
  resp, err := http.Post(srv.URL + "/api/chat", "application/json", strings.NewReader(`{"message":"make new.txt"}`))
  if err != nil {
    t.Fatal(err)
  }

  io.ReadAll(resp.Body) // the handler saves before it returns
  resp.Body.Close()
  srv.Close()

  list, _ := session.List()
  if len(list) != 1 || list[0].Title == "" {
    t.Fatalf("sessions = %+v", list)
  }

  saved, err := session.Load(list[0].ID)
  if err != nil {
    t.Fatal(err)
  }

  cfg.Resumed = saved
  srv = httptest.NewServer(New(context.Background(), cfg).routes())
  defer srv.Close()

  resp, err = http.Get(srv.URL + "/api/history")
  if err != nil {
    t.Fatal(err)
  }

  defer resp.Body.Close()

  var h struct {
    ID      string
    Entries []historyEntry
  }

  json.NewDecoder(resp.Body).Decode(&h)
  if h.ID != saved.ID || len(h.Entries) != 3 || h.Entries[0].Text != "make new.txt" || h.Entries[1].Role != "tool" {
    t.Fatalf("history = %+v", h)
  }
}
