---
name: go-style
description: The user's Go style — 2-space indent (not gofmt), spaced operators and braces, aligned columns, wrapped errors, bitmask flags, allocation-aware code. Load before writing or editing Go.
---
Write Go exactly like this. The code is deliberately NOT gofmt-formatted;
never run gofmt or goimports on it.

## Layout
- 2-space indentation, never tabs. `case` is indented inside
  `switch`/`select`.
- Lines around 70 columns, 80 at most. Wrap aggressively.
- Imports in blank-line-separated groups: standard library, then third
  party, then the module's own packages.
- Multi-line calls put each argument on its own line with a trailing
  comma; signatures that don't fit put the params on their own line:

  ```go
  func PlanEdit(
    cfg *sandbox.Config, tracker *FileTracker, in EditFileInput,
  ) (*Change, error) {
  ```

## Spacing
- A space on both sides of every binary operator, even where gofmt
  would drop it: `len(h) - 1`, `s[:i + 1]`, `64 * 1024`,
  `a + "x" + b`, `f & Flag != 0`, `x := y`. Never `x=y` or `a*b`.
- Single-line braces are padded: `{ expr }`, `[]string{ "a", "b" }`,
  `map[string]string{ "error": err.Error() }`,
  `Options{ Timeout: t }`. `{}` stays tight when empty.

## Alignment
- Align related declarations into columns: struct fields, their types
  and tags; `const` blocks; grouped `var`s; keyed literals.

  ```go
  type Change struct {
    Path, Resolved string
    Replacements   int
    mode           os.FileMode
    Created        bool
  }
  ```
- Group same-typed fields on one line: `Old, New string`.

## Files
- Type declarations live apart from the code: the types of `loop.go`
  go in `loop.type.go` next to it, with only the imports they need.
  A build-constrained file keeps its suffix last:
  `rawmode_linux.go` → `rawmode.type_linux.go`. Test-only helper
  types stay in their `_test.go` file.
- `main.go` holds `main` and `run` only; everything else lives in
  packages.

## Errors
- Wrap every returned error with the function name, as a chain:

  ```go
  return fmt.Errorf("\n\t... in LoadConfig:\n\t%w", err)
  return fmt.Errorf("\n\t... in LoadConfig:\n\t reading %q: %w", path, err)
  ```
- Guard clauses and early returns; keep the happy path unindented.
- Never declare in an `if` header. The statement goes on its own line
  above the `if`, which only tests:

  ```go
  err := json.Unmarshal(body, &parsed)
  if err != nil {
  ```

  not `if err := json.Unmarshal(body, &parsed); err != nil {`. The
  same goes for `else if`: open an `else` block, then declare and
  test inside it. If hoisting would clash with a variable already in
  scope, assign it with `=` when nothing reads the old value
  afterwards; otherwise give it its own name (`statErr`). Never let
  hoisting change what a later line reads.
- Errors a model or user acts on say what to do next, not just what
  failed.

## Types and memory
- Sized integers where the domain has a width: `uint8`, `uint16`,
  `uint32`, `int32` — and for ids, codes, enums and commands use small
  integer types, not strings.
- Two or more related booleans become one flags field:

  ```go
  type RunFlags uint8

  const (
    RunTruncated RunFlags = 1 << iota
    RunDryRun
    RunAll = RunTruncated | RunDryRun
  )

  func (f RunFlags) Truncated() bool { return f & RunTruncated != 0 }
  ```
  Always include an `_ALL`/`All` constant with every bit set.
- Order struct fields to minimize padding (wide fields first, bools
  and bytes last). Measure with `unsafe.Sizeof` before claiming a win.
- Hot paths (per keystroke, per line, per item of a growing
  collection) don't allocate needlessly: scan and slice instead of
  `strings.Split`/`ToLower` on whole inputs, `scanner.Bytes()` over
  `Text()`, reuse buffers. Check total cost over a session, not one
  call — e.g. append-only logs instead of rewriting everything.
- No new dependencies; hand-roll protocol and system code with the
  standard library.

## Naming and comments
- Go naming conventions (MixedCaps). Where a project has layers,
  prefix by layer (e.g. `RepoFetchMember`, `ControlGetEvent`).
- Comments short and plain: say why, not what. Doc comments on
  exported names, one or two sentences.

## Blank lines between blocks
Code is spaced into blocks, not packed:
- a blank line after every closing `}` of an `if` / `for` / `switch`
  when more code follows at that level (not before `else`, `case`
  or another `}`)
- a blank line before a `return` that follows a group of statements
- no blank line when a block holds one statement and its `return`:

  ```go
  resolved, err := cfg.ResolvePath(path)
  if err != nil {
    http.Error(w, err.Error(), http.StatusForbidden)
    return
  }

  info, err := os.Stat(resolved)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Stat:\n\t%w", err)
  }

  size := info.Size()
  log(size)

  return size, nil
  ```

## Before finishing
Build, vet and run the tests. Then re-read your diff for: unspaced
operators, tight braces, unaligned groups, unwrapped errors, lines
over 80 columns.
