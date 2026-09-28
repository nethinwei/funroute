package kit

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Map and SortedKeys never answer nil: a caller that encodes the result
// writes [] for none, never null.
func TestMapAndSortedKeysAreNeverNil(t *testing.T) {
	t.Parallel()
	if got := Map([]int(nil), strconv.Itoa); got == nil || len(got) != 0 {
		t.Errorf("Map(nil, Itoa) = %#v, want an empty slice that is not nil", got)
	}
	if got := SortedKeys(map[string]int(nil)); got == nil || len(got) != 0 {
		t.Errorf("SortedKeys(nil) = %#v, want an empty slice that is not nil", got)
	}
	if got, want := Map([]int{1, 2}, strconv.Itoa), []string{"1", "2"}; !slices.Equal(got, want) {
		t.Errorf("Map([1 2], Itoa) = %q, want %q", got, want)
	}
	if got, want := SortedKeys(map[string]int{"b": 1, "a": 2}), []string{"a", "b"}; !slices.Equal(got, want) {
		t.Errorf("SortedKeys({b, a}) = %q, want %q", got, want)
	}
}

// Repeated finds the first key that comes a second time.
func TestRepeatedFindsTheFirstRepeat(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		items []string
		want  string
		twice bool
	}{
		{nil, "", false},
		{[]string{"a", "b"}, "", false},
		{[]string{"a", "b", "b", "a"}, "b", true},
		{[]string{"", ""}, "", true},
	} {
		t.Run(strconv.Quote(strings.Join(test.items, ",")), func(t *testing.T) {
			t.Parallel()
			if got, twice := Repeated(test.items, Identity[string]); got != test.want || twice != test.twice {
				t.Errorf("Repeated(%q) = %q, %v, want %q, %v", test.items, got, twice, test.want, test.twice)
			}
		})
	}
}

// Reach keeps what the items held, zeroes what they did not, and grows by
// doubling: filling a table index by index allocates a logarithmic number
// of times.
func TestReachKeepsTheItemsAndDoubles(t *testing.T) {
	t.Parallel()
	items := Reach([]int{7}, 3)
	if !slices.Equal(items, []int{7, 0, 0, 0}) {
		t.Fatalf("Reach([7], 3) = %v, want [7 0 0 0]", items)
	}
	if same := Reach(items, 2); &same[0] != &items[0] || len(same) != 4 {
		t.Fatalf("Reach of an index it holds = %v, want the items themselves", same)
	}
	grown := 0
	var table []bool
	for i := range 1 << 12 {
		before := cap(table)
		table = Reach(table, i)
		if cap(table) != before {
			grown++
		}
	}
	if grown > 20 {
		t.Fatalf("filling 4096 entries grew the table %d times, want a logarithmic number", grown)
	}
}
