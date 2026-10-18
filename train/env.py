"""The tool environment: armin's tools, by armin's names, argument
shapes and descriptions, run against one task's sandbox (files,
search corpus, memories, skills). TRL turns every public method of
ArminEnv into a tool; grading lives outside the class for that
reason. Standard library only (pytest for run_tests)."""

import atexit
import json
import os
import re
import shutil
import tempfile

import hints
from common import DATA, final_parse, read_jsonl
from sandbox import run_pytest, run_python
from search import Index

TASKS = {}
INDEX = None

CITE_NOTE = (
  "Cite each claim taken from a result inline as [id]. End your "
  "answer with a Sources list: - [title](url) per id cited."
)
SNIPPET   = 700
READ_MAX  = 400
PLAN_MIN  = 40      # chars; a shorter scratchpad note is not a plan
OUT_REL   = 0.01    # answer vs a number the script printed

_invented_re = re.compile(
  r"NameError: name '\w+' is not defined"
  r"|ModuleNotFoundError: No module named"
  r"|AttributeError: module '[\w.]+' has no attribute"
)
_num_re = re.compile(
  r"-?\d[\d,]*(?:\.\d+)?(?:[eE][-+]?\d+)?|-?\.\d+(?:[eE][-+]?\d+)?"
)


def load(*names):
  global INDEX
  for name in names:
    for t in read_jsonl(os.path.join(DATA, name + ".jsonl")):
      TASKS[t["id"]] = t

  if INDEX is None:
    INDEX = Index(read_jsonl(os.path.join(DATA, "corpus.jsonl")))


