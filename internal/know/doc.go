// Package know is the graph engine behind armin's knowledge trees: the
// project tree (what armin knows about a codebase and its topic) and
// the learner tree (what the user knows). It imports nothing from the
// rest of armin.
//
// A tree is a DAG where every node also has one primary parent; the
// parent links form the outline and the paths (tcp/handshake), the
// edges carry dependencies. IDs are array indexes, strings live in
// one arena, and records are fixed-size so the snapshot can be
// mmapped and used in place.
//
//   graph.go     nodes, strings, authors, evidence: reads and the
//                mutations every change goes through
//   edges.go     out and in edges: CSR arrays plus an in-session delta
//   paths.go     (parent, name) → node lookup and the children index
//   oplog.go     the append-only log: op encoding, apply, replay
//   snap.go      the snapshot: layout, mmap load, compaction
//   store.go     Open and Close: lock, load the snapshot, replay the log
//   dump.go      the text form of a subtree (armin -know dump)
//   edit.go      loading an edited dump back (armin -know edit)
//
// On disk, per tree: graph.snap (header + the arrays exactly as in
// memory, little-endian) and graph.log ([op][len][payload][crc32]
// records). Every mutation is one log record, written before it is
// applied in memory; replaying the log runs the same apply code.
package know
