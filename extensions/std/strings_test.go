package std_test

import (
	"testing"

	"github.com/nethinwei/funroute"
)

// The string and array functions, each shown doing the job it exists for.
func TestStringsAndArrays(t *testing.T) {
	t.Parallel()
	card := funroute.ArgSpec{Name: "card", Type: funroute.StringType}
	fees := funroute.ArgSpec{Name: "fees", Type: funroute.ArrayOf(funroute.IntType)}
	names := funroute.ArgSpec{Name: "names", Type: funroute.ArrayOf(funroute.StringType)}
	for _, test := range []struct {
		name, source string
		specs        []funroute.ArgSpec
		args         map[string]any
		want         any
	}{
		{"取卡 BIN", `slice(trim(card), 0, 6)`, []funroute.ArgSpec{card}, map[string]any{"card": " 4111111111111111 "}, "411111"},
		{"判前缀", `starts_with(card, "4")`, []funroute.ArgSpec{card}, map[string]any{"card": "4111"}, true},
		{"规范化", `upper(card) == "AB"`, []funroute.ArgSpec{card}, map[string]any{"card": "ab"}, true},
		{"拆分再拼", `join(split(card, ","), "|")`, []funroute.ArgSpec{card}, map[string]any{"card": "a,b"}, "a|b"},
		{"替换", `replace(card, "_", ":")`, []funroute.ArgSpec{card}, map[string]any{"card": "a_b"}, "a:b"},
		// A byte that is not UTF-8 is kept as it is, not written as U+FFFD.
		{"大写不改别的字节", `upper(card)`, []funroute.ArgSpec{card}, map[string]any{"card": "a\xffé"}, "A\xffÉ"},
		{"小写不改别的字节", `lower(card)`, []funroute.ArgSpec{card}, map[string]any{"card": "A\xff"}, "a\xff"},
		{"切片不改别的字节", `slice(card, 1, 2)`, []funroute.ArgSpec{card}, map[string]any{"card": "a\xffb"}, "\xff"},
		{"最便宜", `first(sort(fees))`, []funroute.ArgSpec{fees}, map[string]any{"fees": []any{30, 10, 20}}, int64(10)},
		{"最贵", `last(sort(fees))`, []funroute.ArgSpec{fees}, map[string]any{"fees": []any{30, 10, 20}}, int64(30)},
		{"取前两个", `len(take(fees, 2))`, []funroute.ArgSpec{fees}, map[string]any{"fees": []any{1, 2, 3}}, int64(2)},
		{"去重后计数", `len(unique(names))`, []funroute.ArgSpec{names}, map[string]any{"names": []any{"a", "a", "b"}}, int64(2)},
		{"拉平", `len(flatten([[f] for f in fees]))`, []funroute.ArgSpec{fees}, map[string]any{"fees": []any{1, 2}}, int64(2)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := run(t, test.source, test.args, test.specs...)
			if err != nil {
				t.Fatalf("run %s: %v", test.source, err)
			}
			if got != test.want {
				t.Fatalf("%s = %v (%T), want %v", test.source, got, got, test.want)
			}
		})
	}
}

// Padding fills a fixed-width field: on the left or the right, one
// character at a time, and a text already as long is left as it is.
func TestPaddingFillsAField(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		`pad_left("1234", 8, "0")`:   "00001234",
		`pad_right("adyen", 7, ".")`: "adyen..",
		`pad_left("123456", 3, "0")`: "123456",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if got, err := run(t, source, nil); err != nil || got != want {
				t.Fatalf("%s = %v, %v, want %q", source, got, err, want)
			}
		})
	}
	if _, err := run(t, `pad_left("1", 4, "ab")`, nil); err == nil {
		t.Fatal(`pad_left("1", 4, "ab") must fail: the padding is one character`)
	}
}

// A padding width is 0 to 10000 characters: a width written as 4000000000
// is refused, not allocated — in a run and when the compiler folds it.
func TestPaddingWidthIsBounded(t *testing.T) {
	t.Parallel()
	width := funroute.ArgSpec{Name: "width", Type: funroute.IntType}
	if got, err := run(t, `len(pad_left("1", 10000, "0"))`, nil); err != nil || got != int64(10_000) {
		t.Fatalf("pad_left to 10000 = %v, %v, want 10000 characters", got, err)
	}
	for _, source := range []string{`pad_left("1", 10001, "0")`, `pad_right("1", 4000000000, "0")`, `pad_left("1", 0 - 1, "0")`} {
		if _, err := funroute.CompileExpr(source, registry(t), funroute.CompileOptions{}); err == nil {
			t.Errorf("CompileExpr(%q) compiled, want the width refused while folding", source)
		}
	}
	if _, err := run(t, `pad_left("1", width, "0")`, map[string]any{"width": 4_000_000_000}, width); err == nil {
		t.Fatal("pad_left to a run-time width of 4000000000 succeeded, want it refused")
	}
}
