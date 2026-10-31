// Search over node names and claims: BM25 with prefix expansion, a
// subtree filter and a fixed-size top-k. The index is built on the
// first Find; between rebuilds, nodes added or changed since are
// scored by scanning their text, and their old postings are skipped.
// Name terms count twice, so a node called "handshake" outranks one
// that only mentions it.
//
//   Find      query → top hits, optionally under one subtree
//   Under     the subtree test (Euler-tour intervals, or the parents)

package know

import (
  "bytes"
  "math"
  "math/bits"
  "slices"
  "sort"
  "strings"

  "armin/internal/tokens"
)

const (
  bm25K1 = 1.2
  bm25B  = 0.75

  // MaxHits caps Find's k.
  MaxHits = 20

  // rebuildAt is how many added or changed nodes are scanned directly
  // before the index is rebuilt instead.
  rebuildAt = 512

  // prefixWeight scores a prefix match below an exact one; maxExpand
  // caps how many index terms one short prefix expands to.
  prefixWeight = 0.8
  maxExpand    = 64
  minPrefix    = 3
)

// changed is called by apply when a node's text, place or deletion
// changes, so its postings are skipped until the next rebuild.
func (ix *index) changed(id NodeID) {
  if int(id) >= ix.built {
    return
  }

  w, b := id / 64, uint64(1) << (id % 64)
  if ix.stale[w] & b == 0 {
    ix.stale[w] |= b
    ix.nstale++
  }
}

func (ix *index) isStale(id NodeID) bool {
  return int(id) < ix.built && ix.stale[id / 64] & (1 << (id % 64)) != 0
}

// nodeTerms calls emit for each term of a node, name terms twice.
func (g *Graph) nodeTerms(id NodeID, buf []byte, emit func([]byte)) {
  n := g.node(id)
  name := g.Bytes(n.Name)
  tokens.Each(string(name), buf, func(t []byte) { emit(t); emit(t) })
  tokens.Each(string(g.Bytes(n.Claim)), buf, emit)
}

// build indexes every live non-root node. Terms are interned to IDs
// as they're read (one allocation per distinct term), then a counting
// sort by term rank lays out the postings: O(total terms) plus a sort
// of the distinct terms.
func (ix *index) build(g *Graph) {
  n := g.Len()
  type entry struct {
    term uint32
    id   NodeID
  }

  termID := map[string]uint32{}
  var uniq []string
  var all []entry
  docLen := make([]uint8, n)
  buf := make([]byte, 0, 64)
  total, docs := 0, 0
  for id := 1; id < n; id++ {
    if !g.live(NodeID(id)) {
      continue
    }

    count := 0
    g.nodeTerms(NodeID(id), buf, func(t []byte) {
      tid, ok := termID[string(t)]
      if !ok {
        tid = uint32(len(uniq))
        s := string(t)
        termID[s] = tid
        uniq = append(uniq, s)
      }

      all = append(all, entry{ term: tid, id: NodeID(id) })
      count++
    })

    docLen[id] = uint8(min(count, 255))
    total += count
    docs++
  }

  byText := make([]uint32, len(uniq))
  for i := range byText {
    byText[i] = uint32(i)
  }

  slices.SortFunc(byText, func(a, b uint32) int {
    return strings.Compare(uniq[a], uniq[b])
  })

  rank := make([]uint32, len(uniq))
  for r, tid := range byText {
    rank[tid] = uint32(r)
  }

  // stable counting sort by rank; entries arrive in node order, so
  // each term's run stays sorted by node
  start := make([]uint32, len(uniq) + 1)
  for _, e := range all {
    start[rank[e.term] + 1]++
  }

  for i := 1; i < len(start); i++ {
    start[i] += start[i - 1]
  }

  sorted := make([]NodeID, len(all))
  fill := append([]uint32(nil), start[:len(uniq)]...)
  for _, e := range all {
    r := rank[e.term]
    sorted[fill[r]] = e.id
    fill[r]++
  }

  ix.termText = ix.termText[:0]
  ix.termOff = append(ix.termOff[:0], 0)
  ix.poffs = append(ix.poffs[:0], 0)
  ix.post = ix.post[:0]
  for r, tid := range byText {
    ix.termText = append(ix.termText, uniq[tid]...)
    ix.termOff = append(ix.termOff, uint32(len(ix.termText)))
    run := sorted[start[r]:start[r + 1]]
    for i := 0; i < len(run); {
      id := run[i]
      tf := uint32(0)
      for i < len(run) && run[i] == id {
        tf++
        i++
      }

      ix.post = append(ix.post, uint32(id) | min(tf, 7) << 29)
    }

    ix.poffs = append(ix.poffs, uint32(len(ix.post)))
  }

  ix.docLen = docLen
  ix.avgLen = 1
  if docs > 0 {
    ix.avgLen = float32(total) / float32(docs)
  }

  ix.built = n
  ix.stale = make([]uint64, (n + 63) / 64)
  ix.nstale = 0
  ix.euler(g)
}

