package think

import (
  "strings"
  "testing"

  "armin/internal/know"
)

func TestKnowSection(t *testing.T) {
  g := know.New()
  got := knowSection(g)
  if !strings.Contains(got, "Empty so far") ||
      !strings.Contains(got, "know_find first") {
    t.Fatalf("empty tree section:\n%s", got)
  }

  tcp, _ := g.AddNode(0, "tcp", "", know.KindTopic, know.ConfChecked, 0)
  g.AddNode(
    tcp, "handshake", "SYN, SYN-ACK, ACK", know.KindFact, know.ConfChecked, 0,
  )
  got = knowSection(g)
  if !strings.Contains(got, "tcp (1): handshake\n") {
    t.Fatalf("outline missing:\n%s", got)
  }

  // the section is a pure function of the tree: same tree, same bytes
  if knowSection(g) != got {
    t.Fatal("the section isn't byte-stable")
  }
}
