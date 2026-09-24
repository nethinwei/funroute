package lsp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/nethinwei/funroute/internal/machine"
)

// Serve speaks the protocol over a stream, framed the way LSP frames it: a
// Content-Length header, a blank line, then the message. It returns when the
// client sends exit or the stream ends.
func Serve(r io.Reader, w io.Writer, registry *machine.Registry) error {
	var mu sync.Mutex
	server := New(registry, func(message []byte) {
		mu.Lock()
		defer mu.Unlock()
		_, _ = fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(message))
		_, _ = w.Write(message)
	})
	reader := bufio.NewReader(r)
	for !server.Exited() {
		message, err := readFrame(reader)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		server.Handle(message)
	}
	return nil
}

// maxFrame bounds one message. A rule is short; a frame larger than this is
// a broken stream, not a program.
const maxFrame = 16 << 20

// maxHeaders is how many header lines one frame may have. LSP sends one or
// two, each well within the reader's buffer: a stream with more, or with a
// line longer than the buffer, is broken, and reading on would hold it all.
const maxHeaders = 16

func readFrame(r *bufio.Reader) ([]byte, error) {
	length := -1
	for headers := 0; ; headers++ {
		if headers == maxHeaders {
			return nil, fmt.Errorf("a frame has more than %d header lines", maxHeaders)
		}
		raw, err := r.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			return nil, fmt.Errorf("a header line is longer than %d bytes", r.Size())
		}
		if err != nil {
			return nil, err
		}
		line := strings.TrimRight(string(raw), "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			if length, err = strconv.Atoi(strings.TrimSpace(value)); err != nil {
				return nil, fmt.Errorf("bad Content-Length %q", value)
			}
		}
	}
	if length < 0 || length > maxFrame {
		return nil, fmt.Errorf("a frame needs a Content-Length up to %d, got %d", maxFrame, length)
	}
	body := make([]byte, length)
	_, err := io.ReadFull(r, body)
	return body, err
}
