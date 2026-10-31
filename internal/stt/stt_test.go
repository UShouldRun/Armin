package stt

import (
  "context"
  "encoding/binary"
  "os"
  "path/filepath"
  "strings"
  "testing"
  "time"
)

// wav builds a 16-bit mono WAV of samples.
func wav(samples func(i int) int16, n int) []byte {
  b := make([]byte, 44, 44 + 2 * n)
  copy(b, "RIFF")
  for i := 0; i < n; i++ {
    b = binary.LittleEndian.AppendUint16(b, uint16(samples(i)))
  }

  return b
}

func TestFlat(t *testing.T) {
  stuck := wav(func(int) int16 { return -32768 }, 32000)
  speech := wav(func(i int) int16 { return int16(i * 37 % 5000 - 2500) }, 32000)
  if !Flat(stuck) || Flat(speech) || !Flat(make([]byte, 44)) {
    t.Fatal("flat detection")
  }
}

func TestClean(t *testing.T) {
  got := Clean(
    " [BLANK_AUDIO]\n Hello there.\n (soft music) How are you? [MUSIC]\n",
  )
  if got != "Hello there. How are you?" {
    t.Fatalf("Clean = %q", got)
  }
}

func TestExpand(t *testing.T) {
  got := strings.Join(expand("whisper -f {wav} -l auto", "/t/a.wav"), " ")
  if got != "whisper -f /t/a.wav -l auto" {
    t.Fatal(got)
  }

  got = strings.Join(expand("transcribe --fast", "/t/a.wav"), " ")
  if got != "transcribe --fast /t/a.wav" {
    t.Fatal(got)
  }
}

// TestTranscribe runs a fake transcriber script.
func TestTranscribe(t *testing.T) {
  dir := t.TempDir()
  script := filepath.Join(dir, "fake-whisper")
  os.WriteFile(
    script,
    []byte("#!/bin/sh\necho '[BLANK_AUDIO]'\necho \"heard $2\"\n"),
    0o755,
  )
  speech := filepath.Join(dir, "s.wav")
  os.WriteFile(
    speech,
    wav(func(i int) int16 { return int16(i * 37 % 5000) }, 32000),
    0o644,
  )

  got, err := Transcribe(context.Background(), script + " -f {wav}", speech)
  if err != nil || got != "heard " + speech {
    t.Fatalf("Transcribe = %q, %v", got, err)
  }

  stuck := filepath.Join(dir, "stuck.wav")
  os.WriteFile(stuck, wav(func(int) int16 { return -32768 }, 32000), 0o644)
  _, err = Transcribe(context.Background(), script, stuck)
  if err == nil || !strings.Contains(err.Error(), "flat") {
    t.Fatalf("a stuck mic: %v", err)
  }

  _, err = Transcribe(context.Background(), "", speech)
  if err == nil || !strings.Contains(err.Error(), "stt") {
    t.Fatalf("no command: %v", err)
  }
}

// TestRecorder records with a fake recorder that writes a WAV and
// waits for SIGINT, as parec does.
func TestRecorder(t *testing.T) {
  dir := t.TempDir()
  script := filepath.Join(dir, "fake-rec")
  os.WriteFile(
    script,
    []byte(
      "#!/bin/sh\ntrap 'exit 0' INT\nhead -c 4000 /dev/urandom > " +
        "\"$1\"\nwhile :; do sleep 0.05; done\n",
    ),
    0o755,
  )

  r, err := Start(script + " {wav}", dir)
  if err != nil {
    t.Fatal(err)
  }

  time.Sleep(300 * time.Millisecond) // someone speaking
  path, err := r.Stop()
  if err != nil {
    t.Fatal(err)
  }

  defer os.Remove(path)
  st, _ := os.Stat(path)
  if st.Size() != 4000 {
    t.Fatalf("recorded %d bytes", st.Size())
  }

  silent := filepath.Join(dir, "silent-rec")
  os.WriteFile(
    silent, []byte("#!/bin/sh\necho 'no such device' >&2\nexit 1\n"), 0o755,
  )
  r, err = Start(silent, dir)
  if err != nil {
    t.Fatal(err)
  }

  time.Sleep(300 * time.Millisecond)
  _, err = r.Stop()
  if err == nil || !strings.Contains(err.Error(), "no such device") {
    t.Fatalf("a failing recorder: %v", err)
  }
}
