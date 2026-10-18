"""Synthetic memory, skill and memory_save tasks. Every value is
random per task, so nothing here can be answered from training
data: the model has to read the memory or load the skill.

  memory  recall a value, compute with one (memory_read then
          run_program), or notice the memory does not exist
  skill   follow a skill's randomized formatting rules
  save    save a durable preference with memory_save, and not save
          a one-off instruction"""

import math
import re
import string

WORDS = [
  "amber", "basalt", "cobalt", "dune", "ember", "fjord", "garnet",
  "harbor", "indigo", "juniper", "kestrel", "lumen", "marrow",
  "nimbus", "onyx", "pylon", "quartz", "raven", "sable", "tundra",
  "umber", "vesper", "willow", "yarrow", "zephyr",
]
NAMES = [
  "Ana Duarte", "Bilal Haddad", "Chen Wei", "Dara Okafor",
  "Eitan Levi", "Freya Lund", "Goran Petrovic", "Hana Sato",
  "Iris Moreau", "Joao Pires", "Kofi Mensah", "Lena Vogel",
]
REGIONS = [
  "eu-west-3", "eu-central-2", "us-east-2", "ap-southeast-1",
  "sa-east-1", "ca-central-1", "eu-north-1", "ap-northeast-3",
]
TOOLS = ["ruff", "black", "yapf", "autopep8", "clang-format", "gofumpt"]


def _token(rng, n = 6):
  return "".join(rng.choice(string.ascii_lowercase + string.digits) for _ in range(n))


def _facts(rng):
  """name -> (description, content, value, question)."""
  port = rng.randint(20000, 64000)
  url = f"https://staging-{_token(rng, 5)}.{rng.choice(WORDS)}.internal:{rng.randint(8000, 9999)}/v{rng.randint(2, 5)}"
  region = rng.choice(REGIONS)
  person = rng.choice(NAMES)
  codename = rng.choice(WORDS).capitalize() + " " + rng.choice(WORDS).capitalize()
  user = f"qa-{_token(rng, 4)}@{rng.choice(WORDS)}.test"
  return {
    "db-port": (
      "Port of the local Postgres",
      f"Local Postgres listens on port {port}, not the default 5432, because the CI runner already uses it.",
      str(port),
      "Which port should I point psql at for the local database?",
    ),
    "staging-api-url": (
      "Base URL of the staging API",
      f"The staging API lives at {url}. Integration tests use it; never production.",
      url,
      "What's the base URL for the staging API?",
    ),
    "deploy-region": (
      "Cloud region deploys go to",
      f"All deploys go to {region}; data must stay in that region for compliance.",
      region,
      "Which region do we deploy to?",
    ),
    "on-call": (
      "Who is on call this quarter",
      f"{person} is on call this quarter; page them for production incidents.",
      person,
      "Who do I page if production breaks?",
    ),
    "release-codename": (
      "Codename of the next release",
      f"The next release is codenamed {codename}.",
      codename,
      "What's the codename of the next release?",
    ),
    "qa-login": (
      "Login used for QA",
      f"QA logs in as {user}; the password is in the team vault.",
      user,
      "Which account does QA log in with?",
    ),
  }


def _numeric(rng):
  """name -> (description, content, question, value)."""
  rate = round(rng.uniform(38, 140), 2)
  hours = round(rng.uniform(12, 160) * 4) / 4
  vat = rng.choice([0.06, 0.13, 0.19, 0.21, 0.23])
  batch = rng.randint(700, 9000)
  rows = rng.randint(10 ** 5, 10 ** 7)
  quota = rng.randint(200, 5000)
  per_day = rng.randint(3, 40)
  return {
    "contractor-rate": (
      "Hourly rate contractors bill",
      f"Contractors bill {rate} EUR per hour, plus {int(vat * 100)}% VAT on the total.",
      f"A contractor logged {hours} hours this month. What's the invoice total in EUR, VAT included? Round to cents.",
      round(rate * hours * (1 + vat), 2),
    ),
    "importer-batch-limit": (
      "Row limit per importer batch",
      f"The importer accepts at most {batch} rows per batch; larger batches are rejected.",
      f"How many batches does it take to import {rows:,} rows?",
      math.ceil(rows / batch),
    ),
    "api-quota": (
      "Daily API quota of the partner account",
      f"The partner account may make {quota} API calls per day.",
      f"If each sync makes {per_day} calls, how many full syncs fit in one day's quota?",
      quota // per_day,
    ),
  }


