---
name: bash
description: Write and fix bash scripts that are safe and portable — strict mode, quoting, arrays, traps, argument parsing, shellcheck-clean — and run shell commands without surprises. Load before writing or editing a .sh file or a non-trivial one-liner.
---
## Script header
```bash
#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'
```
- `-e` stops on errors, `-u` on unset variables, `pipefail` makes a
  pipeline fail if any stage fails.
- For POSIX `sh` scripts (like the user's `install.sh`) use `set -eu`
  and none of the bash-only features below.

## Quoting (most bugs live here)
- Always quote expansions: `"$var"`, `"${arr[@]}"`, `"$(cmd)"`.
- Unquoted `$var` is split on whitespace and glob-expanded — a file
  name with a space breaks it.
- `[[ ... ]]` for tests in bash (no word splitting inside); `[ ... ]`
  in POSIX sh, with every variable quoted.

## Variables and arrays
- `local` inside functions. UPPER_CASE only for exported/env vars.
- Defaults: `"${VAR:-default}"`; required: `"${VAR:?VAR must be set}"`.
- Arrays for lists of arguments, never a space-joined string:
  ```bash
  args=( -o "$out" --verbose )
  cmd "${args[@]}"
  ```

## Control flow
- `if cmd; then` tests the command's exit status — no `[ $? -eq 0 ]`.
- Loop over lines safely: `while IFS= read -r line; do …; done < file`.
- Loop over files with globs, not `ls`: `for f in ./*.go; do`.
- `case` for argument parsing:
  ```bash
  while [ $# -gt 0 ]; do
    case "$1" in
      --prefix) prefix="$2"; shift 2 ;;
      -h|--help) usage; exit 0 ;;
      *) echo "unknown: $1" >&2; exit 1 ;;
    esac
  done
  ```

## Cleanup and errors
- `tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT`.
- Errors to stderr: `echo "error: …" >&2`, then `exit 1`.
- Check a command exists: `command -v rg >/dev/null 2>&1`.

## Style (the user's)
2-space indentation, spaces around operators in arithmetic
`$(( a + b ))`, a blank line between blocks, functions as
`name() { … }`, small functions, comments only for the why.

## Before finishing
Run `bash -n script.sh` (syntax) and `shellcheck script.sh` if it's
installed; fix every warning or say why it's a false positive. Test
with a path containing a space.
