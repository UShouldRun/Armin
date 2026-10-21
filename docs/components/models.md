# Models

How armin talks to models and keeps them running. Four packages,
plus `install` for getting one onto the machine:

| package | job |
|---|---|
| `internal/llm` | the client: one HTTP request per model call |
| `internal/llamacpp` | runs the models: starts, health-checks, reaps servers |
| `internal/cloud` | boots and stops the GPU machines remote models run on |
| `internal/catalog` | OpenRouter's model list and prices, for the pickers |
| `internal/install` | `-install`: will a model run here, and why not |

## The client (`llm`)

Every model, local or hosted, speaks the OpenAI-compatible
`/v1/chat/completions`, so one `llm.Client` serves them all:

- `ChatStream` sends the history and tool definitions and reads the
  reply as it streams: answer tokens, thinking, tool-call fragments
  assembled by index, token usage.
- It times out a server that goes **silent**, not one that's slow,
  so a long generation is never cut off.
- **Effort** reaches a hosted model as OpenRouter's `reasoning.effort`
  and a local one as its chat template's switches plus a thinking
  token budget.
- **Private** (`models.<name>.private = true`) adds OpenRouter's
  `provider: {data_collection: deny, zdr: true}`: only providers that
  keep nothing; with none for the model, the request fails.
- `Chore()` is the same model with thinking off, for background work
  (summaries, titles, cataloging).
- `Complete` is inline code completion (llama-server's `/infill`).
- Reads are bounded: error bodies to 64 KiB, whole replies to 16 MiB.

## Running models (`llamacpp`)

config.conf's `models.<name>` entries become `ModelSpec`s, of two
kinds:

- **external**: a URL (a hosted API, a server you run). armin never
  starts or stops it.
- **managed**: a binary, a `.gguf` and a port. armin starts the server
  on first use, waits until it's healthy, and stops it after 20 idle
  minutes.

The `Manager` hands out clients by name:
- `Switch` moves a client to another model, stopping the local one
  being left first, because two rarely fit one GPU.
- Children get an environment without API keys or cloud credentials.
- On Linux a server armin started dies with armin (parent-death
  signal).

A model with a streaming build has **two tiers**: resident (KV cache
in VRAM, fastest) and stream (KV cache from RAM, larger). The agent
moves between them as the context grows (`tier.go`).

## Cloud machines (`cloud`)

A managed model can run on a cloud GPU: AWS EC2, STACKIT or Scaleway.
`cloud.New` builds a `Machine` (start-and-wait, stop) from credentials
in the environment, never from config.conf. `llamacpp/remote.go` then:

1. boots the machine;
2. runs llama-server over one SSH session that also forwards its port;
3. with `stop_on_disconnect`, has the remote side shut the machine
   down if that session drops, so a crash never leaves a GPU billing.

## The catalog (`catalog`)

OpenRouter's public model list, narrowed to models armin can drive
(tool calling), and priced by what a deep session costs. It's fetched
in the background and cached a day, so no picker waits on it.
`region.go` picks OpenRouter's in-region host for your time zone and
falls back to the global one — or, under
`models.<name>.region = eu|us|off`, the host the config forces.

## Installing (`install`)

`armin -install <model>` runs before anything loads (main.go calls
it in place of think):

1. `gguf.go` reads the header, locally or by HTTP range: layers,
   experts, which layers keep a KV cache, and bytes by role, sized
   from the gaps between tensor offsets so unknown quant types work.
2. `machine.go` reads sysfs and /proc: GPUs (nvidia-smi for NVIDIA
   memory, `llamacpp.GPUWarning` for a broken driver), PCIe link,
   RAM and its SMBIOS type and speed, CPU, disk.
3. `plan.go` is the decision tree: placement (all in VRAM, MoE
   experts in RAM, layers split, CPU), then estimates: generation =
   bytes read per token over each side's bandwidth; prefill = FLOPs
   over the GPU's (or CPU's) compute plus PCIe copies of weights in
   RAM. Each limit becomes a finding with its numbers.
4. `hub.go` downloads with resume and SHA-256; `conf.go` appends the
   `models.<name>` block; `llama.go` checks the build knows the
   architecture and runs `llama-bench`.
5. `uninstall.go` is `-uninstall`: the block out of config.conf
   (rewritten atomically) and the files deleted, unless shared.

Estimates checked on an RTX 4060 Laptop: within ~10% for
generation and prefill, all in VRAM and with experts in RAM.

## Connections

- built by: `think.startModels`, from config.conf
- used by: `agent` (every turn), `repl` and `lspserver` (`/model`,
  completion), `convo` (summaries), `library` (cataloging)
