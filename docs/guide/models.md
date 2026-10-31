# Models: local, hosted, cloud

## Model lifecycle

Unlike Ollama (a single daemon juggling many pulled models), a
`llama-server` process serves exactly one already-loaded model — so
`/model <name>` (or `:ArminModel`/`armin.model` over LSP) means
"point at a different URL", not "ask the same daemon for a different
model". Two kinds of `models.<name>` entry:

- **External** (`models.<name>.url = ...`) — something you already
  run yourself, e.g. on a beefy always-on box. armin only ever talks
  to it; it never starts or stops that process.
- **Managed** (`models.<name>.binary`/`.model_path`/`.port`, `.args`
  optional) — armin spawns `llama-server` itself on first use,
  health-polls it before handing back a client, and — mirroring
  `internal/searxng`'s Docker container lifecycle — stops only what
  it started. A managed model left idle (no in-flight request) past
  its `idle_timeout` (default 20m; a negative value disables this) is
  stopped by a background reaper and respawned lazily next time it's
  used, the same tradeoff Ollama's own default `keep_alive` unload
  already makes.

`-model`/`-completion-model` with no matching `models.<name>` entry
fall back to an external, unmanaged spec at `-llama-url`/
`-completion-llama-url` — the zero-config path, for "I already
started a `llama-server`, just talk to it".

### Installing a model

`armin -install <model>` checks whether a model will run well on this
machine before downloading it. `<model>` is any of:

- a name from the built-in table of popular models (`qwen3.5-9b`,
  `gemma-4-26b-a4b`, `lfm2.5-8b-a1b`, …; `armin -install list` shows
  them all), optionally with `:Q4_K_M`;
- a Hugging Face repo, `unsloth/Qwen3-30B-A3B-GGUF[:Q4_K_M]`, or a
  file in one, `org/repo/file.gguf`;
- a Hugging Face link as the site gives it: the repo page, a
  `/tree/<branch>/<folder>`, `/blob/…` or `/resolve/…` file link,
  `hf.co` short links;
- a direct link to a `.gguf` on any other server (ModelScope, a
  GitHub release, your own) — it must serve byte ranges;
- a `.gguf` already on disk.

1. It reads the model's `.gguf` header (a few MB by HTTP range, not
   the weights): dense or MoE, layers, experts, which layers keep a
   KV cache (hybrid and sliding-window models keep less), bytes by
   role.
2. It reads the machine: dedicated and integrated GPUs, VRAM,
   memory bandwidth and prefill TFLOPS (from a table of known GPUs),
   PCIe link, RAM size and free RAM, RAM type and speed (from
   config.conf's `ram = <type>-<speed>`, e.g. `ram = ddr5-5600` or
   `ram = ddr4-3200 1ch`, which `./install.sh` sets up with `sudo
   dmidecode` or by asking; else from SMBIOS when armin runs as
   root; else assumed), CPU cores and AVX2/AVX-512, free disk.
3. A decision tree places the weights — all in VRAM at the largest
   window that fits; for a MoE, the experts of the fewest layers in
   RAM; for a dense model, as many layers in VRAM as fit; or all on
   the CPU — and estimates generation (bound by memory bandwidth)
   and prompt reading (bound by compute, or by PCIe when weights live
   in RAM).
4. It prints the verdict — fine, slow, too slow or won't load — with
   what limits each and the numbers: the GPU's memory bus, system
   RAM's bus, the context memory read every token, GPU or CPU
   compute, the PCIe copies, free RAM, disk, or a llama-server build
   that doesn't know the architecture. Then fixes, and the same
   verdict for every quantization in the repo.
5. At the download question you can also type another quantization
   from the list (`Q4_0`): it reads that one and shows its verdict
   instead. On your yes it downloads (SHA-256 checked; a dropped or
   stalled connection — 30 s without data — is retried from where it
   stopped, waiting 5 s doubling to 60 s, for up to 30 minutes
   without progress; after that, or after Ctrl-C, running the same
   command resumes from the `.part` file) and appends a
   `models.<name>` block with the plan's flags (`-ngl`, `-c`,
   `--n-cpu-moe`) to config.conf, then offers a `llama-bench` run to
   set measured speeds beside the estimates.

