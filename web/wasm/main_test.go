//go:build js && wasm

package main

import (
	"slices"
	"syscall/js"
	"testing"
)

// A message the server sends before the page connects is dropped, not a
// call of the zero js.Value; once connected, each reaches the callback.
func TestThePeerDeliversOnlyOnceConnected(t *testing.T) {
	t.Parallel()
	page := new(peer)
	page.send([]byte("early"))
	var got []string
	callback := js.FuncOf(func(_ js.Value, args []js.Value) any {
		got = append(got, args[0].String())
		return nil
	})
	defer callback.Release()
	page.connect(callback.Value)
	page.send([]byte("one"))
	page.send([]byte("two"))
	if want := []string{"one", "two"}; !slices.Equal(got, want) {
		t.Fatalf("the callback got %q, want %q", got, want)
	}
}