def memory_task(rng, i):
  facts = _facts(rng)
  nums = _numeric(rng)
  pool = list(facts.items()) + [(k, v[:2] + (None, None)) for k, v in nums.items()]
  roll = rng.random()
  if roll < 0.5:
    name, (desc, content, value, question) = rng.choice(list(facts.items()))
    task = {
      "variant": "recall", "solvable": True, "memory": name,
      "user": question, "answers": [value],
    }
    chosen = [(name, desc, content)]

  elif roll < 0.8:
    name, (desc, content, question, value) = rng.choice(list(nums.items()))
    task = {
      "variant": "compute", "solvable": True, "memory": name,
      "user": question, "number": float(value), "tolerance": 1e-4,
    }
    chosen = [(name, desc, content)]

  else:
    name, (desc, _, _, question) = rng.choice(list(facts.items()))
    task = {
      "variant": "absent", "solvable": False, "memory": name,
      "user": question,
      "why": f"no memory records {desc[0].lower() + desc[1:]}",
    }
    chosen = []

  others = [(k, v[0], v[1]) for k, v in pool if k != name]
  rng.shuffle(others)
  chosen += others[:rng.randint(2, 4)]
  rng.shuffle(chosen)
  task.update({
    "id":       f"memory-{i}",
    "kind":     "memory",
    "memories": [
      { "name": n, "scope": rng.choice(["project", "user"]), "description": d, "content": c }
      for n, d, c in chosen
    ],
  })

  return task


def _changes(rng):
  ticket = f"{rng.choice(['PRJ', 'OPS', 'CORE'])}-{rng.randint(100, 999)}"
  pool = [
    f"Fix crash when the config file is empty ({ticket})",
    "Add --dry-run to the importer",
    f"Speed up search indexing by about {rng.randint(15, 60)}%",
    "Drop support for Python 3.9",
    f"Retry uploads on HTTP 503 ({ticket})",
    "Document the new environment variables",
    "Make the progress bar respect NO_COLOR",
  ]
  rng.shuffle(pool)
  return pool[:rng.randint(3, 5)], ticket


def skill_task(rng, i):
  major, minor, patch = rng.randint(1, 9), rng.randint(0, 20), rng.randint(0, 9)
  version = f"{major}.{minor}.{patch}"
  codename = rng.choice(WORDS).capitalize()
  prefix = rng.choice(["- [x] ", "* ", "- ", "+ "])
  limit = rng.randint(6, 9)
  changes, ticket = _changes(rng)
  body = (
    "Release notes format:\n"
    f"1. The first line is exactly: Release {version} \"{codename}\"\n"
    f"2. Every change is one line starting with `{prefix.strip() or prefix}` "
    f"followed by a space and the change in the past tense.\n"
    f"3. Never mention ticket ids such as {ticket[:ticket.index('-')]}-123.\n"
    f"4. At most {limit} lines in total.\n"
  )
  bullet = prefix.rstrip() + " "
  skills = [
    { "name": "release-notes", "description": "How to write release notes", "body": body },
    { "name": "commit-message", "description": "Commit message format", "body": "Subject: vX.Y.Z - lowercase summary." },
    { "name": "code-review", "description": "Checklist for reviewing a diff", "body": "Check tests, naming, error paths." },
  ]
  rng.shuffle(skills)
  return {
    "id":       f"skill-{i}",
    "kind":     "skill",
    "variant":  "format",
    "solvable": True,
    "skill":    "release-notes",
    "skills":   skills,
    "user": (
      f"Write the release notes for version {version} "
      f"(codename {codename}). Changes:\n"
      + "\n".join("- " + c for c in changes)
      + "\nReply with just the notes, then the CONFIDENCE line."
    ),
    "rules": [
      { "type": "first_line", "regex": rf"Release {re.escape(version)} \"{codename}\"" },
      { "type": "bullets", "prefix": bullet },
      { "type": "absent", "text": ticket },
      { "type": "max_lines", "n": limit },
    ],
  }


def save_task(rng, i):
  tool = rng.choice(TOOLS)
  a, b = rng.randint(100, 999), rng.randint(100, 999)
  if rng.random() < 0.6:
    user = (
      f"From now on, format Python in this project with {tool} — the "
      f"team agreed on it last week. Quick one meanwhile: what's "
      f"{a} * {b}?"
    )
    should = True

  else:
    user = (
      f"Just for this one-off script, skip type hints; it gets "
      f"deleted tomorrow. Quick one meanwhile: what's {a} * {b}?"
    )
    should = False

  return {
    "id":          f"save-{i}",
    "kind":        "save",
    "variant":     "should-save" if should else "should-not",
    "solvable":    True,
    "user":        user,
    "should_save": should,
    "key":         tool,
  }


def build(rng, n_memory, n_skill, n_save):
  return (
    [memory_task(rng, i) for i in range(n_memory)],
    [skill_task(rng, i) for i in range(n_skill)],
    [save_task(rng, i) for i in range(n_save)],
  )
