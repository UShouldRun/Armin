// Package web serves armin's chat in a browser on localhost: the same
// agent loop, tools and context management as the REPL, plus file
// previews (PDF, Markdown, code) the terminal can't show. Standard
// library only; the page is one embedded HTML file with inline CSS
// and plain JS.
package web

import (
  "context"
  "embed"
  "encoding/json"
  "errors"
  "fmt"
  "io/fs"
  "mime"
  "net"
  "net/http"
  "os"
  "path/filepath"
  "sort"
  "strconv"
  "strings"
  "sync"
  "sync/atomic"
  "syscall"
  "time"

  "armin/internal/agent"
  "armin/internal/convo"
  "armin/internal/llamacpp"
  "armin/internal/llm"
  "armin/internal/sandbox"
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

// Config is what Serve needs from cmd/armin.
type Config struct {
  Addr         string
  Loop         *agent.Loop
  Client       *llm.Client
  Models       *llamacpp.Manager
  Sandbox      *sandbox.Config
  SystemPrompt string
  // AutoApprove applies file writes without asking.
  AutoApprove bool

  // Resumed, if set, is the session to continue (armin -resume).
  Resumed *session.Saved
  // Autosave writes the session after every turn, like the REPL.
  Autosave bool
  // ScratchBase is the project's scratch folder; the session's own
  // scratchpad is a folder under it named by the session id.
  ScratchBase string
}

// Server holds the one browser session.
type Server struct {
  cfg Config

  mu        sync.Mutex // guards history and turns: one turn at a time
  history   []llm.Message
  tokens    int
  meta      session.Meta
  persisted int // messages of history already on disk

  approvals sync.Map // id → chan decision
  nextID    atomic.Uint64
}

type decision struct {
  OK       bool   `json:"ok"`
  Feedback string `json:"feedback"`
}

// New builds a server with a fresh session, oriented the same way
// the REPL's is (project manifest folded into the system prompt).
func New(ctx context.Context, cfg Config) *Server {
  if cfg.ScratchBase == "" && cfg.Loop.Scratch != nil {
    cfg.ScratchBase = cfg.Loop.Scratch.Dir
  }

  s := &Server{ cfg: cfg }
  if r := cfg.Resumed; r != nil {
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
  if err := session.Append(&s.meta, s.history, &s.persisted); err != nil {
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

  if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
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
  if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
  if h, _, err := net.SplitHostPort(hostport); err == nil {
    host = h
  }

  host = strings.Trim(host, "[]")
  if host == "localhost" {
    return true
  }

  ip := net.ParseIP(host)

  return ip != nil && ip.IsLoopback()
}

func writeJSON(w http.ResponseWriter, v any) {
  w.Header().Set("Content-Type", "application/json")
  json.NewEncoder(w).Encode(v)
}

// --- info / files / raw ---

// modelChoice is one entry of the page's model picker.
type modelChoice struct {
  Name  string `json:"name"`
  Model string `json:"model"`
}

type infoBody struct {
  Root      string        `json:"root"`
  Model     string        `json:"model"`
  Label     string        `json:"label"`
  Models    []modelChoice `json:"models"`
  Window    int      `json:"window"`
  Tokens    int      `json:"tokens"`
  Approx    int      `json:"approx"`
  Tier      string   `json:"tier"`
  Messages  int      `json:"messages"`
  AutoApply bool     `json:"auto_apply"`
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
  s.mu.Lock()
  stats := convo.Analyze(s.history)
  tokens := s.tokens
  s.mu.Unlock()

  b := infoBody{
    Root:      s.cfg.Sandbox.Root,
    Model:     s.cfg.Client.Model,
    Window:    s.cfg.Loop.WindowSize(),
    Tokens:    tokens,
    Approx:    stats.ApproxTokens,
    Messages:  stats.Messages,
    AutoApply: s.cfg.AutoApprove,
  }

  b.Tier = s.cfg.Loop.Tier()

  b.Label = s.label(b.Model)
  if s.cfg.Models != nil {
    for _, st := range s.cfg.Models.Names() {
      b.Models = append(b.Models, modelChoice{ Name: st.Name, Model: st.Model })
    }

    sort.Slice(b.Models, func(i, j int) bool {
      return b.Models[i].Name < b.Models[j].Name
    })
  }

  writeJSON(w, b)
}

func (s *Server) files(w http.ResponseWriter, r *http.Request) {
  root := s.cfg.Sandbox.Root
  var out []string
  filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
    if err != nil || len(out) >= maxListed {
      return nil
    }

    if d.IsDir() {
      if path != root && (tools.SkipDir(d.Name()) || strings.HasPrefix(d.Name(), ".")) {
        return filepath.SkipDir
      }

      return nil
    }

    if rel, err := filepath.Rel(root, path); err == nil {
      out = append(out, filepath.ToSlash(rel))
    }

    return nil
  })
  writeJSON(w, out)
}

// raw serves one project file for preview, jailed like every tool.
func (s *Server) raw(w http.ResponseWriter, r *http.Request) {
  resolved, err := s.cfg.Sandbox.ResolvePath(r.URL.Query().Get("path"))
  if err != nil {
    http.Error(w, err.Error(), http.StatusForbidden)
    return
  }

  info, err := os.Stat(resolved)
  if err != nil || info.IsDir() {
    http.Error(w, "not a file", http.StatusNotFound)
    return
  }

  if info.Size() > maxRawBytes {
    http.Error(w, "file too large to preview", http.StatusRequestEntityTooLarge)
    return
  }

  ctype := mime.TypeByExtension(filepath.Ext(resolved))
  if ctype == "" || strings.HasPrefix(ctype, "text/html") {
    // Never render project HTML as a page on this origin.
    ctype = "text/plain; charset=utf-8"
  }

  w.Header().Set("Content-Type", ctype)
  w.Header().Set("X-Content-Type-Options", "nosniff")
  w.Header().Set("Content-Security-Policy", "sandbox")
  http.ServeFile(w, r, resolved)
}

// --- chat ---

// sse writes one server-sent event and flushes it.
type sse struct {
  w  http.ResponseWriter
  fl http.Flusher
  mu sync.Mutex
}

func (e *sse) send(kind string, v any) {
  raw, _ := json.Marshal(v)
  e.mu.Lock()
  defer e.mu.Unlock()
  fmt.Fprintf(e.w, "event: %s\ndata: %s\n\n", kind, raw)
  e.fl.Flush()
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
  var in struct {
    Message string `json:"message"`
  }

  err := json.NewDecoder(r.Body).Decode(&in)
  if err != nil || strings.TrimSpace(in.Message) == "" {
    http.Error(w, "message is required", http.StatusBadRequest)
    return
  }

  fl, ok := w.(http.Flusher)
  if !ok {
    http.Error(w, "streaming unsupported", http.StatusInternalServerError)
    return
  }

  if !s.mu.TryLock() {
    http.Error(w, "a turn is already running", http.StatusConflict)
    return
  }

  defer s.mu.Unlock()

  w.Header().Set("Content-Type", "text/event-stream")
  w.Header().Set("Cache-Control", "no-cache")
  ev := &sse{ w: w, fl: fl }

  s.history = append(s.history, llm.Message{ Role: "user", Content: in.Message })

  hooks := agent.Hooks{
    Token:     func(d string) { ev.send("token", d) },
    Reasoning: func(d string) { ev.send("reasoning", d) },
    ToolCall: func(name string, args map[string]any) {
      ev.send("tool", map[string]any{
        "name": name,
        "args": args,
        "line": agent.CallLine(name, args),
      })
    },
    ToolResult: func(name, result string) {
      ev.send("result", agent.ResultLine(name, result))
    },
    Notice: func(msg string) { ev.send("notice", msg) },
    Progress: func(done, total int) {
      ev.send("progress", map[string]int{ "done": done, "total": total })
    },
    Usage: func(u llm.Usage) {
      s.tokens = u.Total()
      ev.send("usage", map[string]any{
        "tokens": s.tokens,
        "window": s.cfg.Loop.WindowSize(),
        "tier":   s.cfg.Loop.Tier(),
      })
    },
    Compacted: func(before, after int) {
      s.persisted = 0 // history was rewritten; next save is full
      ev.send("notice", compactedNote(before, after))
    },
  }

  hooks.Approve = func(tool, preview string) (bool, string) {
    if s.cfg.AutoApprove && !agent.AlwaysAsk(tool) {
      return true, ""
    }

    return s.ask(r.Context(), ev, tool, preview)
  }

  // A heartbeat while the turn runs: the page can then tell "armin
  // is working, the model is slow" from "the connection is gone".
  beat := make(chan struct{})
  go func() {
    t := time.NewTicker(pingEvery)
    defer t.Stop()
    for {
      select {
      case <-beat:
        return
      case <-t.C:
        ev.send("ping", time.Now().Unix())
      }
    }
  }()

  answer, err := s.cfg.Loop.Turn(r.Context(), &s.history, hooks)
  close(beat)
  if err != nil {
    ev.send("error", err.Error())
  } else {
    ev.send("done", answer)
  }

  // Saved after the answer is on screen: naming the session costs a
  // model call the user shouldn't wait for. A failed turn is saved
  // too — what it did (edits, runs) happened.
  s.save(context.WithoutCancel(r.Context()))
}

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

// historyEntry is one line of the conversation for the page to
// redraw a resumed session.
type historyEntry struct {
  Role string `json:"role"` // user, assistant, tool
  Text string `json:"text"`
}

// historyView is what the page shows of the history: what the user
// and the model said, and the tools called. Tool results, notes
// armin added and the system prompt stay out.
func (s *Server) historyView(w http.ResponseWriter, r *http.Request) {
  s.mu.Lock()
  defer s.mu.Unlock()

  out := []historyEntry{}
  for _, m := range s.history {
    switch m.Role {
    case "user":
      if !strings.HasPrefix(m.Content, "[armin]") {
        out = append(out, historyEntry{ "user", m.Content })
      }

    case "assistant":
      for _, c := range m.ToolCalls {
        out = append(out, historyEntry{ "tool", agent.CallLine(c.Function.Name, c.Function.Arguments) })
      }

      if strings.TrimSpace(m.Content) != "" {
        out = append(out, historyEntry{ "assistant", m.Content })
      }
    }
  }

  writeJSON(w, map[string]any{
    "title":   s.meta.Title,
    "id":      s.meta.ID,
    "entries": out,
  })
}

// ask sends a write's diff to the page and waits for its decision.
func (s *Server) ask(
  ctx context.Context, ev *sse, tool, preview string,
) (bool, string) {
  id := strconv.FormatUint(s.nextID.Add(1), 10)
  ch := make(chan decision, 1)
  s.approvals.Store(id, ch)
  defer s.approvals.Delete(id)

  ev.send("approve", map[string]string{ "id": id, "tool": tool, "preview": preview })
  select {
  case d := <-ch:
    return d.OK, d.Feedback
  case <-ctx.Done():
    return false, ""
  case <-time.After(approvalWait):
    return false, "no answer from the user in time"
  }
}

func (s *Server) approve(w http.ResponseWriter, r *http.Request) {
  var in struct {
    ID string `json:"id"`
    decision
  }

  if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
    http.Error(w, err.Error(), http.StatusBadRequest)
    return
  }

  v, ok := s.approvals.Load(in.ID)
  if !ok {
    http.Error(w, "no pending approval with that id", http.StatusNotFound)
    return
  }

  v.(chan decision) <- in.decision
  writeJSON(w, map[string]bool{ "ok": true })
}

