// The llama.cpp side: whether the llama-server build knows the
// model's architecture (knowsArch, by finding its name in the build's
// libraries), and llama-bench's measured speeds to set beside the
// estimates (bench).

package install

import (
  "bytes"
  "context"
  "encoding/json"
  "fmt"
  "os"
  "os/exec"
  "path/filepath"
  "time"
)

const benchTimeout = 3 * time.Minute

// knowsArch looks for arch's name in the llama-server binary and the
// libllama next to it, where llama.cpp keeps its table of names.
// checked is false when there was nothing to look in.
func knowsArch(binary, arch string) (known, checked bool) {
  path, err := exec.LookPath(binary)
  if err != nil {
    return false, false
  }

  path, err = filepath.EvalSymlinks(path)
  if err != nil {
    return false, false
  }

  dir := filepath.Dir(path)
  files := []string{ path }
  for _, pattern := range []string{
    filepath.Join(dir, "libllama*.so*"),
    filepath.Join(dir, "..", "lib", "libllama*.so*"),
    filepath.Join(dir, "libllama*.dylib"),
  } {
    found, _ := filepath.Glob(pattern)
    files = append(files, found...)
  }

  // A string table may share tails ("…qwen3" ending another name),
  // so only the terminating NUL is required: a miss is then strong
  // evidence the build predates the architecture.
  needle := append([]byte(arch), 0)
  for _, f := range files {
    b, err := os.ReadFile(f)
    if err != nil {
      continue
    }

    checked = true
    if bytes.Contains(b, needle) {
      return true, true
    }
  }

  return false, checked
}

// bench runs llama-bench (beside binary, or on PATH) with the plan's
// placement and returns measured prefill and generation tokens/s.
func bench(ctx context.Context, binary, model string, p *Plan) (pre, gen float64, err error) {
  tool := filepath.Join(filepath.Dir(binary), "llama-bench")
  _, statErr := os.Stat(tool)
  if statErr != nil {
    tool, err = exec.LookPath("llama-bench")
    if err != nil {
      return 0, 0, fmt.Errorf("\n\t... in bench:\n\t llama-bench not found next to %s or on PATH", binary)
    }
  }

  args := []string{
    "-m", model, "-ngl", fmt.Sprint(p.GPULayers), "-fa", "on",
    "-ctk", "q8_0", "-ctv", "q8_0", "-p", fmt.Sprint(ubatch), "-n", "64",
    "-r", "1", "-o", "json",
  }

  if p.Place == PlaceGPU {
    args[3] = "999"
  }

  if p.CPUMoE > 0 {
    args = append(args, "-ncmoe", fmt.Sprint(p.CPUMoE))
  }

  ctx, cancel := context.WithTimeout(ctx, benchTimeout)
  defer cancel()

  var stderr bytes.Buffer
  cmd := exec.CommandContext(ctx, tool, args...)
  cmd.Stderr = &stderr
  out, err := cmd.Output()
  if err != nil {
    return 0, 0, fmt.Errorf("\n\t... in bench:\n\t %s: %v\n%s", tool, err, lastLines(stderr.String(), 5))
  }

  var rows []benchResult
  err = json.Unmarshal(out, &rows)
  if err != nil {
    return 0, 0, fmt.Errorf("\n\t... in bench:\n\t reading llama-bench's JSON: %w", err)
  }

  for _, r := range rows {
    switch {
    case r.NPrompt > 0 && r.NGen == 0:
      pre = r.AvgTS
    case r.NGen > 0 && r.NPrompt == 0:
      gen = r.AvgTS
    }
  }

  return pre, gen, nil
}

func lastLines(s string, n int) string {
  lines := bytes.Split(bytes.TrimSpace([]byte(s)), []byte("\n"))
  if len(lines) > n {
    lines = lines[len(lines) - n:]
  }

  return string(bytes.Join(lines, []byte("\n")))
}
