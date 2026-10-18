"""Speed of llama-server options on armin-like work, one server per
option: generation tok/s on a copy-heavy edit (the model re-types a
file it just read, like write_file/edit_file), on free prose, and on
a long context. Writes out/SPEED.md.

  python3 specbench.py MODEL.gguf [MODEL2.gguf ...]
"""

import json, os, subprocess, sys, time, urllib.request

from run import LLAMA, wait_http

HERE = os.path.dirname(os.path.abspath(__file__))
PORT = 8191
BASE = [ "--jinja", "-ngl", "999", "-c", "32768", "-lm", "dio", "--no-mmproj",
  "-np", "1", "-fa", "on", "-cram", "0" ]

SRC = open(os.path.join(HERE, "..", "..", "internal", "library", "search.go")).read()
LONG = "\n\n".join(open(os.path.join(HERE, "..", "..", "internal", "library", f)).read()
  for f in ("index.go", "extract.go", "catalog.go", "library.go", "topics.go"))

WORK = {
  "edit": [ { "role": "user", "content": "Here is a Go file:\n\n```go\n" + SRC +
    "\n```\n\nRename the function snippet to passageWindow everywhere and "
    "output the complete updated file in one go code block, nothing else." } ],
  "prose": [ { "role": "user", "content": "Explain how BM25 ranking works and "
    "why it beats plain term frequency, in about 400 words." } ],
  "long": [ { "role": "user", "content": "```go\n" + LONG + "\n```\n\nSummarize "
    "what each of these files does, one paragraph per file." } ],
}

# reasoning low and a small budget: this measures writing speed, not
# how long the model thinks
EXTRA = { "chat_template_kwargs": { "reasoning_effort": "low" },
  "thinking_budget_tokens": 256, "max_tokens": 2500 }


def one(messages):
  body = json.dumps({ "messages": messages, **EXTRA }).encode()
  req = urllib.request.Request(f"http://127.0.0.1:{PORT}/v1/chat/completions",
    data = body, headers = { "Content-Type": "application/json" })
  r = json.loads(urllib.request.urlopen(req, timeout = 900).read())
  t = r.get("timings", {})
  return t.get("predicted_per_second", 0), t.get("predicted_n", 0), t.get("draft_n", 0), \
    t.get("draft_n_accepted", 0)


def bench(name, gguf, args, work):
  log = open(os.path.join(HERE, "out", f"speed-{name}.log"), "w")
  p = subprocess.Popen([ LLAMA, "-m", gguf, "--port", str(PORT), *BASE, *args ],
    stdout = log, stderr = log, start_new_session = True)
  try:
    if not wait_http(f"http://127.0.0.1:{PORT}/health", 180):
      return { w: "server failed" for w in work }

    one(WORK["prose"][:1])  # warm up
    out = {}
    for w in work:
      runs = [ one(WORK[w]) for _ in range(2) ]
      tps = sum(r[0] for r in runs) / len(runs)
      drafted = sum(r[2] for r in runs)
      accepted = sum(r[3] for r in runs)
      acc = f", {accepted}/{drafted} drafts accepted" if drafted else ""
      out[w] = f"{tps:.1f} tok/s ({runs[0][1]} tokens{acc})"
      print(name, w, out[w], flush = True)

    return out
  finally:
    os.killpg(p.pid, 15)
    p.wait()
    time.sleep(2)


def main():
  models = sys.argv[1:]
  q8 = [ "-ctk", "q8_0", "-ctv", "q8_0" ]
  rows = []
  m0 = models[0]
  # SPECS=none,ngram-mod times only those; ONLY_SPEC=1 skips the rest
  specs = os.environ.get("SPECS", "none,ngram-simple,ngram-map-k,ngram-map-k4v,ngram-mod,ngram-cache")
  for spec in specs.split(","):
    args = q8 + ([] if spec == "none" else [ "--spec-type", spec ])
    rows.append((os.path.basename(m0), "q8_0", spec, bench(spec, m0, args, [ "edit", "prose" ])))

  if os.environ.get("ONLY_SPEC"):
    models = models[:1]
    write(rows, os.environ.get("OUT", "SPEED.md"))
    return

  for kv in [ "q8_0", "q4_0" ]:
    rows.append((os.path.basename(m0), kv, "none", bench("kv-" + kv, m0,
      [ "-ctk", kv, "-ctv", kv ], [ "long" ])))

  for m in models[1:]:
    rows.append((os.path.basename(m), "q8_0", "none", bench("model-" + os.path.basename(m), m,
      q8, [ "edit", "prose", "long" ])))

  write(rows, os.environ.get("OUT", "SPEED.md"))


def write(rows, name):
  lines = [ "# Speed of llama-server options (RTX 4060 8 GB)", "",
    "| model | KV | speculation | edit (copy-heavy) | prose | long context |", "|---|---|---|---|---|---|" ]
  for model, kv, spec, out in rows:
    lines.append(f"| {model} | {kv} | {spec} | {out.get('edit', '')} | {out.get('prose', '')} | {out.get('long', '')} |")

  open(os.path.join(HERE, "out", name), "w").write("\n".join(lines) + "\n")
  print("\n".join(lines))


if __name__ == "__main__":
  main()
