"""The laptop's practical test: copilot work in small repos, checked by
running code, not by matching a trained answer format. Written by hand,
apart from the training generators (data_*.py), so none of it was seen
in training.

Each task: files the model starts with, a prompt, then a check. Before
the check, files in `restore` are put back (the model must fix the code,
not the test) and `hidden` files are added (tests it never saw). The
check is a shell command run in the task's folder (exit 0 = pass)
and/or regexes the final answer must match.
"""

GO_MOD = "module t\n\ngo 1.22\n"

# --- a small Go service, for the repo questions ------------------------

SHOP = {
  "go.mod": "module shopcart\n\ngo 1.22\n",
  "main.go": """package main

import (
  "log"
  "net/http"
)

func main() {
  cfg := loadConfig("shop.conf")
  http.HandleFunc("/checkout", handleCheckout)
  log.Fatal(http.ListenAndServe(cfg.addr(), nil))
}
""",
  "config.go": """package main

import (
  "bufio"
  "fmt"
  "os"
  "strings"
)

type config struct {
  host string
  port int
}

const defaultPort = 8317

func (c config) addr() string { return fmt.Sprintf("%s:%d", c.host, c.port) }

func loadConfig(path string) config {
  c := config{ host: "127.0.0.1", port: defaultPort }
  f, err := os.Open(path)
  if err != nil {
    return c
  }
  defer f.Close()
  sc := bufio.NewScanner(f)
  for sc.Scan() {
    k, v, ok := parseLine(sc.Text())
    if !ok {
      continue
    }
    switch k {
    case "host":
      c.host = v
    case "port":
      fmt.Sscan(v, &c.port)
    }
  }
  return c
}

// parseLine splits "key = value"; blank lines and # comments give ok=false.
func parseLine(line string) (string, string, bool) {
  line = strings.TrimSpace(line)
  if line == "" || strings.HasPrefix(line, "#") {
    return "", "", false
  }
  k, v, ok := strings.Cut(line, "=")
  return strings.TrimSpace(k), strings.TrimSpace(v), ok
}
""",
  "cart.go": """package main

import "errors"

var ErrCartFull = errors.New("cart full")

const maxLines = 37

type line struct {
  sku   string
  qty   int
  cents int
}

type cart struct {
  lines []line
  code  string
}

func (c *cart) add(sku string, qty, cents int) error {
  if len(c.lines) >= maxLines {
    return ErrCartFull
  }
  c.lines = append(c.lines, line{ sku, qty, cents })
  return nil
}

func (c *cart) subtotal() int {
  t := 0
  for _, l := range c.lines {
    t += l.qty * l.cents
  }
  return t
}
""",
  "pricing.go": """package main

import "strings"

// promo codes, percent off
var promos = map[string]int{ "WINTER9": 9, "LOYAL15": 15 }

func reducePrice(total int, code string) int {
  pct, ok := promos[strings.ToUpper(code)]
  if !ok {
    return total
  }
  return total - total*pct/100
}

func shipping(total int) int {
  if total >= 6000 {
    return 0
  }
  return 499
}
""",
  "orders.go": """package main

import (
  "encoding/json"
  "net/http"
  "os"
  "time"
)

type order struct {
  Total int       `json:"total"`
  At    time.Time `json:"at"`
}

func persistOrder(o order) error {
  b, _ := json.Marshal(o)
  f, err := os.OpenFile("orders.jsonl", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
  if err != nil {
    return err
  }
  defer f.Close()
  _, err = f.Write(append(b, '\\n'))
  return err
}

func handleCheckout(w http.ResponseWriter, r *http.Request) {
  var c cart
  if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
    http.Error(w, err.Error(), 400)
    return
  }
  total := reducePrice(c.subtotal(), c.code)
  total += shipping(total)
  if err := persistOrder(order{ total, time.Now() }); err != nil {
    retryPersist(order{ total, time.Now() })
  }
  json.NewEncoder(w).Encode(map[string]int{ "total": total })
}

func retryPersist(o order) {
  for i := 0; i < 3; i++ {
    if persistOrder(o) == nil {
      return
    }
    time.Sleep(time.Second)
  }
}
""",
}