`armin -uninstall <name>` takes one out again: it shows the
`models.<name>` lines and the files it would delete (every part of a
split model, any half-finished `.part`), and on your yes removes the
lines — with the comment `-install` wrote above them — and deletes
the files. A file another model also points at is kept; a hosted or
cloud model only loses its lines; and it refuses to remove your
`model =` or `completion_model =` until you point those elsewhere.
`armin -uninstall list` shows every configured model, where it runs
and what it takes on disk.

With no quantization named, it picks the largest one that runs fine
(at 3 bits or more). Diffusion models (Dream, LLaDA) are recognized
and refused: llama-server can't serve them.

### Authenticated / remote models (e.g. Claude)

An external `models.<name>.url` isn't limited to a `llama-server` you
run yourself — any OpenAI-compatible `/v1/chat/completions` endpoint
works, including
[Anthropic's own](https://docs.anthropic.com/en/api/openai-sdk), so
armin can run entirely against Claude instead of a local model. The
only thing a remote endpoint needs that a local `llama-server`
doesn't is auth, sent as `Authorization: Bearer <key>` on every
request when `models.<name>.api_key_env` is set:

```
# ~/.config/armin/.env — "KEY=value", "#" comments, nothing fancier.
# A real exported environment variable always wins over this file.
ANTHROPIC_API_KEY = sk-ant-...
```

```
# ~/.config/armin/config.conf
model = claude
models.claude.url         = https://api.anthropic.com/v1
models.claude.api_key_env = ANTHROPIC_API_KEY
```

A hosted model usually has its own name for the model: put it in
`id` (armin's alias stays the config name), and for a reasoning
model cap its thinking with `reasoning` (`none`, `minimal`, `low`,
`medium`, `high`, `xhigh`, `max`) — Qwen3.8 thinks at `xhigh` by
default, which costs time and tokens. A base URL may or may not end
in `/v1`. For example, a big planning model on OpenRouter beside the
local one:

```
models.planner.url         = https://openrouter.ai/api/v1
models.planner.api_key_env = OPENROUTER_API_KEY
models.planner.id          = qwen/qwen3.8-27b
models.planner.reasoning   = medium
models.planner.region      = auto
```

When the machine's time zone is in a region OpenRouter routes
in-region (the EU or the US), an `openrouter.ai` URL is sent to that
region's host instead — `eu.openrouter.ai` or `us.openrouter.ai` —
so prompts and completions are decrypted and served only there.
`models.<name>.region` decides: `auto` (the default) picks by time
zone, `eu` or `us` forces that region's host whatever the time zone
says, and `off` never leaves the global host — most accounts can't
use the regional ones, so `off` is the sane setting for them.
In-region routing needs OpenRouter's Business plan: without it the
region refuses (403), and armin says so and moves to the global host
for the rest of the session. A URL written as `eu.openrouter.ai` (or
`us.`) in the config never falls back — it stays in the region or
fails.

OpenRouter forwards each request to a provider that hosts the model,
and some providers keep prompts or train on them. `models.<name>.private
= true` sends only to providers that collect nothing and keep nothing
(OpenRouter's `data_collection: deny` and zero data retention). A model
no such provider serves then fails with OpenRouter's "no endpoints"
error instead of going to one that logs. Off by default; OpenRouter's
own privacy settings (openrouter.ai/settings/privacy) do the same for
every request from the key.

`/model planner` (or `:ArminModel planner`) first asks which
OpenRouter model to use — its own id first, Enter keeps it, a number
picks a suggestion, any `vendor/model` id works — and the same
question comes up when armin starts on it (`model = planner`),
before anything connects. Then it switches chat to it;
inline completion stays on the local model, and `/model local`
brings chat back. To try any other model the provider offers
without a new block, name it after the base:
`/model planner:deepseek/deepseek-v4-pro-0813` uses planner's URL,
key and reasoning with that id.

With an OpenRouter model configured, `/model` (and nvim's
`:ArminModel` picker, and the web UI's model menu) also lists
suggestions from OpenRouter's live catalog — each vendor's newest
tool-capable model and its cheapest recent one, with free ones last
— tagged with their price per 1M tokens and what a deep planning
session costs (~1.2M input, 30k output tokens), and shows what's
left on the account (through the same key) with a help line on how
to switch. The catalog is cached a day in armin's data directory;
the balance is asked at most once a minute. Run `/compact` or `/clear` before a switch to keep
a long local conversation from being billed as input.

`api_key_env` names an environment variable — populated from `.env`
above if it isn't already set some other way — never the key itself.
Keep it out of `config.conf`: that file is plain text people back up,
sync, and paste into issues without thinking twice; `.env` (next to
it, same `~/.config/armin` directory) is where a secret belongs.

Two things armin does automatically so that secret doesn't spread
further than it has to:

- **`.env` must be readable only by you.** `config.LoadEnv` refuses
  to read a group- or other-readable `.env` at all (a wrong umask
  leaving it world-readable is exactly the kind of mistake that
  should fail loud, not quietly work) — `./install.sh` creates it at
  mode 600 from the start and re-tightens an existing one on every
  run, so this shouldn't come up in practice.
- **A key configured for one model never reaches another model's
  process.** Spawning a child process inherits the *entire*
  environment by default — so without this, a managed local
  `llama-server` would see `ANTHROPIC_API_KEY` too, despite having
  nothing to do with Claude. `internal/llamacpp.Manager` strips every
  name any `models.<name>.api_key_env` names from a spawned
  process's environment, regardless of which model that name was
  configured for.

## A model on a cloud machine armin starts and stops

Give armin cloud credentials and a machine id — an AWS EC2 instance,
a STACKIT server or a Scaleway instance — and it runs the whole
lifecycle: on first use
it starts the machine (if it's off), waits for it to boot, launches
llama-server on it over SSH with a tunnel to your machine, and when
armin exits it stops the machine again. Nothing listens on a public port — llama-server binds to the
instance's loopback and the traffic goes through SSH.

```
# ~/.config/armin/.env (mode 600)
AWS_ACCESS_KEY_ID     = AKIA...
AWS_SECRET_ACCESS_KEY = ...
```

```
# ~/.config/armin/config.conf
model = qwen
models.qwen.aws_instance = i-0123456789abcdef0
models.qwen.aws_region   = eu-west-1
models.qwen.ssh_key      = ~/.ssh/gpu-box.pem
models.qwen.binary       = /home/ubuntu/llama.cpp/build/bin/llama-server
models.qwen.model_path   = /home/ubuntu/models/qwen-27b-q4_k_m.gguf
models.qwen.args         = --jinja -ngl 999 -c 65536
models.qwen.idle_timeout = 30m
```

| Key | Default | |
|---|---|---|
| `aws_instance` | — | the EC2 instance to use; makes the model AWS-hosted |
| `aws_region` | `$AWS_REGION` | |
| `stackit_server` | — | the STACKIT server id; makes the model STACKIT-hosted |
| `stackit_project` | `$STACKIT_PROJECT_ID` | |
| `stackit_region` | `eu01` | |
| `scaleway_server` | — | the Scaleway instance id; makes the model Scaleway-hosted |
| `scaleway_zone` | `$SCW_DEFAULT_ZONE`, else `fr-par-2` | |
| `ssh_user` | `ubuntu` (`root` on Scaleway) | |
| `ssh_key` | ssh's own defaults | private key for the instance |
| `binary`, `model_path`, `args` | `llama-server` | paths on the instance |
| `remote_port` | `8080` | llama-server's port on the instance |
| `port` | `18180` | local end of the tunnel |
| `idle_timeout` | `20m` | stop the instance after this long unused (it starts again on the next request) |
| `stop_on_disconnect` | `true` | see below (`aws_stop_on_disconnect` also works) |

What to know:

- **Stops only what it started.** An instance that was already
  running when armin got to it is used and left running.
- **Dead man's switch.** When armin started the instance, the remote
  side runs llama-server under `trap 'sudo -n shutdown -h now' HUP`:
  if the SSH session drops — armin killed with `-9`, laptop asleep,
  network gone for about a minute — the instance shuts itself down
  instead of billing. armin restarts it on the next request. Needs
  passwordless `sudo` for `ssh_user` (the default on Ubuntu AMIs);
  turn it off with `stop_on_disconnect = false`.
- **IAM.** The keys need only `ec2:DescribeInstances`,
  `ec2:StartInstances` and `ec2:StopInstances`, ideally scoped to
  that one instance. They never reach ssh or llama-server.
- **Host keys** are pinned per machine id (a stopped machine comes
  back with a new public IP) in
  `~/.local/share/armin/remote_known_hosts`.
- **First start is slow.** Boot plus loading a large model from a
  cold EBS volume can take several minutes; progress is shown as it
  goes. The instance needs a public IP (or a VPN to its private one)
  and SSH open to you in its security group.
- Stopping an instance stops compute billing, not storage: the EBS
  volume holding the model is still charged.

### STACKIT

STACKIT is Schwarz Gruppe's cloud, run from German data centers.
armin authenticates with a service account, using the same variables
as the STACKIT CLI and SDK:

```
# ~/.config/armin/.env (mode 600)
STACKIT_SERVICE_ACCOUNT_KEY_PATH = ~/.config/armin/stackit-sa.json
# or, if the key file has no private key inside:
# STACKIT_PRIVATE_KEY_PATH = ~/.config/armin/stackit-private.pem
# or a static token instead of the key flow:
# STACKIT_SERVICE_ACCOUNT_TOKEN = ...
```

```
# ~/.config/armin/config.conf
model = qwen
models.qwen.stackit_server  = 0b8a…   # server id
models.qwen.stackit_project = 9f1c…   # project id
models.qwen.ssh_key         = ~/.ssh/stackit.pem
models.qwen.model_path      = /home/ubuntu/models/qwen-27b-q6_k.gguf
models.qwen.args            = --jinja -ngl 999 -c 262144
```

With the key flow, armin signs an RS512 JWT with the service
account's key, exchanges it for a short-lived access token, and
caches the token until shortly before it expires. The service
account needs permission to read, start and deallocate the server.

armin **deallocates** the server instead of stopping it. A stopped
STACKIT server stays on its hypervisor and is billed at full price.
A deallocated one is billed only for its volume, and starting it
allocates it again. A GPU flavor can be out of capacity at the moment
armin tries to reallocate it; in that case the start fails with
STACKIT's error, and retrying later works.

### Scaleway

```
# ~/.config/armin/.env (mode 600)
SCW_SECRET_KEY = ...
```

```
# ~/.config/armin/config.conf
model = qwen
models.qwen.scaleway_server = 5f2e…        # instance id
models.qwen.scaleway_zone   = fr-par-2     # where the L40S runs
models.qwen.ssh_key         = ~/.ssh/scaleway
models.qwen.model_path      = /root/models/qwen-27b-q8_0.gguf
models.qwen.args            = --jinja -ngl 999 -c 262144 -fa on -ctk q8_0 -ctv q8_0
```

The secret key goes in an `X-Auth-Token` header, and that's the
whole authentication. armin uses `poweroff`, which releases the
instance's slot on its hypervisor and stops compute billing, never
`stop_in_place`, which keeps the slot reserved. Scaleway bills GPU
instances per minute of uptime, though its general billing FAQ also
mentions a 60-minute minimum per resource, so check before relying
on short sessions. Storage and reserved IPs bill while powered off.

## Running a large model on a remote box

For example a ~27B Qwen on a GPU instance, armin on your laptop:

```sh
# on the GPU box: --jinja for native tool calls and reasoning_content
llama-server -m qwen-27b-q4_k_m.gguf --jinja -ngl 999 \
  -c 65536 --host 127.0.0.1 --port 8080 --api-key "$KEY"

# on your laptop: tunnel instead of exposing the port
ssh -N -L 8080:127.0.0.1:8080 gpu-box
```

```
# ~/.config/armin/config.conf
model = qwen
models.qwen.url         = http://127.0.0.1:8080
models.qwen.api_key_env = QWEN_KEY
max_output = 16000
```

`context_size` is picked up from the server. Raise `max_output` from
its default of 4000 bytes: that default suits small local models,
and a 27B model with a 64k window can take bigger tool results. Use
the sampling settings the model card recommends as llama-server
flags (`--temp`, `--top-p`, `--top-k`, `--min-p`).

