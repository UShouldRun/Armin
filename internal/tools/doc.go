// Package tools is what armin's tools actually do: plain functions
// that take a sandbox (or a symbol index, or the web search service)
// and a typed input, and return a typed output or an error. They know
// nothing about models; the agent's tool table (agent/tools.go)
// decides which the model sees and calls them.
//
//   readfile.go, listdir.go,     read files, PDFs and folders
//   pdf.go
//   edit.go                      plan and apply writes and edits (with
//                                FileTracker: no write to a file the
//                                model hasn't read, or that changed)
//   search.go, semantic.go       ripgrep, and keyword-ranked files
//   symbols.go, doccomment.go    ctags-backed definitions, symbols,
//                                outlines, references, doc comments
//   callgraph.go                 callers, dead code, impact of a change
//   repostat.go                  line counts, languages, file finding,
//                                stat, TODOs, environment, manifest
//   deps.go                      dependencies and the module graph
//   gitinfo.go                   log, blame, diff, show, history search,
//                                owners, commit stats
//   build.go                     the project's test, build and lint
//                                commands, detected or configured
//   runcommand.go                read-only commands on the allowlist
//   runprogram.go                programs the user approves
//   websearch.go                 web search through internal/searxng
//
// Every path goes through the sandbox (internal/sandbox): reads stay
// in the project and the folders the user added, writes in the
// project, and commands are allowlisted and time-limited.
package tools
