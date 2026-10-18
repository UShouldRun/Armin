// nvim's chat: the armin.* commands (ask, clear, history, model,
// stop) run a turn on the shared conversation, relaying its events to
// nvim (toNvim) and to joined terminals (emit); askHooks wires one
// turn's callbacks to both.

package lspserver

import (
  "context"
  "encoding/json"
  "errors"
  "fmt"
  "sort"
  "strings"

  "armin/internal/agent"
  "armin/internal/llm"
  "armin/internal/share"
)

// respondAskError reports a Turn failure to the client. A model
// that can't call tools at all gets a clean, actionable one-liner
// instead of llama-server's raw (and, over JSON-RPC, single-line-
// mangled) HTTP error body — anything else is passed through as-is.
func (s *Server) respondAskError(id json.RawMessage, err error) {
  if errors.Is(err, llm.ErrToolsUnsupported) {
    model := "this model"
    if s.loop != nil {
      model = s.loop.LLM.Model
    }

    s.respondError(id, errInternal, fmt.Sprintf(
      "%s doesn't support tool calling — switch models with :ArminModel", model,
    ))
    return
  }
  s.respondError(id, errInternal, err.Error())
}

func (s *Server) executeCommand(
  ctx context.Context, id json.RawMessage, p ExecuteCommandParams,
) {
  switch p.Command {
  case "armin.ask":
    s.executeAsk(ctx, id, p)
  case "armin.model":
    s.executeModel(ctx, id, p)
  case "armin.clear":
    s.executeClear(ctx, id)
  case "armin.history":
    s.executeHistory(ctx, id)
  case "armin.stop":
    s.Stop()
    s.respond(id, true, nil)
  default:
    s.respondError(
      id, errMethodNotFound,
      fmt.Sprintf("unknown command %q", p.Command),
    )
  }
}

func (s *Server) executeAsk(
  ctx context.Context, id json.RawMessage, p ExecuteCommandParams,
) {
  if len(p.Arguments) == 0 {
    s.respondError(
      id, errInvalidRequest,
      "armin.ask requires an argument: { question, uri }",
    )
    return
  }

  var args AskArgs
  err := json.Unmarshal(p.Arguments[0], &args)
  if err != nil {
    s.respondError(id, errInvalidRequest, err.Error())
    return
  }
  if strings.TrimSpace(args.Question) == "" {
    s.respondError(id, errInvalidRequest, "question is empty")
    return
  }

  file := ""
  if args.URI != "" {
    file = s.relPath(args.URI)
  }

  // Joined: the host runs the turn; its events finish it here.
  if s.remote != nil {
    err = s.remote.Ask(args.Question, file)
    if err != nil {
      s.respondError(id, errInternal, err.Error())
      return
    }

    s.respond(id, "", nil)
    return
  }

  answer, err := s.chatTurn(ctx, args.Question, file, s.origin, id)
  switch {
  case errors.Is(err, share.ErrBusy):
    s.respondError(id, errInvalidRequest, err.Error())
  case errors.Is(err, context.Canceled) && ctx.Err() == nil:
    s.respondError(id, errRequestCancelled, "stopped")
  case err != nil:
    s.respondAskError(id, err)
  default:
    s.respond(id, answer, nil)
  }
}

// chatTurn runs one question through the shared conversation, for
// this nvim (id set) or a joined armin. Every frontend sees its
// events.
func (s *Server) chatTurn(
  ctx context.Context, question, file, origin string, id json.RawMessage,
) (string, error) {
  if !s.chatMu.TryLock() {
    return "", share.ErrBusy
  }

  defer s.chatMu.Unlock()
  defer s.snapshot()

  s.emit(share.Event{ Type: "user", Delta: question, Origin: origin })

  content := share.Question(question, file)

  // The system prompt only changes after armin.model, so the
  // conversation's prefix stays byte-stable for the KV cache.
  model  := s.loop.LLM.Model
  system := agent.WithModelLine(
    s.system + citationInstruction, model, s.models.Label(model),
  )
  if len(s.chat) == 0 {
    s.chat = []llm.Message{ { Role: "system" } }
  }

  s.chat[0].Content = system
  s.chat = append(s.chat, llm.Message{ Role: "user", Content: content })

  turnCtx, cancel := context.WithCancel(ctx)
  defer cancel()

  s.runningMu.Lock()
  s.chatCancel = cancel
  if len(id) > 0 {
    s.running[string(id)] = cancel
  }

  s.runningMu.Unlock()

  defer func() {
    s.runningMu.Lock()
    s.chatCancel = nil
    delete(s.running, string(id))
    s.runningMu.Unlock()
  }()

  answer, err := s.loop.Turn(turnCtx, &s.chat, s.askHooks(id, origin))
  switch {
  case err == nil:
    s.emit(share.Event{ Type: "done", Delta: answer, Origin: origin })
  case turnCtx.Err() == context.Canceled && ctx.Err() == nil:
    s.chat = dropDangling(s.chat)
    err = context.Canceled
    s.emit(share.Event{ Type: "error", Delta: "stopped", Origin: origin })
  default:
    s.chat = dropDangling(s.chat)
    s.emit(share.Event{ Type: "error", Delta: firstLine(err), Origin: origin })
  }

  return answer, err
}

