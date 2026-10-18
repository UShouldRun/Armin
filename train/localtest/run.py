"""Runs the practical test (tasks.py) through armin itself: a
llama-server per model, then one `armin -web` per task in a fresh
folder, driven over its HTTP API like the browser page would.

  python3 run.py --model r1-179=../pod/gguf/armin-r1-179-Q4_K_M.gguf ...
  python3 run.py --report            # table of out/*.jsonl

Writes out/<model>.jsonl (one row per task, appended, so a stopped run
picks up where it was) and out/REPORT.md.
"""

import argparse, json, os, re, shutil, signal, subprocess, sys, tempfile, time
import urllib.request

from tasks import TASKS

# TASKS=all adds tasks_more.py's 32 (60 in all); runs without it stay
# comparable with the earlier 28-task results.
if os.environ.get("TASKS") == "all":
  from tasks_more import TASKS_MORE
  TASKS = TASKS + TASKS_MORE

HERE = os.path.dirname(os.path.abspath(__file__))
# ARMIN, EXTRA (more llama-server flags) and EFFORT (armin's
# -effort) vary a run without editing this file.
ARMIN = os.environ.get("ARMIN", os.path.join(HERE, "..", "..", "armin"))
LLAMA = "/media/henrique/partition/llm/llama.cpp/llama-b11559/llama-server"
LPORT, WPORT = 8190, 7790
# armin's lean defaults (README), at a context the 8 GB card holds
ARGS = [ "--jinja", "-ngl", os.environ.get("NGL", "999"), "-c", os.environ.get("CTX", "32768"), "-lm", "dio", "--no-mmproj",
  "-np", "1", "-fa", "on", "-ctk", os.environ.get("KV", "q8_0"), "-ctv", os.environ.get("KV", "q8_0"), "-cram", "1024",
  *os.environ.get("EXTRA", "").split() ]
EFFORT = [ "-effort", os.environ["EFFORT"] ] if os.environ.get("EFFORT") else []


def wait_http(url, secs):
  end = time.time() + secs
  while time.time() < end:
    try:
      urllib.request.urlopen(url, timeout = 2).read()
      return True
    except Exception:
      time.sleep(0.5)
  return False


def wait_vram(name, mib = 1500):
  """The 8 GB card holds one model: waits while another llama-server
  (the user's own armin) has it."""
  said = False
  while True:
    p = subprocess.run([ "nvidia-smi", "--query-gpu=memory.used", "--format=csv,noheader,nounits" ],
      capture_output = True, text = True)
    used = int(p.stdout.split()[0]) if p.returncode == 0 and p.stdout.strip() else 0
    if used < mib:
      return
    if not said:
      print(f"{name}: waiting for the GPU ({used} MiB in use)", flush = True)
      said = True
    time.sleep(60)


def setup(task, root):
  for path, body in task["files"].items():
    p = os.path.join(root, path)
    os.makedirs(os.path.dirname(p), exist_ok = True)
    open(p, "w").write(body)
  # a git repo, as in real use (armin can diff what it changed)
  subprocess.run("git init -q && git add -A && git -c user.email=t@t -c user.name=t commit -qm start",
    shell = True, cwd = root)


def chat(prompt, limit):
  """One turn over /api/chat's event stream; approvals are granted,
  except access outside the project."""
  req = urllib.request.Request(f"http://127.0.0.1:{WPORT}/api/chat",
    data = json.dumps({ "message": prompt }).encode(),
    headers = { "Content-Type": "application/json" })
  r = { "answer": "", "text": "", "think": 0, "tools": [], "tokens": 0, "error": None }
  start = time.time()
  ev = None
  with urllib.request.urlopen(req, timeout = limit) as resp:
    for raw in resp:
      line = raw.decode("utf-8", "replace").rstrip("\n")
      if line.startswith("event: "):
        ev = line[7:]
        continue
      if not line.startswith("data: "):
        continue
      d = json.loads(line[6:])
      if ev == "token":
        r["text"] += d
      elif ev == "reasoning":
        r["think"] += len(d)
      elif ev == "tool":
        r["tools"].append(d["line"])
      elif ev == "usage":
        r["tokens"] = d["tokens"]
      elif ev == "approve":
        ok = d["tool"] != "request_access"
        urllib.request.urlopen(urllib.request.Request(f"http://127.0.0.1:{WPORT}/api/approve",
          data = json.dumps({ "id": d["id"], "ok": ok }).encode(),
          headers = { "Content-Type": "application/json" }), timeout = 5).read()
      elif ev == "done":
        r["answer"] = d
        break
      elif ev == "error":
        r["error"] = d
        break
      if time.time() - start > limit:
        r["error"] = "timeout"
        break
  return r


def grade(task, root, r):
  for path in task.get("restore", []):
    p = os.path.join(root, path)
    os.makedirs(os.path.dirname(p), exist_ok = True)
    open(p, "w").write(task["files"][path])
  for path, body in task.get("hidden", {}).items():
    open(os.path.join(root, path), "w").write(body)

  ok = True
  if "check" in task:
    p = subprocess.run(task["check"], shell = True, cwd = root, capture_output = True, timeout = 120)
    ok = p.returncode == 0
  said = r["answer"] or r["text"]
  for pat in task.get("answer", []):
    ok = ok and re.search(pat, said) is not None
  return ok


