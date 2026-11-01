package know

// The node-line format measurement (restructure plan, section 5):
// the same 20 questions over the same fixture tree, once with the
// plain-word lines models get (Line) and once with a coded format,
// against a real model. Answers are computed from the graph, so
// scoring is exact. Skipped unless ARMIN_FORMAT_EVAL_URL is set:
//
//   ARMIN_FORMAT_EVAL_URL=https://openrouter.ai/api/v1 \
//   ARMIN_FORMAT_EVAL_MODEL=qwen/qwen3.8-27b \
//   ARMIN_FORMAT_EVAL_KEY=OPENROUTER_API_KEY \
//   go test ./internal/know -run FormatEval -v -timeout 2h
//
// ARMIN_FORMAT_EVAL_SET=hard runs the 30 moderate questions instead,
// =harder the 20 harder ones.

import (
  "bytes"
  "encoding/json"
  "fmt"
  "math/rand"
  "net/http"
  "os"
  "regexp"
  "slices"
  "sort"
  "strings"
  "testing"
  "time"
)

var evalTopics = []string{
  "tcp", "udp", "ip", "tls", "dns", "http", "quic", "ethernet", "routing",
  "sockets",
}

var evalNouns = []string{
  "handshake", "header", "checksum", "window", "timer", "buffer", "state",
  "retransmit", "congestion", "session", "cache", "record", "frame",
  "address", "port", "queue", "flow", "stream", "ack",
}

// evalTree builds the fixture: 10 topics of 19 nodes, edges to earlier
// nodes, a third written by a minion (half of those verified), some
// stale and suspect.
func evalTree() (*Graph, []NodeID) {
  r := rand.New(rand.NewSource(7))
  g := New()
  minion, _ := g.Intern(RoleMinion, "small", "s", 1)
  copilot, _ := g.Intern(RoleCopilot, "big", "s", 0)
  var all []NodeID
  for _, t := range evalTopics {
    top, _ := g.AddNode(0, t, "", KindTopic, ConfChecked, copilot)
    for _, n := range evalNouns {
      kind := []Kind{ KindConcept, KindFact, KindFact, KindDecision }[r.Intn(4)]
      author := copilot
      if r.Intn(3) == 0 {
        author = minion
      }

      claim := fmt.Sprintf(
        "the %s %s %s", t, n,
        []string{
          "is negotiated once per connection", "is recomputed on every send",
          "grows until a loss is seen", "is kept in kernel memory",
          "times out after a fixed interval", "is checked before delivery",
        }[r.Intn(6)],
      )
      id, _ := g.AddNode(top, n, claim, kind, Conf(r.Intn(4)), author)
      if author == minion && r.Intn(2) == 0 {
        g.SetFlags(id, FlagVerified, 0)
      }

      switch r.Intn(10) {
      case 0:
        g.SetFlags(id, FlagStale, 0)
      case 1:
        g.SetFlags(id, FlagSuspect, 0)
      }

      for e := r.Intn(4); e > 0 && len(all) > 0; e-- {
        to := all[r.Intn(len(all))]
        g.AddEdge(
          id, to,
          []EdgeKind{ EdgeRequires, EdgeRequires, EdgeDerivedFrom }[r.Intn(3)],
        )
      }

      all = append(all, id)
    }
  }

  return g, all
}

// codedLine is the alternative: one-letter kind and confidence, m? for
// an unverified minion claim, ← what it needs, → what uses it.
func codedLine(g *Graph, b *strings.Builder, id NodeID) {
  n := g.node(id)
  fmt.Fprintf(
    b, "%d %s %c:%c", id, g.Path(id), "TCFDLSQ"[n.Kind()], "gict"[n.Conf()],
  )
  if g.authors[n.Author].Role == RoleMinion && n.Flags & FlagVerified == 0 {
    b.WriteString(" m?")
  }

  list := func(arrow string, edges []Edge) {
    var ids []string
    for _, e := range edges {
      if e.Kind == EdgeRequires || e.Kind == EdgeDerivedFrom {
        ids = append(ids, fmt.Sprint(e.To))
      }
    }

    if len(ids) > 0 {
      b.WriteString(" " + arrow + strings.Join(ids, ","))
    }
  }

  list("←", g.Out(id, nil))
  list("→", g.In(id, nil))
  fmt.Fprintf(b, " %q", cut(g.Claim(id), ClaimCut))
  if n.Flags & FlagStale != 0 {
    b.WriteString(" !stale")
  }

  if n.Flags & FlagSuspect != 0 {
    b.WriteString(" !suspect")
  }
}

