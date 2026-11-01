// Session commands: /sessions, /resume, /title, /save, /history and
// /clear, on the conversations internal/session keeps on disk.

package repl

import (
  "context"
  "fmt"
  "os"
  "path/filepath"
  "strings"
  "time"

  "armin/internal/session"
  "armin/internal/share"
  "armin/internal/ui"
)

func cmdClear(r *REPL, _ context.Context, _ []string) {
  if len(r.History) == 0 {
    return
  }

  r.History = r.History[:1] // keep the system prompt
  r.lastTokens = 0
  resyncSession(r)
  ui.Info("conversation cleared")
  r.emit(share.Event{ Type: "cleared", Origin: originTerminal })
}

// resyncSession does an immediate full session rewrite and resets
// the append-cursor, for the rare commands that mutate History in
// place (rather than only appending to it) — /clear and /compact.
// Without this, the next autosave's Append would either miss the
// rewrite entirely (if persisted still exceeds the new, shorter
// History) or append onto a log that no longer matches in-memory
// state.
func resyncSession(r *REPL) {
  if r.state & flagAutosave == 0 {
    r.persisted = 0
    return
  }

  err := session.Save(&r.Meta, r.History)
  if err != nil {
    ui.Error("failed to resync session: %s", err)
    return
  }

  r.persisted = len(r.History)
}

func cmdHistory(r *REPL, _ context.Context, _ []string) {
  for i, m := range r.History {
    preview := strings.ReplaceAll(m.Content, "\n", " ")
    if len(preview) > 100 {
      preview = preview[:100] + "…"
    }

    fmt.Printf("  %3d  %-9s %s\n", i, m.Role, ui.Dim(preview))
  }
}

func cmdSave(r *REPL, _ context.Context, _ []string) {
  err := session.Save(&r.Meta, r.History)
  if err != nil {
    ui.Err(err)
    return
  }

  // A full Save just wrote everything — keep the append-cursor in
  // sync, or the next autosave would redundantly re-append messages
  // this Save already wrote, duplicating lines in the log.
  r.persisted = len(r.History)
  ui.Info("session saved")
}

func cmdSessions(r *REPL, _ context.Context, args []string) {
  if len(args) >= 1 && (args[0] == "rm" || args[0] == "delete") {
    if len(args) < 2 {
      ui.Error("usage: /sessions rm <id>")
      return
    }

    r.deleteSession(args[1])

    return
  }

  list, err := session.List()
  if err != nil {
    ui.Err(err)
    return
  }

  if len(list) == 0 {
    ui.Info("no saved sessions yet")
    return
  }

  fmt.Println(ui.Bold("sessions"))
  PrintSessions(list, r.Root, r.Meta.ID, false)
}

// PrintSessions renders a session table: directory, title, short id,
// age and size; * marks root's sessions, → the current one. numbered
// adds menu numbers.
func PrintSessions(list []session.Meta, root, current string, numbered bool) {
  if len(list) > sessionRows {
    list = list[:sessionRows]
  }

  dirW, titleW := 0, 0
  for _, m := range list {
    dirW = max(dirW, len(filepath.Base(m.Root)))
    titleW = max(titleW, min(len(titleOr(m.Title)), 44))
  }

  for i, m := range list {
    mark := "  "
    switch {
    case m.ID == current:
      mark = ui.Green("→ ")
    case m.Root == root:
      mark = ui.Cyan("* ")
    }

    num := ""
    if numbered {
      num = fmt.Sprintf("%2d  ", i + 1)
    }

    title := titleOr(m.Title)
    if len(title) > 44 {
      title = title[:43] + "…"
    }

    fmt.Printf(
      "  %s%s%-*s  %-*s  %s  %s\n",
      mark, num, dirW, filepath.Base(m.Root), titleW, title,
      ui.Dim(ShortID(m.ID)),
      ui.Dim(fmt.Sprintf("%s · %d msgs", ago(m.UpdatedAt), m.Messages)),
    )
  }
}

// ago renders a time as a short age: 5m, 3h, 2d, Jan 2.
func ago(t time.Time) string {
  d := time.Since(t)
  switch {
  case d < time.Hour:
    return fmt.Sprintf("%dm ago", max(int(d.Minutes()), 1))
  case d < 24 * time.Hour:
    return fmt.Sprintf("%dh ago", int(d.Hours()))
  case d < 7 * 24 * time.Hour:
    return fmt.Sprintf("%dd ago", int(d.Hours() / 24))
  }

  return t.Format("Jan 2")
}

// deleteSession removes a saved session and its scratchpad folder,
// after asking. Memories stay: they belong to the project.
func (r *REPL) deleteSession(id string) {
  saved, err := session.Load(id)
  if err != nil {
    ui.Err(err)
    return
  }

  if saved.ID == r.Meta.ID {
    ui.Error("that's the session you're in — /clear starts over, or delete " +
      "it from another session")
    return
  }

  detail := fmt.Sprintf(
    "(%s, %s, %d msgs) and its scratchpad? [y/N]",
    ShortID(saved.ID), filepath.Base(saved.Root), saved.Messages,
  )
  answer, _ := r.ask(fmt.Sprintf("%s %s %s ", ui.Yellow("delete"), titleOr(saved.Title), ui.Dim(detail)))
  a := strings.ToLower(strings.TrimSpace(answer))
  if a != "y" && a != "yes" {
    ui.Info("kept")
    return
  }

  m, err := session.Delete(saved.ID)
  if err != nil {
    ui.Err(err)
    return
  }

  scratch := "no scratchpad"
  if isUUID(m.ID) {
    dir, err := session.ProjectDataDir("scratch", m.Root)
    if err == nil {
      err := os.RemoveAll(filepath.Join(dir, m.ID))
      if err == nil {
        scratch = "its scratchpad"
      }
    }
  }

  ui.Info("deleted %s (%s) and %s", titleOr(m.Title), ShortID(m.ID), scratch)
}

func cmdResume(r *REPL, _ context.Context, args []string) {
  if len(args) == 0 {
    ui.Error("usage: /resume <id> — /sessions lists them")
    return
  }

  saved, err := session.Load(args[0])
  if err != nil {
    ui.Err(err)
    return
  }

  if saved.Root != r.Root {
    ui.Error(
      "that session belongs to %s — run: armin -resume %s",
      saved.Root, ShortID(saved.ID),
    )

    return
  }

  // Keep today's system prompt (memories, skills, model line).
  if len(saved.History) > 0 && saved.History[0].Role == "system" {
    saved.History[0] = r.History[0]
  }

  r.History = saved.History
  r.Meta = saved.Meta
  r.BindScratch()
  r.persisted = 0
  r.lastTokens = 0
  r.Loop.ElideOld(r.History)
  ui.Info("resumed %s (%s), %d messages", titleOr(r.Meta.Title), ShortID(r.Meta.ID), len(r.History))
}

func cmdTitle(r *REPL, _ context.Context, args []string) {
  if len(args) == 0 {
    ui.Info("title: %s", titleOr(r.Meta.Title))
    return
  }

  r.Meta.Title = strings.Join(args, " ")
  r.persisted = 0
  r.maybeAutosave()
  ui.Info("renamed to %s", r.Meta.Title)
}