class ArminEnv:
  def __init__(self):
    self._dir = None
    atexit.register(self._cleanup)

  def reset(self, task_id = None, **_):
    self._cleanup()
    if task_id is None:
      return None

    self._task = TASKS[task_id]
    self._dir = tempfile.mkdtemp(prefix = "armin-env-")
    for path, content in self._task.get("files", {}).items():
      self._write(path, content)

    self._memories = { m["name"]: m for m in self._task.get("memories", []) }
    self._skills = { s["name"]: s for s in self._task.get("skills", []) }
    self._scratch = {}
    self._saved = []
    self._used = { "memory": set(), "skill": set(), "tests": 0, "python": 0 }
    self._sources = {}
    self._snippets = {}
    self._calls = 0
    # For the process and retrieval rewards: the order of the steps
    # that matter, every paragraph retrieved, repeated queries and
    # what the scripts printed.
    self._trace = []
    self._retrieved = set()
    self._queries = set()
    self._repeats = 0
    self._outputs = []
    self._hints = 0

    return None

  # ---- files ----

  def read_file(
    self, path: str, start_line: int | None = None, end_line: int | None = None,
  ) -> str:
    """Read a line range from a file. Files over 400 lines require start_line/end_line — search first to find the range you need.

    Args:
      path: File path, relative to project root
      start_line: 1-indexed start line (inclusive)
      end_line: 1-indexed end line (inclusive)
    """
    self._calls += 1
    with open(self._resolve(path)) as f:
      lines = f.read().splitlines()

    if len(lines) > READ_MAX and start_line is None:
      raise ValueError(
        f"{path} has {len(lines)} lines; pass start_line/end_line"
      )

    lo = max((start_line or 1) - 1, 0)
    hi = end_line or len(lines)
    return json.dumps({
      "content":     "\n".join(lines[lo:hi]),
      "total_lines": len(lines),
    })

  def write_file(self, path: str, content: str) -> str:
    """Create a file, or overwrite one you have already read. Parent directories are created. Use edit_file for partial changes.

    Args:
      path: File path, relative to project root
      content: The complete file content
    """
    self._calls += 1
    full = self._resolve(path)
    old = ""
    created = not os.path.exists(full)
    if not created:
      with open(full) as f:
        old = f.read()

    self._write(path, content)
    self._trace.append("edit")
    return json.dumps({
      "path":          path,
      "lines_added":   content.count("\n") + 1,
      "lines_removed": old.count("\n") + 1 if old else 0,
      "created":       created,
    })

  def edit_file(
    self, path: str, old_string: str, new_string: str,
    replace_all: bool = False,
  ) -> str:
    """Replace an exact string in a file. Read the file first. old_string must match exactly once, whitespace included — add surrounding lines to make it unique, or set replace_all. Prefer this over write_file for changes to existing files.

    Args:
      path: File path, relative to project root
      old_string: Exact text to replace, copied from read_file
      new_string: Replacement text (empty deletes old_string)
      replace_all: Replace every occurrence (default false)
    """
    self._calls += 1
    full = self._resolve(path)
    with open(full) as f:
      text = f.read()

    n = text.count(old_string)
    if n == 0:
      raise ValueError("old_string not found in " + path)

    if n > 1 and not replace_all:
      raise ValueError(
        f"old_string matches {n} times in {path}; add context or "
        "set replace_all"
      )

    text = text.replace(old_string, new_string, -1 if replace_all else 1)
    with open(full, "w") as f:
      f.write(text)

    self._trace.append("edit")

    return json.dumps({
      "path":          path,
      "replacements":  n if replace_all else 1,
      "lines_added":   new_string.count("\n") + 1,
      "lines_removed": old_string.count("\n") + 1,
    })

  def list_directory(self, path: str | None = None, depth: int | None = None) -> str:
    """List a directory as an indented tree, up to a given depth. Skips .git, node_modules, vendor, and build output.

    Args:
      path: Directory to list (default: project root)
      depth: How many levels deep to recurse (default: 2)
    """
    self._calls += 1
    top = self._resolve(path or ".")
    limit = depth or 2
    lines = []
    for cur, dirs, files in os.walk(top):
      level = os.path.relpath(cur, top).count(os.sep)
      if os.path.relpath(cur, top) == ".":
        level = -1

      if level + 1 >= limit:
        dirs[:] = []

      dirs.sort()
      pad = "  " * (level + 1)
      for d in dirs:
        lines.append(pad + d + "/")

      for name in sorted(files):
        lines.append(pad + name)

    return json.dumps({ "tree": "\n".join(lines) })

  def search_code(
    self, pattern: str, path: str | None = None, file_type: str | None = None,
  ) -> str:
    """Search the codebase for a pattern using ripgrep. Returns matched lines with a couple lines of context, not full files.

    Args:
      pattern: Regex or literal pattern to search for
      path: Directory or file to search under (default: project root)
      file_type: Restrict to a file type, e.g. go, py, js
    """
    self._calls += 1
    try:
      rx = re.compile(pattern)

    except re.error:
      rx = re.compile(re.escape(pattern))

    top = self._resolve(path or ".")
    hits = []
    for cur, _, files in os.walk(top):
      for name in sorted(files):
        if file_type and not name.endswith("." + file_type):
          continue

        full = os.path.join(cur, name)
        rel = os.path.relpath(full, self._dir)
        with open(full, errors = "replace") as f:
          for i, line in enumerate(f, 1):
            if rx.search(line):
              hits.append(f"{rel}:{i}:{line.rstrip()}")

    return json.dumps({ "matches": "\n".join(hits[:200]) })

  # ---- running code ----

  def run_tests(self, args: list[str] | None = None) -> str:
    """Run the project's test command (detected from Makefile, go.mod, Cargo.toml, package.json, or Python config; or user-configured). Reports pass/fail, a failure summary, and the end of the output.

    Args:
      args: Extra arguments replacing the default target, e.g. ["./internal/tools", "-run", "TestX"] or ["-k", "parser"]
    """
    self._calls += 1
    self._used["tests"] += 1
    self._trace.append("test")
    code, out = run_pytest(self._dir, args or [])
    return json.dumps({
      "command": "python3 -m pytest " + " ".join(args or []),
      "passed":  code == 0,
      "output":  out,
    })

  def run_program(
    self, binary: str, args: list[str] | None = None, dir: str | None = None,
    stdin: str | None = None, stdout: str | None = None,
  ) -> str:
    """Build, compile, test or run code: make, cmake, gcc, g++, clang, mpicc, mpirun, go, cargo, python3, node, npm... or a program the project built ("./build/app"). Binary plus an args array, no shell: feed a file as input with stdin, save the output with stdout (then diff it). For pipes, use binary bash with args ["-c", "..."] — always asked. The user approves each run. Prefer run_tests / build_check when they fit.

    Args:
      binary: Program: a bare name (make, gcc, mpirun) or a relative path inside the project (./fox)
      args: Arguments as separate entries, e.g. ["-O2", "-o", "fox", "src/fox.c"]
      dir: Working directory inside the project (default: project root), e.g. "build"
      stdin: Project file to feed as the program's input, like < file, e.g. "tests/input/input6"
      stdout: Project file to write the program's output to, like > file, e.g. "build/out6.txt"
    """
    self._calls += 1
    if os.path.basename(binary) not in ("python3", "python"):
      raise ValueError(
        f"{binary} is not installed here; only python3 is available"
      )

    self._used["python"] += 1
    self._trace.append("run")
    cwd = self._resolve(dir or ".")
    feed = self._resolve(stdin) if stdin else None
    code, out = run_python(args or [], cwd, feed)
    self._outputs.append(out)
    if stdout:
      self._write(stdout, out)

    return json.dumps({
      "command":   " ".join(["python3"] + (args or [])),
      "exit_code": code,
      "output":    out,
    })

  # ---- web ----

  def web_search(
    self, query: str, count: int | None = None, topic: str | None = None,
  ) -> str:
    """Search the web for information not in this codebase — library docs, error messages, current facts. Starts a local search service on first use; returns a short list of title/url/snippet results, not full pages.

    Args:
      query: Search query
      count: Number of results to return (default 5, max 10)
      topic: general: web pages and docs (default); code: Stack Overflow and GitHub; science: papers (arXiv, Semantic Scholar, OpenAlex)
    """
    self._calls += 1
    self._trace.append("search")
    key = " ".join(sorted(set(query.lower().split())))
    if key in self._queries:
      self._repeats += 1

    self._queries.add(key)
    n = min(max(count or 5, 1), 10)
    hits = INDEX.search(
      query,
      count   = n,
      hidden  = set(self._task.get("hidden_pids", [])),
      rewrite = self._task.get("rewrite", {}),
      # Search tasks keep the corpus they were built against.
      refs    = self._task["kind"] != "search",
    )
    results = []
    for d in hits:
      self._retrieved.add(d["pid"])
      url = d.get("url") or "https://en.wikipedia.org/wiki/" + d["title"].replace(" ", "_")
      if url not in self._sources:
        self._sources[url] = len(self._sources) + 1

      rid = self._sources[url]
      self._snippets[rid] = d["text"]
      results.append({
        "id":      rid,
        "title":   d["title"],
        "url":     url,
        "snippet": d["text"][:SNIPPET],
      })

    out = { "results": results }
    if results:
      out["cite"] = CITE_NOTE

    return json.dumps(out)

  # ---- notes ----

  def memory_read(self, name: str) -> str:
    """Read a memory listed in the index.

    Args:
      name: Memory name
    """
    self._calls += 1
    m = self._memories.get(name)
    if m is None:
      raise ValueError(f"no memory named {name!r}")

    self._used["memory"].add(name)
    return json.dumps({ "name": name, "content": m["content"] })

  def memory_save(
    self, name: str, description: str, content: str,
    scope: str | None = None,
  ) -> str:
    """Save a durable fact for future sessions: user preferences, project conventions, decisions and their reasons. Not for task progress (use the scratchpad). Overwrites a memory of the same name.

    Args:
      name: Short kebab-case name
      description: One line — shown in the index every session
      content: The fact, plus why it matters and how to apply it
      scope: project (this codebase, default) or user (every project)
    """
    self._calls += 1
    self._saved.append({
      "name":        name,
      "description": description,
      "content":     content,
      "scope":       scope or "project",
    })
    return json.dumps({ "saved": name })

  def use_skill(self, name: str, file: str | None = None) -> str:
    """Load a skill's instructions (see the skill index) before doing a task it covers. With file, read one of its supporting files instead.

    Args:
      name: Skill name
      file: Supporting file path, as listed by a previous use_skill
    """
    self._calls += 1
    s = self._skills.get(name)
    if s is None:
      raise ValueError(f"no skill named {name!r}")

    if file:
      raise ValueError(f"skill {name!r} has no file {file!r}")

    self._used["skill"].add(name)
    return json.dumps({ "name": name, "instructions": s["body"] })

  def scratchpad_write(
    self, name: str, content: str, append: bool = False,
  ) -> str:
    """Write working notes to a scratch file outside the project: plans, findings, long reasoning, checklists. Keep only the file name in mind afterwards and re-read it when needed — this frees context for the work.

    Args:
      name: File name, e.g. plan or findings
      content: Markdown text
      append: Append instead of replacing (default false)
    """
    self._calls += 1
    if len(content.strip()) >= PLAN_MIN:
      self._trace.append("plan")

    prev = self._scratch.get(name, "") if append else ""
    self._scratch[name] = prev + content
    return json.dumps({ "name": name, "bytes": len(self._scratch[name]) })

  def scratchpad_read(
    self, name: str, start_line: int | None = None, end_line: int | None = None,
  ) -> str:
    """Read a scratch file, or a line range of it.

    Args:
      name: File name
      start_line: 1-indexed, inclusive
      end_line: 1-indexed, inclusive
    """
    self._calls += 1
    if name not in self._scratch:
      raise ValueError(f"no scratch file {name!r}")

    lines = self._scratch[name].splitlines()
    lo = max((start_line or 1) - 1, 0)
    return json.dumps({
      "content": "\n".join(lines[lo:end_line or len(lines)]),
    })

  # ---- the user ----

  def ask_user(self, question: str) -> str:
    """Ask the user when you are stuck: the tools can't get you further, a requirement is unclear, or you don't know how to go on. Try the tools first; each question takes the user's time. At most 3 questions per task; each answer tells you more than the last.

    Args:
      question: What you need to know, and what you have tried
    """
    self._calls += 1
    if self._hints >= hints.HINTS:
      raise ValueError(f"no more questions: the user already answered {hints.HINTS}")

    self._hints += 1
    self._trace.append("hint")
    return json.dumps({ "answer": hints.hint(self._task, self._hints, INDEX) })

  # ---- helpers ----

  def _resolve(self, path):
    full = os.path.realpath(os.path.join(self._dir, path))
    if full != self._dir and not full.startswith(self._dir + os.sep):
      raise ValueError(f"{path} is outside the project")

    return full

  def _write(self, path, content):
    full = self._resolve(path)
    os.makedirs(os.path.dirname(full), exist_ok = True)
    with open(full, "w") as f:
      f.write(content)

  def _cleanup(self):
    if self._dir is not None:
      shutil.rmtree(self._dir, ignore_errors = True)
      self._dir = None