// euler numbers the live tree depth first: a is under b exactly when
// tin[b] <= tin[a] and tout[a] <= tout[b].
func (ix *index) euler(g *Graph) {
  n := g.Len()
  ix.tin = slices.Grow(ix.tin[:0], n)[:n]
  ix.tout = slices.Grow(ix.tout[:0], n)[:n]
  g.Children(0, nil) // brings the children index up to date
  kids := func(id NodeID) []uint32 {
    p := &g.paths

    return p.kids[p.koffs[id]:p.koffs[id + 1]]
  }

  type frame struct {
    id   NodeID
    kids []uint32
  }

  clock := uint32(0)
  stack := []frame{ { id: 0, kids: kids(0) } }
  ix.tin[0] = clock
  for len(stack) > 0 {
    top := &stack[len(stack) - 1]
    if len(top.kids) == 0 {
      clock++
      ix.tout[top.id] = clock
      stack = stack[:len(stack) - 1]
      continue
    }

    k := NodeID(top.kids[0])
    top.kids = top.kids[1:]
    clock++
    ix.tin[k] = clock
    stack = append(stack, frame{ id: k, kids: kids(k) })
  }

  ix.eulerDirty = false
}

// Under reports whether a is in the subtree rooted at b (a itself
// included).
func (g *Graph) Under(a, b NodeID) bool {
  if b == 0 {
    return true
  }

  ix := &g.index
  if !ix.eulerDirty && int(a) < len(ix.tin) && int(b) < len(ix.tin) {
    return ix.tin[b] <= ix.tin[a] && ix.tout[a] <= ix.tout[b]
  }

  for ; a != 0; a = g.node(a).Parent {
    if a == b {
      return true
    }
  }

  return false
}

func (ix *index) term(i int) []byte {
  return ix.termText[ix.termOff[i]:ix.termOff[i + 1]]
}

func (ix *index) terms() int {
  return len(ix.termOff) - 1
}

// lookup returns the first term >= t.
func (ix *index) lookup(t []byte) int {
  return sort.Search(ix.terms(), func(i int) bool {
    return bytes.Compare(ix.term(i), t) >= 0
  })
}

func (ix *index) idf(df int) float32 {
  n := float64(max(ix.built, 1))

  return float32(math.Log(1 + (n - float64(df) + 0.5) / (float64(df) + 0.5)))
}

func (ix *index) bm25(idf float32, tf uint32, docLen uint8) float32 {
  f := float32(tf)
  norm := bm25K1 * (1 - bm25B + bm25B * float32(docLen) / ix.avgLen)

  return idf * f * (bm25K1 + 1) / (f + norm)
}

func (ix *index) add(id NodeID, s float32) {
  if ix.score[id] == 0 {
    ix.touched = append(ix.touched, id)
  }

  ix.score[id] += s
}

