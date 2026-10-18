// Tool identifiers.

package agent

// ToolID identifies a tool internally. The model speaks names (the
// OpenAI wire format requires them); a name is looked up once per
// call and everything after that switches on this byte.
type ToolID uint8
