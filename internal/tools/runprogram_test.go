package tools

import (
  "context"
  "os"
  "path/filepath"
  "strings"
  "testing"

  "armin/internal/sandbox"
)

// TestRunProgramStdinStdout: a file fed as input and the output
// saved to a file — mpirun -np 1 ./fox < input6 > out6, then diff.
func TestRunProgramStdinStdout(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "in.txt", "hello\n")

  out, err := RunProgram(context.Background(), cfg, RunProgramInput{
    Binary: "sh", Args: []string{ "-c", "tr a-z A-Z" },
    Stdin: "in.txt", Stdout: "out.txt",
  })
  if err != nil {
    t.Fatal(err)
  }

  got, _ := os.ReadFile(filepath.Join(cfg.Root, "out.txt"))
  if string(got) != "HELLO\n" || out.ExitCode != 0 {
    t.Fatalf("out.txt = %q, exit %d", got, out.ExitCode)
  }

  if !strings.Contains(out.Output, "stdout written to out.txt") || !strings.HasSuffix(out.Command, "< in.txt > out.txt") {
    t.Fatalf("output = %+v", out)
  }

  // A shell pipe, its script not taken for paths.
  out, err = RunProgram(context.Background(), cfg, RunProgramInput{
    Binary: "bash", Args: []string{ "-c", "tr a-z A-Z < in.txt | rev" },
  })
  if err != nil || strings.TrimSpace(out.Output) != "OLLEH" {
    t.Fatalf("pipe = %+v, %v", out, err)
  }

  _, err = RunProgram(context.Background(), cfg, RunProgramInput{
    Binary: "sh", Args: []string{ "-c", "cat" }, Stdin: "/etc/passwd",
  })
  if err == nil {
    t.Fatal("stdin outside the project must be refused")
  }

  if !sandbox.Privileged("bash") || !sandbox.Privileged("sh") {
    t.Fatal("shells must always be asked")
  }
}

func TestIsText(t *testing.T) {
  dir := t.TempDir()
  src := filepath.Join(dir, "a.py")
  bin := filepath.Join(dir, "fox")
  os.WriteFile(src, []byte("print('ç')\n"), 0o644)
  os.WriteFile(bin, []byte("\x7fELF\x02\x01\x01\x00\x00junk"), 0o755)

  if !IsText(src) || IsText(bin) || IsText(filepath.Join(dir, "missing")) {
    t.Fatal("IsText misjudged")
  }
}
