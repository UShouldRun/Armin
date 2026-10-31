// Package stt turns speech into text with programs the user already
// has, the way armin uses rg: a recorder writes a WAV, a speech-to-text
// command (whisper.cpp's whisper-cli, say) reads it. Both are command
// lines from config.conf where {wav} stands for the file; without
// {wav} the path is appended.
//
//   Start, Stop    record from the default microphone
//   Transcribe     run the command on a WAV and clean what it printed
//   Flat           a recording with no sound in it (a stuck microphone)
//
// No audio code in armin itself, and no dependency.
package stt

import (
  "bytes"
  "context"
  "errors"
  "fmt"
  "os"
  "os/exec"
  "path/filepath"
  "regexp"
  "strings"
  "syscall"
  "time"
)

// transcribeTimeout bounds one transcription; a long recording on the
// CPU takes a while, a hung command shouldn't take forever.
const transcribeTimeout = 5 * time.Minute

// stopWait is how long a recorder gets to finish its file after SIGINT.
const stopWait = 3 * time.Second

// recorders are tried in order when no recorder is configured: each
// records 16 kHz mono 16-bit, what whisper wants.
var recorders = []string{
  "parec --rate=16000 --channels=1 --format=s16le --file-format=wav {wav}",
  "pw-record --rate 16000 --channels 1 --format s16 {wav}",
  "arecord -q -f S16_LE -r 16000 -c 1 {wav}",
  "rec -q -r 16000 -c 1 -b 16 {wav}",
}

// DefaultRecorder is the first recorder found on PATH, or "".
func DefaultRecorder() string {
  for _, r := range recorders {
    _, err := exec.LookPath(strings.Fields(r)[0])
    if err == nil {
      return r
    }
  }

  return ""
}

// expand splits a command line and puts path where {wav} is.
func expand(template, path string) []string {
  args := strings.Fields(template)
  found := false
  for i, a := range args {
    if strings.Contains(a, "{wav}") {
      args[i] = strings.ReplaceAll(a, "{wav}", path)
      found = true
    }
  }

  if !found {
    args = append(args, path)
  }

  return args
}

// Start records into a new WAV under dir with the recorder command
// (DefaultRecorder when empty) until Stop.
func Start(recorder, dir string) (*Recorder, error) {
  if recorder == "" {
    recorder = DefaultRecorder()
  }

  if recorder == "" {
    return nil, errors.New("\n\t... in Start:\n\tno recorder: install parec, " +
      "pw-record or arecord, or set stt_record in config.conf")
  }

  f, err := os.CreateTemp(dir, "armin-voice-*.wav")
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Start:\n\t%w", err)
  }

  path := f.Name()
  f.Close()
  args := expand(recorder, path)
  cmd := exec.Command(args[0], args[1:]...)
  var stderr bytes.Buffer
  cmd.Stderr = &stderr
  err = cmd.Start()
  if err != nil {
    os.Remove(path)
    return nil, fmt.Errorf("\n\t... in Start:\n\t%s: %w", args[0], err)
  }

  return &Recorder{ cmd: cmd, path: path, stderr: &stderr }, nil
}

// Stop ends the recording and returns the WAV's path; the caller
// removes it. A recorder writes its WAV header on exit, so it gets
// SIGINT and a moment to finish before it is killed.
func (r *Recorder) Stop() (string, error) {
  r.cmd.Process.Signal(syscall.SIGINT)
  done := make(chan error, 1)
  go func() { done <- r.cmd.Wait() }()
  select {
  case <-done:
  case <-time.After(stopWait):
    r.cmd.Process.Kill()
    <-done
  }

  st, err := os.Stat(r.path)
  if err != nil || st.Size() <= 44 {
    os.Remove(r.path)
    msg := strings.TrimSpace(r.stderr.String())
    if msg == "" {
      msg = "nothing was recorded"
    }

    return "", fmt.Errorf("\n\t... in Stop:\n\t%s", msg)
  }

  return r.path, nil
}

// Flat reports whether a 16-bit WAV holds no sound: a stuck
// microphone records one value over and over, which whisper turns
// into "Thank you." Real sound, even quiet, has thousands of distinct
// sample values in its first seconds.
func Flat(wav []byte) bool {
  const header, window, enough = 44, 64000, 100
  if len(wav) <= header {
    return true
  }

  data := wav[header:min(len(wav), header + window)]
  seen := map[uint16]bool{}
  for i := 0; i + 1 < len(data); i += 2 {
    seen[uint16(data[i]) | uint16(data[i + 1]) << 8] = true
    if len(seen) >= enough {
      return false
    }
  }

  return true
}

// tags are whisper's non-speech markers: [BLANK_AUDIO], [MUSIC] ...
var tags = regexp.MustCompile(
  `\[[A-Z_ ]*\]|\([A-Za-z ]*(music|silence|noise)[A-Za-z ]*\)`,
)

// Clean joins what a transcriber printed into one line without its
// non-speech markers.
func Clean(text string) string {
  text = tags.ReplaceAllString(text, " ")

  return strings.Join(strings.Fields(text), " ")
}

// Transcribe runs the speech-to-text command on a WAV and returns the
// cleaned text.
func Transcribe(ctx context.Context, command, wav string) (string, error) {
  if strings.TrimSpace(command) == "" {
    return "", errors.New("\n\t... in Transcribe:\n\tno stt command: set stt " +
      "in " +
      "config.conf, e.g. stt = whisper-cli -m ggml-large-v3-turbo.bin -f " +
        "{wav} -nt -np")
  }

  data, err := os.ReadFile(wav)
  if err != nil {
    return "", fmt.Errorf("\n\t... in Transcribe:\n\t%w", err)
  }

  if Flat(data) {
    return "", errors.New("\n\t... in Transcribe:\n\tthe microphone recorded " +
      "a flat " +
      "signal (no sound); check the input device, or reload its driver")
  }

  ctx, cancel := context.WithTimeout(ctx, transcribeTimeout)
  defer cancel()

  args := expand(command, wav)
  cmd := exec.CommandContext(ctx, args[0], args[1:]...)
  cmd.Dir = filepath.Dir(wav)
  var stderr bytes.Buffer
  cmd.Stderr = &stderr
  out, err := cmd.Output()
  if err != nil {
    tail := strings.TrimSpace(stderr.String())
    if len(tail) > 400 {
      tail = tail[len(tail) - 400:]
    }

    return "", fmt.Errorf(
      "\n\t... in Transcribe:\n\t%s: %w: %s", args[0], err, tail,
    )
  }

  text := Clean(string(out))
  if text == "" {
    return "", errors.New("\n\t... in Transcribe:\n\theard nothing")
  }

  return text, nil
}