def run_model(name, gguf, ids, limit):
  out = os.path.join(HERE, "out", f"{name}.jsonl")
  os.makedirs(os.path.dirname(out), exist_ok = True)
  done = set()
  if os.path.exists(out):
    done = { json.loads(l)["id"] for l in open(out) }
  todo = [ t for t in TASKS if t["id"] not in done and (not ids or t["id"] in ids) ]
  if not todo:
    return

  wait_vram(name)
  log = open(os.path.join(HERE, "out", f"{name}.llama.log"), "w")
  llama = subprocess.Popen([ LLAMA, "-m", gguf, "--port", str(LPORT), *ARGS ],
    stdout = log, stderr = log, start_new_session = True)
  try:
    if not wait_http(f"http://127.0.0.1:{LPORT}/health", 180):
      sys.exit(f"{name}: llama-server didn't come up, see {log.name}")
    for t in todo:
      root = tempfile.mkdtemp(prefix = f"lt-{t['id']}-")
      setup(t, root)
      armin = subprocess.Popen([ ARMIN, "-web", f"127.0.0.1:{WPORT}", "-root", root, "-yes",
        "-no-save", "-new", "-no-color", "-model", f"test-{name}",
        "-llama-url", f"http://127.0.0.1:{LPORT}", "-timeout", "60s", *EFFORT ],
        stdout = subprocess.DEVNULL, stderr = subprocess.DEVNULL, start_new_session = True)
      start = time.time()
      try:
        if not wait_http(f"http://127.0.0.1:{WPORT}/api/info", 30):
          raise RuntimeError("armin -web didn't come up")
        r = chat(t["prompt"], limit)
      except Exception as e:
        r = { "answer": "", "text": "", "think": 0, "tools": [], "tokens": 0, "error": str(e) }
      secs = time.time() - start
      os.killpg(armin.pid, signal.SIGTERM)
      armin.wait()
      ok = grade(t, root, r)
      row = { "id": t["id"], "cat": t["cat"], "ok": ok, "secs": round(secs, 1),
        "think": r["think"], "calls": len(r["tools"]), "tokens": r["tokens"],
        "error": r["error"], "answer": (r["answer"] or r["text"])[-1500:], "tools": r["tools"] }
      with open(out, "a") as f:
        f.write(json.dumps(row) + "\n")
      print(f"{name} {t['id']:16} {'PASS' if ok else 'fail'} {secs:5.0f}s "
        f"think {r['think']:5} calls {len(r['tools']):2} {r['error'] or ''}", flush = True)
      shutil.rmtree(root, ignore_errors = True)
  finally:
    os.killpg(llama.pid, signal.SIGTERM)
    llama.wait()


def report():
  rows = {}
  for f in sorted(os.listdir(os.path.join(HERE, "out"))):
    if f.endswith(".jsonl"):
      rows[f[:-6]] = [ json.loads(l) for l in open(os.path.join(HERE, "out", f)) ]
  cats = sorted({ t["cat"] for t in TASKS })
  n = { c: sum(t["cat"] == c for t in TASKS) for c in cats }
  lines = [ "# Practical test, through armin", "",
    f"{len(TASKS)} hand-written tasks, checked by running code. Pass counts per category.", "",
    "| model | " + " | ".join(f"{c} ({n[c]})" for c in cats) + " | **total** | avg s | think chars | tool calls |",
    "|---" * (len(cats) + 5) + "|" ]
  # best first: most passed, then fastest
  rows = dict(sorted(rows.items(), key = lambda kv: (-sum(r["ok"] for r in kv[1]),
    sum(r["secs"] for r in kv[1]) / max(1, len(kv[1])))))
  for m, rs in rows.items():
    by = { c: sum(r["ok"] for r in rs if r["cat"] == c) for c in cats }
    k = max(1, len(rs))
    lines.append(f"| {m} | " + " | ".join(str(by[c]) for c in cats)
      + f" | **{sum(r['ok'] for r in rs)}/{len(rs)}** | {sum(r['secs'] for r in rs) / k:.0f}"
      + f" | {sum(r['think'] for r in rs) / k:.0f} | {sum(r['calls'] for r in rs) / k:.1f} |")
  ids = [ t["id"] for t in TASKS ]
  lines += [ "", "## Per task", "", "| task | " + " | ".join(rows) + " |", "|---" * (len(rows) + 1) + "|" ]
  for i in ids:
    cell = lambda rs: next(("✓" if r["ok"] else "✗" for r in rs if r["id"] == i), "")
    lines.append(f"| {i} | " + " | ".join(cell(rs) for rs in rows.values()) + " |")
  open(os.path.join(HERE, "out", "REPORT.md"), "w").write("\n".join(lines) + "\n")
  print("\n".join(lines))


def main():
  ap = argparse.ArgumentParser()
  ap.add_argument("--model", action = "append", default = [], help = "name=path.gguf")
  ap.add_argument("--ids", default = "", help = "comma-separated task ids")
  ap.add_argument("--limit", type = int, default = 480, help = "seconds per task")
  ap.add_argument("--report", action = "store_true")
  args = ap.parse_args()
  ids = set(filter(None, args.ids.split(",")))
  for spec in args.model:
    name, path = spec.split("=", 1)
    run_model(name, os.path.abspath(path), ids, args.limit)
  report()


if __name__ == "__main__":
  main()
