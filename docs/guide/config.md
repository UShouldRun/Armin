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

