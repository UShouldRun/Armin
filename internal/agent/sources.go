// Web citations: number puts one running id on every web_search
// result in a turn, appendix adds a Sources list when the answer
// cited ids but didn't list them.

package agent

import (
  "fmt"
  "regexp"
  "strconv"
  "strings"

  "armin/internal/tools"
)

// citeNote travels with every numbered search result, so even a
// model that skimmed the system prompt sees how to cite.
const citeNote = "Cite each claim taken from a result inline as [id]. " +
  "End your answer with a Sources list: - [title](url) per id cited."

func (ts *turnSources) number(out *tools.WebSearchOutput) {
  if ts == nil || out == nil {
    return
  }

  if ts.byURL == nil {
    ts.byURL = make(map[string]int)
  }

  for i := range out.Results {
    r := &out.Results[i]
    id, seen := ts.byURL[r.URL]
    if !seen {
      ts.list = append(ts.list, source{ title: r.Title, url: r.URL })
      id = len(ts.list)
      ts.byURL[r.URL] = id
    }

    r.ID = id
  }

  if len(out.Results) > 0 {
    out.Cite = citeNote
  }
}

var (
  citeRef       = regexp.MustCompile(`\[(\d{1,3})\]`)
  sourcesHeader = regexp.MustCompile(
    `(?im)^\s*(#+\s*)?\**sources\**\s*:?\s*\**\s*$`,
  )
)

// appendix is the Sources list an answer lacks, or "" when the
// answer already has one or no search ran. It lists the ids the
// answer cites; with none cited, every source the turn looked at.
func (ts *turnSources) appendix(answer string) string {
  if ts == nil || len(ts.list) == 0 || sourcesHeader.MatchString(answer) {
    return ""
  }

  var ids []int
  seen := map[int]bool{}
  for _, m := range citeRef.FindAllStringSubmatch(answer, -1) {
    id, _ := strconv.Atoi(m[1])
    if id >= 1 && id <= len(ts.list) && !seen[id] {
      seen[id] = true
      ids = append(ids, id)
    }
  }

  header := "Sources:"
  if len(ids) == 0 {
    header = "Sources consulted:"
    for id := 1; id <= len(ts.list); id++ {
      ids = append(ids, id)
    }
  }

  var b strings.Builder
  b.WriteString("\n\n")
  b.WriteString(header)
  for _, id := range ids {
    s := ts.list[id - 1]
    fmt.Fprintf(&b, "\n- [%d] [%s](%s)", id, s.title, s.url)
  }

  return b.String()
}
