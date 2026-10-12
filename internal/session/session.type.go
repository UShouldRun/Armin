package session

import (
  "time"

  "armin/internal/llm"
)

// Meta is a session's first line.
type Meta struct {
  ID        string    `json:"id"`
  Root      string    `json:"root"`
  Model     string    `json:"model"`
  Title     string    `json:"title,omitempty"`
  CreatedAt time.Time `json:"created_at"`
  UpdatedAt time.Time `json:"updated_at"`

  // Messages is counted by List and Load; it isn't stored.
  Messages int `json:"-"`
}

// Saved is what Load reconstructs from a session file.
type Saved struct {
  Meta
  History []llm.Message
}
