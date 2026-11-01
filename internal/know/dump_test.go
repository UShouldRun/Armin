package know

import (
  "bytes"
  "strings"
  "testing"
)

func TestDumpFormat(t *testing.T) {
  g := New()
  sample(t, g)
  hs, _ := g.Lookup("tcp/handshake")
  mustDo(t, g.SetFlags(hs, FlagStale | FlagVerified, 0))

  got := dumpAll(t, g)
  for _, want := range []string{
    `author a1 minion model "qwen3.5-9b" session "s1" delegation 7`,
    "n2 tcp/handshake fact checked a0 born 2026-10-31 seen 2026-10-31",
    `  claim "SYN, SYN-ACK, ACK"`,
    "  flags stale verified",
    "  requires n3",
    `  evidence file "tcp/conn.go:10-20" deadbeef`,
    "  requires n3\n  derived-from n2",
  } {
    if !strings.Contains(got, want) {
      t.Errorf("dump lacks %q:\n%s", want, got)
    }
  }
}

func TestEditUnchangedIsANoOp(t *testing.T) {
  g := New()
  sample(t, g)
  before := dumpAll(t, g)

  res := need(g.Edit(strings.NewReader(before), 0))
  if res != (EditResult{}) || dumpAll(t, g) != before {
    t.Fatalf("an unchanged dump changed something: %+v", res)
  }
}

func TestEdit(t *testing.T) {
  g := New()
  sample(t, g)
  text := dumpAll(t, g)

  // rename handshake and rewrite its claim and confidence, drop
  // window's derived-from edge, add a node with an edge to the
  // renamed one, and delete window by flag
  text = strings.Replace(
    text,
    "n2 tcp/handshake fact checked",
    "n2 tcp/hs fact tested", 1,
  )
  text = strings.Replace(
    text,
    `claim "SYN, SYN-ACK, ACK"`,
    `claim "three segments"`, 1,
  )
  text = strings.Replace(text, "  derived-from n2\n", "", 1)
  text += "\nnew tcp/close concept guess\n  claim \"FIN\"\n  requires tcp/hs\n"
  head := "n4 tcp/window concept guess a1 born 2026-10-31 seen 2026-10-31\n"
  text = strings.Replace(text, head, head + "  flags deleted\n", 1)

  res := need(g.Edit(strings.NewReader(text), 0))
  if res.Added != 1 || res.Changed != 1 || res.Deleted != 1 {
    t.Fatalf("result: %+v", res)
  }

  hs, ok := g.Lookup("tcp/hs")
  if !ok || g.Claim(hs) != "three segments" || g.Node(hs).Conf() != ConfTested {
    t.Fatalf("hs: %v %q", ok, g.Claim(hs))
  }

  if g.Node(hs).Flags & FlagUserEdited == 0 {
    t.Fatal("an edited node should be flagged user-edited")
  }

  cl, ok := g.Lookup("tcp/close")
  if !ok || !g.HasEdge(cl, hs, EdgeRequires) {
    t.Fatal("new node or its edge missing")
  }

  if g.Live(4) {
    t.Fatal("window should be deleted")
  }
}

func TestEditRejectsBadInput(t *testing.T) {
  g := New()
  sample(t, g)
  for _, text := range []string{
    "n99 nowhere fact guess\n",
    "n2 tcp/handshake fact sure\n",
    "n2 tcp/handshake fact checked\n  wobbles n3\n",
    "  claim \"orphan\"\n",
    "new missing/parent/x fact guess\n",
    "n2 tcp/handshake fact checked\nn2 tcp/handshake fact checked\n",
    "n2 tcp/hand shake fact checked\n",
  } {
    before := dumpAll(t, g)
    _, err := g.Edit(strings.NewReader(text), 0)
    if err == nil {
      t.Errorf("accepted:\n%s", text)
    }

    if dumpAll(t, g) != before {
      t.Errorf("a rejected edit changed the tree:\n%s", text)
    }
  }
}

func TestDumpSubtree(t *testing.T) {
  g := New()
  sample(t, g)
  need(g.AddNode(0, "udp", "", KindTopic, ConfGuess, 0))
  tcp, _ := g.Lookup("tcp")

  var b bytes.Buffer
  mustDo(t, g.Dump(&b, tcp))
  if strings.Contains(b.String(), "udp") ||
      !strings.Contains(b.String(), "tcp/window") {
    t.Fatalf("subtree dump:\n%s", b.String())
  }
}