const plainSystem = "You read results from armin's knowledge DAG: one line " +
  "per node. After the id and path come the kind and confidence; " +
  "\"minion-unverified\" marks a small model's claim nobody checked yet; " +
  "\"needs\" lists the nodes it depends on, \"used-by\" the nodes that " +
  "depend on it; STALE and SUSPECT mark claims whose evidence changed."

const codedSystem = "You read results from armin's knowledge DAG: one line " +
  "per node: id, path, kind:confidence (kind T topic, C concept, F fact, " +
  "D decision, L file, S symbol, Q question; confidence g guess, i " +
  "inferred, c checked, t tested), m? a small model's claim nobody " +
  "checked yet, ←ids the nodes it depends on, →ids the nodes that depend " +
  "on it, the claim, then !stale or !suspect when its evidence changed."

const answerRule = " Answer the question about the lines. End your reply " +
  "with one line: ANSWER: followed by node ids separated by spaces (or " +
  "none), or yes/no, or one word, as the question asks."

// evalQuestion is one question, the nodes shown with it and its
// expected answer (normalized: sorted ids, or a word).
type evalQuestion struct {
  text  string
  shown []NodeID
  want  string
}

func idsAnswer(ids []NodeID) string {
  if len(ids) == 0 {
    return "none"
  }

  s := make([]string, len(ids))
  slices.Sort(ids)
  for i, id := range ids {
    s[i] = fmt.Sprint(id)
  }

  return strings.Join(s, " ")
}

func evalQuestions(g *Graph, all []NodeID) []evalQuestion {
  r := rand.New(rand.NewSource(11))
  unverified := func(id NodeID) bool {
    n := g.node(id)
    return g.authors[n.Author].Role == RoleMinion && n.Flags & FlagVerified == 0
  }

  deps := func(id NodeID, in bool) []NodeID {
    var out []NodeID
    edges := g.Out(id, nil)
    if in {
      edges = g.In(id, nil)
    }

    for _, e := range edges {
      if (e.Kind == EdgeRequires || e.Kind == EdgeDerivedFrom) &&
          !slices.Contains(out, e.To) {
        out = append(out, e.To)
      }
    }

    return out
  }

  // shown: the target, its neighbours, and random others, ~25 lines
  context := func(target NodeID) []NodeID {
    set := map[NodeID]bool{ target: true }
    for _, d := range append(deps(target, false), deps(target, true)...) {
      set[d] = true
    }

    for len(set) < 25 {
      set[all[r.Intn(len(all))]] = true
    }

    var out []NodeID
    for id := range set {
      out = append(out, id)
    }

    slices.Sort(out)

    return out
  }

  pick := func(ok func(NodeID) bool) NodeID {
    for {
      id := all[r.Intn(len(all))]
      if ok(id) {
        return id
      }
    }
  }

  var qs []evalQuestion
  plan := []int{ 1, 2, 3, 4, 5, 6, 7, 8, 1, 2, 6, 7, 5, 8, 3, 4, 6, 7, 1, 2 }
  for _, kind := range plan {
    var q evalQuestion
    switch kind {
    case 1:
      id := pick(func(id NodeID) bool { return len(deps(id, false)) >= 2 })
      q = evalQuestion{
        text:  fmt.Sprintf("Which nodes does node %d depend on?", id),
        shown: context(id),
        want:  idsAnswer(deps(id, false)),
      }
    case 2:
      id := pick(func(id NodeID) bool { return len(deps(id, true)) >= 2 })
      q = evalQuestion{
        text:  fmt.Sprintf("Which nodes depend on node %d?", id),
        shown: context(id),
        want:  idsAnswer(deps(id, true)),
      }
    case 3:
      id := pick(func(NodeID) bool { return true })
      q = evalQuestion{
        text: "Which of the nodes shown are a small model's claims that " +
          "nobody has checked yet?",
        shown: context(id),
      }
      var ans []NodeID
      for _, s := range q.shown {
        if unverified(s) {
          ans = append(ans, s)
        }
      }

      q.want = idsAnswer(ans)
    case 4:
      id := pick(func(NodeID) bool { return true })
      q = evalQuestion{
        text:  "Which of the nodes shown are stale or suspect?",
        shown: context(id),
      }
      var ans []NodeID
      for _, s := range q.shown {
        if g.node(s).Flags & (FlagStale | FlagSuspect) != 0 {
          ans = append(ans, s)
        }
      }

      q.want = idsAnswer(ans)
    case 5:
      id := pick(func(NodeID) bool { return true })
      q = evalQuestion{
        text: fmt.Sprintf(
          "What is node %d's confidence: guess, inferred, checked or tested?",
          id,
        ),
        shown: context(id),
        want:  g.node(id).Conf().String(),
      }
    case 6:
      id := pick(func(NodeID) bool { return true })
      n := g.node(id)
      ok := n.Conf() >= ConfChecked &&
        n.Flags & (FlagStale | FlagSuspect) == 0 && !unverified(id)
      q = evalQuestion{
        text: fmt.Sprintf(
          "Can node %d be trusted as it is: confidence checked or tested, " +
            "not stale or suspect, and not an unchecked small-model claim? " +
              "yes or no.",
          id,
        ),
        shown: context(id),
        want: map[bool]string{
          true: "yes", false: "no",
        }[ok],
      }
    case 7:
      id := pick(func(id NodeID) bool { return len(deps(id, false)) >= 2 })
      var ans []NodeID
      for _, d := range deps(id, false) {
        if unverified(d) {
          ans = append(ans, d)
        }
      }

      q = evalQuestion{
        text: fmt.Sprintf(
          "Which of the nodes that node %d depends on are a small model's " +
            "claims nobody has checked yet?",
          id,
        ),
        shown: context(id),
        want:  idsAnswer(ans),
      }
    case 8:
      id := pick(func(id NodeID) bool { return len(deps(id, true)) >= 1 })
      q = evalQuestion{
        text: fmt.Sprintf(
          "If node %d turned out wrong, which nodes directly rest on it?", id,
        ),
        shown: context(id),
        want:  idsAnswer(deps(id, true)),
      }
    }

    qs = append(qs, q)
  }

  return qs
}

