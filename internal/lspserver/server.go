// Package lspserver is nvim's frontend: armin as a minimal LSP server,
// so Neovim's built-in vim.lsp client is all nvim needs (see
// docs/nvim.md). Started with -lsp, from the project root.
//
//   server.go      New, Serve: the JSON-RPC loop and dispatch
//                  (handle), open documents, replies
//   transport.go   LSP's Content-Length framing
//   protocol.go    capabilities and result shapes
//   hover.go       hover: a read-only agent answer about the symbol
//                  under the cursor, cached
//   completion.go  inline completion: a fill-in-the-middle request
//                  around the cursor, with nearby files as context
//   symbols.go     go-to-definition and workspace symbols from ctags
//   chat.go        the armin.* commands behind nvim's chat window
//   host.go        serving terminals that join this instance
//   lines.go       finding a line in a document without splitting it
//
// display.runLSP starts it with the Mind (agent loop, completion
// client, models, symbols); with another armin already hosting the
// project, NewProxy forwards everything that thinks to that one.
package lspserver

import (
  "bufio"
  "context"
  "encoding/json"
  "errors"
  "fmt"
  "io"
  "net/url"
  "os"
  "path/filepath"
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

// The server is a share.Host when it is the project's instance.
var _ share.Host = (*Server)(nil)
