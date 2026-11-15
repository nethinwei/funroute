//go:build race

package kit

// Race reports a build with the race detector, whose own allocations a
// test that counts allocations would count: such a test skips under it.
const Race = true
