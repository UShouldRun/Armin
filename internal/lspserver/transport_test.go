package lspserver

import (
  "bufio"
  "bytes"
  "strings"
  "testing"
)

func TestWriteThenReadMessageRoundTrip(t *testing.T) {
  var buf bytes.Buffer
  payload := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "test"}

  err := writeMessage(&buf, payload)
  if err != nil {
    t.Fatalf("writeMessage: %v", err)
  }

  body, err := readMessage(bufio.NewReader(&buf))
  if err != nil {
    t.Fatalf("readMessage: %v", err)
  }
  if !strings.Contains(string(body), `"method":"test"`) {
    t.Fatalf("round-tripped body = %s, missing expected method field", body)
  }
}

func TestReadMessageMissingContentLength(t *testing.T) {
  r := bufio.NewReader(strings.NewReader("Foo: bar\r\n\r\n{}"))
  _, err := readMessage(r)
  if err == nil {
    t.Fatal("expected an error for a frame with no Content-Length header")
  }
}

func TestReadMessageHeaderCaseInsensitive(t *testing.T) {
  raw := "content-length: 2\r\n\r\n{}"
  r := bufio.NewReader(strings.NewReader(raw))

  body, err := readMessage(r)
  if err != nil {
    t.Fatalf("readMessage: %v", err)
  }
  if string(body) != "{}" {
    t.Fatalf("body = %q, want {}", body)
  }
}

func TestReadMessageEOF(t *testing.T) {
  r := bufio.NewReader(strings.NewReader(""))
  _, err := readMessage(r)
  if err == nil {
    t.Fatal("expected an error (EOF) reading from an empty stream")
  }
}

func TestReadMessageIgnoresUnknownHeaders(t *testing.T) {
  raw := "X-Custom: whatever\r\nContent-Length: 13\r\n\r\n{\"a\":\"bcd\"}\r\n"
  r := bufio.NewReader(strings.NewReader(raw))

  body, err := readMessage(r)
  if err != nil {
    t.Fatalf("readMessage: %v", err)
  }
  if string(body) != "{\"a\":\"bcd\"}\r\n" {
    t.Fatalf("body = %q", body)
  }
}

func TestReadMessageSequential(t *testing.T) {
  var buf bytes.Buffer
  err := writeMessage(&buf, map[string]int{"n": 1})
  if err != nil {
    t.Fatal(err)
  }
  err = writeMessage(&buf, map[string]int{"n": 2})
  if err != nil {
    t.Fatal(err)
  }

  r := bufio.NewReader(&buf)
  first, err := readMessage(r)
  if err != nil {
    t.Fatal(err)
  }
  second, err := readMessage(r)
  if err != nil {
    t.Fatal(err)
  }

  if !strings.Contains(string(first), `"n":1`) || !strings.Contains(string(second), `"n":2`) {
    t.Fatalf("messages out of order or corrupted: %s / %s", first, second)
  }
}
