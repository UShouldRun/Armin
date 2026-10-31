// /voice: say the message instead of typing it. The microphone
// records until Enter, the stt command transcribes it, and the text
// comes back as the next prompt's draft, to correct before sending:
// a misheard word in an explanation would teach the advisor something
// false about what you know.

package repl

import (
  "context"
  "os"

  "armin/internal/stt"
  "armin/internal/ui"
)

func cmdVoice(r *REPL, ctx context.Context, _ []string) {
  rec, err := stt.Start(r.STTRecord, "")
  if err != nil {
    ui.Err(err)
    return
  }

  r.ask("  " + ui.Bold("● recording") + ui.Dim(" — Enter to stop "))
  path, err := rec.Stop()
  if err != nil {
    ui.Err(err)
    return
  }

  defer os.Remove(path)
  ui.Info("transcribing…")
  text, err := stt.Transcribe(ctx, r.STT, path)
  if err != nil {
    ui.Err(err)
    return
  }

  r.draft = text
  ui.Info("correct it below if needed, then Enter to send")
}
