// Package web serves armin's chat in a browser on localhost: the same
// agent loop, tools and context management as the REPL, plus file
// previews (PDF, Markdown, code) the terminal can't show. Standard
// library only; the page is one embedded HTML file (static/) with
// inline CSS and plain JS.
//
//   web.go       Serve (loopback only), the routes, the guard against
//                other sites and DNS rebinding, the session it saves
//   handlers.go  the API the page calls, a turn streamed as events
//
// display.runWeb starts it with the Mind's loop, client and models.
package web

import (
  "context"
  "embed"
  "errors"
  "fmt"
  "io/fs"
  "net"
  "net/http"
  "os"
  "path/filepath"
  "strconv"
  "strings"
  "syscall"
  "time"

  "armin/internal/agent"
  "armin/internal/convo"
  "armin/internal/llm"
  "armin/internal/session"
  "armin/internal/tools"
)

//go:embed static
var static embed.FS

const (
  // maxRawBytes caps one previewed file.
  maxRawBytes = 32 << 20
  // maxListed caps the file list.
  maxListed = 20000
  // approvalWait is how long a write waits for a click.
  approvalWait = 10 * time.Minute
  // pingEvery is the heartbeat while a turn runs.
  pingEvery = 5 * time.Second
)

// New builds a server with a fresh session, oriented the same way
// the REPL's is (project manifest folded into the system prompt).
func New(ctx context.Context, cfg Config) *Server {
  if cfg.ScratchBase == "" && cfg.Loop.Scratch != nil {
    cfg.ScratchBase = cfg.Loop.Scratch.Dir
  }

  s := &Server{ cfg: cfg }
  r := cfg.Resumed
  if r != nil {
    s.history = r.History
    s.meta = r.Meta
    // Today's prompt replaces the saved one (memories and skills may
    // have changed), so the first save rewrites the file.
    if len(s.history) > 0 && s.history[0].Role == "system" {
      s.history[0].Content = cfg.SystemPrompt
    }

    cfg.Loop.ElideOld(s.history)
    agent.SetModelLine(s.history, cfg.Client.Model, s.label(cfg.Client.Model))
    s.bindScratch()

    return s
  }

  s.reset(ctx)

  return s
}

// bindScratch gives the session its own scratchpad folder, like the
// REPL's; a legacy session keeps the project-wide one.
func (s *Server) bindScratch() {
  sp := s.cfg.Loop.Scratch
  if sp == nil {
    return
  }

  base := s.cfg.ScratchBase
  sp.Dir = base
  if len(s.meta.ID) == 36 {
    sp.Dir = filepath.Join(base, s.meta.ID)
  }
}

// save appends the turn to the session file, naming the session
// after its first turn. Called with s.mu held.
func (s *Server) save(ctx context.Context) {
  if !s.cfg.Autosave {
    return
  }

  if s.meta.Title == "" {
    tctx, cancel := context.WithTimeout(ctx, 30 * time.Second)
    s.meta.Title = convo.Title(tctx, s.cfg.Client, s.history)
    cancel()
    s.persisted = 0 // the title is in the first line
  }

  s.meta.Model = s.cfg.Client.Model
  err := session.Append(&s.meta, s.history, &s.persisted)
  if err != nil {
    fmt.Fprintf(os.Stderr, "armin: saving session: %s\n", err)
  }
}

// reset starts a new session: a fresh id, prompt and scratchpad.
func (s *Server) reset(ctx context.Context) {
  s.meta = session.Meta{
    ID: session.NewID(), Root: s.cfg.Sandbox.Root, Model: s.cfg.Client.Model,
  }

  s.persisted = 0
  s.bindScratch()

  prompt := s.cfg.SystemPrompt
  out, err := tools.ProjectManifest(ctx, s.cfg.Sandbox, tools.ProjectManifestInput{})
  if err == nil && out.Note != "" {
    prompt += "\n\n" + out.Note
  }

  s.history = []llm.Message{ { Role: "system", Content: prompt } }
  agent.SetModelLine(s.history, s.cfg.Client.Model, s.label(s.cfg.Client.Model))
  s.tokens = 0
}

