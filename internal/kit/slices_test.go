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
