"""Offline stand-in for armin's web_search: BM25 over the paragraphs
in data/corpus.jsonl. Each task can hide paragraphs (unanswerable
questions) or rewrite them (counterfactual ones) without touching
the shared index. Standard library only."""

import math
import re
from collections import defaultdict

K1 = 1.2
B  = 0.75

_word_re = re.compile(r"\w+")
_STOP = frozenset("""
a an and are as at be by for from has have in is it its of on or
that the this to was were what when where which who whom whose why
with did does do how many much
""".split())


def tokens(text):
  return [
    w for w in _word_re.findall(text.lower()) if w not in _STOP
  ]


class Index:
  def __init__(self, docs):
    self.docs = docs
    self.postings = defaultdict(list)
    self.lengths = []
    for i, d in enumerate(docs):
      counts = defaultdict(int)
      words = tokens(d["title"] + " " + d["text"])
      for w in words:
        counts[w] += 1

      for w, c in counts.items():
        self.postings[w].append((i, c))

      self.lengths.append(len(words))

    n = len(docs)
    self.avg = sum(self.lengths) / max(n, 1)
    self.idf = {
      w: math.log(1 + (n - len(p) + 0.5) / (len(p) + 0.5))
      for w, p in self.postings.items()
    }
    self.by_pid = { d["pid"]: i for i, d in enumerate(docs) }

  def search(self, query, count = 5, hidden = (), rewrite = None, refs = True):
    """refs = False leaves out the science reference library."""
    scores = defaultdict(float)
    for w in set(tokens(query)):
      idf = self.idf.get(w)
      if idf is None:
        continue

      for i, c in self.postings[w]:
        norm = 1 - B + B * self.lengths[i] / self.avg
        scores[i] += idf * c * (K1 + 1) / (c + K1 * norm)

    rewrite = rewrite or {}
    ranked = sorted(scores.items(), key = lambda kv: -kv[1])
    out = []
    for i, _ in ranked:
      d = self.docs[i]
      if d["pid"] in hidden or (not refs and d["pid"].startswith("ref-")):
        continue

      out.append(dict(d, text = rewrite.get(d["pid"], d["text"])))
      if len(out) == count:
        break

    return out
