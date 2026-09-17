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
  "strings"
  "sync"
  "time"

  "armin/internal/agent"
  "armin/internal/ctags"
  "armin/internal/llm"
  "armin/internal/sandbox"
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

// citationInstruction is appended to the question for armin.ask
// only — not hover, whose popup can't render the markup and is
// already grounded by a ctags fact when possible. It asks the model
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

// Server is a minimal, single-purpose LSP server: no diagnostics,
// no go-to-definition beyond ctags — hover, one ask command, and
// inline completion.
type Server struct {
  loop            *agent.Loop
  sandbox         *sandbox.Config
  system          string
  completionModel string
  symbols         *ctags.Index

  out   io.Writer
  outMu sync.Mutex

  docsMu sync.Mutex
  docs   map[string]string // uri -> full text, via didOpen/didChange

  hoverMu    sync.Mutex
  hoverCache map[string]string

  inflightMu sync.Mutex
  inflight   map[string]inflightEntry // uri -> its running completion
  nextGen    int64

  warnOnce sync.Once

  shutdownReceived bool
}

type inflightEntry struct {
  gen    int64
  cancel context.CancelFunc
}

// New builds a Server. loop and sb are the same agent/sandbox pair
// the REPL uses — hover and ask questions get the same
// tool-grounded answers, just delivered through vim.lsp instead of
// a terminal chat. completionModel is a separate, ideally
// fill-in-the-middle-capable model used only for inline completion;
// it never goes through loop (no tool calls, no chat formatting —
// see internal/llm.Client.Complete). symbols is the ctags index
// backing textDocument/definition, workspace/symbol, and hover
// grounding — cmd/armin/main.go builds one shared *ctags.Index and
// passes the same one here and into loop.Symbols, so -lsp doesn't
// scan the repo twice; if no universal-ctags binary was found at
// startup, those three degrade gracefully (empty results / plain
// LLM-only hover) rather than failing the server.
func New(
  loop *agent.Loop, sb *sandbox.Config,
  systemPrompt, completionModel string, symbols *ctags.Index,
) *Server {
  return &Server{
    loop:            loop,
    sandbox:         sb,
    system:          systemPrompt,
    completionModel: completionModel,
    symbols:         symbols,
    docs:            make(map[string]string),
    hoverCache:      make(map[string]string),
    inflight:        make(map[string]inflightEntry),
  }
}

// Serve reads Content-Length-framed JSON-RPC from in and writes
// responses/notifications to out until in hits EOF or exit fires.
func (s *Server) Serve(
  ctx context.Context, in io.Reader, out io.Writer,
) error {
  s.out = out
  reader := bufio.NewReader(in)

  for {
    raw, err := readMessage(reader)
    if err == io.EOF {
      return nil
    }
    if err != nil {
      return fmt.Errorf("\n\t... in Serve:\n\t%w", err)
    }

    var msg rpcMessage
    if err := json.Unmarshal(raw, &msg); err != nil {
      continue // malformed frame — nothing sane to respond with
    }

    s.handle(ctx, msg)
  }
}

