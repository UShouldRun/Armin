package lspserver

// Error codes we actually emit — the rest of the JSON-RPC/LSP space
// is unused here.
const (
  errInvalidRequest   = -32600
  errMethodNotFound   = -32601
  errInternal         = -32603
  errRequestCancelled = -32800
)

func initializeResult() initializeResultT {
  return initializeResultT{
    Capabilities: capabilities{
      TextDocumentSync:       1,
      HoverProvider:          true,
      ExecuteCommandProvider: executeCommandOptions{
        Commands: []string{
          "armin.ask", "armin.model", "armin.clear", "armin.history",
          "armin.stop",
        },
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
