package std_test

import (
	"testing"

	"funroute/lang"
)

// Picking one candidate out of several is what a routing rule does, and it
// takes two lists: the candidates and the key each is judged by.
func TestSelectingAmongCandidates(t *testing.T) {
	t.Parallel()
	channels := lang.ArgSpec{Name: "channels", Type: lang.ArrayOf(lang.StringType)}
	fees := lang.ArgSpec{Name: "fees", Type: lang.ArrayOf(lang.IntType)}
	specs := []lang.ArgSpec{channels, fees}
	args := map[string]any{"channels": []any{"adyen", "stripe", "pix"}, "fees": []any{30, 10, 20}}
	for _, test := range []struct {
		name, source string
		want         any
	}{
		{"最便宜的渠道", `channels[arg_min(fees)]`, "stripe"},
		{"最贵的渠道", `channels[arg_max(fees)]`, "adyen"},
		{"按费用排序", `first(sort_by(channels, fees))`, "stripe"},
		{"取最便宜的两个", `len(take(sort_by(channels, fees), 2))`, int64(2)},
		{"元素位置", `index_of(channels, "pix")`, int64(2)},
		{"名次", `rank(fees)[0]`, int64(3)},
		{"平均", `avg(fees)`, 20.0},
		{"中位", `median(fees)`, 20.0},
		{"下标遍历", `len([channels[i] for i in indices(fees) if fees[i] < 25])`, int64(2)},
		{"费率封顶", `min(fees[0], 15)`, int64(15)},
		{"下限", `max(fees[1], 15)`, int64(15)},
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
	// The two lists have to line up: a candidate with no key has no answer.
	if _, err := run(t, `sort_by(channels, [1])`, args, specs...); err == nil {
		t.Fatal("sort_by must refuse lists of different lengths")
	}
	// A missing element is an error, not a -1 the caller has to remember.
	if _, err := run(t, `index_of(channels, "none")`, args, specs...); err == nil {
		t.Fatal("index_of must fail when the item is not there")
	}
}

// take_while and drop_while cut a list where a run of trues ends. The test is
// a parallel array, the same shape every other pair here takes, so "the items
// until the running total passes the cap" needs no lambda.
func TestCuttingWhereARunEnds(t *testing.T) {
	t.Parallel()
	amounts := lang.ArgSpec{Name: "amounts", Type: lang.ArrayOf(lang.IntType)}
	cap := lang.ArgSpec{Name: "cap", Type: lang.IntType}
	specs := []lang.ArgSpec{amounts, cap}
	args := map[string]any{"amounts": []any{300, 400, 500, 200}, "cap": 800}
	running := `[t <= cap for t in cumsum(amounts)]`
	for _, test := range []struct {
		name, source string
		want         any
	}{
		{"取到超限为止", `len(take_while(amounts, ` + running + `))`, int64(2)},
		{"超限的接着拿", `first(drop_while(amounts, ` + running + `))`, int64(500)},
		{"两半加起来是全部", `len(take_while(amounts, ` + running + `)) + len(drop_while(amounts, ` + running + `))`, int64(4)},
		{"第一项就不满足", `len(take_while(amounts, [false, false, false, false]))`, int64(0)},
		{"全部满足", `len(take_while(amounts, [true, true, true, true]))`, int64(4)},
		// The run stops at the first false; a later true does not restart it.
		{"后面的 true 不算", `len(take_while(amounts, [true, false, true, true]))`, int64(1)},
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
	// Mismatched lengths are an error, not a silently shorter answer.
	if _, err := run(t, `len(take_while(amounts, [true, true]))`, args, specs...); err == nil {
		t.Fatal("take_while accepted a test array of a different length")
	}
}
