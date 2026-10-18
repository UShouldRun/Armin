"""Multi-hop search questions from MuSiQue-Ans (dev split). Every
paragraph of every question goes into one shared corpus, so the
right ones have to be found among the distractors. Variants that
make recall useless:

  counterfactual  the answer is changed everywhere in the corpus;
                  answering from memory gives the real-world value
  unanswerable    every paragraph holding the answer is hidden; the
                  only right reply is CANNOT_SOLVE

Each task keeps MuSiQue's decomposition as "hops" (sub-question,
answer, supporting pid; "#1" in a later hop stands for the first
answer), so build_sft can script one search per hop."""

import json
import os
import re

import fetch
from common import DATA, write_jsonl

URL = (
  "https://huggingface.co/datasets/dgslibisey/MuSiQue/resolve/main/"
  "musique_ans_v1.0_dev.jsonl"
)
MAX_TOUCHED = 40

_year_re = re.compile(r"^\d{3,4}$")
_num_re  = re.compile(r"^-?\d[\d,]*(\.\d+)?$")

_corpus = []
_lower = []
_answers = []


def _pattern(text):
  return re.compile(r"(?<!\w)" + re.escape(text) + r"(?!\w)", re.I)


def holding(text):
  rx = _pattern(text)
  return [
    _corpus[i]["pid"] for i, low in enumerate(_lower)
    if text.lower() in low and rx.search(_corpus[i]["text"])
  ]


def build(rng):
  path = fetch.file(URL, "musique_dev.jsonl")
  with open(path) as f:
    rows = [json.loads(l) for l in f if l.strip()]

  seen, titles = {}, {}
  tasks = []
  for row in rows:
    support = []
    for p in row["paragraphs"]:
      key = (p["title"], p["paragraph_text"])
      if key not in seen:
        n = titles.get(p["title"], 0)
        titles[p["title"]] = n + 1
        pid = f"p{len(_corpus)}"
        seen[key] = pid
        _corpus.append({
          "pid":   pid,
          "title": p["title"] if n == 0 else f"{p['title']} ({n + 1})",
          "text":  p["paragraph_text"],
        })

      if p.get("is_supporting"):
        support.append(seen[key])

    _answers.append(row["answer"])
    hops = []
    for h in row.get("question_decomposition") or []:
      i = h.get("paragraph_support_idx")
      p = row["paragraphs"][i] if i is not None else None
      hops.append({
        "question": h["question"],
        "answer":   h["answer"],
        "pid":      seen[(p["title"], p["paragraph_text"])] if p else None,
      })

    tasks.append({
      "id":       "search-" + row["id"],
      "kind":     "search",
      "variant":  "normal",
      "solvable": True,
      "user":     row["question"].strip(),
      "answers":  [row["answer"]] + list(row.get("answer_aliases") or []),
      "support":  support,
      "hops":     hops,
    })

  _lower.extend(d["text"].lower() for d in _corpus)
  write_jsonl(os.path.join(DATA, "corpus.jsonl"), _corpus)
  print(f"search: {len(tasks)} questions, {len(_corpus)} paragraphs")
  rng.shuffle(tasks)

  return tasks


def _replacement(answer, rng):
  if _year_re.match(answer):
    return str(int(answer) + rng.choice([-11, -7, -4, 3, 6, 13]))

  if _num_re.match(answer):
    v = float(answer.replace(",", ""))
    out = v * rng.choice([0.5, 1.5, 2, 3]) + rng.choice([1, 2, 7])
    return str(int(out)) if v.is_integer() else f"{out:.2f}"

  words = len(answer.split())
  pool = [
    a for a in _answers
    if a != answer
    and abs(len(a.split()) - words) <= 1
    and a[:1].isupper() == answer[:1].isupper()
    and not _num_re.match(a)
  ]
  return rng.choice(pool) if pool else None


def variant(task, rng):
  answer = task["answers"][0]
  touched = sorted({ p for a in task["answers"] for p in holding(a) })
  if not touched or len(touched) > MAX_TOUCHED:
    return None

  if rng.random() < 0.5:
    hidden = sorted(set(touched) | set(task["support"][-1:]))
    return dict(
      task,
      id          = task["id"] + "-unans",
      variant     = "unanswerable",
      solvable    = False,
      hidden_pids = hidden,
      why         = "no search result holds the answer to this question",
    )

  # The replacement may exist elsewhere in the corpus, but not in
  # the paragraphs this question depends on.
  by_pid = { d["pid"]: d["text"] for d in _corpus }
  near = [by_pid[p].lower() for p in set(touched) | set(task["support"])]
  other = None
  for _ in range(10):
    cand = _replacement(answer, rng)
    if cand is not None and not any(cand.lower() in t for t in near):
      other = cand
      break

  if other is None:
    return None

  rewrite = {}
  for pid in touched:
    text = by_pid[pid]
    for a in task["answers"]:
      text = _pattern(a).sub(other, text)

    rewrite[pid] = text

  return dict(
    task,
    id           = task["id"] + "-cf",
    variant      = "counterfactual",
    answers      = [other],
    real_answers = task["answers"],
    hops         = task["hops"][:-1] + [dict(task["hops"][-1], answer = other)],
    rewrite      = rewrite,
  )
