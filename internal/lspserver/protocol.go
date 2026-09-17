package lspserver

import "encoding/json"

// rpcMessage covers every shape a JSON-RPC frame can take on the
// wire — request, response, or notification — since LSP multiplexes
// all three over the same Content-Length-framed stream.
type rpcMessage struct {
  JSONRPC string          `json:"jsonrpc"`
  ID      json.RawMessage `json:"id,omitempty"`
  Method  string          `json:"method,omitempty"`
  Params  json.RawMessage `json:"params,omitempty"`
}

// rpcResponse is what the server sends back. Result has no
// "omitempty" on purpose — a nil result (e.g. shutdown's reply)
// must still marshal as an explicit "result": null, not vanish from
// the object.
type rpcResponse struct {
  JSONRPC string          `json:"jsonrpc"`
  ID      json.RawMessage `json:"id"`
  Result  any             `json:"result"`
  Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
  Code    int    `json:"code"`
  Message string `json:"message"`
}

// rpcNotification is a server-to-client push with no id and no
// reply expected — used for armin/askEvent, streaming an in-flight
// armin.ask's tokens and tool calls to the client as they happen,
// ahead of the final workspace/executeCommand response.
type rpcNotification struct {
  JSONRPC string `json:"jsonrpc"`
  Method  string `json:"method"`
  Params  any    `json:"params,omitempty"`
}

// AskEventParams is armin/askEvent's payload. Type is "tool_call"
// (Name/Args set) or "token" (Delta set) — never both. ID echoes the
// originating armin.ask request's JSON-RPC id, so a client tracking
// more than one in-flight ask can route events to the right one;
// the bundled nvim client only ever has one ask in flight and
// ignores it.
type AskEventParams struct {
  ID    json.RawMessage `json:"id"`
  Type  string          `json:"type"`
  Name  string          `json:"name,omitempty"`
  Args  map[string]any  `json:"args,omitempty"`
  Delta string          `json:"delta,omitempty"`
}

// Error codes we actually emit — the rest of the JSON-RPC/LSP space
// is unused here.
const (
  errInvalidRequest   = -32600
  errMethodNotFound   = -32601
  errInternal         = -32603
  errRequestCancelled = -32800
)

// Position and Range keep individually tagged fields, not grouped —
// these cross the wire to a real LSP client, which expects exactly
// "line"/"character", not Go's default capitalized field names.
type Position struct {
  Line      int `json:"line"`
  Character int `json:"character"`
}

type TextDocumentIdentifier struct {
  URI string `json:"uri"`
}

type TextDocumentItem struct {
  URI  string `json:"uri"`
  Text string `json:"text"`
}

type HoverParams struct {
  TextDocument TextDocumentIdentifier `json:"textDocument"`
  Position     Position               `json:"position"`
}

type MarkupContent struct {
  Kind  string `json:"kind"`
  Value string `json:"value"`
}

type Hover struct {
  Contents MarkupContent `json:"contents"`
}

type DidOpenParams struct {
  TextDocument TextDocumentItem `json:"textDocument"`
}

// TextDocumentContentChangeEvent assumes full-document sync (the
// server advertises TextDocumentSyncKind Full = 1): Text is always
// the whole new document, never an incremental range edit.
type TextDocumentContentChangeEvent struct {
  Text string `json:"text"`
}

type DidChangeParams struct {
  TextDocument   TextDocumentIdentifier           `json:"textDocument"`
  ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

type DidCloseParams struct {
  TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type ExecuteCommandParams struct {
  Command   string            `json:"command"`
  Arguments []json.RawMessage `json:"arguments"`
}

// DefinitionParams is identical in shape to HoverParams
// (textDocument + position) but kept as its own type since the two
// requests are conceptually distinct.
type DefinitionParams struct {
  TextDocument TextDocumentIdentifier `json:"textDocument"`
  Position     Position               `json:"position"`
}

type Range struct {
  Start Position `json:"start"`
  End   Position `json:"end"`
}

// Location is deliberately a zero-width Range at the definition's
// start line — armin doesn't have precise end-column information
// for it, and every editor jumps to Range.Start regardless.
type Location struct {
  URI   string `json:"uri"`
  Range Range  `json:"range"`
}

// WorkspaceSymbolParams is workspace/symbol's request — a free-text
// query, fuzzy-ish matched against every known symbol name.
type WorkspaceSymbolParams struct {
  Query string `json:"query"`
}

// SymbolInformation is the (older, simpler) workspace/symbol result
// shape — one entry per match, no hierarchy.
type SymbolInformation struct {
  Name          string   `json:"name"`
  Kind          int      `json:"kind"` // LSP SymbolKind enum
  Location      Location `json:"location"`
  ContainerName string   `json:"containerName,omitempty"`
}

// AskArgs is the argument shape workspace/executeCommand's
// "armin.ask" expects — see the nvim side in docs/nvim.md.
type AskArgs struct {
  URI      string `json:"uri"`
  Question string `json:"question"`
}

// ModelArgs is "armin.model"'s argument: Name empty means "tell me
// the current model and what's installed", Name set means "switch
// to this model".
type ModelArgs struct {
  Name string `json:"name,omitempty"`
}

// ModelChoice is one entry in ModelInfo.Installed — enough for the
// client to make its own "fastest"/"best" recommendation, without
// armin.model dictating opinions the client would just re-render.
type ModelChoice struct {
  Name string `json:"name"`
  Size int64  `json:"size"` // bytes on disk, per Ollama
}

// ModelInfo is "armin.model"'s result.
type ModelInfo struct {
  Current   string        `json:"current"`
  Installed []ModelChoice `json:"installed,omitempty"`
}

// InlineCompletionParams mirrors LSP 3.18's
// textDocument/inlineCompletion request — position plus the
// document it's in, same as hover.
type InlineCompletionParams struct {
  TextDocument TextDocumentIdentifier `json:"textDocument"`
  Position     Position               `json:"position"`
}

// InlineCompletionItem is deliberately just InsertText — no Range,
// no Command, no snippet support. The client inserts this verbatim
// at the cursor.
type InlineCompletionItem struct {
  InsertText string `json:"insertText"`
}

type initializeResultT struct {
  Capabilities capabilities `json:"capabilities"`
}

type capabilities struct {
  TextDocumentSync         int                   `json:"textDocumentSync"` // 1 = Full
  HoverProvider            bool                  `json:"hoverProvider"`
  ExecuteCommandProvider   executeCommandOptions `json:"executeCommandProvider"`
  InlineCompletionProvider bool                  `json:"inlineCompletionProvider"`
  DefinitionProvider       bool                  `json:"definitionProvider"`
  WorkspaceSymbolProvider  bool                  `json:"workspaceSymbolProvider"`
}

type executeCommandOptions struct {
  Commands []string `json:"commands"`
}

func initializeResult() initializeResultT {
  return initializeResultT{
    Capabilities: capabilities{
      TextDocumentSync:       1,
      HoverProvider:          true,
      ExecuteCommandProvider: executeCommandOptions{
        Commands: []string{ "armin.ask", "armin.model" },
      },
      InlineCompletionProvider: true,
      DefinitionProvider:       true,
      WorkspaceSymbolProvider:  true,
    },
  }
}

func hoverResult(answer string) Hover {
  return Hover{
    Contents: MarkupContent{ Kind: "markdown", Value: answer },
  }
}
