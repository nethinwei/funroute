package compile

import (
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
)

// foldRulesRegistry is the kernel with every form and two host functions
// over a list: one that says it hands back no more than it is given, and
// one that does not say.
func foldRulesRegistry(t *testing.T) (*machine.Registry, CompileOptions) {
	t.Helper()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ForForm, machine.ReduceForm, machine.SwitchForm); err != nil {
		t.Fatal(err)
	}
	same := func(xs []int64) []int64 { return xs }
	for _, spec := range []machine.FunctionSpec{
		{Name: "t.keep_v1", Go: same, Doc: machine.Doc{Additive: true}},
		{Name: "t.grow_v1", Go: same},
	} {
		if err := registry.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
	return registry, CompileOptions{Args: []ArgSpec{
		{Name: "xs", Type: machine.ArrayOf(machine.IntType)},
		{Name: "ss", Type: machine.ArrayOf(machine.StringType)},
	}}
}

// A reduce's accumulator may grow by what a turn brings: the next one holds
// each part of this one once, through functions that hand back no more than
// they are given, and anything that does not come from it. What is of a
// fixed size — a count, a flag, a number — may be computed from it any way.
func TestAnAccumulatorThatGrowsByATurnsWorthCompiles(t *testing.T) {
	t.Parallel()
	registry, options := foldRulesRegistry(t)
	for _, source := range []string{
		`reduce(x in xs, a = 0, a + x)`,
		`reduce(x in xs, a = [1], concat(a, [x]))`,
		`reduce(s in ss, t = "", t + s + ",")`,
		`reduce(x in xs, a = [1], if(x > 0, concat(a, [x]), a))`,
		`reduce(x in xs, a = [1], switch(x, case 1 => concat(a, [x]), else => a))`,
		`reduce(x in xs, a = [1], fallback(concat(a, [10 / x]), a))`,
		`reduce(x in xs, a = [1], sort(unique(concat(a, [x]))))`,
		`reduce(x in xs, a = [1], t.keep_v1(a))`,
		`reduce(x in xs, a = [1], let(b = a, concat(b, [x])))`,
		`reduce(x in xs, a = [1], let(a = [x], concat(a, a)))`,
		`reduce(x in xs, a = [1], concat(a, [y * 2 for y in xs]))`,
		`reduce(x in xs, a = [1], concat(a, range(len(a))))`,
		`reduce(x in xs, a = [1], [y for y in a if y != x])`,
		`reduce(x in xs, a = [1], [y + 1 for y in a])`,
		`reduce(x in xs, a = [1], reduce(y in xs, b = a, concat(b, [y])))`,
		`reduce(x in xs, a = {"k": 1}, merge(a, {"j": x}))`,
		`reduce(x in xs, a = {"k": 1}, {k: v for k, v in a if v != x})`,
		// What is of a fixed size reads the accumulator as it likes.
		`reduce(x in xs, a = [1], concat(a, [len(concat(a, a))]))`,
		`reduce(s in ss, t = "", if(len(split(t + t, ",")) > 3, t, t + s))`,
		`reduce(x in xs, a = [1], let(n = len(join([string(y) for y in a], ",")), concat(a, [n])))`,
		// A let's value that is never read for what it holds is not held to
		// the rule: only its length is asked here.
		`reduce(s in ss, t = "", let(j = replace("aa", "a", t), if(len(j) > 3, t, t + s)))`,
		// A record's fields are parts of their own.
		`reduce(x in xs, a = {ids: [1], n: 0}, {ids: concat(a.ids, [x]), n: a.n + len(a.ids)})`,
		`reduce(x in xs, a = {ids: [1], n: 0}, a with {ids: concat(a.ids, [x])})`,
		`reduce(x in xs, a = {ids: [1], n: 0}, a with {n: len(concat(a.ids, a.ids))})`,
		`reduce(x in xs, a = {l: [1], r: [2]}, {l: a.r, r: a.l})`,
		`reduce(x in xs, a = {l: [1], r: [2]}, {l: concat(a.l, [x]), r: concat(a.r, [x])})`,
		`reduce(x in xs, a = {box: {l: [1], r: [2]}}, {box: {l: a.box.r, r: a.box.l}})`,
	} {
		if _, err := CompileExpr(source, registry, options); err != nil {
			t.Errorf("%s: %v", source, err)
		}
	}
}

