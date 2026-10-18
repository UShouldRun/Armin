# Memory and sessions

What armin keeps between turns and between runs.

| what | package | where | lifetime |
|---|---|---|---|
| sessions | `internal/session` | `~/.local/share/armin/sessions/` | until deleted (`/sessions rm`) |
| memories | `internal/notes` (`memory.go`) | `~/.local/share/armin/memory/` | until deleted |
| scratchpad | `internal/notes` (`scratch.go`) | `~/.local/share/armin/scratch/<project>/<session>/` | one session |
| skills | `internal/notes` (`skills.go`) | `~/.config/armin/skills/`, `<project>/.armin/skills/` | until removed |
| context stats | `internal/convo` | memory only | one run |

- **Sessions** are append-only logs: after each turn only the new
  messages are written, never the whole history again. `-resume`
  continues one, in its project and on its model. Readable by you
  alone (folder 0700, files 0600).
- **Memories** are durable facts the model saves (`memory_save`):
  your preferences, project conventions, decisions. Per project or for
  every project. The system prompt carries their index (names and one
  line each); the model reads a memory when it needs it.
- **The scratchpad** holds working notes outside the project: plans,
  findings, and old tool results archived before the context trims
  them, so the model re-reads instead of re-running.
- **Skills** are instruction folders (a `SKILL.md` plus files). The
  system prompt lists them; `use_skill` loads one, `/<name>` runs it.
- **convo** measures a conversation (tokens, tool calls), writes
  compaction summaries and the `/suggest` follow-ups.