// answerLine finds the last "ANSWER:" label, tolerating a misspelled
// one (ANSER:, ANWSER:): a slip in the label isn't a wrong answer.
var answerLine = regexp.MustCompile(`(?i)\bAN[A-Z]{1,4}R\s*:`)

// normalize reads the ANSWER line: ids sorted, or a lowercase word.
func normalize(reply string) string {
  all := answerLine.FindAllStringIndex(reply, -1)
  if len(all) == 0 {
    return ""
  }

  ans := strings.TrimSpace(reply[all[len(all) - 1][1]:])
  ans, _, _ = strings.Cut(ans, "\n")
  ans = strings.ToLower(strings.Trim(ans, " .*`"))
  var ids []int
  for _, f := range strings.FieldsFunc(
    ans, func(r rune) bool { return r == ' ' || r == ',' },
  ) {
    var v int
    _, err := fmt.Sscanf(strings.TrimPrefix(f, "n"), "%d", &v)
    if err != nil {
      return ans
    }

    ids = append(ids, v)
  }

  if len(ids) == 0 {
    return ans
  }

  sort.Ints(ids)
  s := make([]string, len(ids))
  for i, v := range ids {
    s[i] = fmt.Sprint(v)
  }

  return strings.Join(s, " ")
}

type evalReply struct {
  Choices []struct {
    Message struct {
      Content   string `json:"content"`
      Reasoning string `json:"reasoning"`
      // llama-server's name for it
      ReasoningContent string `json:"reasoning_content"`
    } `json:"message"`
    Finish string `json:"finish_reason"`
  } `json:"choices"`
  Usage struct {
    Prompt     int `json:"prompt_tokens"`
    Completion int `json:"completion_tokens"`
  } `json:"usage"`
}

func ask(base, model, key, system, user string) (evalReply, error) {
  body := map[string]any{
    "model": model,
    "messages": []map[string]string{
      { "role": "system", "content": system },
      { "role": "user", "content": user },
    },
    "max_tokens":  8000,
    "temperature": 0.2,
  }

  if strings.Contains(base, "openrouter") {
    body["reasoning"] = map[string]string{ "effort": "medium" }
  }

  raw, _ := json.Marshal(body)
  req, _ := http.NewRequest(
    "POST",
    strings.TrimSuffix(base, "/") + "/chat/completions",
    bytes.NewReader(raw),
  )
  req.Header.Set("Content-Type", "application/json")
  if key != "" {
    req.Header.Set("Authorization", "Bearer " + key)
  }

  client := &http.Client{ Timeout: 10 * time.Minute }
  resp, err := client.Do(req)
  if err != nil {
    return evalReply{}, err
  }

  defer resp.Body.Close()
  var out evalReply
  err = json.NewDecoder(resp.Body).Decode(&out)
  if err == nil && len(out.Choices) == 0 {
    err = fmt.Errorf("no choices (status %s)", resp.Status)
  }

  return out, err
}

