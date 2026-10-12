package lspserver

import (
  "bufio"
  "encoding/json"
  "fmt"
  "io"
  "strconv"
  "strings"
)

// readMessage reads one Content-Length-framed JSON-RPC message —
// the wire format every LSP client speaks, distinct from MCP's bare
// newline-delimited JSON (see internal/mcpserver).
func readMessage(r *bufio.Reader) ([]byte, error) {
  length := -1

  for {
    line, err := r.ReadString('\n')
    if err != nil {
      return nil, err
    }

    line = strings.TrimRight(line, "\r\n")
    if line == "" {
      break // blank line ends the header block
    }

    name, value, ok := strings.Cut(line, ":")
    if !ok {
      continue
    }
    if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
      n, err := strconv.Atoi(strings.TrimSpace(value))
      if err != nil {
        return nil, fmt.Errorf(
          "\n\t... in readMessage:\n\t bad Content-Length %q: %w",
          value, err,
        )
      }
      length = n
    }
  }

  if length < 0 {
    return nil, fmt.Errorf(
      "\n\t... in readMessage:\n\t missing Content-Length header",
    )
  }

  body := make([]byte, length)
  _, err := io.ReadFull(r, body)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in readMessage:\n\t%w", err)
  }
  return body, nil
}

func writeMessage(w io.Writer, v any) error {
  body, err := json.Marshal(v)
  if err != nil {
    return fmt.Errorf("\n\t... in writeMessage:\n\t%w", err)
  }

  header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
  _, err = io.WriteString(w, header)
  if err != nil {
    return fmt.Errorf("\n\t... in writeMessage:\n\t%w", err)
  }
  _, err = w.Write(body)
  if err != nil {
    return fmt.Errorf("\n\t... in writeMessage:\n\t%w", err)
  }
  return nil
}
