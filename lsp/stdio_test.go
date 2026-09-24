package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestServeFramesTheProtocolOnAStream(t *testing.T) {
	t.Parallel()
	var input strings.Builder
	for _, message := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"no/such"}`,
		`{"jsonrpc":"2.0","id":3,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
		`{"jsonrpc":"2.0","id":4,"method":"shutdown"}`,
	} {
		fmt.Fprintf(&input, "Content-Length: %d\r\n\r\n%s", len(message), message)
	}
	// Built here rather than in the goroutine: t.Fatal must not run off the
	// test's own goroutine.
	registry := standard(t)
	reader, writer := io.Pipe()
	go func() {
		_ = Serve(strings.NewReader(input.String()), writer, registry)
		_ = writer.Close()
	}()
	var ids []string
	frames := bufio.NewReader(reader)
	for {
		message, err := readFrame(frames)
		if err != nil {
			break
		}
		var decoded map[string]any
		_ = json.Unmarshal(message, &decoded)
		ids = append(ids, fmt.Sprint(decoded["id"], decoded["error"] != nil))
	}
	if strings.Join(ids, " ") != "1 false 2 true 3 false" {
		t.Errorf("the stream answered %v, want [1 false 2 true 3 false] and nothing after exit", ids)
	}
}

// A frame's headers are a few short lines: one longer than the reader's
// buffer, or more of them than a frame has, is a broken stream, refused
// before it is held in memory.
func TestAFramesHeadersAreBounded(t *testing.T) {
	t.Parallel()
	for name, stream := range map[string]string{
		"a line past the buffer": "X-Long: " + strings.Repeat("a", 8192) + "\r\n\r\n{}",
		"too many header lines":  strings.Repeat("X-Header: 1\r\n", maxHeaders) + "Content-Length: 2\r\n\r\n{}",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if message, err := readFrame(bufio.NewReader(strings.NewReader(stream))); err == nil {
				t.Fatalf("readFrame(%s) = %s, nil, want an error", name, message)
			}
		})
	}
	message, err := readFrame(bufio.NewReader(strings.NewReader("Content-Type: application/vscode-jsonrpc\r\nContent-Length: 2\r\n\r\n{}")))
	if err != nil || string(message) != "{}" {
		t.Fatalf("readFrame(two headers) = %s, %v, want {}", message, err)
	}
}