func TestFormatEval(t *testing.T) {
  base := os.Getenv("ARMIN_FORMAT_EVAL_URL")
  if base == "" {
    t.Skip("set ARMIN_FORMAT_EVAL_URL (and _MODEL, _KEY) to run")
  }

  model := os.Getenv("ARMIN_FORMAT_EVAL_MODEL")
  key := os.Getenv(os.Getenv("ARMIN_FORMAT_EVAL_KEY"))
  g, all := evalTree()
  qs := evalQuestions(g, all)
  switch os.Getenv("ARMIN_FORMAT_EVAL_SET") {
  case "hard":
    qs = evalHardQuestions(g, all)
  case "harder":
    qs = evalHarderQuestions(g, all)
  }

  t.Logf("fixture: %d nodes, %d edges", g.Len(), g.edges.count())

  // ARMIN_FORMAT_EVAL_ONLY=10,24 runs those questions only;
  // ARMIN_FORMAT_EVAL_REPEAT=3 asks each that many times;
  // ARMIN_FORMAT_EVAL_DUMP=dir saves every reply with its reasoning
  only := map[int]bool{}
  for _, f := range strings.Split(os.Getenv("ARMIN_FORMAT_EVAL_ONLY"), ",") {
    var n int
    _, err := fmt.Sscanf(strings.TrimSpace(f), "%d", &n)
    if err == nil {
      only[n] = true
    }
  }

  repeat := 1
  fmt.Sscanf(os.Getenv("ARMIN_FORMAT_EVAL_REPEAT"), "%d", &repeat)
  dump := os.Getenv("ARMIN_FORMAT_EVAL_DUMP")

  type tally struct{ right, asked, prompt, completion int }
  var res [2]tally
  for qi, q := range qs {
    if len(only) > 0 && !only[qi + 1] {
      continue
    }

    for f := 0; f < 2 * repeat; f++ {
      rep := f / 2
      f := f % 2
      var b strings.Builder
      system := plainSystem
      for _, id := range q.shown {
        if f == 0 {
          g.Line(&b, id, false)
        } else {
          codedLine(g, &b, id)
        }

        b.WriteByte('\n')
      }

      if f == 1 {
        system = codedSystem
      }

      reply, err := ask(
        base, model, key, system + answerRule, b.String() + "\n" + q.text,
      )
      if err != nil {
        t.Fatalf("q%d: %v", qi + 1, err)
      }

      ch := reply.Choices[0]
      got := normalize(ch.Message.Content)
      ok := got == q.want
      res[f].asked++
      if ok {
        res[f].right++
      }

      if dump != "" {
        name := fmt.Sprintf(
          "%s/q%02d-%s-%d.txt", dump,
          qi + 1,
          []string{ "plain", "coded" }[f],
          rep,
        )
        os.WriteFile(
          name,
          []byte(
            fmt.Sprintf(
              "want %s, got %q, %v, finish %s, %d completion tokens\n\n=== " +
                "reasoning\n%s%s\n\n=== content\n%s\n",
              q.want, got, ok, ch.Finish, reply.Usage.Completion,
              ch.Message.Reasoning, ch.Message.ReasoningContent,
              ch.Message.Content,
            ),
          ),
          0o644,
        )
      }

      res[f].prompt += reply.Usage.Prompt
      res[f].completion += reply.Usage.Completion
      t.Logf(
        "q%-2d %-5s %-5v want %-14q got %q",
        qi + 1,
        []string{ "plain", "coded" }[f],
        ok, q.want, got,
      )
    }
  }

  for f, name := range []string{ "plain", "coded" } {
    t.Logf(
      "%s: %d/%d right, %d prompt tokens, %d completion tokens",
      name,
      res[f].right,
      res[f].asked,
      res[f].prompt,
      res[f].completion,
    )
  }
}

// TestFormatEvalFixture checks the fixture and questions without a
// model: every answer is computable and the two formats differ.
func TestFormatEvalFixture(t *testing.T) {
  g, all := evalTree()
  if g.Len() != 1 + len(evalTopics) * (1 + len(evalNouns)) || len(all) != 190 {
    t.Fatalf("fixture size %d", g.Len())
  }

  qs := evalQuestions(g, all)
  if len(qs) != 20 {
    t.Fatalf("%d questions", len(qs))
  }

  for i, q := range qs {
    if q.want == "" || len(q.shown) < 25 {
      t.Fatalf("q%d: %+v", i + 1, q)
    }
  }

  hard := evalHardQuestions(g, all)
  if len(hard) != 30 {
    t.Fatalf("%d hard questions", len(hard))
  }

  for i, q := range hard {
    if q.want == "" || len(q.shown) < 50 {
      t.Fatalf("hard q%d: %+v", i + 1, q)
    }
  }

  harder := evalHarderQuestions(g, all)
  if len(harder) != 20 {
    t.Fatalf("%d harder questions", len(harder))
  }

  for i, q := range harder {
    if q.want == "" || len(q.shown) < 80 {
      t.Fatalf("harder q%d: %+v", i + 1, q)
    }
  }

  if normalize("so: 3, 12\nANSER: 12 3") != "3 12" {
    t.Fatal("a misspelled label")
  }

  if normalize("thinking...\nANSWER: n12, n3") != "3 12" ||
      normalize("ANSWER: Yes.") != "yes" {
    t.Fatal("normalize")
  }
}

