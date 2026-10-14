// Package lspserver exposes armin's codebase-grounded agent as a
// minimal LSP server: hover to explain code under the cursor, plus
// one workspace command for free-form questions. The point is to
// ride Neovim's built-in vim.lsp client — no plugin, no bespoke UI
// — so a minimal nvim config stays minimal. Started explicitly
// (nvim side never auto-attaches this on buffer open; see
// docs/nvim.md), on -root and -lsp.
package lspserver

import (
  "bufio"
  "context"
  "crypto/sha256"
  "encoding/hex"
  "encoding/json"
  "errors"
  "fmt"
  "io"
  "net/url"
  "os"
  "path/filepath"
  "sort"
  "strings"
  "time"

  "armin/internal/agent"
  "armin/internal/ctags"
  "armin/internal/llamacpp"
  "armin/internal/llm"
  "armin/internal/sandbox"
  "armin/internal/share"
)

// askTimeout bounds one hover or workspace/executeCommand round
// trip — generous, since a question may take a few tool-call
// rounds against a small local model.
const askTimeout = 2 * time.Minute

// hoverContextLines is how many lines of surrounding code, each
// side of the cursor, get sent along with a hover request.
const hoverContextLines = 6

// maxHoverChars caps what comes back in a hover popup — enforced
// in code, not left to the model's sense of "concise" (same
// philosophy as the sandbox's output truncation).
const maxHoverChars = 1500

// citationInstruction ends the chat's system prompt (armin.ask
// only — not hover, whose popup can't render the markup and is
// already grounded by a ctags fact when possible). It asks the model
// to ground anything worth flagging (a risk, a doubt, a notable
// behavior) in its exact location, using a convention the nvim
// client renders as a jump-to-reference rather than raw brackets.
const citationInstruction = "\n\n" +
  "When you flag something worth the user's attention — a risk, a " +
  "doubt about correctness, a notable or surprising behavior — " +
  "ground that specific claim in its exact location using the form " +
  "[[path/to/file.go:123]] (or [[path/to/file.go:120-135]] for a " +
  "range) right after the claim. Use this only for things actually " +
  "worth flagging, never for routine file mentions."

// New builds a Server. loop and sb are the same agent/sandbox pair
// the REPL uses — hover and ask questions get the same
// tool-grounded answers, just delivered through vim.lsp instead of
// a terminal chat. completionLLM is a separate Client, ideally
// pointed at a fill-in-the-middle-capable model, used only for
// inline completion; it never goes through loop (no tool calls, no
// chat formatting — see internal/llm.Client.Complete) and is a
// distinct Client (not just a different model name on loop.LLM)
// because llama.cpp — unlike Ollama — serves exactly one model per
// process, so a separate completion model means a separate
// llama-server. models is the same Manager cmd/armin/main.go built
// both Clients from, kept here so armin.model can switch loop.LLM
// live (see executeModel). symbols is the ctags index backing
// textDocument/definition, workspace/symbol, and hover grounding —
// cmd/armin/main.go builds one shared *ctags.Index and passes the
// same one here and into loop.Symbols, so -lsp doesn't scan the
// repo twice; if no universal-ctags binary was found at startup,
// those three degrade gracefully (empty results / plain LLM-only
// hover) rather than failing the server.
func New(
  loop *agent.Loop, sb *sandbox.Config,
  systemPrompt string, completionLLM *llm.Client,
  models *llamacpp.Manager, symbols *ctags.Index,
) *Server {
  return &Server{
    loop:          loop,
    sandbox:       sb,
    system:        systemPrompt,
    completionLLM: completionLLM,
    models:        models,
    symbols:       symbols,
    docs:          make(map[string]string),
    edits:         make(map[string]docEdit),
    hoverCache:    make(map[string]string),
    inflight:      make(map[string]inflightEntry),
    running:       make(map[string]context.CancelFunc),
    origin:        "nvim",
  }
}

