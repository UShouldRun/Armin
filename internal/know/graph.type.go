// The types of graph.go.

package know

import (
  "os"
)

// NodeID indexes a tree's nodes; 0 is the root. Only the low 29 bits
// are usable, since an edge packs its kind into the top 3.
type NodeID uint32

// StrRef is a byte offset into the string arena, where each string is
// stored as a uvarint length followed by its bytes. 0 is the empty
// string.
type StrRef uint32

// AuthorID indexes the interned author table.
type AuthorID uint16

// Kind is what a node is, in 3 bits.
type Kind uint8

const (
  KindTopic    Kind = iota
  KindConcept
  KindFact
  KindDecision
  KindFile
  KindSymbol
  KindQuestion
)

// Conf is how far a claim has been established, in 2 bits.
type Conf uint8

const (
  ConfGuess    Conf = iota
  ConfInferred
  ConfChecked
  ConfTested
)

// NodeFlags packs a node's states into one byte.
type NodeFlags uint8

const (
  FlagStale      NodeFlags = 0b00000001 // its evidence changed
  FlagSuspect    NodeFlags = 0b00000010 // something it rests on did
  FlagPinned     NodeFlags = 0b00000100 // shown in every outline
  FlagUserEdited NodeFlags = 0b00001000 // changed through -know edit
  FlagVerified   NodeFlags = 0b00010000 // a minion claim, checked
  FlagDeleted    NodeFlags = 0b00100000 // a tombstone; IDs never move
  FlagChosen     NodeFlags = 0b01000000 // the chosen option of a decision
  FlagCycle      NodeFlags = 0b10000000 // a question naming an open cycle
  FlagAll        NodeFlags = 0b11111111
)

// Node is one record of the node array: 20 bytes, no pointers, laid
// out the same in memory and in the snapshot.
type Node struct {
  Parent      NodeID
  Name, Claim StrRef
  Born, Seen  uint16 // days since 2026-01-01
  Author      AuthorID
  KindConf    uint8 // kind in bits 0-2, confidence in bits 3-4
  Flags       NodeFlags
}

// Role is who wrote something.
type Role uint8

const (
  RoleUser    Role = iota
  RoleMinion
  RoleCopilot
  RoleAdvisor
)

// Author is one interned (role, model, session, delegation): 16
// bytes, so one uint16 on each node is enough to trace a claim back.
type Author struct {
  Model, Session StrRef
  Delegation     uint32 // 0 when not written inside a delegation
  Role           Role
  _              [3]uint8
}

// RefKind is what a piece of evidence points at.
type RefKind uint8

const (
  RefFile    RefKind = iota // "path:start-end", Hash over the lines
  RefCommit
  RefLibrary // a library page
  RefURL
  RefSession // a session message
  RefCheck   // a check or test result
  RefSymbol  // reserved for code graph symbols (Poneglyph)

  refDeleted RefKind = 0xff
)

// EvidenceID indexes the evidence array.
type EvidenceID uint32

// Evidence ties a node to what supports it: 16 bytes.
type Evidence struct {
  Node NodeID
  Ref  StrRef
  Hash uint32 // of the referenced content, compared only with itself
  Kind RefKind
  _    [3]uint8
}

// Graph is one tree, loaded. The base slices of each array alias the
// mmapped snapshot (a private, copy-on-write mapping, so in-place
// changes never reach the file); everything added since lives in the
// heap slices after them. IDs and offsets run across both.
type Graph struct {
  dir string

  nodes, moreNodes   []Node
  arena, moreArena   []byte
  authors            []Author
  evidence, moreEv   []Evidence

  edges edges
  paths paths

  log    *os.File
  logGen uint64
  lock   *os.File
  mapped []byte
  ro     bool

  // scratch for encoding one op, reused
  op []byte
}

// Edge is one decoded edge.
type Edge struct {
  To   NodeID
  Kind EdgeKind
}

// EdgeKind is what an edge means, in 3 bits.
type EdgeKind uint8

const (
  EdgeRequires    EdgeKind = iota // to understand From, first To
  EdgeImplements                  // project From implements concept To
  EdgeExplains                    // From explains To
  EdgeContradicts                 // the two disagree
  EdgeDerivedFrom                 // From was concluded from To
)