// Stop cancels the chat turn in flight, whoever asked.
func (s *Server) Stop() {
  if s.remote != nil {
    s.remote.Stop()
    return
  }

  s.runningMu.Lock()
  cancel := s.chatCancel
  s.runningMu.Unlock()

  if cancel != nil {
    cancel()
  }
}

// executeClear answers "armin.clear": forget the conversation.
func (s *Server) executeClear(ctx context.Context, id json.RawMessage) {
  var err error
  if s.remote != nil {
    err = s.remote.Clear(ctx)
  } else {
    err = s.Clear(s.origin)
  }

  if err != nil {
    s.respondError(id, errInvalidRequest, err.Error())
    return
  }

  s.respond(id, true, nil)
}

func (s *Server) executeHistory(ctx context.Context, id json.RawMessage) {
  if s.remote == nil {
    s.respond(id, s.History(), nil)
    return
  }

  lines, err := s.remote.Lines(ctx)
  if err != nil {
    s.respondError(id, errInternal, err.Error())
    return
  }

  s.respond(id, lines, nil)
}

// Clear forgets the conversation, for whoever asked.
func (s *Server) Clear(origin string) error {
  if !s.chatMu.TryLock() {
    return errors.New("still answering — stop it first")
  }

  s.chat = nil
  s.chatMu.Unlock()
  s.snapshot()
  s.emit(share.Event{ Type: "cleared", Origin: origin })

  return nil
}

// dropDangling cuts a stopped turn back to a history the model can
// continue: an assistant message whose tool calls didn't all get
// results goes, with the results it did get.
func dropDangling(h []llm.Message) []llm.Message {
  for i := len(h) - 1; i >= 0; i-- {
    if h[i].Role == "tool" {
      continue
    }

    if h[i].Role == "assistant" && len(h[i].ToolCalls) > len(h) - 1 - i {
      return h[:i]
    }

    return h
  }

  return h
}

func (s *Server) cancel(id json.RawMessage) {
  s.runningMu.Lock()
  cancel := s.running[string(id)]
  s.runningMu.Unlock()

  if cancel != nil {
    cancel()
  }
}

// askHooks builds the streaming callbacks a chat turn passes into
// Turn; every event goes to nvim and each attached TUI, tagged with
// who asked. Reasoning is announced once per stretch, not per
// delta: the frontends show "thinking", not the trace itself.
func (s *Server) askHooks(id json.RawMessage, origin string) agent.Hooks {
  thinking := false
  send := func(e share.Event) {
    e.ID, e.Origin = id, origin
    s.emit(e)
  }

  return agent.Hooks{
    Token: func(delta string) {
      thinking = false
      send(share.Event{ Type: "token", Delta: delta })
    },
    Reasoning: func(string) {
      if !thinking {
        thinking = true
        send(share.Event{ Type: "reasoning" })
      }
    },
    ToolCall: func(name string, args map[string]any) {
      thinking = false
      send(share.Event{ Type: "tool_call", Name: name, Args: args })
    },
    ToolResult: func(name, result string) {
      send(share.Event{
        Type:  "tool_result",
        Name:  name,
        Delta: agent.ResultLine(name, result),
      })
    },
    Notice: func(msg string) {
      send(share.Event{ Type: "notice", Delta: msg })
    },
    Usage: func(u llm.Usage) {
      send(share.Event{
        Type:   "usage",
        Tokens: u.Total(),
        Window: s.loop.WindowSize(),
        Tier:   s.loop.Tier(),
      })
    },
  }
}

// emit sends a chat event to this nvim and every joined armin.
func (s *Server) emit(e share.Event) {
  s.toNvim(e)
  s.hub.Emit(e)
}