# ---- grading ----

_MONTHS = (
  "january|february|march|april|may|june|july|august|september"
  "|october|november|december"
)
_mdy_re = re.compile(r"\b(" + _MONTHS + r")\s+(\d{1,2}),?\s+(\d{4})\b")


def _norm(text):
  text = text.lower().replace("%", " percent ")
  text = _mdy_re.sub(r"\2 \1 \3", text)   # June 10, 1819 = 10 June 1819
  text = re.sub(r"[^\w\s]", " ", text)
  words = [w for w in text.split() if w not in ("a", "an", "the")]
  return " ".join(words)


_tail_re = re.compile(r"\s+(?:\(|\[|—|–|-\s|;|, (?:i\.e\.|which|the|a|an)\b)")


def _matches(pred, golds):
  """Right if the answer, or the answer without a trailing aside
  ("South Central Coast (Duyen hai...) - the region ..."), is a gold
  answer give or take a few words."""
  heads = [pred]
  m = _tail_re.search(pred)
  if m is not None and m.start() > 0:
    heads.append(pred[:m.start()])

  for head in heads:
    p = _norm(head)
    for g in golds:
      n = _norm(g)
      if not n:
        continue

      if p == n:
        return True

      if n in p and len(p.split()) <= len(n.split()) + 4:
        return True

      # North-Western = Northwestern, whatever the hyphen
      if p.replace(" ", "") == n.replace(" ", ""):
        return True

  return False


