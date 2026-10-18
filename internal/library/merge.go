package library

import (
  "context"
  "fmt"
  "os"
  "sort"
  "strings"
)

const (
  // mergeAbove is how many distinct topics Sync tolerates before it
  // has the model fold them together; mergeTarget is what it asks for.
  mergeAbove  = 80
  mergeTarget = 60
)

const mergeSystem = `You tidy the topic list of a technical library's index, so that each subject has one name and the list stays short.

Map every topic you're given to a canonical topic. Reply with lines only, no other text, one per topic given:
old -> new

- Merge synonyms, spelling variants, and narrow topics into the broader subject a reader would look up ("concurrency-go" -> concurrency, "functions-defer" -> go, "tcp" -> tcp-ip).
- Keep languages, tools and big fields as topics of their own (go, c, javascript, git, calculus, linear-algebra).
- Prefer the preferred names when one fits; new names are lowercase-kebab-case.
- Aim for about %d canonical topics in total.`

// aliases reads .armin/aliases.tsv: a merged topic → its canonical
// name.
func (l *Library) aliases() map[string]string {
  data, _ := os.ReadFile(l.dir("aliases.tsv"))
  m := map[string]string{}
  for _, line := range strings.Split(string(data), "\n") {
    from, to, ok := strings.Cut(line, "\t")
    if ok && from != "" && to != "" {
      m[from] = to
    }
  }

  return m
}

func (l *Library) saveAliases(m map[string]string) error {
  keys := make([]string, 0, len(m))
  for k := range m {
    keys = append(keys, k)
  }

  sort.Strings(keys)
  var b strings.Builder
  for _, k := range keys {
    fmt.Fprintf(&b, "%s\t%s\n", k, m[k])
  }

  err := os.WriteFile(l.dir("aliases.tsv"), []byte(b.String()), 0o644)
  if err != nil {
    return fmt.Errorf("\n\t... in saveAliases:\n\t%w", err)
  }

  return nil
}

// canonical follows aliases to a topic's canonical name.
func canonical(aliases map[string]string, topic string) string {
  for range 8 { // a chain, never a cycle worth following further
    to, ok := aliases[topic]
    if !ok || to == topic {
      break
    }

    topic = to
  }

  return topic
}

// distinctTopics counts the rows of each topic, most used first.
func distinctTopics(rows []Topic) ([]string, map[string]int) {
  count := map[string]int{}
  var names []string
  for _, t := range rows {
    if count[t.Topic] == 0 {
      names = append(names, t.Topic)
    }

    count[t.Topic]++
  }

  sort.SliceStable(names, func(i, j int) bool {
    return count[names[i]] > count[names[j]]
  })

  return names, count
}

// NeedsMerge reports whether the topic table has grown past
// mergeAbove distinct topics.
func (l *Library) NeedsMerge() bool {
  names, _ := distinctTopics(l.Topics())
  return len(names) > mergeAbove
}

// MergeTopics has the model fold the table's topics into about
// mergeTarget canonical ones: rows are renamed (duplicates dropped)
// and the mapping is kept in aliases.tsv, so later catalogs use the
// same names. It returns the topic counts before and after.
func (l *Library) MergeTopics(ctx context.Context, ask Ask) (int, int, error) {
  rows := l.Topics()
  names, count := distinctTopics(rows)
  if len(names) == 0 {
    return 0, 0, nil
  }

  var b strings.Builder
  fmt.Fprintf(&b, "Preferred names: %s\n\nTopics (rows using each):\n",
    strings.Join(seedTopics, ", "))
  for _, n := range names {
    fmt.Fprintf(&b, "%s (%d)\n", n, count[n])
  }

  system := fmt.Sprintf(mergeSystem, mergeTarget)
  reply, err := ask(ctx, system, b.String())
  if err != nil {
    n := len(names)
    return n, n, fmt.Errorf("\n\t... in MergeTopics:\n\t%w", err)
  }

  given := make(map[string]bool, len(names))
  for _, n := range names {
    given[n] = true
  }

  aliases := l.aliases()
  for _, line := range strings.Split(reply, "\n") {
    from, to, ok := strings.Cut(strings.Trim(line, "-*` "), "->")
    if !ok {
      continue
    }

    from, to = kebab(from), kebab(to)
    if given[from] && to != "" && to != from {
      aliases[from] = to
    }
  }

  merged := mergeRows(rows, aliases)
  err = l.saveTopics(merged)
  if err != nil {
    n := len(names)
    return n, n, fmt.Errorf("\n\t... in MergeTopics:\n\t%w", err)
  }

  err = l.saveAliases(aliases)
  if err != nil {
    n := len(names)
    return n, n, fmt.Errorf("\n\t... in MergeTopics:\n\t%w", err)
  }

  after, _ := distinctTopics(merged)

  return len(names), len(after), nil
}

// mergeRows renames each row's topic to its canonical name and drops
// rows that become exact duplicates.
func mergeRows(rows []Topic, aliases map[string]string) []Topic {
  seen := make(map[Topic]bool, len(rows))
  out := make([]Topic, 0, len(rows))
  for _, t := range rows {
    t.Topic = canonical(aliases, t.Topic)
    if seen[t] {
      continue
    }

    seen[t] = true
    out = append(out, t)
  }

  return out
}
