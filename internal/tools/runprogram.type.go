package tools

// RunProgramInput is the argument set for run_program.
type RunProgramInput struct {
  Binary string   `json:"binary"`
  Args   []string `json:"args,omitempty"`
  Dir    string   `json:"dir,omitempty"` // inside the project; default its root

  // Stdin is a project file fed to the program's input ("< file");
  // Stdout a project file its output goes to ("> file"), e.g. to diff
  // against an expected output after.
  Stdin  string `json:"stdin,omitempty"`
  Stdout string `json:"stdout,omitempty"`
}

// RunProgramOutput keeps the tail of the output, where compilers and
// test runners put the errors and the verdict.
type RunProgramOutput struct {
  Command   string `json:"command"`
  Dir       string `json:"dir,omitempty"`
  ExitCode  int    `json:"exit_code"`
  Output    string `json:"output"`
  Truncated bool   `json:"truncated,omitempty"`
}