_SUP     = str.maketrans("⁰¹²³⁴⁵⁶⁷⁸⁹⁻⁺", "0123456789-+")
_PREFIX  = { "": 1, "k": 1e3, "M": 1e6, "G": 1e9, "c": 1e-2, "m": 1e-3,
             "u": 1e-6, "µ": 1e-6, "μ": 1e-6, "n": 1e-9 }
_val_re  = re.compile(
  r"(-?\d[\d,]*(?:\.\d+)?(?:[eE][-+]?\d+)?|-?\.\d+(?:[eE][-+]?\d+)?)"
  r"(?:\s*/\s*(\d+(?:\.\d+)?)(?![\d.]*\s*[a-zA-Z(]))?"   # a fraction, not a rate
  r"\s*(%|percent\b)?"
  r"(?:\s*([kMGcmuµμn]?)(mol|Pa|eV|Hz|g|m|L|l|s|J|W|N|V)(?![A-Za-z]))?"
)
_aside_re = re.compile(r"\s*(?:\(|\[|—|;|\bi\.e\.|\bor\b)")
_parts_re = re.compile(r"\((?:\d|[IVX]+)\)|[①-⑩]|(?:^|\s)\d\)\s")


def _clean(text):
  t = re.sub(r"10([⁻⁺]?[⁰¹²³⁴⁵⁶⁷⁸⁹]+)", lambda m: "10^" + m.group(1).translate(_SUP), text)
  for a, b in (("−", "-"), ("‑", "-"), ("$", ""), ("**", ""), ("\\approx", "≈"),
               ("\\cdot", "×"), ("\\,", ""), ("\\ ", " "), ("\\left", ""), ("\\right", "")):
    t = t.replace(a, b)
  t = re.sub(r"\\text\{([^}]*)\}", r"\1", t)
  t = re.sub(r"\\[dt]?frac\{(-?[\d.]+)\}\{(-?[\d.]+)\}", r"\1/\2", t)
  return re.sub(
    r"(\d)\s*(?:\\times|×|x|\*|·)\s*10\s*\^\s*\{?\s*([-+]?\d+)\s*\}?", r"\1e\2", t,
  )


