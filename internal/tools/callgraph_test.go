package tools

import (
  "context"
  "testing"

  "armin/internal/ctags"
  "armin/internal/sandbox"
)

func writeCallGraphFixture(t *testing.T, root string) {
  t.Helper()
  writeFile(t, root, "a.go", `package fixture

func Helper() int { return 1 }

func CallsHelper() int { return Helper() }

func CallsCallsHelper() int { return CallsHelper() }

// Never called anywhere in this fixture.
func Orphan() int { return 2 }
`)
}

// --- find_callers ---

func TestFindCallersFindsCallSites(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeCallGraphFixture(t, cfg.Root)

  out, err := FindCallers(context.Background(), cfg, FindCallersInput{Name: "Helper"})
  if err != nil {
    t.Fatalf("FindCallers: %v", err)
  }
  if len(out.Calls) != 1 || out.Calls[0].Line != 5 {
    t.Fatalf("Calls = %+v, want one call at line 5", out.Calls)
  }
}

func TestFindCallersNoCallSites(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeCallGraphFixture(t, cfg.Root)

  out, err := FindCallers(context.Background(), cfg, FindCallersInput{Name: "Orphan"})
  if err != nil {
    t.Fatalf("FindCallers: %v", err)
  }
  if len(out.Calls) != 0 {
    t.Fatalf("Calls = %+v, want none", out.Calls)
  }
}

func TestFindCallersRequiresName(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  if _, err := FindCallers(context.Background(), cfg, FindCallersInput{}); err == nil {
    t.Fatal("expected an error for an empty name")
  }
}

// --- impact_analysis ---

func TestImpactAnalysisTransitiveCallers(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeCallGraphFixture(t, cfg.Root)

  // depth 1: only CallsHelper (the direct caller of Helper).
  out, err := ImpactAnalysis(context.Background(), cfg, ImpactAnalysisInput{Name: "Helper", Depth: 1})
  if err != nil {
    t.Fatalf("ImpactAnalysis: %v", err)
  }
  if len(out.Affected) != 1 || out.Affected[0] != "CallsHelper" {
    t.Fatalf("Affected (depth 1) = %v, want [CallsHelper]", out.Affected)
  }

  // depth 2: CallsHelper, then whoever calls that (CallsCallsHelper).
  out, err = ImpactAnalysis(context.Background(), cfg, ImpactAnalysisInput{Name: "Helper", Depth: 2})
  if err != nil {
    t.Fatalf("ImpactAnalysis: %v", err)
  }
  if len(out.Affected) != 2 {
    t.Fatalf("Affected (depth 2) = %v, want 2 entries", out.Affected)
  }
}

func TestImpactAnalysisRequiresName(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  if _, err := ImpactAnalysis(context.Background(), cfg, ImpactAnalysisInput{}); err == nil {
    t.Fatal("expected an error for an empty name")
  }
}

// --- find_dead_code ---

func TestFindDeadCodeNilIndexIsCleanError(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeCallGraphFixture(t, cfg.Root)
  if _, err := FindDeadCode(context.Background(), nil, cfg, FindDeadCodeInput{}); err == nil {
    t.Fatal("expected an error for a nil ctags index")
  }
}

func TestFindDeadCodeEmptyIndexReturnsNoCandidates(t *testing.T) {
  // Same graceful-degradation path as symbols_test.go: an index with
  // no configured binary always serves an empty symbol set, so
  // find_dead_code (which cross-references ctags' exported symbols)
  // correctly reports nothing rather than erroring, with no real
  // ctags install required for this test.
  cfg := newTestSandbox(t, sandbox.Options{})
  writeCallGraphFixture(t, cfg.Root)
  idx := ctags.New(cfg.Root, "")

  out, err := FindDeadCode(context.Background(), idx, cfg, FindDeadCodeInput{})
  if err != nil {
    t.Fatalf("FindDeadCode: %v", err)
  }
  if len(out.Candidates) != 0 {
    t.Fatalf("Candidates = %+v, want none (empty ctags index)", out.Candidates)
  }
}