// NewProxy builds a Server for an nvim joining the project's armin
// instance: definition and symbols answer here, from ctags; chat,
// hover, completion and models go to the host.
func NewProxy(sb *sandbox.Config, symbols *ctags.Index, host *share.Client) *Server {
  return &Server{
    sandbox:    sb,
    symbols:    symbols,
    docs:       make(map[string]string),
    edits:      make(map[string]docEdit),
    hoverCache: make(map[string]string),
    inflight:   make(map[string]inflightEntry),
    running:    make(map[string]context.CancelFunc),
    remote:     host,
    origin:     host.Origin,
  }
}

// Follow makes completion follow chat between local models (nil:
// completion_model pins it).
func (s *Server) Follow(completion *llm.Client) { s.follower = completion }

// Share makes this server's conversation and models the project's
// instance (see share.Listen).
func (s *Server) Share(hub *share.Hub) { s.hub = hub }

// Serve reads Content-Length-framed JSON-RPC from in and writes
// responses/notifications to out until in hits EOF or exit fires.
func (s *Server) Serve(
  ctx context.Context, in io.Reader, out io.Writer,
) error {
  s.out = out
  reader := bufio.NewReader(in)

  if s.remote != nil {
    go s.relay()
  }

  for {
    raw, err := readMessage(reader)
    if err == io.EOF {
      return nil
    }
    if err != nil {
      return fmt.Errorf("\n\t... in Serve:\n\t%w", err)
    }

    var msg rpcMessage
    err = json.Unmarshal(raw, &msg)
    if err != nil {
      continue // malformed frame — nothing sane to respond with
    }

    // exit returns instead of calling os.Exit, so the caller's
    // deferred cleanup runs — it stops the llama-server processes
    // (and any cloud machine) armin started.
    if msg.Method == "exit" {
      if s.shutdownReceived {
        return nil
      }
      return ErrExitWithoutShutdown
    }

    s.handle(ctx, msg)
  }
}

// ErrExitWithoutShutdown is the LSP spec's "exit before shutdown":
// the server stops, with a failing exit code.
var ErrExitWithoutShutdown = errors.New("lsp: exit received before shutdown")

func (s *Server) handle(ctx context.Context, msg rpcMessage) {
  switch msg.Method {
  case "initialize":
    s.respond(msg.ID, initializeResult(), nil)

  case "initialized":
    s.warnGPU()

  case "$/setTrace", "workspace/didChangeConfiguration":
    // notifications we accept and ignore

  case "$/cancelRequest":
    var p CancelParams
    err := json.Unmarshal(msg.Params, &p)
    if err == nil {
      s.cancel(p.ID)
    }

  case "shutdown":
    s.shutdownReceived = true
    s.respond(msg.ID, nil, nil)

  case "textDocument/didOpen":
    var p DidOpenParams
    err := json.Unmarshal(msg.Params, &p)
    if err == nil {
      s.setDoc(p.TextDocument.URI, p.TextDocument.Text)
    }

  case "textDocument/didChange":
    var p DidChangeParams
    err := json.Unmarshal(msg.Params, &p)
    if err == nil && len(p.ContentChanges) > 0 {
      last := p.ContentChanges[len(p.ContentChanges) - 1]
      s.setDoc(p.TextDocument.URI, last.Text)
    }

  case "textDocument/didClose":
    var p DidCloseParams
    err := json.Unmarshal(msg.Params, &p)
    if err == nil {
      s.dropDoc(p.TextDocument.URI)
    }

  case "textDocument/hover":
    var p HoverParams
    err := json.Unmarshal(msg.Params, &p)
    if err != nil {
      s.respondError(msg.ID, errInvalidRequest, err.Error())
      return
    }
    go s.hover(ctx, msg.ID, p)

  case "workspace/executeCommand":
    var p ExecuteCommandParams
    err := json.Unmarshal(msg.Params, &p)
    if err != nil {
      s.respondError(msg.ID, errInvalidRequest, err.Error())
      return
    }
    go s.executeCommand(ctx, msg.ID, p)

  case "textDocument/inlineCompletion":
    var p InlineCompletionParams
    err := json.Unmarshal(msg.Params, &p)
    if err != nil {
      s.respondError(msg.ID, errInvalidRequest, err.Error())
      return
    }
    go s.inlineCompletion(ctx, msg.ID, p)

  case "textDocument/definition":
    var p DefinitionParams
    err := json.Unmarshal(msg.Params, &p)
    if err != nil {
      s.respondError(msg.ID, errInvalidRequest, err.Error())
      return
    }
    go s.definition(ctx, msg.ID, p)

  case "workspace/symbol":
    var p WorkspaceSymbolParams
    err := json.Unmarshal(msg.Params, &p)
    if err != nil {
      s.respondError(msg.ID, errInvalidRequest, err.Error())
      return
    }
    go s.workspaceSymbol(ctx, msg.ID, p)

  default:
    if len(msg.ID) > 0 {
      s.respondError(
        msg.ID, errMethodNotFound,
        fmt.Sprintf("method not supported: %s", msg.Method),
      )
    }
    // unhandled notifications are silently ignored, per spec
  }
}

