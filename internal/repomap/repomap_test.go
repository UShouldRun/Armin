package repomap

import (
  "context"
  "os"
  "os/exec"
  "path/filepath"
  "strings"
  "testing"

  "armin/internal/ctags"
)

// universalCtags is the binary, or the test is skipped.
func universalCtags(t *testing.T) string {
  t.Helper()
  p, err := exec.LookPath("universal-ctags")
  if err == nil {
    return p
  }

  home, _ := os.UserHomeDir()
  p = filepath.Join(home, ".local", "bin", "universal-ctags")
  _, err = os.Stat(p)
  if err != nil {
    t.Skip("no universal-ctags")
  }

  return p
}

func TestBuildRanksWhatIsUsed(t *testing.T) {
  bin := universalCtags(t)
  root := t.TempDir()
  write := func(name, src string) {
    os.WriteFile(filepath.Join(root, name), []byte(src), 0o644)
  }

  write(
    "store.go",
    "package kv\n\ntype Store struct{}\n\n" +
      "func (s *Store) Put(k string) {}\n\nfunc Open() *Store { return nil }\n",
  )
  write(
    "main.go",
    "package kv\n\nfunc main() {\n  s := Open()\n" +
      "  s.Put(\"a\")\n  s.Put(\"b\")\n}\n",
  )
  write("unused.go", "package kv\n\nfunc lonely() {}\n")

  got := Build(context.Background(), ctags.New(root, bin), 4000)
  if !strings.HasPrefix(got, "store.go\n") {
    t.Fatalf("the most used file should come first:\n%s", got)
  }

  if !strings.Contains(got, "Store.Put 5") ||
    strings.Index(got, "Store.Put") > strings.Index(got, "Open") {
    t.Fatalf("a method with its receiver, most used first:\n%s", got)
  }

  small := Build(context.Background(), ctags.New(root, bin), 40)
  if strings.Contains(small, "unused.go") || len(small) > 40 {
    t.Fatalf("the budget should cut the least used:\n%s", small)
  }
}

func TestEachIdent(t *testing.T) {
  var got []string
  eachIdent(
    []byte("s.Put(x_1, 2b) // ok"),
    func(w string) {
      got = append(got, w)
    },
  )

  if strings.Join(got, " ") != "s Put x_1 ok" {
    t.Fatalf("idents: %v", got)
  }
}
