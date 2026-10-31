package agent

// The tool table: every tool the model can call, one toolSpec each,
// indexed by ToolID. The specs live by topic in tools_files.go,
// tools_search.go, tools_code.go, tools_git.go, tools_run.go and
// tools_notes.go; this file builds the table from them and holds what
// works across all tools:
//
//   specs          the table; toolIDs maps a wire name to its ID
//   Definitions    what this Loop offers the model (ToolDefinitions:
//                  the sandboxed tools alone, for mcpserver)
//   execute        one call: checks, then the spec's run
//   inSandbox,     adapters for the common shapes of a tools.*
//   withSymbols    function: decode, call, wrap the error
//
// A tool's flow: the model names it, Turn (loop.go) looks the name up
// here, execute runs it, and display.go renders the call and result.

import (
  "context"
  "encoding/json"
  "fmt"

  "armin/internal/ctags"
  "armin/internal/llm"
  "armin/internal/sandbox"
)

// Write: changes files, so read-only sessions drop it. Run: runs a
// program, so the same result twice is a loop. Agent: needs approvals
// or state, so MCP doesn't serve it. Session: needs a Loop's stores,
// so ToolDefinitions leaves it out. The rest say which store (Know:
// the project's knowledge tree; Minion: a minion model to delegate to).
const (
  flagWrite    toolFlags = 0b0000000000000001
  flagRun      toolFlags = 0b0000000000000010
  flagAgent    toolFlags = 0b0000000000000100
  flagSession  toolFlags = 0b0000000000001000
  flagMemories toolFlags = 0b0000000000010000
  flagScratch  toolFlags = 0b0000000000100000
  flagSkills   toolFlags = 0b0000000001000000
  flagLibrary  toolFlags = 0b0000000010000000
  flagKnow     toolFlags = 0b0000000100000000
  flagMinion   toolFlags = 0b0000001000000000
  flagAll      toolFlags = 0b1111111111111111
)

// specs is the table, built once from the topic files' lists.
var specs = index(
  fileTools, searchTools, codeTools, gitTools, runTools, noteTools,
  knowTools, delegateTools,
)

// toolIDs maps wire names to IDs. A miss yields toolUnknown.
var toolIDs = names(specs)

// index places each spec at its ID. A tool missing or listed twice is
// a programming error, caught the moment the package loads.
func index(groups ...[]toolSpec) [toolCount]toolSpec {
  var t [toolCount]toolSpec
  for _, g := range groups {
    for _, s := range g {
      if t[s.id].run != nil {
        panic("agent: tool listed twice: " + s.def.Name)
      }

      t[s.id] = s
    }
  }

  for id := toolUnknown + 1; id < toolCount; id++ {
    if t[id].run == nil {
      panic(fmt.Sprintf("agent: tool %d has no spec", id))
    }
  }

  return t
}

func names(t [toolCount]toolSpec) map[string]ToolID {
  m := make(map[string]ToolID, toolCount)
  for id := toolUnknown + 1; id < toolCount; id++ {
    m[t[id].def.Name] = id
  }

  return m
}

func (id ToolID) has(f toolFlags) bool { return specs[id].flags & f != 0 }

// isWrite reports whether id changes files (run_program builds and
// runs code, so it counts).
func (id ToolID) isWrite() bool { return id.has(flagWrite) }

// runTool: the same call returning the same result again is a loop,
// not progress.
func runTool(name string) bool { return toolIDs[name].has(flagRun) }

// ToolDefinitions is the schema of the sandboxed tools: everything a
// Loop offers except what needs its stores. mcpserver serves these.
func ToolDefinitions() []llm.ToolDefinition {
  var defs []llm.ToolDefinition
  for id := toolUnknown + 1; id < toolCount; id++ {
    if !id.has(flagSession) {
      defs = append(defs, wrap(specs[id].def))
    }
  }

  return defs
}

// offered reports whether this Loop has what the tool needs, and its
// role may use it.
func (l *Loop) offered(id ToolID) bool {
  switch {
  case !id.allows(l.Role):
    return false
  case l.ReadOnly && id.isWrite():
    return false
  case id.has(flagMemories):
    return l.Memories != nil
  case id.has(flagScratch):
    return l.Scratch != nil
  case id.has(flagSkills):
    return l.Skills != nil && l.Skills.Len() > 0
  case id.has(flagLibrary):
    return l.Library != nil
  case id.has(flagKnow):
    return l.Know != nil
  case id.has(flagMinion):
    return l.Minion != nil
  }

  return true
}

// Definitions is what this Loop offers the model in its current role,
// in table order. Built once per role.
func (l *Loop) Definitions() []llm.ToolDefinition {
  return l.defsFor().list
}

func (l *Loop) defsFor() *roleDefs {
  d := &l.roleDefs[l.Role.slot()]
  d.once.Do(func() {
    for id := toolUnknown + 1; id < toolCount; id++ {
      if l.offered(id) {
        d.list = append(d.list, wrap(specs[id].def))
      }
    }

    raw, _ := json.Marshal(d.list)
    d.tokens = len(raw) / 4

    d.params = make(map[string]map[string]bool, len(d.list))
    for _, def := range d.list {
      names := make(map[string]bool, len(def.Function.Parameters.Properties))
      for k := range def.Function.Parameters.Properties {
        names[k] = true
      }

      d.params[def.Function.Name] = names
    }
  })

  return d
}

func wrap(f llm.ToolFunction) llm.ToolDefinition {
  return llm.ToolDefinition{ Type: "function", Function: f }
}