// evalHardQuestions are 30 moderate ones: about 50 lines each, and
// answers that need two hops, a transitive closure, or several
// criteria at once. Everything an answer needs is among the lines.
func evalHardQuestions(g *Graph, all []NodeID) []evalQuestion {
  r := rand.New(rand.NewSource(23))
  unverified := func(id NodeID) bool {
    n := g.node(id)
    return g.authors[n.Author].Role == RoleMinion && n.Flags & FlagVerified == 0
  }

  flagged := func(id NodeID) bool {
    return g.node(id).Flags & (FlagStale | FlagSuspect) != 0
  }

  trusted := func(id NodeID) bool {
    return g.node(id).Conf() >= ConfChecked && !flagged(id) && !unverified(id)
  }

  deps := func(id NodeID, in bool) []NodeID {
    var out []NodeID
    edges := g.Out(id, nil)
    if in {
      edges = g.In(id, nil)
    }

    for _, e := range edges {
      if (e.Kind == EdgeRequires || e.Kind == EdgeDerivedFrom) &&
          !slices.Contains(out, e.To) {
        out = append(out, e.To)
      }
    }

    return out
  }

  closure := func(id NodeID, in bool) []NodeID {
    seen := map[NodeID]bool{ id: true }
    queue, out := []NodeID{ id }, []NodeID(nil)
    for len(queue) > 0 {
      n := queue[0]
      queue = queue[1:]
      for _, d := range deps(n, in) {
        if !seen[d] {
          seen[d] = true
          out = append(out, d)
          queue = append(queue, d)
        }
      }
    }

    return out
  }

  // shown: the nodes the answer needs, their neighbours, and others
  context := func(need ...NodeID) []NodeID {
    set := map[NodeID]bool{}
    for _, n := range need {
      set[n] = true
      for _, d := range append(deps(n, false), deps(n, true)...) {
        set[d] = true
      }
    }

    for len(set) < 50 {
      set[all[r.Intn(len(all))]] = true
    }

    var out []NodeID
    for id := range set {
      out = append(out, id)
    }

    slices.Sort(out)

    return out
  }

  pick := func(ok func(NodeID) bool) NodeID {
    for {
      id := all[r.Intn(len(all))]
      if ok(id) {
        return id
      }
    }
  }

  filter := func(ids []NodeID, ok func(NodeID) bool) []NodeID {
    var out []NodeID
    for _, id := range ids {
      if ok(id) {
        out = append(out, id)
      }
    }

    return out
  }

  var qs []evalQuestion
  for round := 0; round < 3; round++ {
    // 1. two hops down
    id := pick(
      func(id NodeID) bool {
        n := len(closure(id, false))
        return len(deps(id, false)) >= 2 && n >= 3 && n <= 10
      },
    )
    two := deps(id, false)
    for _, d := range deps(id, false) {
      for _, dd := range deps(d, false) {
        if dd != id && !slices.Contains(two, dd) {
          two = append(two, dd)
        }
      }
    }

    qs = append(
      qs,
      evalQuestion{
        text: fmt.Sprintf(
          "Which nodes does node %d depend on, directly or through " +
            "exactly one node in between?",
          id,
        ),
        shown: context(append([]NodeID{ id }, two...)...), want: idsAnswer(two),
      },
    )

    // 2. everything resting on a node, at any depth
    id = pick(
      func(id NodeID) bool {
        n := len(closure(id, true))
        return n >= 3 && n <= 8
      },
    )
    up := closure(id, true)
    qs = append(
      qs,
      evalQuestion{
        text: fmt.Sprintf(
          "If node %d turned out wrong, which nodes rest on it, directly or " +
            "indirectly at any depth?",
          id,
        ),
        shown: context(append([]NodeID{ id }, up...)...), want: idsAnswer(up),
      },
    )

    // 3. trusted direct dependencies
    id = pick(func(id NodeID) bool { return len(deps(id, false)) >= 3 })
    qs = append(
      qs,
      evalQuestion{
        text: fmt.Sprintf(
          "Of the nodes node %d depends on directly, which can " +
            "be trusted as they are: " +
            "confidence checked or tested, not stale or suspect, not an " +
              "unchecked small-model claim?",
          id,
        ),
        shown: context(id), want: idsAnswer(filter(deps(id, false), trusted)),
      },
    )

    // 4. unverified claims that something shown rests on
    shown := context(pick(func(NodeID) bool { return true }))
    q4 := filter(
      shown,
      func(s NodeID) bool {
        if !unverified(s) {
          return false
        }

        for _, u := range deps(s, true) {
          if slices.Contains(shown, u) {
            return true
          }
        }

        return false
      },
    )
    qs = append(
      qs,
      evalQuestion{
        text: "Which of the nodes shown are unchecked small-model " +
          "claims that at least one other node shown depends on?",
        shown: shown, want: idsAnswer(q4),
      },
    )

    // 5. solid-looking facts on shaky ground
    shown = context(pick(func(NodeID) bool { return true }))
    q5 := filter(
      shown,
      func(s NodeID) bool {
        n := g.node(s)
        if n.Kind() != KindFact || n.Conf() < ConfChecked {
          return false
        }

        // only what the lines show: a dependency off the page can't
        // be seen to be stale
        for _, d := range deps(s, false) {
          if flagged(d) && slices.Contains(shown, d) {
            return true
          }
        }

        return false
      },
    )
    qs = append(
      qs,
      evalQuestion{
        text: "Which facts shown, with confidence checked or tested, " +
          "directly depend on a node shown that is stale or suspect?",
        shown: shown, want: idsAnswer(q5),
      },
    )

    // 6. a count
    shown = context(pick(func(NodeID) bool { return true }))
    count := len(
      filter(
        shown,
        func(s NodeID) bool {
          return g.node(s).Kind() == KindDecision &&
            g.node(s).Conf() <= ConfInferred
        },
      ),
    )
    qs = append(
      qs,
      evalQuestion{
        text: "How many of the nodes shown are decisions with confidence " +
          "guess or inferred? Answer with a number.",
        shown: shown, want: fmt.Sprint(count),
      },
    )

    // 7. the most depended-on node, picked so there's no tie
    for {
      shown = context(pick(func(NodeID) bool { return true }))
      best, bestN, tie := NodeID(0), -1, false
      for _, s := range shown {
        n := len(deps(s, true))
        switch {
        case n > bestN:
          best, bestN, tie = s, n, false
        case n == bestN:
          tie = true
        }
      }

      if !tie {
        qs = append(
          qs,
          evalQuestion{
            text: "Which node shown has the most nodes depending on it " +
              "directly (count every dependent listed in its line)? Answer " +
                "with one id.",
            shown: shown, want: fmt.Sprint(best),
          },
        )
        break
      }
    }

    // 8. dependencies across topics
    topic := evalTopics[r.Intn(len(evalTopics))]
    top, _ := g.Lookup(topic)
    kids := g.Children(top, nil)
    q8 := filter(
      kids,
      func(s NodeID) bool {
        for _, d := range deps(s, false) {
          if !strings.HasPrefix(g.Path(d), topic + "/") {
            return true
          }
        }

        return false
      },
    )
    qs = append(
      qs,
      evalQuestion{
        text: fmt.Sprintf(
          "Which nodes under %s/ depend directly on a node outside %s/?",
          topic, topic,
        ),
        shown: context(kids...), want: idsAnswer(q8),
      },
    )

    // 9. shaky at two hops
    id = pick(func(id NodeID) bool { return len(deps(id, false)) >= 2 })
    q9 := filter(
      deps(id, false),
      func(d NodeID) bool {
        if flagged(d) {
          return true
        }

        for _, dd := range deps(d, false) {
          if flagged(dd) {
            return true
          }
        }

        return false
      },
    )
    need := []NodeID{ id }
    for _, d := range deps(id, false) {
      need = append(need, d)
      need = append(need, deps(d, false)...)
    }

    qs = append(
      qs,
      evalQuestion{
        text: fmt.Sprintf(
          "Of the nodes node %d depends on directly, which are " +
            "stale or suspect themselves, " +
            "or directly depend on a node that is stale or suspect?",
          id,
        ),
        shown: context(need...), want: idsAnswer(q9),
      },
    )

    // 10. between two nodes
    var a, b NodeID
    var mid []NodeID
    for len(mid) == 0 {
      a = pick(func(id NodeID) bool { return len(deps(id, false)) >= 1 })
      d := deps(a, false)
      dd := deps(d[r.Intn(len(d))], false)
      if len(dd) == 0 {
        continue
      }

      b = dd[r.Intn(len(dd))]
      mid = filter(
        deps(a, false),
        func(m NodeID) bool { return slices.Contains(deps(m, false), b) },
      )
    }

    qs = append(
      qs,
      evalQuestion{
        text: fmt.Sprintf(
          "Which nodes does node %d depend on directly that in turn depend " +
            "directly on node %d?",
          a, b,
        ),
        shown: context(
          append([]NodeID{ a, b }, mid...)...,
        ), want: idsAnswer(mid),
      },
    )
  }

  return qs
}