// Find returns up to k nodes (k ≤ MaxHits) best matching query, under
// the subtree at under (0 for the whole tree), best first.
func (g *Graph) Find(query string, under NodeID, k int) []Hit {
  k = min(max(k, 1), MaxHits)
  ix := &g.index
  if ix.built == 0 || g.Len() - ix.built + ix.nstale > rebuildAt {
    ix.build(g)
  } else if ix.eulerDirty {
    ix.euler(g)
  }

  qs := tokens.Query(query)
  if len(qs) == 0 {
    return nil
  }

  if len(ix.score) < g.Len() {
    ix.score = make([]float32, g.Len())
  }

  ix.touched = ix.touched[:0]
  idfs := make([]float32, len(qs))
  for qi, q := range qs {
    qb := []byte(q)
    i := ix.lookup(qb)
    exact := i < ix.terms() && bytes.Equal(ix.term(i), qb)
    idfs[qi] = ix.idf(0)
    if exact {
      idfs[qi] = ix.idf(int(ix.poffs[i + 1] - ix.poffs[i]))
      ix.scoreTerm(i, idfs[qi], 1)
      i++
    }

    if len(qb) < minPrefix {
      continue
    }

    for j := 0; i < ix.terms() && j < maxExpand; i, j = i + 1, j + 1 {
      if !bytes.HasPrefix(ix.term(i), qb) {
        break
      }

      df := int(ix.poffs[i + 1] - ix.poffs[i])
      ix.scoreTerm(i, ix.idf(df), prefixWeight)
    }
  }

  g.scanUnindexed(qs, idfs)

  var top [MaxHits]Hit
  n := 0
  for _, id := range ix.touched {
    s := ix.score[id]
    ix.score[id] = 0
    if !g.live(id) || !g.Under(id, under) {
      continue
    }

    if n == k && s <= top[n - 1].Score {
      continue
    }

    j := min(n, k - 1)
    for j > 0 &&
        (top[j - 1].Score < s || top[j - 1].Score == s && top[j - 1].ID > id) {
      top[j] = top[j - 1]
      j--
    }

    top[j] = Hit{ ID: id, Score: s }
    n = min(n + 1, k)
  }

  return append([]Hit(nil), top[:n]...)
}

func (ix *index) scoreTerm(i int, idf float32, weight float32) {
  for _, p := range ix.post[ix.poffs[i]:ix.poffs[i + 1]] {
    id := NodeID(p & (maxNodes - 1))
    if ix.isStale(id) {
      continue
    }

    ix.add(id, weight * ix.bm25(idf, p >> 29, ix.docLen[id]))
  }
}

// scanUnindexed scores the nodes the index doesn't cover by reading
// their text: those added since the build, and the stale ones.
func (g *Graph) scanUnindexed(qs []string, idfs []float32) {
  ix := &g.index
  var exact, prefix [16]uint32
  qs = qs[:min(len(qs), len(exact))]
  score := func(id NodeID) {
    if !g.live(id) {
      return
    }

    clear(exact[:])
    clear(prefix[:])
    length := 0
    g.nodeTerms(id, ix.buf[:0], func(t []byte) {
      length++
      for qi, q := range qs {
        if string(t) == q {
          exact[qi]++
        } else if len(q) >= minPrefix && bytes.HasPrefix(t, []byte(q)) {
          prefix[qi]++
        }
      }
    })

    dl := uint8(min(length, 255))
    for qi := range qs {
      if exact[qi] > 0 {
        ix.add(id, ix.bm25(idfs[qi], min(exact[qi], 7), dl))
      } else if prefix[qi] > 0 {
        ix.add(id, prefixWeight * ix.bm25(idfs[qi], min(prefix[qi], 7), dl))
      }
    }
  }

  for id := ix.built; id < g.Len(); id++ {
    score(NodeID(id))
  }

  if ix.nstale == 0 {
    return
  }

  for w, set := range ix.stale {
    for set != 0 {
      b := set & -set
      set ^= b
      score(NodeID(w * 64 + bits.TrailingZeros64(b)))
    }
  }
}
