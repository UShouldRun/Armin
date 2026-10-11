---
name: zsh
description: Work in the user's zsh — how it differs from bash (no word splitting of unquoted variables, arrays start at 1, glob errors), writing .zshrc config, functions, completion, and avoiding bash-isms that silently break. Load when writing zsh config or commands that will run in zsh.
---
The user's interactive shell is zsh. Commands typed or pasted there,
and their `~/.zshrc`, follow zsh rules, not bash's.

## Differences that bite
- **Unquoted variables are not word-split.** `opts="-a -b"; cmd $opts`
  passes one argument `-a -b`. Use an array (`opts=(-a -b); cmd
  $opts`) or split explicitly: `cmd ${=opts}`.
- **Arrays start at index 1**: `${arr[1]}` is the first element.
  `$arr` expands to all elements.
- **A glob that matches nothing is an error** ("no matches found"):
  `rm *.tmp` fails if there are none. Quote patterns meant for the
  command (`find . -name '*.go'`, `grep --include='*.go'`), or use
  `*(N)` (null glob) to get nothing.
- `=` at the start of a word expands to a command path (`=ls` →
  `/usr/bin/ls`): quote a literal `=`, e.g. `echo '===='`.
- Aliases apply to commands in scripts sourced by zsh: the user's
  `ls` may be an alias. In scripts use `command ls` or plain tools.
- `$0` in a function is the function name; `${(%):-%x}` is the file.

## Scripts
- Shebang `#!/usr/bin/env zsh` only for zsh scripts; anything meant
  to be portable is `#!/bin/sh` or bash (see the bash skill).
- `emulate -L zsh` at the top of functions in config, so the user's
  options don't change their behavior.
- `setopt err_exit pipe_fail no_unset` is the strict-mode
  equivalent.

## Config (~/.zshrc)
- Back it up before editing (`cp ~/.zshrc ~/.zshrc.bak`) and change
  the smallest thing; never rewrite it wholesale.
- PATH: `path=($HOME/.local/bin $path)` then `export PATH` — the
  `path` array is tied to `$PATH`; `typeset -U path` drops duplicates.
- Aliases and functions: `alias gs='git status'`;
  `mkcd() { mkdir -p -- "$1" && cd -- "$1" }`.
- Completion: `autoload -Uz compinit && compinit` once, after
  `fpath` changes.
- Test changes in a new shell (`zsh -i -c 'echo ok'`) before telling
  the user to `source ~/.zshrc`; a broken rc locks them out of new
  terminals.

## Useful expansions
`${var:t}` basename, `${var:h}` dirname, `${var:r}` without extension,
`${(U)var}` uppercase, `**/*.go` recursive glob, `*(.)` files only,
`*(/)` directories only, `*(om[1])` newest file.
