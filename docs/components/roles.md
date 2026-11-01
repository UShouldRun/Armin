# Roles, delegation and the learner

How much armin does on its own, and for whom. `internal/agent`
(`role.go`, `tools_delegate.go`, `tools_teach.go`) and
`internal/learner`.

## Roles

Three, switched with `/minion`, `/copilot` and `/advisor`, each on
the model config.conf names for it (`minion = local`, `copilot =
planner`, ...):

| role | its tools | for |
|---|---|---|
| minion | everything but `run_program` | searches and small edits on a cheap local model; what it writes to the knowledge DAG stays marked as unchecked |
| copilot | everything | the full agent, with approvals for writes and runs |
| advisor | no file changes (`edit_file`, `write_file`, `run_program`) | decisions and learning: explains, asks, plans |

Each role gets its own tool list, pinned byte for byte by a golden
test (`testdata/definitions.<role>.json`). A switch mid-session
reminds the model which role it is now.

## Delegation

The copilot hands a task to the minion (`delegate`) with an
acceptance check run afterwards: the tests, the build, the linter, a
command, or none. A passing check raises what the minion wrote to
the DAG from inferred to checked, with the check as evidence.

## The learner

What the user knows: a knowledge DAG of topics and concepts (one per
user, shared across projects) plus events (a check question, what
the user says they know, their own explanation, a refresher), adding
up to a status per concept with spaced repetition. The logic is
built and tested; teaching (the advisor's probe, plan and lessons)
is the next step of the restructure.

## Connections

- set by: `config` (role keys), `repl` (the slash commands)
- uses: `llamacpp` (each role's model), `know` (minion authorship and
  confidence)
