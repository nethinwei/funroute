package machine

import (
	"math/rand/v2"
	"testing"
)

// s[i] and len(s) on the cursor answer as the functions do, whatever the
// cursor was left at: down a text and up it, jumping, past either end, on
// one text and on another, with characters of several bytes and bytes
// that are not UTF-8.
func TestTheTextCursorAnswersAsTheFunctions(t *testing.T) {
	t.Parallel()
	random := rand.New(rand.NewPCG(13, 14))
	pieces := []string{"a", "é", "名", "𝄞", "\xff", "\xe2\x82", "\xed\xa0\x80", "\xc3", "b"}
	b := newBanks(4, nil)
	for range 2000 {
		texts := make([]string, 1+random.IntN(3))
		for i := range texts {
			for range random.IntN(12) {
				texts[i] += pieces[random.IntN(len(pieces))]
			}
		}
		for step := range 40 {
			text := texts[random.IntN(len(texts))]
			index := cursorIndex(random, step, len(text))
			b.setValue(0, String(text))
			b.ints[1] = index
			want, wantErr := evalStringAt(t.Context(), []Value{String(text), Int(index)})
			got := charAt(&b, &rinstr{op: rAtS, a: 0, b: 1, c: 2})
			if got != (wantErr == nil) || got && b.regs[2].s != want.s {
				t.Fatalf("%q[%d] = %q, %v; the function says %q, %v", text, index, b.regs[2].s, got, want.s, wantErr)
			}
			if random.IntN(2) == 0 {
				continue
			}
			textLength(&b, &rinstr{op: rLenS, a: 0, c: 3})
			if length, _ := evalStringLength(t.Context(), []Value{String(text)}); b.ints[3] != length.i {
				t.Fatalf("len(%q) = %d, the function says %d", text, b.ints[3], length.i)
			}
		}
	}
}

// cursorIndex is a run's next index: going down the text, up it, or
// anywhere, past its ends among them.
func cursorIndex(random *rand.Rand, step, n int) int64 {
	switch random.IntN(3) {
	case 0:
		return int64(step % (n + 2))
	case 1:
		return int64(n - step%(n+2))
	}
	return int64(random.IntN(n+3)) - 1
}
