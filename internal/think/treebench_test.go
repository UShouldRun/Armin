package think

// The knowledge DAG's benchmark: the same model on the same pairs of
// real questions about armin's own code, in up to three arms: no tree,
// the tree, and a static repo map in the prompt (internal/repomap, the
// baseline: how much of the tree's value is any map at all). Each pair
// is fresh sessions: the first asks about an area, the next follow up
// on it, where the tree should spare the model from exploring again.
// ARMIN_TREEBENCH_ARMS picks arms (default "plain,tree,map"). Skipped
// unless ARMIN_TREEBENCH_MODEL is set:
//
//   ARMIN_TREEBENCH_URL=https://openrouter.ai/api/v1 \
//   ARMIN_TREEBENCH_MODEL=qwen/qwen3.8-27b \
//   ARMIN_TREEBENCH_KEY=OPENROUTER_API_KEY \
//   ARMIN_TREEBENCH_DUMP=dir \
//   go test ./internal/think -run TreeBench -v -timeout 2h

import (
  "context"
  "fmt"
  "os"
  "os/exec"
  "path/filepath"
  "regexp"
  "slices"
  "strings"
  "sync"
  "testing"
  "time"

  "armin/internal/agent"
  "armin/internal/ctags"
  "armin/internal/know"
  "armin/internal/llm"
  "armin/internal/metrics"
  "armin/internal/notes"
  "armin/internal/repomap"
  "armin/internal/sandbox"
)

// benchPair is three questions on one area, asked in three fresh
// sessions: the first explores, the second follows up, the third comes
// back "a week later" (H3: the tree's standing cost is per session, so
// it should compound). Each answer must match all of its checks
// (case-insensitive regular expressions).
type benchPair struct {
  name                              string
  first, second, third              string
  firstWant, secondWant, thirdWant []string
}

// sessions is how many sessions a pair runs.
const sessions = 3

// The arms: no tree, the tree, the static repo map.
const (
  armPlain = iota
  armTree
  armMap
  arms
)

// armNames name the arms in the log; armFiles in the dump's file
// names and ARMIN_TREEBENCH_ARMS.
var (
  armNames = [arms]string{ "no tree", "tree", "map" }
  armFiles = [arms]string{ "plain", "tree", "map" }
)

// mapBytes is the repo map's budget, about 2k tokens: aider's default
// map is 1k, and ours has no chat files to focus it.
const mapBytes = 8000

var benchPairs = []benchPair{
  {
    name: "repeat-guard",
    first: "How does armin's agent loop deal with a model that keeps " +
      "repeating a tool call that already failed?",
    firstWant: []string{ `turn\.go`, `repeat|block` },
    second: "I want the loop to tolerate 3 refused repeats before it " +
      "turns tools off, instead of the current limit. What exactly do " +
      "I change, in which file?",
    secondWant: []string{ `maxBlocked`, `turn\.go` },
    third: "When the repeat guard has refused enough calls and stops " +
      "tools for the rest of the turn, what is the model told?",
    thirdWant: []string{ `tools (are )?off|repeatNote` },
  },
  {
    name: "golden-tools",
    first: "How does armin make sure the tool definitions sent to the " +
      "model don't change by accident, for each role?",
    firstWant: []string{ `golden` },
    second: "Which file pins the advisor's tool list, and what command " +
      "regenerates it?",
    secondWant: []string{ `definitions\.advisor\.json`, `-update` },
    third: "When the golden definitions test fails, where does it " +
      "write the new output so I can diff it?",
    thirdWant: []string{ `\.got` },
  },
  {
    name: "staleness",
    first: "How does the knowledge DAG (internal/know) notice that " +
      "code cited as evidence changed or moved?",
    firstWant: []string{ `hash` },
    second: "When a node of the knowledge DAG goes stale, what happens " +
      "to the nodes that depend on it, and which function does it?",
    secondWant: []string{ `suspect`, `MarkSuspect` },
    third: "Where does armin keep the stamps of the files the tree " +
      "cites, and what is in a stamp?",
    thirdWant: []string{ `stamps`, `ctime|change time` },
  },
  {
    name: "roles",
    first: "How do armin's roles (minion, copilot, advisor) limit which " +
      "tools the model can call?",
    firstWant: []string{ `role` },
    second: "Can the advisor role call run_program? Where is that " +
      "decided in the code?",
    secondWant: []string{
      `\b(no|not|cannot|can't)\b`, `RoleCopilot|tools_run\.go`,
    },
    third: "When I switch to the advisor in the middle of a " +
      "conversation, how is the model told, and does the system " +
      "prompt change?",
    thirdWant: []string{ `remind`, `next user message|takeReminder` },
  },
  {
    name: "project-dirs",
    first: "How does armin save chat sessions to disk?",
    firstWant: []string{ `append` },
    second: "How does armin name the per-project folders it keeps for " +
      "memory, the scratchpad and the knowledge DAG?",
    secondWant: []string{ `ProjectKey`, `hash` },
    // the same area as the first two (internal/session): an earlier
    // third question asked about internal/know, which no node could
    // cover, so it didn't test compounding
    third: "What is the first line of a saved session file, and what " +
      "does it hold?",
    thirdWant: []string{ `Meta`, `first` },
  },
}

