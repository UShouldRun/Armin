package mcpserver

import (
  "io"
  "sync"

  "armin/internal/agent"
  "armin/internal/sandbox"
)

// Server is a minimal MCP server. Each request is handled in its own
// goroutine — most tools here are fast, local, sandboxed I/O, but a
// few (build_check, run_tests, web_search's first-call Docker
// startup) can run long enough to block the stdio read loop; without
// this, one slow call would head-of-line-block every other pending
// request, including a trivial ping. Responses race to the writer in
// whatever order they finish, which JSON-RPC allows since callers
// match responses to requests by id, not by arrival order — writeMu
// only keeps two responses from interleaving mid-write, and wg lets
// Serve drain in-flight goroutines before returning at EOF.
type Server struct {
  sandbox *sandbox.Config
  loop    *agent.Loop // runs the tools: the same table the REPL's agent uses
  out     io.Writer
  writeMu   sync.Mutex
  wg        sync.WaitGroup
}
