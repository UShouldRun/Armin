// The API the page calls, one handler per route (see routes in
// web.go): info and files for the sidebar, raw for file previews
// (jailed to the project, never served as HTML), history to redraw a
// session, chat to run a turn as server-sent events, approve to
// answer a write the turn is waiting on, command for slash commands.

package web

import (
  "context"
  "encoding/json"
  "fmt"
  "io/fs"
  "mime"
  "net/http"
  "os"
  "path/filepath"
  "sort"
  "strconv"
  "strings"
  "time"

  "armin/internal/agent"
  "armin/internal/catalog"
  "armin/internal/convo"
  "armin/internal/llm"
  "armin/internal/tools"
  "armin/internal/ui"
)

func writeJSON(w http.ResponseWriter, v any) {
  w.Header().Set("Content-Type", "application/json")
  json.NewEncoder(w).Encode(v)
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
      choice := modelChoice{ Name: st.Name, Model: st.Model }
      cost, ok := catalog.Price(st.Model)
      if ok && !st.Managed {
        choice.Cost = &cost
      }

      b.Models = append(b.Models, choice)
    }

    sort.Slice(b.Models, func(i, j int) bool {
      return b.Models[i].Name < b.Models[j].Name
    })

    b.Warning = s.cfg.Models.Warning
    b.Hosted = s.cfg.Models.OpenRouter()
    if b.Hosted != "" {
      b.Suggested = catalog.Suggestions()
      url, key := s.cfg.Models.Endpoint(b.Hosted)
      b.Balance = catalog.CachedCredits(url, key)
    }
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

    rel, err := filepath.Rel(root, path)
    if err == nil {
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

func (e *sse) send(kind string, v any) {
  raw, _ := json.Marshal(v)
  e.mu.Lock()
  defer e.mu.Unlock()
  fmt.Fprintf(e.w, "event: %s\ndata: %s\n\n", kind, raw)
  e.fl.Flush()
}

// chat runs one turn for POST /api/chat and streams it back as
// server-sent events: the request is decoded and the turn planned
// (effort, kind of task), then the agent loop runs with hooks that
// turn each token, tool call and approval into an event (see hooks),
// a heartbeat keeps the connection visibly alive, and the session is
// saved once the answer is on screen. One turn at a time.
func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
  var in struct {
    Message string `json:"message"`
    Effort  string `json:"effort"` // optional: auto, off, low, medium, high
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

  effort := s.cfg.Effort
  asked, ok := agent.ParseEffort(in.Effort)
  if ok {
    effort = asked
  }

  auto := effort == agent.EffortAuto
  effort, task := agent.PlanTurn(
    r.Context(), s.cfg.Client, s.cfg.Loop.Aids, effort, in.Message,
  )
  if auto {
    ev.send("effort", effort.String())
  }

  hooks := s.hooks(r.Context(), ev, effort, task)

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
    // collapsed message for the page, the full chain expandable
    // under it (see the "error" case in index.html)
    message, trace := ui.SplitTrace(err.Error())
    if j := ui.JsonErrorMessage(message); j != "" {
      message = j
    }

    ev.send("error", map[string]any{ "message": message, "trace": trace })
  } else {
    ev.send("done", answer)
  }

  // Saved after the answer is on screen: naming the session costs a
  // model call the user shouldn't wait for. A failed turn is saved
  // too — what it did (edits, runs) happened.
  s.save(context.WithoutCancel(r.Context()))
}

// hooks turns a turn's callbacks into server-sent events for the
// page; approvals wait for a click (or pass under -yes, except what
// always asks).
func (s *Server) hooks(
  ctx context.Context, ev *sse, effort agent.Effort, task agent.Task,
) agent.Hooks {
  hooks := agent.Hooks{
    Effort:    effort.Wire(),
    Task:      task,
    Aside:     func(msg string) { ev.send("aside", msg) },
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

    return s.ask(ctx, ev, tool, preview)
  }

  return hooks
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

  err := json.NewDecoder(r.Body).Decode(&in)
  if err != nil {
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

func (s *Server) command(w http.ResponseWriter, r *http.Request) {
  var in struct {
    Name, Arg string
  }

  err := json.NewDecoder(r.Body).Decode(&in)
  if err != nil {
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
    err := convo.Compact(r.Context(), s.cfg.Client, &s.history, 6, "")
    if err != nil {
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

    err := s.cfg.Models.Switch(r.Context(), s.cfg.Client, in.Arg)
    if err != nil {
      http.Error(w, err.Error(), http.StatusBadRequest)
      return
    }

    s.cfg.Loop.Window = nil
    win := s.cfg.Models.Window(in.Arg)
    if win != nil {
      s.cfg.Loop.Window = win
    }

    n, err := s.cfg.Client.ContextSize(r.Context())
    if err == nil && n > 0 {
      s.cfg.Loop.ContextSize = n
    }

    agent.SetModelLine(s.history, in.Arg, s.label(in.Arg))
    s.tokens = 0

    writeJSON(w, map[string]string{ "result": "switched to " + in.Arg })

  default:
    http.Error(w, "unknown command", http.StatusBadRequest)
  }
}
