package lspserver

import (
  "encoding/json"
)

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

// CancelParams is $/cancelRequest's: the request to stop.
type CancelParams struct {
  ID json.RawMessage `json:"id"`
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