// toNvim notifies nvim of e. The plugin knows its own questions by
// the origin "nvim", whatever the host calls them.
func (s *Server) toNvim(e share.Event) {
  switch e.Origin {
  case s.origin:
    e.Origin = "nvim"
  case "nvim":
    e.Origin = "another nvim"
  }

  s.notify("armin/askEvent", e)
}

// warnGPU shows, once nvim is ready for messages, why local models
// will be slow — this instance's warning, or the host's.
func (s *Server) warnGPU() {
  warning := ""
  switch {
  case s.remote != nil:
    warning = s.remote.Model.Warning
  case s.models != nil:
    warning = s.models.Warning
  }

  if warning == "" {
    return
  }

  s.notify("window/showMessage", map[string]any{
    "type":    2,
    "message": "armin: " + warning,
  })
}

// relay forwards the host's events to nvim; when the host goes away
// it says so, since nothing here can answer without it.
func (s *Server) relay() {
  for e := range s.remote.Events {
    s.toNvim(e)
  }

  s.notify("window/showMessage", map[string]any{
    "type":    1,
    "message": fmt.Sprintf(
      "armin: instance %d went away — :ArminStop then :ArminLsp to start " +
        "one here",
      s.remote.Instance,
    ),
  })
}

// executeModel answers "armin.model": switch the chat model live
// (no arguments needed elsewhere — the LSP process just keeps
// running with s.loop.LLM mutated in place from then on, see
// llamacpp.Manager.Switch), or report the current model plus every
// configured one when called with no name, mirroring the REPL's
// /model command.
func (s *Server) executeModel(
  ctx context.Context, id json.RawMessage, p ExecuteCommandParams,
) {
  var args ModelArgs
  if len(p.Arguments) > 0 {
    err := json.Unmarshal(p.Arguments[0], &args)
    if err != nil {
      s.respondError(id, errInvalidRequest, err.Error())
      return
    }
  }

  var info share.ModelInfo
  var err error
  switch {
  case s.remote != nil && args.Name != "":
    info, err = s.remote.SwitchModel(ctx, args.Name)
  case s.remote != nil:
    info, err = s.remote.Models(ctx)
  case args.Name != "":
    info, err = s.SwitchModel(ctx, args.Name)
  default:
    info = s.Models()
  }

  if err != nil {
    s.respondError(id, errInvalidRequest, err.Error())
    return
  }

  s.respond(id, info, nil)
}

// SwitchModel moves chat (and completion, sharing its Client) to
// name, and tells every frontend.
func (s *Server) SwitchModel(ctx context.Context, name string) (share.ModelInfo, error) {
  if !s.models.Has(name) {
    return share.ModelInfo{}, fmt.Errorf("no model %q in config.conf", name)
  }

  err := s.models.SwitchChat(ctx, s.loop.LLM, s.follower, name)
  if err != nil {
    return share.ModelInfo{}, err
  }

  info := s.Models()
  s.emit(share.Event{ Type: "model", Model: &info })

  return info, nil
}

// Models is the current models plus every configured one, by name.
func (s *Server) Models() share.ModelInfo {
  info := share.ModelInfo{
    Current: s.loop.LLM.Model, Completion: s.completionLLM.Model,
    Warning: s.models.Warning,
  }
  if s.hub != nil {
    info.Instance = s.hub.Instance
  }

  for _, status := range s.models.Names() {
    info.Configured = append(info.Configured, share.ModelChoice{
      Name: status.Name, Model: status.Model,
      Managed: status.Managed, Running: status.Running,
    })
  }

  sort.Slice(info.Configured, func(i, j int) bool {
    return info.Configured[i].Name < info.Configured[j].Name
  })

  hosted := s.models.OpenRouter()
  url, key := s.models.Endpoint(hosted)
  share.Suggest(&info, hosted, url, key)

  return info
}

// ask runs one stateless turn for hover through the same agent loop
// the REPL uses: fresh history every call, full tool access.
func (s *Server) ask(
  ctx context.Context, question string, hooks agent.Hooks,
) (string, error) {
  askCtx, cancel := context.WithTimeout(ctx, askTimeout)
  defer cancel()

  // The model line is set per ask, so it's right after armin.model.
  model := s.loop.LLM.Model
  system := agent.WithModelLine(s.system, model, s.models.Label(model))

  history := []llm.Message{
    { Role: "system", Content: system },
    { Role: "user", Content: question },
  }
  return s.loop.Turn(askCtx, &history, hooks)
}