def _value(m):
  """(value, percent, unit prefix, unit) of a _val_re match."""
  try:
    v = float(m.group(1).replace(",", ""))
    if m.group(2):
      v /= float(m.group(2))

  except (ValueError, ZeroDivisionError):
    return None

  return v, bool(m.group(3)), m.group(4) or "", m.group(5)


def _number(text):
  m = _val_re.search(_clean(text))
  if m is None:
    return None

  c = _value(m)
  if c is None:
    return None

  return c[0] / 100 if c[1] else c[0]


def _stated(text):
  """The values an answer states: its first number, and each one after
  an = or ≈ ("m = 50/21 ≈ 2.38 kg"), up to the first aside."""
  m = _aside_re.search(text)
  head = text[:m.start()] if m is not None and m.start() > 0 else text
  out = []
  for piece in re.split(r"=|≈|~", head)[:4]:
    m = _val_re.search(piece)
    c = _value(m) if m is not None else None
    if c is not None:
      out.append(c)

  return out


def _close(pred, task):
  """The answer states the gold number, within the task's tolerance or
  the gold's own rounding: in its unit up to an SI prefix (5 L = 5000
  mL), as a percent or a fraction (a percent gold may mean either),
  and, for an elasticity, either sign. A multi-part question (1),
  (2)... is right if any part states it: the dataset keeps one part's
  answer. The gold comes from the task's answer text when it has one:
  older builds stored 2.2e15 as 2.2."""
  gold, rel = task["number"], task["tolerance"]
  question = task.get("user", "")
  t = _clean(pred)
  g = _val_re.search(_clean(str(task.get("answer", ""))))
  gprefix, gunit = (g.group(4) or "", g.group(5)) if g is not None else ("", None)
  gpct = g is not None and bool(g.group(3))
  c = _value(g) if g is not None else None
  if c is not None:
    gold = c[0] / 100 if c[1] else c[0]

  pieces = [t]
  if len(_parts_re.findall(question)) >= 2:
    pieces += [p for p in re.split(_parts_re.pattern + r"|;", t) if p and p.strip()]

  places = 0
  s = repr(float(gold))
  if "e" not in s and not float(gold).is_integer():
    places = len(s.split(".")[1])

  first = _val_re.search(t)   # the old rule: the answer's first number
  firsts = [_value(first)] if first is not None else []
  for piece in pieces:
    for v, pct, prefix, unit in [c for c in _stated(piece) + firsts if c is not None]:
      if unit is not None and unit == gunit:
        v *= _PREFIX[prefix] / _PREFIX[gprefix]

      alts = { v / 100 if pct else v }
      if pct or gpct:
        alts |= { v, v / 100, v * 100 }
      if "elastic" in question.lower():
        alts |= { -x for x in alts }

      for x in alts:
        d = abs(x - gold)
        if d <= rel * max(abs(gold), 1e-9) or d < 1e-9:
          return True

        if 0 < places <= 3 and d <= 0.5 * 10 ** -places + 1e-12:
          return True

  return False


