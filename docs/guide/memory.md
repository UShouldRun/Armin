# Memories, skills, scratchpad and sessions

## Memories, skills and the scratchpad

Three kinds of notes, all plain Markdown on disk; only short indexes
and pointers ever sit in the conversation.

**Memories** are durable facts for future sessions: your
preferences, project conventions, decisions and why. The model saves
them with `memory_save` (or ask it with `/remember <text>`), each
with a one-line description. Every session's system prompt lists
names and descriptions; `memory_read` loads one.

- project scope: `~/.local/share/armin/memory/<project>-<hash>/`
- user scope (every project): `~/.local/share/armin/memory/user/`

A checked-in `ARMIN.md`, `AGENTS.md` or `CLAUDE.md` at the project
root (first found) is loaded into the system prompt as project
instructions.

**Skills** are instructions loaded only when a task needs them: a
directory with a `SKILL.md` and any supporting files.

```
~/.config/armin/skills/<name>/SKILL.md     every project
<root>/.armin/skills/<name>/SKILL.md       this project (wins on a name clash)
```

```markdown
---
name: release
description: Cut a release — changelog, version bump, tag
---
1. Read CHANGELOG.md and summarize commits since the last tag.
2. ...
```

Bundled skills, in `skills/` and installed by `./install.sh`:

| Skill | For |
|---|---|
| `go-style` | Go: 2-space, spaced operators/braces, aligned columns, wrapped errors, bitmask flags, allocation-aware code |
| `react-style` | JSX/TSX/HTML: spaced and aligned attribute `=`, padded `{ expr }` |
| `js-style` | JS/TS: aligned `=` / `from` / object values, padded `${ x }`, try/catch layout |
| `sql-style` | lowercase SQL, aligned types, `in_`/`v_` prefixes, lean id-based queries |
| `typst` | Typst CVs, letters and reports, with `cv.typ` / `letter.typ` templates |
| `c-style` | C: `lib/<name>/{include,src}` with `_private.h`, `u8..u64`, `__module_` internals, aligned prototypes, arenas |
| `cpp-style` | C++ kept as close to C as possible: header-only + `.inl`, templates only for compile-time widths, HLS rules |
| `commit-message` | `vX.Y.Z - summary` subjects with semver bumps, no AI attribution |
| `code-review` | bugs first, then security, performance, tests and style; ranked findings with fixes |
| `research-paper` | paper structure, claims backed by numbers, related work via `web_search` |
| `pytorch-experiments` | reproducible runs: seeds, folds, baselines, logged metrics |
| `riscv-asm` | RV32IM encodings, calling convention, custom-opcode space |
| `self-hosting` | Docker Compose, Cloudflare Tunnel, JWT, backups and restores |
| `git` | safe git: inspect first, branches, conflicts, undo by least-destructive step, never force or reset without asking |
| `bash` | strict-mode, quoted, shellcheck-clean scripts; arrays, traps, argument parsing |
| `zsh` | zsh vs bash pitfalls (no word splitting, 1-based arrays, glob errors), `.zshrc` editing |
| `markdown` | READMEs and docs: prose wrapped at 80 columns (or narrower if asked), tables exempt but aligned |
| `linux` | diagnosing processes, disk, memory, GPU, services, logs, network; asks before system-wide changes |

The system prompt lists each skill's name and description; the
model loads one with `use_skill` (and its files with `use_skill`
+ `file`). `/skill release 1.4.0` or just `/release 1.4.0` runs one
directly, with its instructions inlined.

**The scratchpad** is per-project working memory outside the repo
(`~/.local/share/armin/scratch/<project>-<hash>/`). The model writes
its plan, findings and long reasoning there with `scratchpad_write`,
keeps only the file names in mind, and re-reads with
`scratchpad_read` — so a long task doesn't need everything in
context at once. Compaction summaries point at these files.
`/scratch` lists them, `/scratch clear` empties it.

## The knowledge tree

What armin establishes about a project, kept between sessions in
`~/.local/share/armin/know/<project>-<hash>/`: nodes (topics,
concepts, facts, decisions, files, symbols, questions) in a tree of
paths like `tcp/handshake`, each with a claim, a confidence (guess,
inferred, checked, tested), evidence (file ranges, URLs, commits) and
who wrote it, plus edges for what a node needs or was derived from.

The system prompt carries the top two levels, frozen at session
start. The model searches with `know_find` before exploring, records
what it finds with `know_add`, and cites nodes as `[n42]` instead of
restating them. A minion's claims stay marked unverified until the
copilot or advisor checks them, so a wrong conclusion can be traced
back (`know_trace`).

Where notes go:

| About | Store |
|---|---|
| how you want things done | memory |
| how to do a task | skill |
| what is true about the code or topic | the tree (the only store checked for staleness) |
| what's being done right now | scratchpad |

At each start armin checks the tree against the code, with or without
version control: each cited file's size, inode, modification and change
time are compared with the last check's, and only the files that
changed are re-hashed where evidence cites them. Code that only moved within its
file is followed; a claim whose lines changed is marked STALE, and
whatever needs it or was derived from it SUSPECT, until the model
re-reads it. A `requires` loop (A needs B needs A) gets a question
under `cycles/` that the copilot or advisor must resolve.

`memory_save` refuses a note that cites a file and line and points to
`know_add`. To read or change the tree yourself: `armin -know dump`,
`armin -know edit` (or `=tcp` for a subtree), and once, `armin -know
import-memories` to move older code-fact memories into it.

## Sessions

Every conversation is saved as its own session: a file named by a
UUID under `~/.local/share/armin/sessions/`, whose first line holds
the project directory, the model, a short title the model writes
after the first turn, and timestamps. Messages are appended as they
happen, never rewritten.

```sh
armin -resume                 # menu: directory, title, id, age, size
armin -resume 999cd437        # by id, or any unique prefix of it
armin -resume=999cd437        # same
```

The menu lists sessions newest first, with this directory's marked
`*`; type a number or an id, or Enter for a new session. Resuming
by id works from anywhere: armin opens in that session's directory.
On exit armin prints `resume with: armin -resume <id>`.

In the REPL:

| Command | Does |
|---|---|
| `/sessions` | list saved sessions (`→` this one, `*` this directory) |
| `/sessions rm <id>` | delete a session and its scratchpad, after asking |
| `/resume <id>` | switch to another session of this directory |
| `/title <text>` | rename this session |

Each session has its **own scratchpad** (`scratch/<project>/<id>/`):
its plan, archived tool results and thinking notes don't mix with
another session's. **Memories** are shared by every session of the
project (and user memories by every project) — durable facts should
carry over; `/memory rm <name>` deletes one. Pass `-no-save` to keep
a session off disk. Sessions saved before ids existed (one file per
project) still list and resume.

