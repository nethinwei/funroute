package compile

import (
	"strings"
	"testing"
)

// Exact money flows to the round around it through sums, counts,
// comparisons, the branches of if and switch, fallback and let, and the
// rounding variants take it too.
func TestExactMoneyFlowsToItsRound(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"round(a * 2.9% + a * 1%, @up)",
		"round((a * 2.9%) * 3 - a, @up)",
		"round(if(a * 2.9% > b, a * 2.9%, b), @up)",
		"round(let(fee = a * 2.9%, fee + fee), @up)",
		"round(fallback(a * 2.9%, b), @up)",
		"round(switch(f, case true => a * 2.9%, else => b), @up)",
		"round(-(a * 2.9%), @up)",
		"round(mul(a * 2.9%, 50%, @up) + a * 1%, @down)",
		"if(round(a * 2.9%, @up) == b, 1, sign(a))",
		"round(a * 2.9%, @up) + mul(a, 1%, @down)",
	} {
		if _, err := compileMoney(t, source, "a: money; b: money; f: bool", ""); err != nil {
			t.Errorf("CompileExpr(%q) error = %v", source, err)
		}
	}
}

// Exact money reaches nothing that has no place for money between minor
// units: a host function, a container, a record, a comprehension, the
// amounts a step divides by, or the result.
func TestExactMoneyStaysInsideItsRound(t *testing.T) {
	t.Parallel()
	for source, where := range map[string]string{
		"round([a * 2.9%][0], @up)":                        "an array",
		"round({fee: a * 2.9%}.fee, @up)":                  "a record",
		`round({"x": a * 2.9%}["x"], @up)`:                 "a dictionary",
		"round(minor(a * 2.9%) * a, @up)":                  "minor",
		"round(prorate(a, a * 2.9%, b), @up)":              "prorate's other operands",
		"round(reduce(x in xs, acc = a, acc * 2.9%), @up)": "a reduce",
		"round(len([x * 2.9% for x in xs]) * a, @up)":      "a comprehension",
		// A loop's name hides a let's only inside the loop: its source and a
		// reduce's init are read outside, where the let's name is still bound.
		"round(let(e = a * 33%, reduce(e in xs, s = e, s)), @up)": "a reduce",
		"round(let(e = a * 33%, [e for e in [e]][0]), @up)":       "an array",
	} {
		_, err := compileMoney(t, source, "a: money; b: money; xs: array<money>", "")
		if err == nil || !strings.Contains(err.Error(), "only a round takes it out") || !strings.Contains(err.Error(), where) {
			t.Errorf("CompileExpr(%q) error = %v, want one saying it cannot go into %s", source, err, where)
		}
	}
}

// A step outside every round names its rounding or it does not compile,
// wherever it sits — a let outside the round does not count as inside.
func TestAStepOutsideARoundNamesItsRounding(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"a * 2.9%", "a / 97%", "let(fee = a * 2.9%, round(fee + a * 1%, @up))", "[x * 2.9% for x in xs]", "prorate(a, 1, 3)"} {
		_, err := compileMoney(t, source, "a: money; xs: array<money>", "")
		if err == nil || !strings.Contains(err.Error(), "lands between two minor units") {
			t.Errorf("CompileExpr(%q) error = %v, want one naming the step", source, err)
		}
	}
}
