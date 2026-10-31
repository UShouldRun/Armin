package know

import (
  "fmt"
  "strings"
  "testing"
)

func TestLine(t *testing.T) {
  g := New()
  sample(t, g)
  hs, _ := g.Lookup("tcp/handshake")
  seq, _ := g.Lookup("tcp/seqnum")
  win, _ := g.Lookup("tcp/window")
  mustDo(t, g.SetFlags(hs, FlagStale, 0))

  var b strings.Builder
  g.Line(&b, hs, false)
  want := `n2 tcp/handshake fact checked needs n3 used-by n4 ` +
    `"SYN, SYN-ACK, ACK" at tcp/conn.go:10-20 STALE`
  if b.String() != want {
    t.Fatalf("line:\n%s\nwant:\n%s", b.String(), want)
  }

  // stale nodes never say current; a fresh one does
  mustDo(t, g.SetFlags(hs, 0, FlagStale))
  b.Reset()
  g.Line(&b, hs, false)
  if !strings.Contains(b.String(), " at tcp/conn.go:10-20 current") {
    t.Fatalf("current missing: %s", b.String())
  }

  b.Reset()
  g.Line(&b, seq, false)
  if strings.Contains(b.String(), " at ") {
    t.Fatalf("a URL-only node carries an at-ref: %s", b.String())
  }

  if !strings.Contains(
    b.String(), "concept inferred minion-unverified used-by n2 n4",
  ) {
    t.Fatalf("minion line: %s", b.String())
  }

  mustDo(t, g.SetFlags(seq, FlagVerified, 0))
  b.Reset()
  g.Line(&b, seq, false)
  if strings.Contains(b.String(), "minion-unverified") {
    t.Fatal("a verified minion node is still marked unverified")
  }

  mustDo(t, g.SetClaim(win, strings.Repeat("word ", 40)))
  b.Reset()
  g.Line(&b, win, false)
  if !strings.Contains(b.String(), "…\"") || len(b.String()) > 200 {
    t.Fatalf("long claim not cut: %s", b.String())
  }

  b.Reset()
  g.Line(&b, win, true)
  if strings.Contains(b.String(), "…") {
    t.Fatal("a full line should not cut the claim")
  }
}

func TestOutline(t *testing.T) {
  g := New()
  sample(t, g)
  udp := need(g.AddNode(0, "udp", "", KindTopic, ConfGuess, 0))
  need(g.AddNode(udp, "datagram", "", KindConcept, ConfGuess, 0))
  got := g.Outline(300)
  want := "tcp (3): handshake, seqnum, window\nudp (1): datagram\n"
  if got != want {
    t.Fatalf("outline:\n%q\nwant:\n%q", got, want)
  }

  mustDo(t, g.SetFlags(udp, FlagPinned, 0))
  if !strings.HasPrefix(g.Outline(300), "udp") {
    t.Fatal("pinned branches come first")
  }

  for i := 0; i < 50; i++ {
    need(g.AddNode(0, fmt.Sprintf("topic%d", i), "", KindTopic, ConfGuess, 0))
  }

  got = g.Outline(200)
  if len(got) > 200 || !strings.Contains(got, "more\n") {
    t.Fatalf(
      "outline over the cap or missing the rest count (%d bytes):\n%s",
      len(got),
      got,
    )
  }
}

func TestLineListsEachNodeOnce(t *testing.T) {
  g := New()
  a := need(g.AddNode(0, "a", "", KindFact, ConfChecked, 0))
  b := need(g.AddNode(0, "b", "", KindFact, ConfChecked, 0))
  mustDo(t, g.AddEdge(b, a, EdgeRequires))
  mustDo(t, g.AddEdge(b, a, EdgeDerivedFrom))

  var line strings.Builder
  g.Line(&line, a, false)
  if !strings.HasSuffix(line.String(), "used-by n2") {
    t.Fatalf("duplicate in used-by: %s", line.String())
  }
}
