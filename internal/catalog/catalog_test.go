package catalog

import "testing"

func TestPick(t *testing.T) {
  data := []byte(`{"data":[
    {"id":"qwen/qwen3.8-27b","created":30000000,"context_length":1000,"supported_parameters":["tools"],"pricing":{"prompt":"0.00000004","completion":"0.0000023"}},
    {"id":"qwen/qwen3.8-flash","created":4,"supported_parameters":["tools"],"pricing":{"prompt":"0","completion":"0"}},
    {"id":"qwen/qwen3.5-9b","created":1,"supported_parameters":["tools"],"pricing":{"prompt":"0","completion":"0"}},
    {"id":"anthropic/claude-opus-5.5","created":20000000,"supported_parameters":["tools"],"pricing":{"prompt":"0.000004","completion":"0.00002"}},
    {"id":"anthropic/claude-opus-5.5:batch","created":5,"supported_parameters":["tools"],"pricing":{"prompt":"0","completion":"0"}},
    {"id":"openai/no-tools","created":9,"supported_parameters":[],"pricing":{"prompt":"0","completion":"0"}},
    {"id":"nvidia/big:free","created":7,"supported_parameters":["tools"],"pricing":{"prompt":"0","completion":"0"}},
    {"id":"some/other","created":8,"supported_parameters":["tools"],"pricing":{"prompt":"0","completion":"0"}}
  ]}`)

  got, err := Pick(data)
  if err != nil {
    t.Fatal(err)
  }

  if len(got) != 3 {
    t.Fatalf("got %+v", got)
  }

  if got[0].ID != "qwen/qwen3.8-27b" || got[1].ID != "anthropic/claude-opus-5.5" || !got[2].Free {
    t.Fatalf("order = %s, %s, %s", got[0].ID, got[1].ID, got[2].ID)
  }

  if got[0].In < 0.039 || got[0].In > 0.041 || got[0].Session < 0.11 || got[0].Session > 0.12 {
    t.Fatalf("qwen prices = %+v", got[0])
  }
}