def _check_rules(body, rules):
  lines = [
    l for l in body.strip().splitlines()
    if not l.startswith(("CONFIDENCE:", "ANSWER:"))
  ]
  text = "\n".join(lines).strip()
  first = text.splitlines()[0] if text else ""
  for r in rules:
    kind = r["type"]
    if kind == "first_line" and re.fullmatch(r["regex"], first.strip()) is None:
      return False

    if kind == "bullets":
      items = [l for l in lines if l.lstrip().startswith(("-", "*", "+", "•"))]
      if not items or any(not l.lstrip().startswith(r["prefix"]) for l in items):
        return False

    if kind == "contains" and r["text"] not in text:
      return False

    if kind == "absent" and r["text"].lower() in text.lower():
      return False

    if kind == "max_lines" and len([l for l in lines if l.strip()]) > r["n"]:
      return False

  return True


def _support(env, task):
  """Share of the paragraphs the answer rests on that the episode
  retrieved (visible ones only, for unanswerable tasks)."""
  hidden = set(task.get("hidden_pids", []))
  need = { p for p in task.get("support", []) if p not in hidden }
  return len(need & env._retrieved) / len(need) if need else 0.0


def _printed(answer, outputs):
  """The answer is a number one of the scripts printed."""
  v = _number(answer or "")
  if v is None:
    return False

  for out in outputs:
    for m in _num_re.findall(out.replace(",", "")):
      try:
        x = float(m)

      except ValueError:
        continue

      if abs(x - v) <= OUT_REL * max(abs(v), 1e-9):
        return True

  return False