func (s *Server) hover(
  ctx context.Context, id json.RawMessage, p HoverParams,
) {
  text := s.doc(p.TextDocument.URI)
  if text == "" {
    s.respond(id, nil, nil)
    return
  }

  snippet, lineText := contextWindow(
    text, p.Position.Line, hoverContextLines,
  )
  if strings.TrimSpace(lineText) == "" {
    s.respond(id, nil, nil)
    return
  }

  // Ground the answer in a deterministic fact from the ctags index
  // when the cursor sits on a known symbol, instead of leaving the
  // model to guess where something is defined.
  word := wordAt(lineText, p.Position.Character)
  fact := s.symbolFact(ctx, word, p.TextDocument.URI)

  key := cacheKey(p.TextDocument.URI, p.Position.Line, snippet + "|" + word)
  cached, ok := s.cachedHover(key)
  if ok {
    s.respond(id, hoverResult(cached), nil)
    return
  }

  question := fmt.Sprintf(
    "Explain concisely (max one short paragraph, no preamble) "+
      "what this code does. File: %s, near line %d%s:\n\n```\n%s\n```",
    s.relPath(p.TextDocument.URI), p.Position.Line + 1, fact, snippet,
  )

  answer, err := s.explain(ctx, question)
  if err != nil {
    s.respondAskError(id, err)
    return
  }

  answer = capLength(answer, maxHoverChars)
  s.storeHover(key, answer)
  s.respond(id, hoverResult(answer), nil)
}

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
      "armin: instance %d went away — :ArminStop then :ArminLsp to start one here",
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

// setDoc stores a document's new text and, when it replaces an
// older one, the line of the first byte that changed.
func (s *Server) setDoc(uri, text string) {
  s.docsMu.Lock()
  defer s.docsMu.Unlock()

  old, ok := s.docs[uri]
  if ok && old != text {
    i, n := 0, min(len(old), len(text))
    for i < n && old[i] == text[i] {
      i++
    }

    s.editSeq++
    s.edits[uri] = docEdit{
      line: strings.Count(text[:i], "\n"), seq: s.editSeq,
    }
  }

  s.docs[uri] = text
}

func (s *Server) dropDoc(uri string) {
  s.docsMu.Lock()
  defer s.docsMu.Unlock()
  delete(s.docs, uri)
  delete(s.edits, uri)
}

func (s *Server) doc(uri string) string {
  s.docsMu.Lock()
  defer s.docsMu.Unlock()
  return s.docs[uri]
}

func (s *Server) cachedHover(key string) (string, bool) {
  s.hoverMu.Lock()
  defer s.hoverMu.Unlock()
  v, ok := s.hoverCache[key]
  return v, ok
}

func (s *Server) storeHover(key, value string) {
  s.hoverMu.Lock()
  defer s.hoverMu.Unlock()
  s.hoverCache[key] = value
}

