package std_test

import (
	"context"
	"strings"
	"testing"

	"funroute/extensions/std"
	"funroute/lang"
)

// registry is a console with the structural forms and this pack: what a rule
// needs to map over an input and collapse the result.
func registry(t *testing.T) *lang.Registry {
	t.Helper()
	registry := lang.CoreRegistry()
	if err := registry.EnableForm(lang.SwitchForm, lang.ForForm); err != nil {
		t.Fatalf("enable forms: %v", err)
	}
	if err := std.Register(registry); err != nil {
		t.Fatalf("register std: %v", err)
	}
	return registry
}

func run(t *testing.T, source string, args map[string]any, specs ...lang.ArgSpec) (any, error) {
	t.Helper()
	artifact, err := lang.CompileExpr(source, registry(t), lang.CompileOptions{Args: specs})
	if err != nil {
		return nil, err
	}
	runtime, err := lang.Instantiate(artifact, registry(t))
	if err != nil {
		return nil, err
	}
	value, err := runtime.Run(context.Background(), args, lang.RunOptions{Fuel: 100_000})
	if err != nil {
		return nil, err
	}
	return value.Any(), nil
}

func TestAggregationsReplaceTheFoldConstruct(t *testing.T) {
	prices := lang.ArgSpec{Name: "prices", Type: lang.ArrayOf(lang.IntType)}
	rates := lang.ArgSpec{Name: "rates", Type: lang.ArrayOf(lang.FloatType)}
	for _, test := range []struct {
		name, source string
		specs        []lang.ArgSpec
		args         map[string]any
		want         any
	}{
		{"总额", "sum(prices)", []lang.ArgSpec{prices}, map[string]any{"prices": []any{3, 4, 5}}, int64(12)},
		{"筛选后求和", "sum([p for p in prices if p >= 4])", []lang.ArgSpec{prices}, map[string]any{"prices": []any{3, 4, 5}}, int64(9)},
		{"最大值", "max(rates)", []lang.ArgSpec{rates}, map[string]any{"rates": []any{0.2, 0.9, 0.5}}, 0.9},
		{"最小值", "min(prices)", []lang.ArgSpec{prices}, map[string]any{"prices": []any{3, 4, 5}}, int64(3)},
		{"任一为真", "any([p > 4 for p in prices])", []lang.ArgSpec{prices}, map[string]any{"prices": []any{3, 4, 5}}, true},
		{"全部为真", "all([p > 4 for p in prices])", []lang.ArgSpec{prices}, map[string]any{"prices": []any{3, 4, 5}}, false},
		{"空数组求和是零", "sum([p for p in prices if p > 99])", []lang.ArgSpec{prices}, map[string]any{"prices": []any{3}}, int64(0)},
		{"序列求和", "sum(range(4))", nil, nil, int64(6)},
		{"带步长的序列", "len(range(1, 10, 3))", nil, nil, int64(3)},
		{"常量绑定可以喂给序列", "sum(let(n = 2 + 1, range(n)))", nil, nil, int64(3)},
	} {
		t.Run(test.name, func(t *testing.T) {
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

func TestEmptyArrayHasNoExtreme(t *testing.T) {
	prices := lang.ArgSpec{Name: "prices", Type: lang.ArrayOf(lang.IntType)}
	_, err := run(t, "max([p for p in prices if p > 99])", map[string]any{"prices": []any{3}}, prices)
	if err == nil {
		t.Fatal("max of an empty array must fail rather than invent a value")
	}
}

// TestRangeTakesBoundedLengths is the bound this pack promises: a length the
// inputs already bound is fine, an arbitrary run-time scalar is not — that is
// the one that would let a single argument stand for an arbitrarily long array
// and break docs/termination.md.
func TestRangeTakesBoundedLengths(t *testing.T) {
	fees := lang.ArgSpec{Name: "fees", Type: lang.ArrayOf(lang.IntType)}
	bounded := map[string]any{"fees": []any{1, 2, 3}}
	for _, source := range []string{
		`len(range(3))`,
		`len(range(len(fees)))`,
		`len(range(len(fees) * 2))`,
	} {
		if _, err := run(t, source, bounded, fees); err != nil {
			t.Fatalf("%s should be allowed: %v", source, err)
		}
	}
	_, err := run(t, "sum(range(n))", map[string]any{"n": 3}, lang.ArgSpec{Name: "n", Type: lang.IntType})
	if err == nil {
		t.Fatal("range must refuse a length that is any scalar at run time")
	}
	if !strings.Contains(err.Error(), "bound") {
		t.Fatalf("error should say why: %v", err)
	}
}

// The string and array functions, each shown doing the job it exists for.
func TestStringsAndArrays(t *testing.T) {
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

// Everything in this pack is pure, so a call that reads no argument is done
// while the rule compiles.
func TestPackIsConstexpr(t *testing.T) {
	artifact, err := lang.CompileExpr(`sum(range(4)) + len(unique(["a", "a"]))`, registry(t), lang.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact.Instructions) != 1 {
		t.Fatalf("compiled to %d instructions, want one load", len(artifact.Instructions))
	}
}

// Out-of-range and empty cases are errors, not quiet answers.
func TestPackRefusesWhatHasNoAnswer(t *testing.T) {
	for _, source := range []string{
		`slice("abc", 0, 9)`,
		`first([n for n in [1] if n > 9])`,
		`split("a,b", "")`,
		`take([1, 2], 0 - 1)`,
	} {
		if _, err := run(t, source, nil); err == nil {
			t.Fatalf("%s must fail", source)
		}
	}
}

// The corners where one type used to have an operation and its neighbour did
// not, and the one place a dictionary could still be built with a repeated key.
func TestPackIsSymmetric(t *testing.T) {
	names := lang.ArgSpec{Name: "names", Type: lang.ArrayOf(lang.StringType)}
	fees := lang.ArgSpec{Name: "fees", Type: lang.ArrayOf(lang.IntType)}
	for _, test := range []struct {
		name, source string
		specs        []lang.ArgSpec
		args         map[string]any
		want         any
	}{
		{"字符串取最小", `min(names)`, []lang.ArgSpec{names}, map[string]any{"names": []any{"b", "a"}}, "a"},
		{"字符串取最大", `max(names)`, []lang.ArgSpec{names}, map[string]any{"names": []any{"b", "a"}}, "b"},
		{"数组取一段", `len(slice(fees, 1, 3))`, []lang.ArgSpec{fees}, map[string]any{"fees": []any{1, 2, 3, 4}}, int64(2)},
		{"整数绝对值", `abs(0 - 5)`, nil, nil, int64(5)},
		{"浮点绝对值", `abs(0.0 - 1.5)`, nil, nil, 1.5},
		// Rounding answers with an int: the caller wanted an integer, and
		// having to write int(floor(x)) to get one is a tax on every use.
		{"向上取整", `ceil(1.2)`, nil, nil, int64(2)},
		{"向下取整", `floor(1.8)`, nil, nil, int64(1)},
		{"四舍五入", `round(1.5)`, nil, nil, int64(2)},
		{"负数四舍五入", `round(0.0 - 1.5)`, nil, nil, int64(-2)},
		{"取整后直接算整数", `floor(1250.0 / 100.0) * 100`, nil, nil, int64(1200)},
		{"要 float 就显式转", `float(floor(1.8)) / 2.0`, nil, nil, 0.5},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := run(t, test.source, test.args, test.specs...)
			if err != nil {
				t.Fatalf("run %s: %v", test.source, err)
			}
			if got != test.want {
				t.Fatalf("%s = %v (%T), want %v", test.source, got, got, test.want)
			}
		})
	}
	// A dictionary refuses a repeated key whichever door it came in by.
	if _, err := run(t, `{k: 1 for k in names}`, map[string]any{"names": []any{"a", "a"}}, names); err == nil {
		t.Fatal("a comprehension that produces one key twice must fail")
	}
	// round takes a float to a whole float; turning it into an int is the
	// caller's explicit step, like every other cross-type move.
	got, err := run(t, `int(round(2.5))`, nil)
	if err != nil || got != int64(3) {
		t.Fatalf("int(round(2.5)) = %v, %v", got, err)
	}
}

// Picking one candidate out of several is what a routing rule does, and it
// takes two lists: the candidates and the key each is judged by.
func TestSelectingAmongCandidates(t *testing.T) {
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

// Grouping and running totals: the two list operations a single fold cannot do.
func TestGroupingAndRunningTotals(t *testing.T) {
	specs := []lang.ArgSpec{
		{Name: "amounts", Type: lang.ArrayOf(lang.IntType)},
		{Name: "channels", Type: lang.ArrayOf(lang.StringType)},
	}
	args := map[string]any{"amounts": []any{100, 200, 300}, "channels": []any{"adyen", "stripe", "adyen"}}
	totals, err := run(t, `{k: sum(v) for k, v in group_by(amounts, channels)}`, args, specs...)
	if err != nil {
		t.Fatal(err)
	}
	// dict<int> hands back its backing, which is a map[string]int64.
	grouped, _ := totals.(map[string]int64)
	if grouped["adyen"] != 400 || grouped["stripe"] != 200 {
		t.Fatalf("totals = %v", totals)
	}
	over, err := run(t, `first([i for i in indices(amounts) if cumsum(amounts)[i] > 250])`, args, specs...)
	if err != nil || over != int64(1) {
		t.Fatalf("the first transaction over the limit = %v, %v", over, err)
	}
}

// The statistics and formatting a rule reaches for when it judges an SLA or
// writes a reconciliation line.
func TestStatisticsAndFormatting(t *testing.T) {
	latencies := lang.ArgSpec{Name: "latencies", Type: lang.ArrayOf(lang.IntType)}
	args := map[string]any{"latencies": []any{100, 200, 300, 400}}
	for _, test := range []struct {
		name, source string
		specs        []lang.ArgSpec
		args         map[string]any
		want         any
	}{
		{"中位分位", `percentile(latencies, 0.5)`, []lang.ArgSpec{latencies}, args, 250.0},
		{"最低分位", `percentile(latencies, 0.0)`, []lang.ArgSpec{latencies}, args, 100.0},
		{"最高分位", `percentile(latencies, 1.0)`, []lang.ArgSpec{latencies}, args, 400.0},
		{"标准差", `stddev([2, 4, 4, 4, 5, 5, 7, 9])`, nil, nil, 2.0},
		{"整数幂", `pow(2, 10)`, nil, nil, int64(1024)},
		{"浮点幂", `pow(9.0, 0.5)`, nil, nil, 3.0},
		{"指数退避", `pow(2, 3) * 200`, nil, nil, int64(1600)},
		{"左补零", `pad_left("1234", 8, "0")`, nil, nil, "00001234"},
		{"右补点", `pad_right("adyen", 7, ".")`, nil, nil, "adyen.."},
		{"已够长就原样", `pad_left("123456", 3, "0")`, nil, nil, "123456"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := run(t, test.source, test.args, test.specs...)
			if err != nil {
				t.Fatalf("run %s: %v", test.source, err)
			}
			if got != test.want {
				t.Fatalf("%s = %v (%T), want %v", test.source, got, got, test.want)
			}
		})
	}
	for _, source := range []string{
		`percentile(latencies, 1.5)`,
		`pow(2, 0 - 1)`,
		`pad_left("1", 4, "ab")`,
		`stddev([n for n in latencies if n > 999])`,
	} {
		if _, err := run(t, source, args, latencies); err == nil {
			t.Fatalf("%s must fail", source)
		}
	}
}

// The shortcuts that exist so a rule writer does not have to compose them:
// "the three cheapest", "descending", "the last three", "the difference from
// the previous one".
func TestConvenienceShortcuts(t *testing.T) {
	channels := lang.ArgSpec{Name: "channels", Type: lang.ArrayOf(lang.StringType)}
	fees := lang.ArgSpec{Name: "fees", Type: lang.ArrayOf(lang.IntType)}
	allow := lang.ArgSpec{Name: "allow", Type: lang.ArrayOf(lang.StringType)}
	specs := []lang.ArgSpec{channels, fees, allow}
	args := map[string]any{
		"channels": []any{"adyen", "stripe", "pix"},
		"fees":     []any{30, 10, 20},
		"allow":    []any{"stripe", "pix"},
	}
	for _, test := range []struct {
		name, source string
		want         any
	}{
		{"最便宜的一个", `first(bottom_k(channels, fees, 1))`, "stripe"},
		{"最贵的一个", `first(top_k(channels, fees, 1))`, "adyen"},
		{"按费用降序", `first(sort_by_desc(channels, fees))`, "adyen"},
		{"降序排序", `first(sort_desc(fees))`, int64(30)},
		{"白名单交集", `len(intersect(channels, allow))`, int64(2)},
		{"黑名单差集", `first(except(channels, allow))`, "adyen"},
		{"滑动窗口", `len(windows(fees, 2))`, int64(2)},
		{"窗口内合计", `first([sum(w) for w in windows(fees, 2)])`, int64(40)},
		{"分批", `len(chunk(fees, 2))`, int64(2)},
		{"相邻差", `first(deltas(fees))`, int64(-20)},
		{"k 超过长度就取完", `len(bottom_k(channels, fees, 99))`, int64(3)},
		{"窗口比数组宽就没有", `len(windows(fees, 9))`, int64(0)},
		{"一项没有相邻差", `len(deltas([1]))`, int64(0)},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := run(t, test.source, args, specs...)
			if err != nil {
				t.Fatalf("run %s: %v", test.source, err)
			}
			if got != test.want {
				t.Fatalf("%s = %v (%T), want %v", test.source, got, got, test.want)
			}
		})
	}
	for _, source := range []string{`windows(fees, 0)`, `chunk(fees, 0)`, `bottom_k(channels, fees, 0 - 1)`} {
		if _, err := run(t, source, args, specs...); err == nil {
			t.Fatalf("%s must fail", source)
		}
	}
}

// take_while and drop_while cut a list where a run of trues ends. The test is
// a parallel array, the same shape every other pair here takes, so "the items
// until the running total passes the cap" needs no lambda.
func TestCuttingWhereARunEnds(t *testing.T) {
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

// A dictionary's missing key stays an error, because the language has no null
// to put in a routing decision. get is where a rule says what the absence
// means, and merge is how two layers of configuration become one.
func TestDictionaryDefaultsAndLayers(t *testing.T) {
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

// A name that serves several element types has to serve all of them. Dropping
// one registration is invisible: the function still exists and still works for
// the types it kept, so nothing fails until a rule reaches the missing type in
// production — and what it gets then is "no overload", which reads like the
// rule is wrong rather than the pack incomplete.
//
// The counts below are the point of the test, so each one says where it comes
// from. A number that changes here is either a deliberate extension or the
// registration that went missing.
func TestNamesCoverEveryElementTypeTheyClaim(t *testing.T) {
	overloads := map[string]int{}
	for _, function := range lang.Catalog(registry(t)).Functions {
		overloads[function.Name]++
	}
	for _, expected := range []struct {
		name  string
		count int
		why   string
	}{
		{"sum", 2, "int / float"},
		{"avg", 2, "int / float"},
		{"median", 2, "int / float"},
		{"stddev", 2, "int / float"},
		{"percentile", 2, "int / float"},
		{"cumsum", 2, "int / float"},
		{"deltas", 2, "int / float"},
		{"abs", 2, "int / float"},
		{"pow", 2, "int / float"},
		{"sort", 3, "int / float / string"},
		{"sort_desc", 3, "int / float / string"},
		{"rank", 3, "int / float / string"},
		{"arg_min", 3, "键是 int / float / string"},
		{"arg_max", 3, "键是 int / float / string"},
		{"sort_by", 3, "键是 int / float / string"},
		{"sort_by_desc", 3, "键是 int / float / string"},
		{"top_k", 3, "键是 int / float / string"},
		{"bottom_k", 3, "键是 int / float / string"},
		{"min", 6, "数组版 3 种元素类型，二元版 3 种"},
		{"max", 6, "数组版 3 种元素类型，二元版 3 种"},
	} {
		if got := overloads[expected.name]; got != expected.count {
			t.Errorf("%s 有 %d 个重载，应为 %d（%s）", expected.name, got, expected.count, expected.why)
		}
	}
}