// execute runs one tool call and returns a JSON-marshalable result.
// Errors are returned as structured data in the result itself, not
// as Go errors, so a bad call becomes something the model can read
// and correct on the next round rather than a crash.
func (l *Loop) execute(
  ctx context.Context, call llm.ToolCall, hooks Hooks,
) any {
  name := call.Function.Name
  raw, err := json.Marshal(call.Function.Arguments)
  if err != nil {
    return map[string]string{
      "error": fmt.Sprintf("could not encode arguments: %s", err),
    }
  }

  id := toolIDs[name]
  if id == toolUnknown || !l.offered(id) {
    if id != toolUnknown && !id.allows(l.Role) {
      return map[string]string{
        "error": fmt.Sprintf(
          "the %s role can't use %s; the user can switch roles with /copilot",
          l.Role, name,
        ),
      }
    }

    if l.ReadOnly && id.isWrite() {
      return map[string]string{
        "error": "this session is read-only; " + name + " is unavailable",
      }
    }

    return map[string]string{ "error": fmt.Sprintf("unknown tool %q", name) }
  }

  msg := l.unknownParams(name, call.Function.Arguments)
  if msg != "" {
    return map[string]string{ "error": msg }
  }

  return specs[id].run(ctx, l, toolCall{ name: name, raw: raw, hooks: hooks })
}

// RunTool runs a sandboxed tool by name outside a turn, with no one
// to approve anything: mcpserver's entry point. Tools that need the
// agent are refused.
func (l *Loop) RunTool(ctx context.Context, name string, raw []byte) any {
  id := toolIDs[name]
  if id == toolUnknown || id.has(flagAgent | flagSession) {
    return map[string]string{ "error": fmt.Sprintf("unknown tool %q", name) }
  }

  return specs[id].run(ctx, l, toolCall{ name: name, raw: raw })
}

// Served reports whether RunTool runs name.
func Served(name string) bool {
  id := toolIDs[name]
  return id != toolUnknown && !id.has(flagAgent | flagSession)
}

// readPaths are the paths a call will open, for the access check.
func readPaths(call llm.ToolCall) []string {
  paths := specs[toolIDs[call.Function.Name]].paths
  if paths == nil {
    return nil
  }

  return paths(call.Function.Arguments)
}

// pathArg is a call's "path" argument, the common case.
func pathArg(args map[string]any) []string {
  p, _ := args["path"].(string)
  if p == "" {
    return nil
  }

  return []string{ p }
}

// decode reads a call's arguments into In; a malformed call becomes
// a result telling the model what to fix.
func decode[In any](c toolCall) (In, map[string]string) {
  var in In
  err := json.Unmarshal(c.raw, &in)
  if err != nil {
    return in, malformed(c.name, err)
  }

  return in, nil
}

// inSandbox adapts the common tools.* shape: arguments in, an output
// or an error back, acting in the project's sandbox.
func inSandbox[In, Out any](
  f func(context.Context, *sandbox.Config, In) (Out, error),
) runFunc {
  return func(ctx context.Context, l *Loop, c toolCall) any {
    in, bad := decode[In](c)
    if bad != nil {
      return bad
    }

    out, err := f(ctx, l.Sandbox, in)
    if err != nil {
      return errResult(err)
    }

    return out
  }
}

// withSymbols adapts the tools that read the ctags index.
func withSymbols[In, Out any](
  f func(context.Context, *ctags.Index, In) (Out, error),
) runFunc {
  return func(ctx context.Context, l *Loop, c toolCall) any {
    in, bad := decode[In](c)
    if bad != nil {
      return bad
    }

    out, err := f(ctx, l.Symbols, in)
    if err != nil {
      return errResult(err)
    }

    return out
  }
}

// withSymbolsAndSandbox adapts the tools that need both.
func withSymbolsAndSandbox[In, Out any](
  f func(context.Context, *ctags.Index, *sandbox.Config, In) (Out, error),
) runFunc {
  return func(ctx context.Context, l *Loop, c toolCall) any {
    in, bad := decode[In](c)
    if bad != nil {
      return bad
    }

    out, err := f(ctx, l.Symbols, l.Sandbox, in)
    if err != nil {
      return errResult(err)
    }

    return out
  }
}

func errResult(err error) map[string]string {
  return map[string]string{ "error": err.Error() }
}

func malformed(toolName string, err error) map[string]string {
  return map[string]string{
    "error": fmt.Sprintf(
      "malformed arguments for %s: %s — check the parameter "+
        "names and types against the tool schema and try again",
      toolName, err,
    ),
  }
}

// toolFunc builds a definition from its parts, for the tools whose
// schema is short enough to read inline.
func toolFunc(
  name, description string,
  props map[string]llm.ToolProperty, required ...string,
) llm.ToolFunction {
  if props == nil {
    props = map[string]llm.ToolProperty{}
  }

  return llm.ToolFunction{
    Name:        name,
    Description: description,
    Parameters: llm.ToolParameters{
      Type:       "object",
      Properties: props,
      Required:   required,
    },
  }
}

func stringProp(description string) llm.ToolProperty {
  return llm.ToolProperty{ Type: "string", Description: description }
}

// projectRunFunc is the shared schema of run_tests, build_check and
// lint_check.
func projectRunFunc(name, description, argsHelp string) llm.ToolFunction {
  return llm.ToolFunction{
    Name:        name,
    Description: description,
    Parameters: llm.ToolParameters{
      Type: "object",
      Properties: map[string]llm.ToolProperty{
        "args": {
          Type:        "array",
          Description: argsHelp,
          Items:       &llm.ToolProperty{ Type: "string" },
        },
      },
    },
  }
}