// relPath renders a file:// URI relative to the sandbox root for
// prompts — shorter and more legible to the model than an absolute
// path or the raw URI.
func (s *Server) relPath(uri string) string {
  u, err := url.Parse(uri)
  if err != nil || u.Scheme != "file" {
    return uri
  }

  rel, err := filepath.Rel(s.sandbox.Root, u.Path)
  if err != nil {
    return u.Path
  }
  return rel
}

func contextWindow(
  text string, line, radius int,
) (snippet, lineText string) {
  start, end, ok := lineOffset(text, line)
  if !ok {
    return "", ""
  }
  lineText = text[start:end]

  winStart, _, ok := lineOffset(text, line - radius)
  if !ok {
    winStart = 0
  }
  _, winEnd, ok := lineOffset(text, line + radius)
  if !ok {
    winEnd = len(text)
  }

  return text[winStart:winEnd], lineText
}

func cacheKey(uri string, line int, snippet string) string {
  sum := sha256.Sum256([]byte(snippet))
  return fmt.Sprintf("%s:%d:%s", uri, line, hex.EncodeToString(sum[:8]))
}

func capLength(s string, max int) string {
  if len(s) <= max {
    return s
  }
  return s[:max] + "\n\n…(truncated)"
}

func (s *Server) respond(id json.RawMessage, result any, errObj *rpcError) {
  if len(id) == 0 {
    return // notification — nothing to respond to
  }
  s.write(rpcResponse{
    JSONRPC: "2.0", ID: id, Result: result, Error: errObj,
  })
}

func (s *Server) respondError(id json.RawMessage, code int, message string) {
  s.respond(id, nil, &rpcError{ Code: code, Message: message })
}

func (s *Server) write(msg rpcResponse) {
  s.outMu.Lock()
  defer s.outMu.Unlock()
  err := writeMessage(s.out, msg)
  if err != nil {
    fmt.Fprintln(os.Stderr, "armin lsp: write failed:", err)
  }
}

// notify pushes a server-to-client notification — no id, no reply
// expected. Shares out/outMu with write and respond so notifications
// (armin/askEvent) never interleave mid-frame with a concurrent
// request's response — every executeCommand/hover runs on its own
// goroutine (see handle), all writing through this one lock.
func (s *Server) notify(method string, params any) {
  s.outMu.Lock()
  defer s.outMu.Unlock()
  msg := rpcNotification{ JSONRPC: "2.0", Method: method, Params: params }
  err := writeMessage(s.out, msg)
  if err != nil {
    fmt.Fprintln(os.Stderr, "armin lsp: write failed:", err)
  }
}

// History is the conversation as displayed, as of the last turn.
func (s *Server) History() []share.ChatLine {
  lines := s.lines.Load()
  if lines == nil {
    return []share.ChatLine{}
  }

  return *lines
}

// snapshot refreshes History; the caller holds chatMu or is the
// only one touching chat.
func (s *Server) snapshot() {
  lines := share.Lines(s.chat)
  s.lines.Store(&lines)
}

// Ask runs a joined armin's question (see share.Host).
func (s *Server) Ask(ctx context.Context, question, file, origin string) error {
  _, err := s.chatTurn(ctx, question, file, origin, nil)
  return err
}

// Complete runs a joined armin's inline completion.
func (s *Server) Complete(ctx context.Context, f share.Fill) (string, error) {
  return s.completionLLM.Complete(ctx, f.Prefix, f.Suffix, f.Opts)
}

// Explain answers a joined armin's hover question.
func (s *Server) Explain(ctx context.Context, question string) (string, error) {
  return s.ask(ctx, question, agent.Hooks{})
}

// explain is hover's question, here or at the host.
func (s *Server) explain(ctx context.Context, question string) (string, error) {
  if s.remote != nil {
    return s.remote.Explain(ctx, question)
  }

  return s.ask(ctx, question, agent.Hooks{})
}

// complete is inline completion, here or at the host.
func (s *Server) complete(ctx context.Context, f share.Fill) (string, error) {
  if s.remote != nil {
    return s.remote.Complete(ctx, f)
  }

  return s.Complete(ctx, f)
}

// The server is a share.Host when it is the project's instance.
var _ share.Host = (*Server)(nil)