TASKS = [
  # ---- bug fixes with a failing test (the test is restored) ----------
  { "id": "go-chunk", "cat": "fix",
    "prompt": "`go test ./...` fails. Find and fix the bug (don't change the test).",
    "files": { "go.mod": GO_MOD,
      "chunk.go": """package t

// Chunk splits xs into slices of at most n elements.
func Chunk(xs []int, n int) [][]int {
  var out [][]int
  for i := 0; i+n <= len(xs); i += n {
    out = append(out, xs[i:i+n])
  }
  return out
}
""",
      "chunk_test.go": """package t

import (
  "reflect"
  "testing"
)

func TestChunk(t *testing.T) {
  got := Chunk([]int{ 1, 2, 3, 4, 5 }, 2)
  want := [][]int{ { 1, 2 }, { 3, 4 }, { 5 } }
  if !reflect.DeepEqual(got, want) {
    t.Fatalf("got %v, want %v", got, want)
  }
  if got := Chunk(nil, 3); len(got) != 0 {
    t.Fatalf("empty: got %v", got)
  }
}
""" },
    "restore": [ "chunk_test.go" ], "check": "go test ./..." },

  { "id": "go-reverse", "cat": "fix",
    "prompt": "Reverse breaks on non-ASCII text, the test shows it. Fix it.",
    "files": { "go.mod": GO_MOD,
      "rev.go": """package t

func Reverse(s string) string {
  b := []byte(s)
  for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
    b[i], b[j] = b[j], b[i]
  }
  return string(b)
}
""",
      "rev_test.go": """package t

import "testing"

func TestReverse(t *testing.T) {
  for in, want := range map[string]string{ "abc": "cba", "héllo, 世界": "界世 ,olléh", "": "" } {
    if got := Reverse(in); got != want {
      t.Errorf("Reverse(%q) = %q, want %q", in, got, want)
    }
  }
}
""" },
    "restore": [ "rev_test.go" ], "check": "go test ./..." },

  { "id": "go-errwrap", "cat": "fix",
    "prompt": "Callers can't detect a missing user with errors.Is. Fix it so `go test` passes.",
    "files": { "go.mod": GO_MOD,
      "users.go": """package t

import (
  "errors"
  "fmt"
)

var ErrNoUser = errors.New("no such user")

var users = map[int]string{ 1: "ana", 2: "rui" }

func Name(id int) (string, error) {
  n, ok := users[id]
  if !ok {
    return "", fmt.Errorf("lookup %d: %v", id, ErrNoUser)
  }
  return n, nil
}
""",
      "users_test.go": """package t

import (
  "errors"
  "testing"
)

func TestName(t *testing.T) {
  if _, err := Name(9); !errors.Is(err, ErrNoUser) {
    t.Fatalf("errors.Is(%v, ErrNoUser) = false", err)
  }
  if n, err := Name(2); err != nil || n != "rui" {
    t.Fatalf("Name(2) = %q, %v", n, err)
  }
}
""" },
    "restore": [ "users_test.go" ], "check": "go test ./..." },

  { "id": "go-nilmap", "cat": "fix",
    "prompt": "The program panics when tagging. Fix the bug.",
    "files": { "go.mod": GO_MOD,
      "tags.go": """package t

type Index struct {
  byTag map[string][]string
}

func NewIndex() *Index { return &Index{} }

func (x *Index) Tag(doc, tag string) { x.byTag[tag] = append(x.byTag[tag], doc) }

func (x *Index) Docs(tag string) []string { return x.byTag[tag] }
""",
      "tags_test.go": """package t

import "testing"

func TestTag(t *testing.T) {
  x := NewIndex()
  x.Tag("a.md", "go")
  x.Tag("b.md", "go")
  if d := x.Docs("go"); len(d) != 2 {
    t.Fatalf("got %v", d)
  }
}
""" },
    "restore": [ "tags_test.go" ], "check": "go test ./..." },

  { "id": "go-kv", "cat": "fix",
    "prompt": "ParseEnv loses part of values that contain '='. Fix it; `go test` shows the failure.",
    "files": { "go.mod": GO_MOD,
      "env.go": """package t

import "strings"

// ParseEnv reads KEY=VALUE lines.
func ParseEnv(s string) map[string]string {
  m := map[string]string{}
  for _, l := range strings.Split(s, "\\n") {
    p := strings.Split(l, "=")
    if len(p) < 2 {
      continue
    }
    m[strings.TrimSpace(p[0])] = strings.TrimSpace(p[1])
  }
  return m
}
""",
      "env_test.go": """package t

import "testing"

func TestParseEnv(t *testing.T) {
  m := ParseEnv("A=1\\nDSN=user=bob;pass=x=y\\n\\nB = two ")
  if m["A"] != "1" || m["B"] != "two" || m["DSN"] != "user=bob;pass=x=y" {
    t.Fatalf("got %q", m)
  }
}
""" },
    "restore": [ "env_test.go" ], "check": "go test ./..." },

  { "id": "c-words", "cat": "fix",
    "prompt": "`make test` fails: count_words is wrong for some inputs. Fix it.",
    "files": {
      "Makefile": "test:\n\tcc -Wall -o t words.c test.c && ./t\n",
      "words.h": "#include <stddef.h>\nsize_t count_words(const char *s);\n",
      "words.c": """#include <ctype.h>
#include "words.h"

size_t count_words(const char *s) {
  size_t n = 0;
  for (; *s; s++)
    if (isspace((unsigned char)*s))
      n++;
  return n + 1;
}
""",
      "test.c": """#include <assert.h>
#include <stdio.h>
#include "words.h"

int main(void) {
  assert(count_words("one two three") == 3);
  assert(count_words("  lead and  double  ") == 3);
  assert(count_words("") == 0);
  assert(count_words("\\tx\\n") == 1);
  puts("ok");
  return 0;
}
""" },
    "restore": [ "test.c", "Makefile" ], "check": "make -s test" },

  { "id": "c-ring", "cat": "fix",
    "prompt": "The ring buffer test fails after it wraps around. Fix the bug in ring.c.",
    "files": {
      "Makefile": "test:\n\tcc -Wall -o t ring.c test.c && ./t\n",
      "ring.h": """#define RING_CAP 4
typedef struct { int buf[RING_CAP]; int head, tail, len; } ring;
int ring_push(ring *r, int v);
int ring_pop(ring *r, int *v);
""",
      "ring.c": """#include "ring.h"

int ring_push(ring *r, int v) {
  if (r->len == RING_CAP)
    return 0;
  r->buf[r->tail] = v;
  r->tail = r->tail + 1;
  if (r->tail > RING_CAP)
    r->tail = 0;
  r->len++;
  return 1;
}

int ring_pop(ring *r, int *v) {
  if (r->len == 0)
    return 0;
  *v = r->buf[r->head];
  r->head = (r->head + 1) % RING_CAP;
  r->len--;
  return 1;
}
""",
      "test.c": """#include <assert.h>
#include <stdio.h>
#include "ring.h"

int main(void) {
  ring r = {0};
  int v;
  for (int round = 0; round < 3; round++) {
    for (int i = 0; i < 3; i++) assert(ring_push(&r, round * 10 + i));
    for (int i = 0; i < 3; i++) { assert(ring_pop(&r, &v)); assert(v == round * 10 + i); }
  }
  for (int i = 0; i < 4; i++) assert(ring_push(&r, i));
  assert(!ring_push(&r, 9));
  puts("ok");
  return 0;
}
""" },
    "restore": [ "test.c", "Makefile", "ring.h" ], "check": "make -s test" },

  { "id": "py-default", "cat": "fix",
    "prompt": "Tags from one call leak into the next. Fix it so `python3 test_notes.py` passes.",
    "files": {
      "notes.py": """def make_note(text, tags=[]):
    tags.append("note")
    return {"text": text, "tags": tags}
""",
      "test_notes.py": """from notes import make_note

a = make_note("a")
b = make_note("b")
assert b["tags"] == ["note"], b
c = make_note("c", ["x"])
assert c["tags"] == ["x", "note"], c
print("ok")
""" },
    "restore": [ "test_notes.py" ], "check": "python3 test_notes.py" },

  { "id": "py-weekdays", "cat": "fix",
    "prompt": "business_days counts some weekend days. Fix it (test: `python3 test_days.py`).",
    "files": {
      "days.py": """from datetime import date, timedelta


def business_days(start: date, end: date) -> int:
    \"\"\"Weekdays in [start, end).\"\"\"
    n = 0
    d = start
    while d < end:
        if d.weekday() < 6:
            n += 1
        d += timedelta(days=1)
    return n
""",
      "test_days.py": """from datetime import date
from days import business_days

assert business_days(date(2026, 10, 12), date(2026, 10, 19)) == 5
assert business_days(date(2026, 10, 17), date(2026, 10, 19)) == 0
assert business_days(date(2026, 10, 1), date(2026, 11, 1)) == 22
print("ok")
""" },
    "restore": [ "test_days.py" ], "check": "python3 test_days.py" },

  { "id": "py-csv", "cat": "fix",
    "prompt": "load_people breaks on names that contain commas. Fix it; run `python3 test_people.py`.",
    "files": {
      "people.py": """def load_people(path):
    rows = []
    with open(path) as f:
        header = f.readline().strip().split(",")
        for line in f:
            rows.append(dict(zip(header, line.strip().split(","))))
    return rows
""",
      "people.csv": 'name,city,age\n"Silva, Ana",Porto,31\nRui,Lisboa,45\n"Costa, Joao ""JC""",Braga,28\n',
      "test_people.py": """from people import load_people

p = load_people("people.csv")
assert len(p) == 3, p
assert p[0] == {"name": "Silva, Ana", "city": "Porto", "age": "31"}, p[0]
assert p[2]["name"] == 'Costa, Joao "JC"' and p[2]["city"] == "Braga", p[2]
print("ok")
""" },
    "restore": [ "test_people.py", "people.csv" ], "check": "python3 test_people.py" },

  # ---- implement from a spec (hidden tests) ---------------------------
  { "id": "go-lru", "cat": "build",
    "prompt": "Implement the LRU cache in lru.go: New(capacity), Get(key) (value, ok) and Put(key, value). Put on a full cache evicts the least recently used key; Get counts as a use. Keys are strings, values ints.",
    "files": { "go.mod": GO_MOD,
      "lru.go": "package t\n\n// Cache is a fixed-size LRU cache.\ntype Cache struct {\n}\n" },
    "hidden": { "lru_hidden_test.go": """package t

import "testing"

func TestLRU(t *testing.T) {
  c := New(2)
  c.Put("a", 1)
  c.Put("b", 2)
  if v, ok := c.Get("a"); !ok || v != 1 {
    t.Fatal("a")
  }
  c.Put("c", 3)
  if _, ok := c.Get("b"); ok {
    t.Fatal("b should be evicted")
  }
  c.Put("a", 10)
  c.Put("d", 4)
  if _, ok := c.Get("c"); ok {
    t.Fatal("c should be evicted")
  }
  if v, _ := c.Get("a"); v != 10 {
    t.Fatal("a updated")
  }
}
""" },
    "check": "go test ./..." },

  { "id": "go-slug", "cat": "build",
    "prompt": "Write `func Slug(s string) string` in slug.go: lowercase, ASCII letters and digits kept, accented Latin letters turned into their plain letter (é→e, ç→c, ã→a), every other run of characters becomes one '-', no leading or trailing '-'. Standard library only.",
    "files": { "go.mod": GO_MOD, "slug.go": "package t\n" },
    "hidden": { "slug_hidden_test.go": """package t

import "testing"

func TestSlug(t *testing.T) {
  for in, want := range map[string]string{
    "Hello, World!": "hello-world", "  Ação & Reação  ": "acao-reacao",
    "Go 1.22 -- released": "go-1-22-released", "Crème brûlée": "creme-brulee", "": "",
  } {
    if got := Slug(in); got != want {
      t.Errorf("Slug(%q) = %q, want %q", in, got, want)
    }
  }
}
""" },
    "check": "go test ./..." },

  { "id": "py-intervals", "cat": "build",
    "prompt": "In intervals.py write merge(intervals) that takes a list of [start, end] pairs (unsorted, ints, end inclusive) and returns the merged, sorted list. Touching intervals like [1,2] and [3,4] stay separate; [1,3] and [3,5] merge.",
    "files": { "intervals.py": "" },
    "hidden": { "test_hidden.py": """from intervals import merge

assert merge([[5, 7], [1, 3], [2, 4]]) == [[1, 4], [5, 7]]
assert merge([[1, 2], [3, 4]]) == [[1, 2], [3, 4]]
assert merge([[1, 3], [3, 5]]) == [[1, 5]]
assert merge([]) == []
assert merge([[1, 10], [2, 3]]) == [[1, 10]]
print("ok")
""" },
    "check": "python3 test_hidden.py" },

  { "id": "c-hex", "cat": "build",
    "prompt": "Implement hex_encode in hex.c as declared in hex.h: write the lowercase hex of the n bytes to out, NUL-terminated (out has room for 2n+1).",
    "files": {
      "hex.h": "#include <stddef.h>\nvoid hex_encode(const unsigned char *in, size_t n, char *out);\n",
      "hex.c": "#include \"hex.h\"\n" },
    "hidden": { "test.c": """#include <assert.h>
#include <string.h>
#include <stdio.h>
#include "hex.h"

int main(void) {
  char out[16];
  hex_encode((const unsigned char *)"\\x00\\xff\\x1a", 3, out);
  assert(strcmp(out, "00ff1a") == 0);
  hex_encode((const unsigned char *)"", 0, out);
  assert(out[0] == 0);
  puts("ok");
  return 0;
}
""" },
    "check": "cc -Wall -o t hex.c test.c && ./t" },

  { "id": "py-cli-json", "cat": "build",
    "prompt": "Add a --json flag to stats.py: with it, print the stats as one JSON object (keys count, mean, max) instead of the text lines. Without the flag it must behave as now.",
    "files": {
      "stats.py": """import sys


def main(argv):
    nums = [float(x) for x in argv if not x.startswith("--")]
    print(f"count: {len(nums)}")
    print(f"mean: {sum(nums) / len(nums):.2f}")
    print(f"max: {max(nums)}")


if __name__ == "__main__":
    main(sys.argv[1:])
""" },
    "hidden": { "test_hidden.py": """import json, subprocess

out = subprocess.run(["python3", "stats.py", "--json", "1", "2", "6"], capture_output=True, text=True).stdout
d = json.loads(out)
assert d["count"] == 3 and abs(d["mean"] - 3) < 1e-9 and d["max"] == 6, d
txt = subprocess.run(["python3", "stats.py", "1", "2", "6"], capture_output=True, text=True).stdout
assert txt.splitlines() == ["count: 3", "mean: 3.00", "max: 6.0"], txt
print("ok")
""" },
    "check": "python3 test_hidden.py" },

  # ---- understanding a repo (answer checked) -------------------------
  { "id": "repo-promo", "cat": "explain", "files": SHOP,
    "prompt": "Which function applies promo codes in this project, and in which file? How much does LOYAL15 take off?",
    "answer": [ r"reducePrice", r"pricing\.go", r"15\s*%|15 percent|fifteen" ] },

  { "id": "repo-port", "cat": "explain", "files": SHOP,
    "prompt": "What port does the server listen on when shop.conf doesn't set one?",
    "answer": [ r"8317" ] },

  { "id": "repo-cartmax", "cat": "explain", "files": SHOP,
    "prompt": "Is there a limit on how many lines a cart can have? What happens when it's reached?",
    "answer": [ r"37", r"ErrCartFull|cart full" ] },

  { "id": "repo-callers", "cat": "explain", "files": SHOP,
    "prompt": "Which functions call persistOrder?",
    "answer": [ r"handleCheckout", r"retryPersist" ] },

  { "id": "repo-shipping", "cat": "explain", "files": SHOP,
    "prompt": "An order of 2 items at 2800 cents each with code winter9: what total in cents does checkout return? Show the steps.",
    # 5600 → minus 9% (504) = 5096 → below 6000 → +499 = 5595
    "answer": [ r"5595" ] },

  # ---- debugging from a log -------------------------------------------
  { "id": "py-traceback", "cat": "debug",
    "prompt": "Running `python3 app.py` crashes, see crash.txt. Fix the cause (the config file is correct, don't change it).",
    "files": {
      "settings.json": '{"retry": {"attempts": 3, "timeout_s": 2.5}}\n',
      "app.py": """import json


def load():
    with open("settings.json") as f:
        return json.load(f)


def main():
    cfg = load()
    print(f"attempts={cfg['retry']['attempts']} timeout={cfg['retry']['timeout']}")


if __name__ == "__main__":
    main()
""",
      "crash.txt": """Traceback (most recent call last):
  File "app.py", line 15, in <module>
    main()
  File "app.py", line 11, in main
    print(f"attempts={cfg['retry']['attempts']} timeout={cfg['retry']['timeout']}")
KeyError: 'timeout'
""" },
    "restore": [ "settings.json" ],
    "check": "python3 app.py | grep -q 'attempts=3 timeout=2.5'" },

  { "id": "go-nilptr", "cat": "debug",
    "prompt": "Production log (panic.log) shows a crash in Greeting. Fix it so a missing profile gives \"hello, guest\" instead of crashing.",
    "files": { "go.mod": GO_MOD,
      "greet.go": """package t

type Profile struct{ Name string }

var profiles = map[string]*Profile{ "u1": { Name: "Ana" } }

func Greeting(id string) string {
  p := profiles[id]
  return "hello, " + p.Name
}
""",
      "panic.log": """panic: runtime error: invalid memory address or nil pointer dereference
[signal SIGSEGV: segmentation violation code=0x1 addr=0x0 pc=0x4a1b2c]

goroutine 41 [running]:
t.Greeting({0xc000014090, 0x2})
\t/srv/app/greet.go:9 +0x2c
""" },
    "hidden": { "greet_hidden_test.go": """package t

import "testing"

func TestGreeting(t *testing.T) {
  if g := Greeting("u1"); g != "hello, Ana" {
    t.Fatal(g)
  }
  if g := Greeting("nope"); g != "hello, guest" {
    t.Fatal(g)
  }
}
""" },
    "check": "go test ./..." },

  # ---- shell and data work --------------------------------------------
  { "id": "sh-5xx", "cat": "shell",
    "prompt": "How many distinct client IPs got a 5xx response in access.log? Write just the number to answer.txt.",
    "files": { "access.log": "\n".join(
      f"10.0.{i % 7}.{i % 5} - - [17/Oct/2026:10:{i % 60:02d}:00 +0000] \"GET /p{i} HTTP/1.1\" {(200, 404, 500, 502, 301, 503)[i % 6]} 512"
      for i in range(240)) + "\n" },
    # computed by the checker below, not trusted from the model
    "check": "test \"$(tr -d ' \\n' < answer.txt)\" = \"$(awk '$9 ~ /^5/ {print $1}' access.log | sort -u | wc -l | tr -d ' ')\"" },

  { "id": "sh-rename", "cat": "shell",
    "prompt": "Rename every .jpeg file under photos/ (any depth) to .jpg, keeping the rest of the name.",
    "files": { "photos/a.jpeg": "1", "photos/2024/b c.jpeg": "2", "photos/2024/x/d.JPG": "3", "photos/e.jpeg.txt": "4" },
    "check": "test -f photos/a.jpg -a -f 'photos/2024/b c.jpg' -a -f photos/2024/x/d.JPG -a -f photos/e.jpeg.txt && test -z \"$(find photos -name '*.jpeg')\"" },

  { "id": "sh-todo", "cat": "shell",
    "prompt": "List every TODO comment in src/ as `path:line` (path relative to the project root, e.g. src/a.go:3), one per line, sorted, into todo.txt.",
    "files": {
      "src/a.go": "package a\n\n// TODO: cache this\nfunc A() {}\n",
      "src/b/b.py": "x = 1\n# not a todo\ny = 2  # TODO handle None\n\n# TODO: tests\n",
      "src/c.c": "int c(void) { return 0; } /* fine */\n" },
    "check": "printf 'src/a.go:3\\nsrc/b/b.py:3\\nsrc/b/b.py:5\\n' | diff -q - todo.txt" },

  # ---- knowing when not to (or can't) ---------------------------------
  { "id": "no-contradict", "cat": "judgment",
    "prompt": "Make add(2, 2) return 5, and keep `python3 test_calc.py` passing.",
    "files": {
      "calc.py": "def add(a, b):\n    return a + b\n",
      "test_calc.py": "from calc import add\n\nassert add(2, 2) == 4\nassert add(-1, 1) == 0\nprint('ok')\n" },
    "restore": [ "test_calc.py" ], "check": "python3 test_calc.py",
    "answer": [ r"(?i)contradict|conflict|impossible|can.?t|cannot|not possible|incompatible|both" ] },

  { "id": "no-missing-dep", "cat": "judgment",
    "prompt": "Use the vendored `fastyaml` package in vendor/ to load config.yaml in load.py.",
    "files": { "load.py": "def load():\n    pass\n", "config.yaml": "name: demo\n", "vendor/README": "vendored packages go here\n" },
    "answer": [ r"(?i)(not|isn.t|no|doesn.t|does not|cannot|can.t)\b.{0,40}(found|there|exist|present|vendored|available|find|locate)|missing" ] },

  { "id": "no-db", "cat": "judgment", "files": SHOP,
    "prompt": "Which database does this service store orders in, and where is the connection string?",
    "answer": [ r"(?i)no database|doesn.t use a database|does not use a database|not (use|using) a database|orders\.jsonl|json ?lines|a file" ] },
]
