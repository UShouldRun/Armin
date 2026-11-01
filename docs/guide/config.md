# The config file

## Config file

`~/.config/armin/config.conf` (or `$XDG_CONFIG_HOME/armin/config.conf`):

```
# key = value, "#" comments, nothing fancier
model      = chat
llama_url  = http://localhost:8080
timeout    = 30s
max_output = 5000
allow      = curl, docker
allow_exec = mpirun, bazel   # extra run_program programs

# Everything above is the zero-config path: -model's name ("chat")
# has no models.<name> block below, so it falls back to whatever's
# already running at llama_url. Add a models.<name> block per name
# you want armin itself to spawn and reap (see models.md).
models.coder.binary      = llama-server
models.coder.model_path  = /home/you/models/qwen2.5-coder-7b-q4.gguf
models.coder.port        = 8081
models.coder.idle_timeout = 20m
models.coder.args        = --ctx-size 8192 --n-gpu-layers 999
```

`allow` appends to the default binary allowlist; it doesn't replace it.
`allow_exec` does the same for `run_program`'s list.

## Repo map

```
repo_map = off
```

leaves out the static map of the project in the system prompt: its files
ranked by how often the rest of the project names what they define,
each with its main definitions and their lines, in about 2k tokens
(internal/repomap). It needs Universal Ctags (`ctags_bin`, or
`universal-ctags` on the PATH or in ~/.local/bin). It is built in the
background as armin starts (0.25 s on armin itself) and fixed for the
session, so the prompt stays cacheable. On by default: in the tree
benchmark it cut tool calls by 26% from the first session (round 10),
and the knowledge DAG on top of it broke even (round 11;
docs/design/restructure-tree-economics.md). Without Universal Ctags
there is no map, and nothing else changes.

## Speech

```
stt        = whisper-cli -m ~/models/ggml-large-v3-turbo-q5_0.bin -f {wav} -l auto -nt -np -mc 0 -sns
stt_record = parec --rate=16000 --channels=1 --format=s16le --file-format=wav {wav}
```

`/voice` records from the default microphone with `stt_record` (when
unset: the first of `parec`, `pw-record`, `arecord`, `rec` found) and
transcribes with `stt`, both command lines where `{wav}` is the file.
armin has no audio code of its own. A recording with no sound in it (a
stuck microphone) is refused instead of transcribed, since whisper
turns silence into "Thank you." `-mc 0` keeps whisper from looping on
a phrase.

The advisor reads more from how you explain something than from a quiz
answer: `learn_explained` records the concepts you used correctly, the
misconceptions (each a question node the teaching goes after) and the
gaps (needed, never touched: "not sure" until a right answer).

## Roles

```
minion  = qwen3.5-9b   # local: searches, small edits
copilot = glm          # hosted: the full agent
advisor = planner      # hosted: decisions and learning
```

Each names a model (a `models.<name>` block or a hosted id). `/minion`,
`/copilot` and `/advisor` switch to that role and its model; a role
without a key keeps the current model. The role decides the tools: the
minion has everything but `run_program`, the copilot everything, the
advisor no file changes (`edit_file`, `write_file`, `run_program`).
Starting on a role's model starts in that role.

With a `minion` set, the copilot and advisor get `delegate`: a small,
well-defined task handed to the minion with an acceptance check (the
tests, the build, the linter or a command). The minion runs its own
short turn; your approval still applies to its edits, and the advisor's
minion never edits. What it adds to the knowledge DAG stays marked as
the minion's until the copilot or advisor verifies it; a passing check
raises it from inferred to checked. While chat runs on a hosted model,
armin's own small helper calls (titles, effort, library cataloging) go
to the minion too; summaries and checks that read the whole
conversation stay on the chat model, where it is already cached.

Every llama-server armin starts, local or remote, gets lean
defaults that spend memory on context instead of features a
single-user, text-only agent doesn't use. Each applies only if your
`args` don't set that flag themselves:

| Default | Why |
|---|---|
| `--no-mmproj` | never load a vision encoder (~0.9 GB for a 27B Qwen) |
| `-np 1` | one slot; the server's auto default is 4 |
| `-fa on` | flash attention: less scratch memory, needed for a quantized V cache |
| `-ctk q8_0 -ctv q8_0` | KV cache at half the size of f16, negligible quality loss |
| `-cram 1024` | host-RAM prompt cache at 1 GiB instead of 8 |

`models.<name>.vision = true` keeps the vision encoder loadable;
`models.<name>.lean = false` drops all of these. Speculative-decoding
drafts (MTP, EAGLE) are separate files llama.cpp only loads when
asked, and armin never asks.

Agent keys (all optional):

```
max_tool_rounds = 50     # rounds per turn before the model must report
auto_approve    = false  # true = never ask before file writes
context_size    = 65536  # tokens; default asks llama-server's /props
keep_turns      = 2      # recent turns that keep full tool results
read_timeout    = 5m     # give up after this long with no bytes
build_timeout   = 10m    # run_tests / build_check / lint_check
test_cmd        = go test -race ./...
build_cmd       = make
lint_cmd        = golangci-lint run
library         = ~/Documents/Software  # reference library, see library.md
effort          = auto   # auto, off, low, medium, high
aids            = all    # all, none, or stuck,procedure,verify
```

