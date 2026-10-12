package tools

import (
  "context"
  "fmt"
  "io/fs"
  "os"
  "path/filepath"
  "sort"
  "strings"
  "unicode"

  "armin/internal/sandbox"
)

const (
  defaultSemanticCount = 5
  maxSemanticCount     = 15
  maxSemanticSnippet   = 200
  // maxSemanticFileSize skips anything bigger than this rather than
  // reading it fully into memory — this tool walks and reads every
  // file on every call (no index, unlike ctags/callgraph), so a
  // large generated/vendored file would otherwise cost real memory
  // for a keyword score that's rarely useful on that kind of file.
  maxSemanticFileSize = 1 << 20 // 1MiB
)

// SemanticSearch is keyword/term-frequency relevance ranking, NOT
// real semantic/embedding search — armin has no vector index or
// embedding model and isn't taking on that dependency (the same
// zero-dependency stance internal/searxng takes for web_search). It
// tokenizes Query, scores each file by how many times those terms
// appear, and returns the top matches. Useful for "what files are
// about X" when you don't know the exact string to grep for; it
// will miss genuinely synonymous code that shares no vocabulary with
// Query. Prefer search_code when you know the literal text to find.
func SemanticSearch(
  ctx context.Context, cfg *sandbox.Config, in SemanticSearchInput,
) (*SemanticSearchOutput, error) {
  if strings.TrimSpace(in.Query) == "" {
    return nil, fmt.Errorf("\n\t... in SemanticSearch:\n\t query is required")
  }
  count := clampCount(in.Count, defaultSemanticCount, maxSemanticCount)

  terms := tokenize(in.Query)
  if len(terms) == 0 {
    return nil, fmt.Errorf(
      "\n\t... in SemanticSearch:\n\t query has no searchable terms",
    )
  }

  var results []SemanticSearchResult
  err := filepath.WalkDir(cfg.Root, func(p string, d fs.DirEntry, err error) error {
    if err != nil {
      return err
    }
    if d.IsDir() {
      if d.Name() != "." && skipDirs[d.Name()] {
        return filepath.SkipDir
      }
      return nil
    }

    info, err := d.Info()
    if err != nil || info.Size() > maxSemanticFileSize {
      return nil
    }

    data, err := os.ReadFile(p)
    if err != nil {
      return nil
    }
    if isBinary(data[:min(len(data), 512)]) {
      return nil
    }

    content := string(data)
    lower := strings.ToLower(content)
    score := 0
    for _, term := range terms {
      score += strings.Count(lower, term)
    }
    if score == 0 {
      return nil
    }

    rel, relErr := filepath.Rel(cfg.Root, p)
    if relErr != nil {
      rel = p
    }
    results = append(results, SemanticSearchResult{
      File: filepath.ToSlash(rel), Score: score,
      Snippet: firstMatchingSnippet(content, lower, terms),
    })
    return nil
  })
  if err != nil {
    return nil, fmt.Errorf("\n\t... in SemanticSearch:\n\t%w", err)
  }

  sort.Slice(results, func(i, j int) bool {
    if results[i].Score != results[j].Score {
      return results[i].Score > results[j].Score
    }
    return results[i].File < results[j].File
  })
  if len(results) > count {
    results = results[:count]
  }

  return &SemanticSearchOutput{ Results: results }, nil
}

// tokenize lowercases and splits on anything that isn't a letter or
// digit — good enough for keyword matching, not a real tokenizer.
func tokenize(s string) []string {
  var terms []string
  var cur strings.Builder
  for _, r := range strings.ToLower(s) {
    if unicode.IsLetter(r) || unicode.IsDigit(r) {
      cur.WriteRune(r)
    } else if cur.Len() > 0 {
      terms = append(terms, cur.String())
      cur.Reset()
    }
  }
  if cur.Len() > 0 {
    terms = append(terms, cur.String())
  }
  return terms
}

func firstMatchingSnippet(content, lower string, terms []string) string {
  for _, term := range terms {
    idx := strings.Index(lower, term)
    if idx < 0 {
      continue
    }
    start := max(0, idx - 40)
    end := min(len(content), idx + len(term) + maxSemanticSnippet - 40)
    snippet := strings.TrimSpace(content[start:end])
    return strings.ReplaceAll(snippet, "\n", " ")
  }
  return ""
}
