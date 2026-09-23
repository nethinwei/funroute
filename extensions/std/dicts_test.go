package std_test

import (
	"testing"

	"funroute/lang"
)

// A dictionary's missing key stays an error, because the language has no null
// to put in a routing decision. get is where a rule says what the absence
// means, and merge is how two layers of configuration become one.
func TestDictionaryDefaultsAndLayers(t *testing.T) {
	t.Parallel()
	defaults := lang.ArgSpec{Name: "defaults", Type: lang.DictOf(lang.IntType)}
	overrides := lang.ArgSpec{Name: "overrides", Type: lang.DictOf(lang.IntType)}
	specs := []lang.ArgSpec{defaults, overrides}
	args := map[string]any{
		"defaults":  map[string]any{"SG": 250, "US": 300},
		"overrides": map[string]any{"SG": 180, "JP": 120},
	}
	for _, test := range []struct {
		name, source string
		want         any
	}{
		{"命中就用它", `get(defaults, "SG", 0)`, int64(250)},
		{"缺键用默认", `get(defaults, "ZZ", 0)`, int64(0)},
		{"覆盖层优先", `get(merge(defaults, overrides), "SG", 0)`, int64(180)},
		{"没被覆盖的留着", `get(merge(defaults, overrides), "US", 0)`, int64(300)},
		{"覆盖层带来的新键", `get(merge(defaults, overrides), "JP", 0)`, int64(120)},
		{"合并后的键数", `len(merge(defaults, overrides))`, int64(3)},
		{"合并自己不改变什么", `len(merge(defaults, defaults))`, int64(2)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := run(t, test.source, args, specs...)
			if err != nil {
				t.Fatalf("run %s: %v", test.source, err)
			}
			if got != test.want {
				t.Fatalf("%s = %v (%T), want %v", test.source, got, got, test.want)
			}
		})
	}
	// The plain lookup keeps reporting a missing key, which is what makes get
	// a decision rather than a habit.
	if _, err := run(t, `defaults["ZZ"]`, args, specs...); err == nil {
		t.Fatal("a missing key was not an error")
	}
}
