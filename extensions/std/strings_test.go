package std_test

import (
	"testing"

	"funroute/lang"
)

// The string and array functions, each shown doing the job it exists for.
func TestStringsAndArrays(t *testing.T) {
	t.Parallel()
	card := lang.ArgSpec{Name: "card", Type: lang.StringType}
	fees := lang.ArgSpec{Name: "fees", Type: lang.ArrayOf(lang.IntType)}
	names := lang.ArgSpec{Name: "names", Type: lang.ArrayOf(lang.StringType)}
	for _, test := range []struct {
		name, source string
		specs        []lang.ArgSpec
		args         map[string]any
		want         any
	}{
		{"取卡 BIN", `slice(trim(card), 0, 6)`, []lang.ArgSpec{card}, map[string]any{"card": " 4111111111111111 "}, "411111"},
		{"判前缀", `starts_with(card, "4")`, []lang.ArgSpec{card}, map[string]any{"card": "4111"}, true},
		{"规范化", `upper(card) == "AB"`, []lang.ArgSpec{card}, map[string]any{"card": "ab"}, true},
		{"拆分再拼", `join(split(card, ","), "|")`, []lang.ArgSpec{card}, map[string]any{"card": "a,b"}, "a|b"},
		{"替换", `replace(card, "_", ":")`, []lang.ArgSpec{card}, map[string]any{"card": "a_b"}, "a:b"},
		{"最便宜", `first(sort(fees))`, []lang.ArgSpec{fees}, map[string]any{"fees": []any{30, 10, 20}}, int64(10)},
		{"最贵", `last(sort(fees))`, []lang.ArgSpec{fees}, map[string]any{"fees": []any{30, 10, 20}}, int64(30)},
		{"取前两个", `len(take(fees, 2))`, []lang.ArgSpec{fees}, map[string]any{"fees": []any{1, 2, 3}}, int64(2)},
		{"去重后计数", `len(unique(names))`, []lang.ArgSpec{names}, map[string]any{"names": []any{"a", "a", "b"}}, int64(2)},
		{"拉平", `len(flatten([[f] for f in fees]))`, []lang.ArgSpec{fees}, map[string]any{"fees": []any{1, 2}}, int64(2)},
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