// evalHarderQuestions are 20 harder ones: about 80 lines each, full
// closures, two closures compared, paths and root causes. Every node
// an answer needs is among the lines.
func evalHarderQuestions(g *Graph, all []NodeID) []evalQuestion {
  r := rand.New(rand.NewSource(37))
  unverified := func(id NodeID) bool {
    n := g.node(id)
    return g.authors[n.Author].Role == RoleMinion && n.Flags & FlagVerified == 0
  }

  flagged := func(id NodeID) bool {
    return g.node(id).Flags & (FlagStale | FlagSuspect) != 0
  }

  trusted := func(id NodeID) bool {
    return g.node(id).Conf() >= ConfChecked && !flagged(id) && !unverified(id)
  }

  deps := func(id NodeID, in bool) []NodeID {
    var out []NodeID
    edges := g.Out(id, nil)
    if in {
      edges = g.In(id, nil)
    }

    for _, e := range edges {
      if (e.Kind == EdgeRequires || e.Kind == EdgeDerivedFrom) &&
          !slices.Contains(out, e.To) {
        out = append(out, e.To)
      }
    }

    return out
  }

  // dist is the hop count from id along deps (in: upward), -1 unreached
  dist := func(id NodeID, in bool) map[NodeID]int {
    d := map[NodeID]int{ id: 0 }
    queue := []NodeID{ id }
    for len(queue) > 0 {
      n := queue[0]
      queue = queue[1:]
      for _, x := range deps(n, in) {
        _, seen := d[x]
        if !seen {
          d[x] = d[n] + 1
          queue = append(queue, x)
        }
      }
    }

    return d
  }

  closure := func(id NodeID, in bool) []NodeID {
    var out []NodeID
    for n := range dist(id, in) {
      if n != id {
        out = append(out, n)
      }
    }

    slices.Sort(out)

    return out
  }

  context := func(need ...NodeID) []NodeID {
    set := map[NodeID]bool{}
    for _, n := range need {
      set[n] = true
    }

    for len(set) < 80 {
      set[all[r.Intn(len(all))]] = true
    }

    var out []NodeID
    for id := range set {
      out = append(out, id)
    }

    slices.Sort(out)

    return out
  }

  pick := func(ok func(NodeID) bool) NodeID {
    for {
      id := all[r.Intn(len(all))]
      if ok(id) {
        return id
      }
    }
  }

  filter := func(ids []NodeID, ok func(NodeID) bool) []NodeID {
    var out []NodeID
    for _, id := range ids {
      if ok(id) {
        out = append(out, id)
      }
    }

    return out
  }

  sized := func(in bool, lo, hi int) func(NodeID) bool {
    return func(id NodeID) bool {
      n := len(closure(id, in))
      return n >= lo && n <= hi
    }
  }

  var qs []evalQuestion
  add := func(text string, want string, need ...NodeID) {
    qs = append(
      qs, evalQuestion{ text: text, shown: context(need...), want: want },
    )
  }

  for round := 0; round < 2; round++ {
    // 1. within three hops down
    id := pick(sized(false, 5, 14))
    var within []NodeID
    for n, d := range dist(id, false) {
      if d >= 1 && d <= 3 {
        within = append(within, n)
      }
    }

    add(
      fmt.Sprintf(
        "Which nodes does node %d depend on within three steps (directly, or " +
          "through one or two nodes in between)?",
        id,
      ),
      idsAnswer(within),
      append(closure(id, false), id)...,
    )

    // 2. every prerequisite at any depth
    id = pick(sized(false, 5, 12))
    add(
      fmt.Sprintf(
        "Which nodes does node %d depend on, directly or indirectly " +
          "at any depth?",
        id,
      ),
      idsAnswer(closure(id, false)),
      append(closure(id, false), id)...,
    )

    // 3. unverified claims among everything resting on a node
    id = pick(
      func(id NodeID) bool {
        up := closure(id, true)
        return len(up) >= 4 && len(up) <= 12 && len(filter(up, unverified)) >= 1
      },
    )
    up := closure(id, true)
    add(
      fmt.Sprintf(
        "Of all the nodes that rest on node %d, directly or " +
          "indirectly at any depth, which are unchecked small-model " +
          "claims?",
        id,
      ),
      idsAnswer(filter(up, unverified)),
      append(up, id)...,
    )

    // 4. prerequisites two nodes share, at any depth
    var a, b NodeID
    var both []NodeID
    for len(both) == 0 {
      a = pick(sized(false, 4, 12))
      b = pick(sized(false, 4, 12))
      if a == b {
        continue
      }

      cb := closure(b, false)
      both = filter(
        closure(a, false),
        func(n NodeID) bool { return slices.Contains(cb, n) },
      )
    }

    add(
      fmt.Sprintf(
        "Which nodes do both node %d and node %d depend on, directly or " +
          "indirectly at any depth?",
        a, b,
      ),
      idsAnswer(both),
      append(append(closure(a, false), closure(b, false)...), a, b)...,
    )

    // 5. does one depend on the other at any depth: one yes, one no
    id = pick(sized(false, 5, 12))
    down := closure(id, false)
    target := down[r.Intn(len(down))]
    want := "yes"
    if round == 1 {
      target = pick(
        func(n NodeID) bool {
          return n != id && !slices.Contains(down, n) && len(deps(n, true)) > 0
        },
      )
      want = "no"
    }

    add(
      fmt.Sprintf(
        "Does node %d depend on node %d, directly or indirectly at " +
          "any depth? yes or no.",
        id, target,
      ),
      want,
      append(down, id, target)...,
    )

    // 6. trustworthy among what rests on a node
    id = pick(sized(true, 4, 12))
    up = closure(id, true)
    add(
      fmt.Sprintf(
        "Of all the nodes that rest on node %d, directly or " +
          "indirectly at any depth, which can be trusted as they are: " +
          "confidence checked or tested, not stale or suspect, not an " +
            "unchecked " +
            "small-model claim?",
        id,
      ),
      idsAnswer(filter(up, trusted)),
      append(up, id)...,
    )

    // 7. a count over a closure
    id = pick(sized(false, 6, 14))
    down = closure(id, false)
    add(
      fmt.Sprintf(
        "How many of the nodes node %d depends on, directly or indirectly at " +
          "any depth, are stale or suspect? Answer with a number.",
        id,
      ),
      fmt.Sprint(len(filter(down, flagged))),
      append(down, id)...,
    )

    // 8. where the shakiness starts
    id = pick(
      func(id NodeID) bool {
        down := closure(id, false)
        return len(down) >= 5 && len(down) <= 14 &&
          len(filter(down, flagged)) >= 1
      },
    )
    down = closure(id, false)
    roots := filter(
      down,
      func(n NodeID) bool {
        if !flagged(n) {
          return false
        }

        for _, d := range closure(n, false) {
          if flagged(d) {
            return false
          }
        }

        return true
      },
    )
    add(
      fmt.Sprintf(
        "Among the nodes node %d depends on at any depth, which are " +
          "stale or suspect while depending on nothing stale or " +
          "suspect themselves, at any depth?",
        id,
      ),
      idsAnswer(roots),
      append(down, id)...,
    )

    // 9. shortest path
    id = pick(sized(false, 6, 14))
    d := dist(id, false)
    far := id
    for n, k := range d {
      if k > d[far] || k == d[far] && n < far {
        far = n
      }
    }

    add(
      fmt.Sprintf(
        "What is the fewest number of steps from node %d down to node %d, " +
          "following what each node depends on? Answer with a number.",
        id, far,
      ),
      fmt.Sprint(d[far]),
      append(closure(id, false), id)...,
    )

    // 10. affected but still trusted, two levels up
    id = pick(
      func(id NodeID) bool {
        n := len(closure(id, true))
        return n >= 3 && n <= 10
      },
    )
    var two []NodeID
    for n, k := range dist(id, true) {
      if k >= 1 && k <= 2 && trusted(n) {
        two = append(two, n)
      }
    }

    add(
      fmt.Sprintf(
        "If node %d turned out wrong, which trusted nodes " +
          "(confidence checked or tested, not stale or suspect, " +
          "not an unchecked small-model claim) would be affected within two " +
            "steps: resting on it directly, or through one node in between?",
        id,
      ),
      idsAnswer(two),
      append(closure(id, true), id)...,
    )
  }

  return qs
}
