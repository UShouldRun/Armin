// A suggestion and OpenRouter's model and credit shapes.

package catalog

// Suggestion is one hosted model worth trying, with what it costs.
type Suggestion struct {
  ID      string  `json:"id"`      // the provider's id: "qwen/qwen3.8-27b"
  In      float64 `json:"in"`      // dollars per million input tokens
  Out     float64 `json:"out"`     // dollars per million output tokens
  Context int     `json:"context"` // tokens
  Session float64 `json:"session"` // dollars for a deep session, see SessionIn/Out
  Planner bool    `json:"planner"` // a vendor's strongest current model
  Free    bool    `json:"free"`
}

// Balance is what an OpenRouter account has left, and this key's
// own cap.
type Balance struct {
  Credits  float64 `json:"credits"` // bought, dollars
  Used     float64 `json:"used"`    // spent, all keys
  Today    float64 `json:"today"`   // spent today with this key
  KeyLimit float64 `json:"key_limit,omitempty"` // this key's cap; 0 = none
  KeyLeft  float64 `json:"key_left,omitempty"`
}

// orModel is the slice of OpenRouter's /models entry armin reads.
type orModel struct {
  ID      string   `json:"id"`
  Created int64    `json:"created"`
  Context int      `json:"context_length"`
  Params  []string `json:"supported_parameters"`
  Pricing struct {
    Prompt     string `json:"prompt"`
    Completion string `json:"completion"`
  } `json:"pricing"`
}