// benchRun is what one session cost and whether it answered right.
type benchRun struct {
  prompt, completion, rounds, tools int
  cached                            int     // prompt tokens read from cache
  cost                              float64 // dollars, as OpenRouter bills
  right                             bool
  answer                            string
  calls                             []string // each tool call, short
  tree                              string   // the tree after it, as text
}

// benchKey resolves the key: the env var it names, falling back to
// the same ~/.config/armin/.env armin itself loads (KEY = value, the
// spaces around = included), so the bench runs without a shell dance.
func benchKey(envName string) string {
  v := os.Getenv(envName)
  if v != "" {
    return v
  }

  if envName == "" {
    return ""
  }

  dir, err := os.UserConfigDir()
  if err != nil {
    return ""
  }

  data, err := os.ReadFile(filepath.Join(dir, "armin", ".env"))
  if err != nil {
    return ""
  }

  re := regexp.MustCompile(
    `(?m)^` + regexp.QuoteMeta(envName) + `\s*=\s*(.+)$`,
  )
  m := re.FindStringSubmatch(string(data))
  if m != nil {
    return strings.TrimSpace(m[1])
  }

  return ""
}

func TestTreeBench(t *testing.T) {
  model := os.Getenv("ARMIN_TREEBENCH_MODEL")
  if model == "" {
    t.Skip("set ARMIN_TREEBENCH_MODEL (and _URL, _KEY) to run")
  }

  root, err := filepath.Abs("../..")
  if err != nil {
    t.Fatal(err)
  }

  sb, err := sandbox.New(root, sandbox.Options{
    Allowlist: sandbox.DefaultAllowlist(),
    Timeout:   20 * time.Second,
    MaxOutput: 8000,
  })
  if err != nil {
    t.Fatal(err)
  }

  client := func() *llm.Client {
    c := llm.New(os.Getenv("ARMIN_TREEBENCH_URL"), "bench")
    c.ID = model
    c.APIKey = benchKey(os.Getenv("ARMIN_TREEBENCH_KEY"))
    c.Effort = "medium"
    c.Private = true

    return c
  }

  dump := os.Getenv("ARMIN_TREEBENCH_DUMP")

  // consolidation between sessions, as armin-test runs it at start;
  // the bench's model does it (there's no local minion here), and its
  // cost is charged to the tree
  consolidate := os.Getenv("ARMIN_TREEBENCH_CONSOLIDATE") != ""
  if dump != "" {
    os.MkdirAll(dump, 0o755)
  }

  var on [arms]bool
  for _, a := range strings.Split(
    benchEnv("ARMIN_TREEBENCH_ARMS", "plain,tree,map"), ",",
  ) {
    i := slices.Index(armFiles[:], strings.TrimSpace(a))
    if i < 0 {
      t.Fatalf("ARMIN_TREEBENCH_ARMS: no arm %q", a)
    }

    on[i] = true
  }

  // the map, built once: every session of the map arm sees the same
  repoMap := ""
  if on[armMap] {
    repoMap = repomap.Build(
      context.Background(), ctags.New(root, benchCtags(t)), mapBytes,
    )
    t.Logf("MAP %d bytes, %d files", len(repoMap),
      strings.Count(repoMap, "\n") / 2)
  }

  // pair, arm, session
  results := make([][arms][sessions]benchRun, len(benchPairs))
  var wg sync.WaitGroup
  for pi, pair := range benchPairs {
    for cond := range arms {
      if !on[cond] {
        continue
      }

      wg.Add(1)
      go func() {
        defer wg.Done()

        var tree *know.Graph
        if cond == armTree {
          tree = know.New()
        }

        sessionMap := ""
        if cond == armMap {
          sessionMap = repoMap
        }

        questions := [sessions]string{ pair.first, pair.second, pair.third }
        wants := [sessions][]string{
          pair.firstWant, pair.secondWant, pair.thirdWant,
        }

        for si, q := range questions {
          want := wants[si]

          run := benchSession(t, client(), sb, tree, sessionMap, q, want)
          if tree != nil && consolidate && si < sessions - 1 {
            benchConsolidate(t, client(), sb, tree)
          }

          results[pi][cond][si] = run
          if dump != "" {
            name := fmt.Sprintf("%s/%s-%s-%d.md", dump, pair.name,
              armFiles[cond], si + 1)
            os.WriteFile(name, []byte(fmt.Sprintf(
              "%s\n\nright %v, %d prompt (%d cached), %d completion, "+
                "$%.5f, %d rounds, %d tools\n\n"+
                "## answer\n%s\n\n## calls\n%s\n\n## tree after\n%s\n",
              q, run.right, run.prompt, run.cached, run.completion, run.cost,
              run.rounds, run.tools, run.answer, strings.Join(run.calls, "\n"),
              run.tree,
            )), 0o644)
          }
        }
      }()
    }
  }

  wg.Wait()

  var total [arms][sessions]benchRun
  for pi, pair := range benchPairs {
    for cond := range arms {
      if !on[cond] {
        continue
      }

      for si := 0; si < sessions; si++ {
        r := results[pi][cond][si]
        t.Logf(
          "%-13s %-7s s%d  right %-5v  in %7d (cached %7d)  out %6d  "+
            "$%.5f  rounds %2d  tools %2d",
          pair.name, armNames[cond], si + 1, r.right,
          r.prompt, r.cached, r.completion, r.cost, r.rounds, r.tools,
        )
        tot := &total[cond][si]
        tot.prompt += r.prompt
        tot.cached += r.cached
        tot.cost += r.cost
        tot.completion += r.completion
        tot.rounds += r.rounds
        tot.tools += r.tools
      }
    }
  }

  for cond := range arms {
    if !on[cond] {
      continue
    }

    for si := 0; si < sessions; si++ {
      right := 0
      for pi := range benchPairs {
        if results[pi][cond][si].right {
          right++
        }
      }

      tot := total[cond][si]
      t.Logf(
        "TOTAL %-7s session %d: %d/%d right, in %d (cached %d), out %d, "+
          "$%.4f, rounds %d, tools %d",
        armNames[cond], si + 1, right, len(benchPairs),
        tot.prompt, tot.cached, tot.completion, tot.cost, tot.rounds, tot.tools,
      )
    }
  }

  // recall, when on: how often it attached nodes, how often the model
  // cited them ([nN]), and how often it only read their files
  snap := metrics.Snapshot()
  if snap["recall.turns"].Count > 0 {
    t.Logf("RECALL turns %d, attached %d, cited %d, read %d, empty %d",
      snap["recall.turns"].Count, snap["recall.attached"].Count,
      snap["recall.cited"].Count, snap["recall.read"].Count,
      snap["recall.empty"].Count)
  }

  // what all the sessions cost in dollars, each arm against no tree,
  // the consolidation passes charged to the tree
  var pair [arms]float64
  for cond := range arms {
    for si := 0; si < sessions; si++ {
      pair[cond] += total[cond][si].cost
    }
  }

  cons := snap["know.consolidate.cost"]
  if cons.Count > 0 {
    pair[armTree] += cons.Sum
    t.Logf("CONSOLIDATE calls %d, $%.4f, pointers %d, claims %d, "+
      "linked %d, empty %d",
      cons.Count, cons.Sum, snap["know.consolidate.pointers"].Count,
      snap["know.consolidate.claims"].Count,
      snap["know.consolidate.linked"].Count,
      snap["know.consolidate.empty"].Count)
  }

  if pair[armPlain] == 0 {
    return
  }

  line := fmt.Sprintf("PAIR COST no tree $%.4f", pair[armPlain])
  for _, cond := range []int{ armTree, armMap } {
    if on[cond] {
      line += fmt.Sprintf(", %s $%.4f (%+.1f%%)", armNames[cond],
        pair[cond], 100 * (pair[cond] - pair[armPlain]) / pair[armPlain])
    }
  }

  t.Log(line)
}