def _process(env, kind, f):
  """The workflow armin wants. Math and science: (look things up,)
  write a plan to the scratchpad, then run a script whose output is
  the answer. Search: search, keep notes in the scratchpad, answer.
  Code: run the tests after the last change; for a planted bug, also
  before the first (reproduce, fix, verify). None for families
  without one."""
  t = env._trace
  if kind == "code":
    if "edit" not in t:
      return False

    last = len(t) - 1 - t[::-1].index("edit")
    verified = "test" in t[last + 1:]
    if env._task.get("bugs"):
      return verified and "test" in t[:t.index("edit")]

    return verified

  if kind in ("math", "science"):
    if "run" not in t or "plan" not in t[:t.index("run")]:
      return False

    return f["abstain"] is not None or _printed(f["answer"], env._outputs)

  if kind == "search":
    return "search" in t and "plan" in t

  return None


def grade(env, final_text):
  task = env._task
  f = final_parse(final_text)
  kind = task["kind"]
  g = {
    "format":       f["conf"] is not None,
    "conf":         f["conf"],
    "abstained":    f["abstain"] is not None,
    "correct":      False,
    "hallucinated": False,
    "grounded":     True,
    "calls":        env._calls,
    "searches":     env._trace.count("search"),
    "repeats":      env._repeats,
    "process":      _process(env, kind, f),
    "hints":        env._hints,
  }
  if kind == "search":
    g["support"] = _support(env, task)

  answered = f["answer"] is not None
  if kind in ("search", "math", "science", "memory") and not (
    answered or g["abstained"]
  ):
    g["format"] = False

  if not g["format"]:
    return g

  if g["abstained"]:
    g["correct"] = not task["solvable"]
    return g

  if not task["solvable"]:
    g["hallucinated"] = True
    if kind == "code" and task["variant"] == "contradict":
      g["hallucinated"] = False

    return g

  if kind == "code":
    return _grade_code(env, task, g)

  if kind == "search":
    g["correct"] = _matches(f["answer"], task["answers"])
    real = task.get("real_answers")
    if real and not g["correct"] and _matches(f["answer"], real):
      g["hallucinated"] = True

    golds = [_norm(a) for a in task["answers"] if _norm(a)]
    g["grounded"] = any(
      a in _norm(env._snippets.get(c, "")) for c in f["cites"] for a in golds
    )
    return g

  if kind in ("math", "science"):
    g["correct"] = _close(f["answer"], task)
    g["grounded"] = env._used["python"] > 0
    return g

  if kind == "memory":
    if "number" in task:
      g["correct"] = _close(f["answer"], task)
      g["grounded"] = (
        task["memory"] in env._used["memory"] and env._used["python"] > 0
      )

    else:
      g["correct"] = _matches(f["answer"], task["answers"])
      g["grounded"] = task["memory"] in env._used["memory"]

    return g

  if kind == "skill":
    g["correct"] = _check_rules(f["body"], task["rules"])
    g["grounded"] = task["skill"] in env._used["skill"]
    return g

  if kind == "save":
    key = task["key"].lower()
    hit = any(
      key in (s["content"] + " " + s["description"]).lower()
      for s in env._saved
    )
    g["correct"] = hit if task["should_save"] else not env._saved
    return g

  raise ValueError("unknown task kind " + kind)


def _grade_code(env, task, g):
  work = tempfile.mkdtemp(prefix = "grade-")
  try:
    sol = os.path.join(env._dir, "solution.py")
    if not os.path.exists(sol):
      return g

    shutil.copy(sol, os.path.join(work, "solution.py"))
    with open(os.path.join(work, "test_solution.py"), "w") as f:
      f.write(task["hidden"])

    code, out = run_pytest(work, ["-x", "test_solution.py"])

  finally:
    shutil.rmtree(work, ignore_errors = True)

  g["correct"] = code == 0
  g["hallucinated"] = code != 0 and _invented_re.search(out) is not None
  g["grounded"] = env._used["tests"] > 0 or env._used["python"] > 0
  return g
