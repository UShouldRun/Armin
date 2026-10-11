package agent

import (
  "context"
  "net/http"
  "strings"
  "testing"

  "armin/internal/llm"
  "armin/internal/tools"
)

func TestSourcesNumberAcrossSearches(t *testing.T) {
  ts := &turnSources{}
  a := &tools.WebSearchOutput{ Results: []tools.WebSearchResult{
    { Title: "Go 1.23", URL: "https://go.dev/1.23" },
    { Title: "Iterators", URL: "https://go.dev/iter" },
  } }
  b := &tools.WebSearchOutput{ Results: []tools.WebSearchResult{
    { Title: "Iterators", URL: "https://go.dev/iter" },
    { Title: "Range funcs", URL: "https://go.dev/range" },
  } }
  ts.number(a)
  ts.number(b)
  if a.Results[1].ID != 2 || b.Results[0].ID != 2 || b.Results[1].ID != 3 {
    t.Fatalf("ids = %d %d %d, want 2 2 3", a.Results[1].ID, b.Results[0].ID, b.Results[1].ID)
  }

  if a.Cite == "" {
    t.Fatal("results carry no citation instruction")
  }

  add := ts.appendix("Range functions landed in 1.23 [1][3].")
  want := "\n\nSources:\n- [1] [Go 1.23](https://go.dev/1.23)\n- [3] [Range funcs](https://go.dev/range)"
  if add != want {
    t.Fatalf("appendix =\n%q\nwant\n%q", add, want)
  }

  if ts.appendix("text [1]\n\n## Sources\n- [Go](https://go.dev)") != "" {
    t.Fatal("appended although the answer already lists sources")
  }

  if !strings.HasPrefix(ts.appendix("no citations"), "\n\nSources consulted:") {
    t.Fatal("uncited answer should list what was consulted")
  }

  if (&turnSources{}).appendix("anything") != "" {
    t.Fatal("appendix without any search")
  }
}

func TestTurnAppendsSourcesTheModelForgot(t *testing.T) {
  round := 0
  loop, _ := newTestLoop(t, func(w http.ResponseWriter, r *http.Request) {
    round++
    writeChatResponse(t, w, llm.Message{ Role: "assistant", Content: "Answer citing [1]." })
  })

  var streamed strings.Builder
  sources := &turnSources{}
  sources.number(&tools.WebSearchOutput{ Results: []tools.WebSearchResult{
    { Title: "Doc", URL: "https://example.com/doc" },
  } })
  history := []llm.Message{ { Role: "assistant", Content: "Answer citing [1]." } }
  got := loop.finish(&history, sources, Hooks{ Token: func(s string) { streamed.WriteString(s) } })

  if !strings.Contains(got, "- [1] [Doc](https://example.com/doc)") || !strings.Contains(history[0].Content, "Sources:") {
    t.Fatalf("answer = %q", got)
  }

  if !strings.Contains(streamed.String(), "Sources:") {
    t.Fatal("the appended sources weren't streamed")
  }

  _ = context.Background
}
