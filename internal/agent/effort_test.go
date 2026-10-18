package agent

import (
  "context"
  "encoding/json"
  "net/http"
  "net/http/httptest"
  "testing"

  "armin/internal/llm"
)

func TestChooseEffort(t *testing.T) {
  calls := 0
  reply := "High debug."
  answer := func(w http.ResponseWriter, r *http.Request) {
    calls++
    var req map[string]any
    json.NewDecoder(r.Body).Decode(&req)
    kw, _ := req["chat_template_kwargs"].(map[string]any)
    if kw["enable_thinking"] != false {
      t.Errorf("auto's question should go with thinking off: %v", req)
    }

    msg := map[string]any{ "role": "assistant", "content": reply }
    json.NewEncoder(w).Encode(map[string]any{
      "choices": []any{ map[string]any{ "message": msg } },
    })
  }

  server := httptest.NewServer(http.HandlerFunc(answer))
  defer server.Close()

  c := llm.New(server.URL, "local")
  ctx := context.Background()
  e, task := Assess(ctx, c, "why does this deadlock under load sometimes?")
  if e != EffortHigh || task != TaskDebug {
    t.Fatalf("Assess = %v %v, want high debug", e, task)
  }

  reply = "I think it is tricky"
  e, task = Assess(ctx, c, "rename the variable foo to bar everywhere")
  if e != EffortMedium || task != TaskOther {
    t.Fatalf("unparseable reply gave %v %v, want medium other", e, task)
  }

  before := calls
  e, _ = Assess(ctx, c, "thanks!")
  if e != EffortLow || calls != before {
    t.Fatalf("short request: %v after %d calls, want low with no call",
      e, calls - before)
  }

  if c.Effort != "" {
    t.Fatal("Assess changed the caller's client")
  }
}

func TestEffortNames(t *testing.T) {
  for _, name := range []string{ "auto", "off", "low", "medium", "high" } {
    e, ok := ParseEffort(name)
    if !ok || e.String() != name {
      t.Fatalf("ParseEffort(%q) = %v, %v", name, e, ok)
    }
  }

  _, ok := ParseEffort("default")
  if ok {
    t.Fatal("default isn't a level a user picks")
  }

  if EffortOff.Wire() != "none" || EffortAuto.Wire() != "" ||
    EffortHigh.Wire() != "high" {
    t.Fatal("Wire")
  }
}
