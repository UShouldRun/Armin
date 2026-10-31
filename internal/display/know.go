// -know: the project's knowledge tree without a session.
//
//   dump[=path]       print the tree (or the subtree at path) as text
//   edit[=path]       open that text in $EDITOR and load it back
//   import-memories   offer each project memory that cites code as a
//                     node, and move the accepted ones into the tree

package display

import (
  "bufio"
  "fmt"
  "os"
  "os/exec"
  "path/filepath"
  "strings"

  "armin/internal/agent"
  "armin/internal/input"
  "armin/internal/know"
  "armin/internal/notes"
  "armin/internal/session"
  "armin/internal/think"
  "armin/internal/ui"
)

// Know runs one -know command and exits.
func Know(opts *input.Options) error {
  g, err := think.OpenKnow(opts.Root)
  if err != nil {
    return fmt.Errorf("\n\t... in Know:\n\t%w", err)
  }

  defer g.Close()

  cmd, arg, _ := strings.Cut(opts.Know, "=")
  if cmd != "dump" && g.ReadOnly() {
    return fmt.Errorf(
      "\n\t... in Know:\n\tanother armin holds this project's tree; close it " +
        "first",
    )
  }

  switch cmd {
  case "dump":
    id, err := knowPath(g, arg)
    if err != nil {
      return err
    }

    return g.Dump(os.Stdout, id)

  case "edit":
    id, err := knowPath(g, arg)
    if err != nil {
      return err
    }

    err = editKnow(g, id)
    if err != nil {
      return err
    }

  case "import-memories":
    err = importMemories(g, opts.Root)
    if err != nil {
      return err
    }

  default:
    return fmt.Errorf(
      "\n\t... in Know:\n\t-know %q: want dump[=path], edit[=path] or " +
        "import-memories",
      opts.Know,
    )
  }

  if g.Changed() {
    return g.Compact()
  }

  return nil
}

func knowPath(g *know.Graph, path string) (know.NodeID, error) {
  id, ok := g.Lookup(strings.Trim(path, "/"))
  if !ok {
    return 0, fmt.Errorf("\n\t... in Know:\n\tno node at %q", path)
  }

  return id, nil
}

// editKnow dumps the subtree to a temporary file, opens the editor,
// and loads the result; on a mistake it says what and offers the
// file again.
func editKnow(g *know.Graph, id know.NodeID) error {
  f, err := os.CreateTemp("", "armin-know-*.txt")
  if err != nil {
    return fmt.Errorf("\n\t... in editKnow:\n\t%w", err)
  }

  path := f.Name()
  defer os.Remove(path)

  err = g.Dump(f, id)
  f.Close()
  if err != nil {
    return fmt.Errorf("\n\t... in editKnow:\n\t%w", err)
  }

  editor := os.Getenv("VISUAL")
  if editor == "" {
    editor = os.Getenv("EDITOR")
  }

  if editor == "" {
    editor = "vi"
  }

  in := bufio.NewReader(os.Stdin)
  for {
    args := append(strings.Fields(editor), path)
    cmd := exec.Command(args[0], args[1:]...)
    cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
    err = cmd.Run()
    if err != nil {
      return fmt.Errorf("\n\t... in editKnow:\n\t%s: %w", editor, err)
    }

    text, err := os.Open(path)
    if err != nil {
      return fmt.Errorf("\n\t... in editKnow:\n\t%w", err)
    }

    res, err := g.Edit(text, 0) // author 0 is the user
    text.Close()
    if err == nil {
      ui.Info(
        "%d added, %d changed, %d deleted", res.Added, res.Changed, res.Deleted,
      )
      return nil
    }

    ui.Error("%s", lastLine(err))
    fmt.Print("edit again? [Y/n] ")
    answer, _ := in.ReadString('\n')
    if strings.HasPrefix(strings.ToLower(strings.TrimSpace(answer)), "n") {
      return nil
    }
  }
}

// importMemories walks the project's memories; each that cites a file
// and line is offered as a fact node, and moved there if accepted.
func importMemories(g *know.Graph, root string) error {
  dir, err := session.ProjectDataDir("memory", root)
  if err != nil {
    return fmt.Errorf("\n\t... in importMemories:\n\t%w", err)
  }

  data, err := session.DataDir()
  if err != nil {
    return fmt.Errorf("\n\t... in importMemories:\n\t%w", err)
  }

  mem := &notes.Memories{
    ProjectDir: dir, UserDir: filepath.Join(data, "memory", "user"),
  }
  all, err := mem.List()
  if err != nil {
    return fmt.Errorf("\n\t... in importMemories:\n\t%w", err)
  }

  in := bufio.NewReader(os.Stdin)
  moved, offered := 0, 0
  for _, m := range all {
    if m.Scope != notes.ScopeProject {
      continue
    }

    _, body, err := mem.Read(m.Name)
    if err != nil || !agent.CitesFileLine(body) {
      continue
    }

    offered++
    path := "memories/" + m.Name
    fmt.Printf(
      "\n%s — %s\n%s\n",
      ui.Bold(m.Name),
      m.Description,
      ui.Dim(strings.TrimSpace(body)),
    )
    fmt.Printf("as fact node [%s], another path, or s to skip: ", path)
    answer, _ := in.ReadString('\n')
    answer = strings.TrimSpace(answer)
    if answer == "s" {
      continue
    }

    if answer != "" {
      path = strings.Trim(answer, "/")
    }

    err = addFact(g, path, strings.TrimSpace(body))
    if err != nil {
      ui.Error("%s", lastLine(err))
      continue
    }

    _, err = mem.Delete(m.Name)
    if err != nil {
      ui.Error("added to the tree, but the memory stays: %s", lastLine(err))
      continue
    }

    moved++
  }

  ui.Info("%d of %d code-fact memories moved into the tree", moved, offered)

  return nil
}

// addFact adds a user-written fact at path, adding missing levels as
// topics.
func addFact(g *know.Graph, path, claim string) error {
  parts := strings.Split(path, "/")
  id := know.NodeID(0)
  for _, part := range parts[:len(parts) - 1] {
    next, ok := g.Child(id, part)
    if !ok {
      var err error
      next, err = g.AddNode(id, part, "", know.KindTopic, know.ConfChecked, 0)
      if err != nil {
        return err
      }
    }

    id = next
  }

  _, err := g.AddNode(
    id, parts[len(parts) - 1], claim, know.KindFact, know.ConfInferred, 0,
  )

  return err
}

func lastLine(err error) string {
  msg := strings.TrimSpace(err.Error())
  i := strings.LastIndex(msg, "\n")

  return strings.TrimSpace(msg[i + 1:])
}