func compactedNote(before, after int) string {
  return fmt.Sprintf(
    "compacted ~%dk → ~%dk tokens", before / 1000, after / 1000,
  )
}

// label is what serves a model, "" without a manager.
func (s *Server) label(name string) string {
  if s.cfg.Models == nil {
    return ""
  }

  return s.cfg.Models.Label(name)
}

// --- commands ---

func (s *Server) command(w http.ResponseWriter, r *http.Request) {
  var in struct {
    Name, Arg string
  }

  if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
    http.Error(w, err.Error(), http.StatusBadRequest)
    return
  }

  if !s.mu.TryLock() {
    http.Error(w, "a turn is running", http.StatusConflict)
    return
  }

  defer s.mu.Unlock()

  switch in.Name {
  case "clear":
    s.reset(r.Context())
    writeJSON(w, map[string]string{ "result": "conversation cleared" })

  case "compact":
    before := convo.Analyze(s.history).ApproxTokens
    if err := convo.Compact(r.Context(), s.cfg.Client, &s.history, 6, ""); err != nil {
      http.Error(w, err.Error(), http.StatusBadRequest)
      return
    }

    s.persisted = 0
    s.save(r.Context())

    after := convo.Analyze(s.history).ApproxTokens
    writeJSON(w, map[string]string{
      "result": compactedNote(before, after),
    })

  case "model":
    if s.cfg.Models == nil {
      http.Error(w, "no model manager", http.StatusBadRequest)
      return
    }

    if err := s.cfg.Models.Switch(r.Context(), s.cfg.Client, in.Arg); err != nil {
      http.Error(w, err.Error(), http.StatusBadRequest)
      return
    }

    s.cfg.Loop.Window = nil
    if win := s.cfg.Models.Window(in.Arg); win != nil {
      s.cfg.Loop.Window = win
    }

    if n, err := s.cfg.Client.ContextSize(r.Context()); err == nil && n > 0 {
      s.cfg.Loop.ContextSize = n
    }

    agent.SetModelLine(s.history, in.Arg, s.label(in.Arg))
    s.tokens = 0

    writeJSON(w, map[string]string{ "result": "switched to " + in.Arg })

  default:
    http.Error(w, "unknown command", http.StatusBadRequest)
  }
}
