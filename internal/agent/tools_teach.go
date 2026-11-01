package agent

// The advisor's teaching tools, on the learner tree: what the user
// knows (package learner).
//
//   learn_explained   record what one of the user's explanations shows:
//                     concepts used correctly, misconceptions (each a
//                     question node, so teaching targets it), gaps

import (
  "context"
  "fmt"
  "strings"
  "time"

  "armin/internal/know"
  "armin/internal/learner"
  "armin/internal/llm"
)

var teachTools = []toolSpec{
  {
    id:    toolLearnExplained,
    flags: flagAgent | flagSession | flagLearner,
    roles: RoleAdvisor,
    run:   runLearnExplained,
    def: toolFunc(
      "learn_explained",
      "After the user explains how they think about something (typed or " +
        "spoken), record what it shows about what they know: concepts " +
        "they used correctly, misconceptions, and gaps (concepts the " +
        "explanation needed but never touched). Concepts are paths in the " +
        "learner tree; a concept not there yet needs its definition in " +
        "define.",
      map[string]llm.ToolProperty{
        "correct": {
          Type:        "array",
          Description: "Concepts used correctly",
          Items:       &llm.ToolProperty{ Type: "string" },
        },
        "misconceptions": {
          Type:        "array",
          Description: "\"concept: what was misunderstood\", one each",
          Items:       &llm.ToolProperty{ Type: "string" },
        },
        "gaps": {
          Type:        "array",
          Description: "Concepts the explanation needed but never touched",
          Items:       &llm.ToolProperty{ Type: "string" },
        },
        "define": {
          Type:        "array",
          Description: "\"path: definition\" for concepts to add to the " +
            "learner tree first",
          Items:       &llm.ToolProperty{ Type: "string" },
        },
      },
    ),
  },
}

func runLearnExplained(_ context.Context, l *Loop, c toolCall) any {
  in, bad := decode[explainedArgs](c)
  if bad != nil {
    return bad
  }

  l.knowMu.Lock()
  defer l.knowMu.Unlock()

  tree := l.Learner.Tree
  author, err := tree.Intern(know.RoleAdvisor, l.model(), l.Session, 0)
  if err != nil {
    return knowErr(err)
  }

  for _, d := range in.Define {
    path, def, ok := strings.Cut(d, ":")
    path, def = strings.Trim(
      strings.TrimSpace(path), "/",
    ), strings.TrimSpace(def)
    if !ok || path == "" || def == "" {
      return errResult(fmt.Errorf("define %q: want \"path: definition\"", d))
    }

    _, exists := tree.Lookup(path)
    if exists {
      continue
    }

    err = addConcept(tree, path, def, author)
    if err != nil {
      return knowErr(err)
    }
  }

  // resolve everything before recording anything
  find := func(path string) (know.NodeID, error) {
    id, ok := tree.Lookup(strings.Trim(strings.TrimSpace(path), "/"))
    if !ok {
      return 0, fmt.Errorf(
        "%q isn't in the learner tree: add it with define", path,
      )
    }

    return id, nil
  }

  type mark struct {
    id     know.NodeID
    result learner.Result
    note   string
  }

  var marks []mark
  for _, p := range in.Correct {
    id, err := find(p)
    if err != nil {
      return errResult(err)
    }

    marks = append(marks, mark{ id: id, result: learner.Right })
  }

  for _, m := range in.Misconceptions {
    path, what, ok := strings.Cut(m, ":")
    if !ok || strings.TrimSpace(what) == "" {
      return errResult(
        fmt.Errorf(
          "misconception %q: want \"concept: what was misunderstood\"", m,
        ),
      )
    }

    id, err := find(path)
    if err != nil {
      return errResult(err)
    }

    marks = append(
      marks,
      mark{ id: id, result: learner.Wrong, note: strings.TrimSpace(what) },
    )
  }

  for _, p := range in.Gaps {
    id, err := find(p)
    if err != nil {
      return errResult(err)
    }

    marks = append(marks, mark{ id: id, result: learner.Partial })
  }

  now := time.Now()
  var questions []know.NodeID
  for _, m := range marks {
    err = l.Learner.Record(
      m.id, learner.EventExplanation, m.result, learner.SureMostly, now,
    )
    if err != nil {
      return knowErr(err)
    }

    if m.note == "" {
      continue
    }

    q, err := misconception(tree, m.id, m.note, author)
    if err != nil {
      return knowErr(err)
    }

    questions = append(questions, q)
  }

  out := map[string]string{
    "recorded": fmt.Sprintf("%d correct, %d misconceptions, %d gaps",
      len(in.Correct), len(in.Misconceptions), len(in.Gaps)),
  }

  if len(questions) > 0 {
    var b strings.Builder
    for i, q := range questions {
      if i > 0 {
        b.WriteByte('\n')
      }

      tree.Line(&b, q, false)
    }

    out["misconceptions"] = b.String()
    out["next"] = "teach to these before going on"
  }

  return out
}

func (l *Loop) model() string {
  if l.LLM == nil {
    return ""
  }

  return l.LLM.Model
}

// addConcept adds a concept at path, missing levels as topics.
func addConcept(
  tree *know.Graph, path, def string, author know.AuthorID,
) error {
  parts := strings.Split(path, "/")
  id := know.NodeID(0)
  for _, part := range parts[:len(parts) - 1] {
    next, ok := tree.Child(id, part)
    if !ok {
      var err error
      next, err = tree.AddNode(
        id, part, "", know.KindTopic, know.ConfChecked, author,
      )
      if err != nil {
        return err
      }
    }

    id = next
  }

  _, err := tree.AddNode(
    id, parts[len(parts) - 1], def, know.KindConcept, know.ConfChecked, author,
  )

  return err
}

// misconception adds a question node under the concept naming what
// was misunderstood.
func misconception(
  tree *know.Graph, concept know.NodeID, what string, author know.AuthorID,
) (know.NodeID, error) {
  name := ""
  for i := 1; ; i++ {
    name = fmt.Sprintf("misconception-%d", i)
    _, taken := tree.Child(concept, name)
    if !taken {
      break
    }
  }

  q, err := tree.AddNode(
    concept, name, what, know.KindQuestion, know.ConfChecked, author,
  )
  if err != nil {
    return 0, err
  }

  return q, tree.AddEdge(q, concept, know.EdgeExplains)
}
