//go:build js && wasm

// Command wasm is the language server compiled for the browser: the same
// lsp.Server that runs on stdio, reached through two functions instead of a
// stream. The page, or rather its worker, calls funroute.connect(callback)
// once and funroute.send(message) for every message; the server calls
// callback with each message it sends.
//
// The registry is the example console's: its functions are plain Go, so the
// page can run programs as well as check them.
package main

import (
	"sync"
	"syscall/js"

	"funroute/examples/payment"
	"funroute/lang/lsp"
)

func main() {
	registry, err := payment.NewRegistry()
	if err != nil {
		panic(err)
	}
	var callback js.Value
	server := lsp.New(registry, func(message []byte) { callback.Invoke(string(message)) })
	inbox := newQueue()
	js.Global().Set("funroute", js.ValueOf(map[string]any{
		"connect": js.FuncOf(func(_ js.Value, args []js.Value) any {
			callback = args[0]
			return nil
		}),
		// send must not block: a JS callback that waits deadlocks the Go
		// runtime. It queues, and the loop below handles in order.
		"send": js.FuncOf(func(_ js.Value, args []js.Value) any {
			inbox.push([]byte(args[0].String()))
			return nil
		}),
	}))
	for {
		server.Handle(inbox.pop())
	}
}

// queue keeps messages in the order they were sent without ever making the
// sender wait.
type queue struct {
	mu    sync.Mutex
	items [][]byte
	ready chan struct{}
}

func newQueue() *queue { return &queue{ready: make(chan struct{}, 1)} }

func (q *queue) push(message []byte) {
	q.mu.Lock()
	q.items = append(q.items, message)
	q.mu.Unlock()
	select {
	case q.ready <- struct{}{}:
	default:
	}
}

func (q *queue) pop() []byte {
	for {
		q.mu.Lock()
		if len(q.items) > 0 {
			message := q.items[0]
			q.items = q.items[1:]
			q.mu.Unlock()
			return message
		}
		q.mu.Unlock()
		<-q.ready
	}
}