// benchEnv is the environment variable, or def when it's unset.
func benchEnv(name, def string) string {
  v := os.Getenv(name)
  if v == "" {
    return def
  }

  return v
}

// benchCtags is Universal Ctags, from ARMIN_TREEBENCH_CTAGS, the PATH
// or ~/.local/bin; the map arm can't run without it.
func benchCtags(t *testing.T) string {
  p := os.Getenv("ARMIN_TREEBENCH_CTAGS")
  if p != "" {
    return p
  }

  p, err := exec.LookPath("universal-ctags")
  if err == nil {
    return p
  }

  home, _ := os.UserHomeDir()
  p = filepath.Join(home, ".local", "bin", "universal-ctags")
  _, err = os.Stat(p)
  if err != nil {
    t.Fatal("the map arm needs universal-ctags (ARMIN_TREEBENCH_CTAGS)")
  }

  return p
}

// benchSession runs one fresh session: armin's own system prompt (with
// the tree's section when there is a tree, the map's when there is a
// map), one question, read-only.
func benchSession(
  t *testing.T, client *llm.Client, sb *sandbox.Config, tree *know.Graph,
  repoMap, question string, want []string,
) benchRun {
  loop := agent.New(client, sb)
  loop.ReadOnly = true
  loop.MaxRounds = 40
  loop.ContextSize = 200000
  loop.Session = fmt.Sprintf("bench-%d", time.Now().UnixNano())
  if tree != nil {
    loop.Know = tree
  }

  // no memories or skills: what's known comes from the tree alone
  mem := &notes.Memories{ ProjectDir: t.TempDir(), UserDir: t.TempDir() }
  recall := tree != nil && os.Getenv("ARMIN_TREEBENCH_RECALL") != ""
  loop.Recall = recall
  system := buildSystemPrompt(
    basePrompt, sb.Root, mem, &notes.Skills{}, nil, tree, recall,
  ) + mapSection(repoMap)
  history := []llm.Message{
    { Role: "system", Content: system },
    { Role: "user", Content: question },
  }

  var run benchRun
  hooks := agent.Hooks{
    Usage: func(u llm.Usage) {
      run.prompt += u.PromptTokens
      run.completion += u.CompletionTokens
      run.cached += u.PromptDetails.CachedTokens
      run.cost += u.Cost
      run.rounds++
    },
    ToolCall: func(name string, args map[string]any) {
      run.tools++
      short := fmt.Sprint(args)
      if len(short) > 140 {
        short = short[:140] + "…"
      }

      run.calls = append(run.calls, name + " " + short)
    },
  }

  ctx, cancel := context.WithTimeout(context.Background(), 20 * time.Minute)
  defer cancel()

  // what the user sees is every message of the turn, not the last:
  // a model that answers, then records, then closes with "recorded
  // as [n3]" has still answered
  var said strings.Builder
  hooks.Token = func(d string) { said.WriteString(d) }

  answer, err := loop.Turn(ctx, &history, hooks)
  if err != nil {
    t.Logf("session failed: %v", err)
    return run
  }

  run.answer = said.String()
  if strings.TrimSpace(run.answer) == "" {
    run.answer = answer // a server that didn't stream
  }

  if tree != nil {
    var b strings.Builder
    tree.Dump(&b, 0)
    run.tree = b.String()
  }

  run.right = true
  for _, w := range want {
    if !regexp.MustCompile(`(?i)` + w).MatchString(run.answer) {
      run.right = false
    }
  }

  return run
}

// benchConsolidate runs the consolidation pass on a pair's tree between
// two sessions, with the bench's model as the minion.
func benchConsolidate(
  t *testing.T, client *llm.Client, sb *sandbox.Config, tree *know.Graph,
) {
  loop := agent.New(client, sb)
  loop.Know = tree
  loop.Session = fmt.Sprintf("consolidate-%d", time.Now().UnixNano())
  loop.Minion = func(context.Context) (*llm.Client, error) {
    return client, nil
  }

  ctx, cancel := context.WithTimeout(context.Background(), 5 * time.Minute)
  defer cancel()

  _, err := loop.Consolidate(ctx, 10)
  if err != nil {
    t.Logf("consolidation failed: %v", err)
  }
}