func (s *Server) handle(ctx context.Context, msg rpcMessage) {
  switch msg.Method {
  case "initialize":
    s.respond(msg.ID, initializeResult(), nil)

  case "initialized", "$/setTrace", "workspace/didChangeConfiguration":
    // notifications we accept and ignore

  case "shutdown":
    s.shutdownReceived = true
    s.respond(msg.ID, nil, nil)

  case "exit":
    if s.shutdownReceived {
      os.Exit(0)
    }
    os.Exit(1)

  case "textDocument/didOpen":
    var p DidOpenParams
    if err := json.Unmarshal(msg.Params, &p); err == nil {
      s.setDoc(p.TextDocument.URI, p.TextDocument.Text)
    }

  case "textDocument/didChange":
    var p DidChangeParams
    err := json.Unmarshal(msg.Params, &p)
    if err == nil && len(p.ContentChanges) > 0 {
      last := p.ContentChanges[len(p.ContentChanges)-1]
      s.setDoc(p.TextDocument.URI, last.Text)
    }

  case "textDocument/didClose":
    var p DidCloseParams
    if err := json.Unmarshal(msg.Params, &p); err == nil {
      s.dropDoc(p.TextDocument.URI)
    }

  case "textDocument/hover":
    var p HoverParams
    if err := json.Unmarshal(msg.Params, &p); err != nil {
      s.respondError(msg.ID, errInvalidRequest, err.Error())
      return
    }
    go s.hover(ctx, msg.ID, p)

  case "workspace/executeCommand":
    var p ExecuteCommandParams
    if err := json.Unmarshal(msg.Params, &p); err != nil {
      s.respondError(msg.ID, errInvalidRequest, err.Error())
      return
    }
    go s.executeCommand(ctx, msg.ID, p)

  case "textDocument/inlineCompletion":
    var p InlineCompletionParams
    if err := json.Unmarshal(msg.Params, &p); err != nil {
      s.respondError(msg.ID, errInvalidRequest, err.Error())
      return
    }
    go s.inlineCompletion(ctx, msg.ID, p)

  case "textDocument/definition":
    var p DefinitionParams
    if err := json.Unmarshal(msg.Params, &p); err != nil {
      s.respondError(msg.ID, errInvalidRequest, err.Error())
      return
    }
    go s.definition(ctx, msg.ID, p)

  case "workspace/symbol":
    var p WorkspaceSymbolParams
    if err := json.Unmarshal(msg.Params, &p); err != nil {
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

  key := cacheKey(p.TextDocument.URI, p.Position.Line, snippet+"|"+word)
  if cached, ok := s.cachedHover(key); ok {
    s.respond(id, hoverResult(cached), nil)
    return
  }

  question := fmt.Sprintf(
    "Explain concisely (max one short paragraph, no preamble) "+
      "what this code does. File: %s, near line %d%s:\n\n```\n%s\n```",
    s.relPath(p.TextDocument.URI), p.Position.Line+1, fact, snippet,
  )

  answer, err := s.ask(ctx, question, nil, nil)
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
// instead of Ollama's raw (and, over JSON-RPC, single-line-mangled)
// HTTP error body — anything else is passed through as-is.
func (s *Server) respondAskError(id json.RawMessage, err error) {
  if errors.Is(err, llm.ErrToolsUnsupported) {
    s.respondError(id, errInternal, fmt.Sprintf(
      "%s doesn't support tool calling — switch models with :ArminModel",
      s.loop.LLM.Model,
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
  if err := json.Unmarshal(p.Arguments[0], &args); err != nil {
    s.respondError(id, errInvalidRequest, err.Error())
    return
  }
  if strings.TrimSpace(args.Question) == "" {
    s.respondError(id, errInvalidRequest, "question is empty")
    return
  }

  question := args.Question
  if args.URI != "" {
    question = fmt.Sprintf(
      "Currently editing: %s\n\n%s", s.relPath(args.URI), args.Question,
    )
  }
  question += citationInstruction

  answer, err := s.ask(ctx, question, s.sendAskToken(id), s.sendAskToolCall(id))
  if err != nil {
    s.respondAskError(id, err)
    return
  }
  s.respond(id, answer, nil)
}

// sendAskToken and sendAskToolCall build the per-request streaming
// callbacks armin.ask passes into Turn — each armin/askEvent
// notification echoes id so a client tracking more than one
// in-flight ask can route them correctly (see AskEventParams).
func (s *Server) sendAskToken(id json.RawMessage) llm.OnToken {
  return func(delta string) {
    s.notify("armin/askEvent", AskEventParams{
      ID: id, Type: "token", Delta: delta,
    })
  }
}

func (s *Server) sendAskToolCall(
  id json.RawMessage,
) func(name string, args map[string]any) {
  return func(name string, args map[string]any) {
    s.notify("armin/askEvent", AskEventParams{
      ID: id, Type: "tool_call", Name: name, Args: args,
    })
  }
}

// executeModel answers "armin.model": switch the chat model live
// (no arguments needed elsewhere — the LSP process just keeps
// running with the new r.loop.LLM.Model from then on), or report
// the current model plus what Ollama has installed when called with
// no name, mirroring the REPL's /model command.
func (s *Server) executeModel(
  ctx context.Context, id json.RawMessage, p ExecuteCommandParams,
) {
  var args ModelArgs
  if len(p.Arguments) > 0 {
    if err := json.Unmarshal(p.Arguments[0], &args); err != nil {
      s.respondError(id, errInvalidRequest, err.Error())
      return
    }
  }

  if args.Name != "" {
    s.loop.LLM.Model = args.Name
    s.respond(id, ModelInfo{ Current: args.Name }, nil)
    return
  }

  info := ModelInfo{ Current: s.loop.LLM.Model }
  if models, err := s.loop.LLM.ListModels(ctx); err == nil {
    for _, m := range models {
      info.Installed = append(
        info.Installed, ModelChoice{ Name: m.Name, Size: m.Size },
      )
    }
  }
  s.respond(id, info, nil)
}

// ask runs one stateless turn through the same agent loop the REPL
// uses: fresh history every call, full tool access. onToken and
// onToolCall are nil for hover (its popup isn't incremental); armin.ask
// passes both to stream progress to the client as armin/askEvent
// notifications ahead of this call's return.
func (s *Server) ask(
  ctx context.Context, question string,
  onToken llm.OnToken, onToolCall func(name string, args map[string]any),
) (string, error) {
  askCtx, cancel := context.WithTimeout(ctx, askTimeout)
  defer cancel()

  history := []llm.Message{
    { Role: "system", Content: s.system },
    { Role: "user", Content: question },
  }
  return s.loop.Turn(askCtx, &history, onToken, onToolCall)
}

func (s *Server) setDoc(uri, text string) {
  s.docsMu.Lock()
  defer s.docsMu.Unlock()
  s.docs[uri] = text
}

func (s *Server) dropDoc(uri string) {
  s.docsMu.Lock()
  defer s.docsMu.Unlock()
  delete(s.docs, uri)
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

  winStart, _, ok := lineOffset(text, line-radius)
  if !ok {
    winStart = 0
  }
  _, winEnd, ok := lineOffset(text, line+radius)
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
  if err := writeMessage(s.out, msg); err != nil {
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
  if err := writeMessage(s.out, msg); err != nil {
    fmt.Fprintln(os.Stderr, "armin lsp: write failed:", err)
  }
}
