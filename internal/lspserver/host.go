// nvim's armin as the project's instance: the share.Host methods
// joined terminals call (History, Ask, Complete, Explain), each
// running on this server's models and conversation.

package lspserver

import (
  "context"

  "armin/internal/agent"
  "armin/internal/share"
)

// History is the conversation as displayed, as of the last turn.
func (s *Server) History() []share.ChatLine {
  lines := s.lines.Load()
  if lines == nil {
    return []share.ChatLine{}
  }

  return *lines
}

// snapshot refreshes History; the caller holds chatMu or is the
// only one touching chat.
func (s *Server) snapshot() {
  lines := share.Lines(s.chat)
  s.lines.Store(&lines)
}

// Ask runs a joined armin's question (see share.Host).
func (s *Server) Ask(ctx context.Context, question, file, origin string) error {
  _, err := s.chatTurn(ctx, question, file, origin, nil)
  return err
}

// Complete runs a joined armin's inline completion.
func (s *Server) Complete(ctx context.Context, f share.Fill) (string, error) {
  return s.completionLLM.Complete(ctx, f.Prefix, f.Suffix, f.Opts)
}

// Explain answers a joined armin's hover question.
func (s *Server) Explain(ctx context.Context, question string) (string, error) {
  return s.ask(ctx, question, agent.Hooks{})
}

// explain is hover's question, here or at the host.
func (s *Server) explain(ctx context.Context, question string) (string, error) {
  if s.remote != nil {
    return s.remote.Explain(ctx, question)
  }

  return s.ask(ctx, question, agent.Hooks{})
}

// complete is inline completion, here or at the host.
func (s *Server) complete(ctx context.Context, f share.Fill) (string, error) {
  if s.remote != nil {
    return s.remote.Complete(ctx, f)
  }

  return s.Complete(ctx, f)
}