// Serve listens on cfg.Addr until ctx ends. It refuses a non-loopback
// address: the API runs tools on the user's machine.
func Serve(ctx context.Context, cfg Config) error {
  host, _, err := net.SplitHostPort(cfg.Addr)
  if err != nil {
    return fmt.Errorf("\n\t... in Serve:\n\t %q: %w", cfg.Addr, err)
  }

  ip := net.ParseIP(host)
  if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
    return fmt.Errorf(
      "\n\t... in Serve:\n\t refusing to listen on %q: the web UI runs "+
        "tools on this machine, so it only binds to localhost",
      cfg.Addr,
    )
  }

  s := New(ctx, cfg)
  srv := &http.Server{
    Addr:              cfg.Addr,
    Handler:           s.routes(),
    ReadHeaderTimeout: 10 * time.Second,
  }

  go func() {
    <-ctx.Done()
    shut, cancel := context.WithTimeout(context.Background(), 5 * time.Second)
    defer cancel()
    srv.Shutdown(shut)
  }()

  ln, err := net.Listen("tcp", cfg.Addr)
  if errors.Is(err, syscall.EADDRINUSE) {
    return fmt.Errorf("\n\t... in Serve:\n\t %s", inUse(cfg.Addr))
  }

  if err != nil {
    return fmt.Errorf("\n\t... in Serve:\n\t%w", err)
  }

  fmt.Fprintf(os.Stderr, "armin web UI: http://%s\n", cfg.Addr)
  err = srv.Serve(ln)
  if err != nil && !errors.Is(err, http.ErrServerClosed) {
    return fmt.Errorf("\n\t... in Serve:\n\t%w", err)
  }

  return nil
}

func (s *Server) routes() http.Handler {
  mux := http.NewServeMux()
  page, _ := fs.Sub(static, "static")
  mux.Handle("GET /", http.FileServerFS(page))
  mux.HandleFunc("GET /api/info", s.info)
  mux.HandleFunc("GET /api/files", s.files)
  mux.HandleFunc("GET /api/history", s.historyView)
  mux.HandleFunc("GET /api/raw", s.raw)
  mux.HandleFunc("POST /api/chat", s.chat)
  mux.HandleFunc("POST /api/approve", s.approve)
  mux.HandleFunc("POST /api/command", s.command)

  return localOnly(mux)
}

// localOnly rejects requests whose Host isn't loopback (DNS
// rebinding) and cross-site POSTs (another page driving the API).
func localOnly(next http.Handler) http.Handler {
  return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if !isLocalHost(r.Host) {
      http.Error(w, "forbidden host", http.StatusForbidden)
      return
    }

    if r.Method != http.MethodGet {
      origin := r.Header.Get("Origin")
      origin = strings.TrimPrefix(origin, "http://")
      origin = strings.TrimPrefix(origin, "https://")
      if origin != "" && !isLocalHost(origin) {
        http.Error(w, "forbidden origin", http.StatusForbidden)
        return
      }
    }

    next.ServeHTTP(w, r)
  })
}

func isLocalHost(hostport string) bool {
  host := hostport
  h, _, err := net.SplitHostPort(hostport)
  if err == nil {
    host = h
  }

  host = strings.Trim(host, "[]")
  if host == "localhost" {
    return true
  }

  ip := net.ParseIP(host)

  return ip != nil && ip.IsLoopback()
}

// --- info / files / raw ---

// --- chat ---

// inUse explains a busy address: another armin web UI (it answers
// /api/info) or some other program.
func inUse(addr string) string {
  host, port, _ := net.SplitHostPort(addr)
  next, _ := strconv.Atoi(port)
  other := net.JoinHostPort(host, strconv.Itoa(next + 1))

  client := http.Client{ Timeout: time.Second }
  resp, err := client.Get("http://" + addr + "/api/info")
  if err == nil {
    resp.Body.Close()
    if resp.StatusCode == http.StatusOK {
      return "another armin web UI is already running at http://" + addr +
        ": open that one, or stop it first (Ctrl-C in its terminal), " +
        "or use another address (-web " + other + ")"
    }
  }

  return addr + " is in use by another program: pick another address (-web " + other + ")"
}

// --- commands ---

