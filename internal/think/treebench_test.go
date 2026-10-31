package think

// The knowledge tree's benchmark: the same model on the same pairs of
// real questions about armin's own code, with the tree and without.
// Each pair is two fresh sessions: the first asks about an area, the
// second follows up on it, where the tree should spare the model from
// exploring again. Skipped unless ARMIN_TREEBENCH_MODEL is set:
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
  "path/filepath"
  "regexp"
  "strings"
  "sync"
  "testing"
  "time"

  "armin/internal/agent"
  "armin/internal/know"
  "armin/internal/llm"
  "armin/internal/notes"
  "armin/internal/sandbox"
)

// benchPair is two questions on one area; each answer must match all
// of its checks (case-insensitive regular expressions).
type benchPair struct {
  name                 string
  first, second        string
  firstWant, secondWant []string
}

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
  },
  {
    name: "golden-tools",
    first: "How does armin make sure the tool definitions sent to the " +
      "model don't change by accident, for each role?",
    firstWant: []string{ `golden` },
    second: "Which file pins the advisor's tool list, and what command " +
      "regenerates it?",
    secondWant: []string{ `definitions\.advisor\.json`, `-update` },
  },
  {
    name: "staleness",
    first: "How does the knowledge tree (internal/know) notice that " +
      "code cited as evidence changed or moved?",
    firstWant: []string{ `hash` },
    second: "When a node of the knowledge tree goes stale, what happens " +
      "to the nodes that depend on it, and which function does it?",
    secondWant: []string{ `suspect`, `MarkSuspect` },
  },
  {
    name: "roles",
    first: "How do armin's roles (minion, copilot, advisor) limit which " +
      "tools the model can call?",
    firstWant: []string{ `role` },
    second: "Can the advisor role call run_program? Where is that " +
      "decided in the code?",
    secondWant: []string{ `\b(no|not|cannot|can't)\b`, `RoleCopilot|tools_run\.go` },
  },
  {
    name: "project-dirs",
    first: "How does armin save chat sessions to disk?",
    firstWant: []string{ `append` },
    second: "How does armin name the per-project folders it keeps for " +
      "memory, the scratchpad and the knowledge tree?",
    secondWant: []string{ `ProjectKey`, `hash` },
  },
}

// benchRun is what one session cost and whether it answered right.
type benchRun struct {
  prompt, completion, rounds, tools int
  right                             bool
  answer                            string
  calls                             []string // each tool call, short
  tree                              string   // the tree after it, as text
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
    c.APIKey = os.Getenv(os.Getenv("ARMIN_TREEBENCH_KEY"))
    c.Effort = "medium"
    c.Private = true

    return c
  }

  dump := os.Getenv("ARMIN_TREEBENCH_DUMP")
  results := make([][2][2]benchRun, len(benchPairs)) // pair, tree?, session
  var wg sync.WaitGroup
  for pi, pair := range benchPairs {
    for cond := 0; cond < 2; cond++ {
      wg.Add(1)
      go func() {
        defer wg.Done()

        var tree *know.Graph
        if cond == 1 {
          tree = know.New()
        }

        for si, q := range []string{ pair.first, pair.second } {
          want := pair.firstWant
          if si == 1 {
            want = pair.secondWant
          }

          run := benchSession(t, client(), sb, tree, q, want)
          results[pi][cond][si] = run
          if dump != "" {
            name := fmt.Sprintf("%s/%s-%s-%d.md", dump, pair.name,
              []string{ "plain", "tree" }[cond], si + 1)
            os.WriteFile(name, []byte(fmt.Sprintf(
              "%s\n\nright %v, %d prompt, %d completion, %d rounds, %d tools\n\n"+
                "## answer\n%s\n\n## calls\n%s\n\n## tree after\n%s\n",
              q, run.right, run.prompt, run.completion, run.rounds,
              run.tools, run.answer, strings.Join(run.calls, "\n"), run.tree,
            )), 0o644)
          }
        }
      }()
    }
  }

  wg.Wait()

  var total [2][2]benchRun
  for pi, pair := range benchPairs {
    for cond := 0; cond < 2; cond++ {
      for si := 0; si < 2; si++ {
        r := results[pi][cond][si]
        t.Logf("%-13s %-7s s%d  right %-5v  in %7d  out %6d  rounds %2d  tools %2d",
          pair.name, []string{ "no tree", "tree" }[cond], si + 1, r.right,
          r.prompt, r.completion, r.rounds, r.tools)
        tot := &total[cond][si]
        tot.prompt += r.prompt
        tot.completion += r.completion
        tot.rounds += r.rounds
        tot.tools += r.tools
      }
    }
  }

  for cond := 0; cond < 2; cond++ {
    for si := 0; si < 2; si++ {
      right := 0
      for pi := range benchPairs {
        if results[pi][cond][si].right {
          right++
        }
      }

      tot := total[cond][si]
      t.Logf("TOTAL %-7s session %d: %d/%d right, in %d, out %d, rounds %d, tools %d",
        []string{ "no tree", "tree" }[cond], si + 1, right, len(benchPairs),
        tot.prompt, tot.completion, tot.rounds, tot.tools)
    }
  }
}

// benchSession runs one fresh session: armin's own system prompt (with
// the tree's section when there is a tree), one question, read-only.
func benchSession(
  t *testing.T, client *llm.Client, sb *sandbox.Config, tree *know.Graph,
  question string, want []string,
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
  system := buildSystemPrompt(basePrompt, sb.Root, mem, &notes.Skills{}, nil, tree)
  history := []llm.Message{
    { Role: "system", Content: system },
    { Role: "user", Content: question },
  }

  var run benchRun
  hooks := agent.Hooks{
    Usage: func(u llm.Usage) {
      run.prompt += u.PromptTokens
      run.completion += u.CompletionTokens
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

  answer, err := loop.Turn(ctx, &history, hooks)
  if err != nil {
    t.Logf("session failed: %v", err)
    return run
  }

  run.answer = answer
  if tree != nil {
    var b strings.Builder
    tree.Dump(&b, 0)
    run.tree = b.String()
  }

  run.right = true
  for _, w := range want {
    if !regexp.MustCompile(`(?i)` + w).MatchString(answer) {
      run.right = false
    }
  }

  return run
}
