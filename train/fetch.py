"""Downloads with the standard library only: whole files, and
Hugging Face datasets-server pages (resumable, throttled, retried
on rate limits)."""

import json
import os
import sys
import time
import urllib.parse
import urllib.request

from common import DATA, read_jsonl

SERVER = "https://datasets-server.huggingface.co"
PAGE   = 100
PAUSE  = 2


def get(url, attempts = 10):
  for attempt in range(attempts):
    try:
      # Some hosts (NIST) refuse urllib's default User-Agent.
      req = urllib.request.Request(url, headers = { "User-Agent": "armin-train/1" })
      with urllib.request.urlopen(req, timeout = 120) as r:
        return r.read()

    except OSError as e:
      wait = min(2 ** attempt, 120)
      print(f"\n  {e}; retrying in {wait}s", file = sys.stderr)
      time.sleep(wait)

  raise RuntimeError("giving up on " + url)


def file(url, name):
  path = os.path.join(DATA, "raw", name)
  if not os.path.exists(path):
    os.makedirs(os.path.dirname(path), exist_ok = True)
    print("downloading", url)
    data = get(url)
    with open(path + ".tmp", "wb") as f:
      f.write(data)

    os.replace(path + ".tmp", path)

  return path


def rows(dataset, name, split = "train", where = None, limit = None):
  """All rows (or the first `limit`) of a split, cached in
  data/raw/<name>.jsonl. A partial file resumes the download."""
  path = os.path.join(DATA, "raw", name + ".jsonl")
  if os.path.exists(path):
    return read_jsonl(path)

  partial = path + ".partial"
  got = read_jsonl(partial) if os.path.exists(partial) else []
  os.makedirs(os.path.dirname(path), exist_ok = True)
  total = None
  with open(partial, "a") as out:
    while total is None or len(got) < total:
      q = {
        "dataset": dataset,
        "config":  "default",
        "split":   split,
        "offset":  len(got),
        "length":  PAGE,
      }
      endpoint = "/rows"
      if where:
        q["where"] = where
        endpoint = "/filter"

      page = json.loads(get(SERVER + endpoint + "?" + urllib.parse.urlencode(q)))
      total = page["num_rows_total"]
      if limit:
        total = min(total, limit)

      if not page["rows"]:
        break

      for r in page["rows"]:
        out.write(json.dumps(r["row"]) + "\n")
        got.append(r["row"])

      out.flush()
      print(f"\r{name}: {len(got)}/{total}", end = "", flush = True)
      time.sleep(PAUSE)

  print()
  os.replace(partial, path)

  return got