// One that a turn could make a multiple of what it was is refused where the
// rule is compiled, with what would do it: the accumulator used twice, sent
// through a function that may hand back more than it is given, read for
// every item of a loop, or its items made anew.
func TestAnAccumulatorThatCouldMultiplyIsRefused(t *testing.T) {
	t.Parallel()
	registry, options := foldRulesRegistry(t)
	for source, want := range map[string]string{
		`reduce(x in xs, a = [1], concat(a, a))`:                                                         "goes twice",
		`reduce(x in [1, 2, 3], a = [1], concat(a, a))`:                                                  "goes twice",
		`reduce(s in ss, t = "ab", t + t)`:                                                               "goes twice",
		`reduce(x in xs, a = [[1]], [first(a), first(a)])`:                                               "goes twice",
		`reduce(x in xs, a = [[1]], concat(a, [first(a)]))`:                                              "goes twice",
		`reduce(x in xs, a = [1], if(x > 0, concat(a, a), a))`:                                           "goes twice",
		`reduce(x in xs, a = [1], switch(x, case 1 => a, else => concat(a, a)))`:                         "goes twice",
		`reduce(x in xs, a = [1], fallback(concat(a, a), a))`:                                            "goes twice",
		`reduce(x in xs, a = [1], let(b = a, concat(b, b)))`:                                             "goes twice",
		`reduce(x in xs, a = [1], let(b = a, concat(a, b)))`:                                             "goes twice",
		`reduce(x in xs, a = [1], let(b = concat(a, [x]), c = b, concat(c, a)))`:                         "goes twice",
		`reduce(x in xs, a = {box: {l: [1]}, m: [2]}, {box: a.box, m: a.box.l})`:                         "goes twice",
		`reduce(x in xs, a = {box: {l: [1]}, m: [2]}, {box: {l: a.box.l}, m: first([a]).m})`:             "goes twice",
		`reduce(x in xs, a = {l: [1], r: [2]}, {l: a.l, r: a.l})`:                                        "goes twice",
		`reduce(x in xs, a = {l: [1], r: [2]}, {l: concat(a.l, a.r), r: concat(a.r, a.l)})`:              "goes twice",
		`reduce(x in xs, a = {l: [1], r: [2]}, a with {l: concat(a.l, a.r)})`:                            "goes twice",
		`reduce(x in xs, a = {l: [1], n: 1}, {l: concat(a.l, (a with {n: 2}).l), n: 1})`:                 "goes twice",
		`reduce(x in xs, a = {box: {l: [1], r: [2]}}, {box: {l: a.box.l, r: concat(a.box.r, a.box.l)}})`: "goes twice",
		`reduce(x in xs, a = {box: {l: [1], r: [2]}}, {box: (a.box with {l: a.box.r})})`:                 "goes twice",
		`reduce(s in ss, t = "ab", join(split(t, ""), "x"))`:                                             "join may hand back more",
		`reduce(s in ss, t = "ab", replace("aa", "a", t))`:                                               "replace may hand back more",
		`reduce(s in ss, t = ["ab"], [join(t, ",")])`:                                                    "join may hand back more",
		`reduce(x in xs, a = [1], t.grow_v1(a))`:                                                         "t.grow_v1 may hand back more",
		`reduce(x in xs, a = [1], [z for y in xs for z in a])`:                                           "goes into a loop",
		`reduce(x in xs, a = [[1]], [a[0] for y in xs])`:                                                 "goes into a loop",
		`reduce(x in xs, a = [1], reduce(y in xs, b = [0], concat(b, a)))`:                               "goes into a loop",
		`reduce(x in xs, a = [1], let(b = a, flatten([b for y in xs])))`:                                 "goes into a loop",
		`reduce(s in ss, a = ["a"], [y + y for y in a])`:                                                 "made anew",
		`reduce(x in xs, a = [[1]], [concat(y, y) for y in a])`:                                          "made anew",
		`reduce(x in xs, a = [1], [z for y in a for z in [y, y]])`:                                       "made anew",
		`reduce(x in xs, a = {"k": "v"}, {k + k: v for k, v in a})`:                                      "made anew",
		`reduce(x in xs, a = [1], reduce(y in a, b = [0], concat(b, [y])))`:                              "made anew",
	} {
		_, err := CompileExpr(source, registry, options)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error = %v, want one that says %q", source, err, want)
		}
	}
}
