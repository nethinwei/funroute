package std_test

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"strings"
	"testing"

	"funroute/extensions/std"
	"funroute/lang"
)

// moneyPack is a console that declared the ISO table before registering the
// pack, the order the pack needs.
func moneyPack(t *testing.T) *lang.Registry {
	t.Helper()
	return moneyPackRounding(t, lang.RoundHalfEven)
}

// moneyPackRounding is moneyPack with another default rounding.
func moneyPackRounding(t *testing.T, rounding lang.Rounding) *lang.Registry {
	t.Helper()
	registry := lang.CoreRegistry()
	if err := registry.EnableForm(lang.ForForm, lang.ReduceForm); err != nil {
		t.Fatal(err)
	}
	if err := registry.DeclareMoney(lang.MoneySpec{Rounding: rounding, Currencies: std.ISO4217()}); err != nil {
		t.Fatal(err)
	}
	if err := std.Register(registry); err != nil {
		t.Fatal(err)
	}
	return registry
}

func runMoney(t *testing.T, source string, args map[string]any, specs ...lang.ArgSpec) (string, error) {
	t.Helper()
	return runMoneyOn(t, moneyPack(t), source, args, specs...)
}

func runMoneyOn(t *testing.T, registry *lang.Registry, source string, args map[string]any, specs ...lang.ArgSpec) (string, error) {
	t.Helper()
	artifact, err := lang.CompileExpr(source, registry, lang.CompileOptions{Args: specs})
	if err != nil {
		return "", err
	}
	runtime, err := lang.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(t.Context(), args, lang.RunOptions{Fuel: 100_000})
	if err != nil {
		return "", err
	}
	return fmt.Sprint(value.Any()), nil
}

// The argument shapes the money tests share: fees whose currency the
// contract names (money<c>), fees in a currency only the run knows (money),
// an amount that binds c, and candidates to select among.
var (
	boundFees   = lang.ArgSpec{Name: "fees", Type: lang.ArrayOf(lang.MoneyOf("c"))}
	unknownFees = lang.ArgSpec{Name: "fees", Type: lang.ArrayOf(lang.MoneyOf(""))}
	boundAmount = lang.ArgSpec{Name: "amount", Type: lang.MoneyOf("c")}
	candidates  = lang.ArgSpec{Name: "names", Type: lang.ArrayOf(lang.StringType)}
)

// minor is money in the JSON form that takes any minor-unit figure, the
// currency-less zero ("", 0) and the int64 extremes included.
func minor(currency string, amount int64) map[string]any {
	return map[string]any{"currency": currency, "minor": amount}
}

// euros is a list of EUR amounts given in cents.
func euros(cents ...int64) []any {
	out := make([]any, len(cents))
	for i, cent := range cents {
		out[i] = minor("EUR", cent)
	}
	return out
}

var zero = minor("", 0)

type moneyCase struct {
	name, source string
	args         map[string]any
	want         string
}

// checkMoney runs each case as its own subtest against specs.
func checkMoney(t *testing.T, specs []lang.ArgSpec, cases []moneyCase) {
	t.Helper()
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := runMoney(t, test.source, test.args, specs...)
			if err != nil || got != test.want {
				t.Fatalf("%s with %v = %s, %v; want %s", test.source, test.args, got, err, test.want)
			}
		})
	}
}

// expectMoney is one run that must answer want.
func expectMoney(t *testing.T, registry *lang.Registry, source string, args map[string]any, want string, specs ...lang.ArgSpec) {
	t.Helper()
	got, err := runMoneyOn(t, registry, source, args, specs...)
	if err != nil || got != want {
		t.Errorf("%s with %v = %s, %v; want %s", source, args, got, err, want)
	}
}

// expectMoneyError is one run that must fail with target.
func expectMoneyError(t *testing.T, source string, args map[string]any, target error, specs ...lang.ArgSpec) {
	t.Helper()
	got, err := runMoney(t, source, args, specs...)
	if !errors.Is(err, target) {
		t.Errorf("%s with %v = %s, error %v; want %v", source, args, got, err, target)
	}
}

func TestAggregatesKeepTheCurrency(t *testing.T) {
	t.Parallel()
	fees := lang.ArgSpec{Name: "fees", Type: lang.ArrayOf(lang.MoneyOf("c"))}
	args := map[string]any{"fees": []any{"EUR 0.30", "EUR 0.25", "EUR 0.40", "EUR 0.26"}}
	for source, want := range map[string]string{
		"sum(fees)":             "{EUR 121}",
		"max(fees)":             "{EUR 40}",
		"arg_min(fees)":         "1",
		"avg(fees)":             "{EUR 30}",
		"median(fees)":          "{EUR 28}",
		"round(avg(fees), @up)": "{EUR 31}",
		"sort(fees)[0]":         "{EUR 25}",
		"cumsum(fees)[1]":       "{EUR 55}",
		"sum([f for f in fees if f > like(f, 100)])": "{EUR 0}",
		`bottom_k(["a","b","c","d"], fees, 2)`:       "[b d]",
	} {
		got, err := runMoney(t, source, args, fees)
		if err != nil || got != want {
			t.Fatalf("%s = %s, %v, want %s", source, got, err, want)
		}
	}
}

// Each function on ordinary lists: one amount, negative amounts, currencies
// with 0 and 3 decimal places, ties.
func TestMoneyAggregatesOverOrdinaryLists(t *testing.T) {
	t.Parallel()
	checkMoney(t, []lang.ArgSpec{boundFees}, []moneyCase{
		{"sum 正负相抵", "sum(fees)", map[string]any{"fees": euros(30, -10, 5)}, "{EUR 25}"},
		{"sum 单元素", "sum(fees)", map[string]any{"fees": euros(-7)}, "{EUR -7}"},
		{"sum 全负", "sum(fees)", map[string]any{"fees": euros(-1, -2)}, "{EUR -3}"},
		{"sum 零位小数", "sum(fees)", map[string]any{"fees": []any{"JPY 100", "JPY 5"}}, "{JPY 105}"},
		{"sum 三位小数", "sum(fees)", map[string]any{"fees": []any{"KWD 1.005", "KWD 0.001"}}, "{KWD 1006}"},
		{"min 负金额", "min(fees)", map[string]any{"fees": euros(3, -4, 0)}, "{EUR -4}"},
		{"max 全负", "max(fees)", map[string]any{"fees": euros(-3, -1, -2)}, "{EUR -1}"},
		{"min 单元素", "min(fees)", map[string]any{"fees": euros(9)}, "{EUR 9}"},
		{"max 单元素", "max(fees)", map[string]any{"fees": euros(9)}, "{EUR 9}"},
		{"arg_min 并列取第一个", "arg_min(fees)", map[string]any{"fees": euros(2, 1, 1)}, "1"},
		{"arg_max 并列取第一个", "arg_max(fees)", map[string]any{"fees": euros(3, 1, 3)}, "0"},
		{"arg_min 负金额", "arg_min(fees)", map[string]any{"fees": euros(0, -1, 5)}, "1"},
		{"arg_max 单元素", "arg_max(fees)", map[string]any{"fees": euros(-5)}, "0"},
		{"二元 min", "min(fees[0], fees[1])", map[string]any{"fees": euros(5, -5)}, "{EUR -5}"},
		{"二元 max", "max(fees[0], fees[1])", map[string]any{"fees": euros(5, -5)}, "{EUR 5}"},
		{"二元 max 相等", "max(fees[0], fees[1])", map[string]any{"fees": euros(5, 5)}, "{EUR 5}"},
		{"二元 min 与 like", "min(fees[0], like(fees[0], 3))", map[string]any{"fees": euros(100)}, "{EUR 3}"},
		{"abs 负", "abs(fees[0])", map[string]any{"fees": euros(-150)}, "{EUR 150}"},
		{"abs 正", "abs(fees[0])", map[string]any{"fees": euros(150)}, "{EUR 150}"},
		{"abs 零", "abs(fees[0])", map[string]any{"fees": euros(0)}, "{EUR 0}"},
		{"sort 含负", "sort(fees)", map[string]any{"fees": euros(3, -1, 0, -2)}, "[{EUR -2} {EUR -1} {EUR 0} {EUR 3}]"},
		{"sort_desc 含负", "sort_desc(fees)", map[string]any{"fees": euros(3, -1, 0, -2)}, "[{EUR 3} {EUR 0} {EUR -1} {EUR -2}]"},
		{"sort 单元素", "sort(fees)", map[string]any{"fees": euros(4)}, "[{EUR 4}]"},
		{"cumsum 含负", "cumsum(fees)", map[string]any{"fees": euros(5, -7, 2)}, "[{EUR 5} {EUR -2} {EUR 0}]"},
		{"cumsum 单元素", "cumsum(fees)", map[string]any{"fees": euros(5)}, "[{EUR 5}]"},
		{"deltas 含负", "deltas(fees)", map[string]any{"fees": euros(5, -7, 2)}, "[{EUR -12} {EUR 9}]"},
		{"deltas 单元素没有差", "deltas(fees)", map[string]any{"fees": euros(5)}, "[]"},
		{"avg 单元素", "avg(fees)", map[string]any{"fees": euros(-7)}, "{EUR -7}"},
		{"avg 整除", "avg(fees)", map[string]any{"fees": euros(-3, 9)}, "{EUR 3}"},
		{"median 单元素", "median(fees)", map[string]any{"fees": euros(-7)}, "{EUR -7}"},
		{"median 奇数个", "median(fees)", map[string]any{"fees": euros(5, 1, 3)}, "{EUR 3}"},
		{"median 奇数个含负", "median(fees)", map[string]any{"fees": euros(-5, 10, -1, 7, -9)}, "{EUR -1}"},
		{"median 偶数个整除", "median(fees)", map[string]any{"fees": euros(7, 1, 5, 3)}, "{EUR 4}"},
		{"median 偶数个含负", "median(fees)", map[string]any{"fees": euros(-4, 2)}, "{EUR -1}"},
	})
}

// sum of nothing is a zero; the extremes and averages of nothing are errors.
func TestEmptyMoneyLists(t *testing.T) {
	t.Parallel()
	bound := map[string]any{"fees": []any{}, "amount": "EUR 1.00", "names": []any{}}
	t.Run("有结果", func(t *testing.T) {
		t.Parallel()
		checkMoney(t, []lang.ArgSpec{boundFees, boundAmount, candidates}, []moneyCase{
			{"sum 补上绑定的币种", "sum(fees)", bound, "{EUR 0}"},
			{"sort", "sort(fees)", bound, "[]"},
			{"sort_desc", "sort_desc(fees)", bound, "[]"},
			{"cumsum", "cumsum(fees)", bound, "[]"},
			{"deltas", "deltas(fees)", bound, "[]"},
			{"sort_by", "sort_by(names, fees)", bound, "[]"},
			{"top_k", "top_k(names, fees, 3)", bound, "[]"},
		})
	})
	// Nothing binds c when the only money is an empty list, so the zero keeps
	// no currency; the same with a result of unknown currency.
	for name, spec := range map[string]lang.ArgSpec{"币种变量未绑定": boundFees, "未知币种": unknownFees} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := runMoney(t, "sum(fees)", map[string]any{"fees": []any{}}, spec)
			if err != nil || got != "{ 0}" {
				t.Fatalf("sum of an empty %s = %s, %v; want { 0}", spec.Type, got, err)
			}
		})
	}
	for _, source := range []string{
		"min(fees)", "max(fees)", "arg_min(fees)", "arg_max(fees)", "avg(fees)", "median(fees)",
		"avg(fees, @up)", "median(fees, @down)", "round(avg(fees), @floor)", "round(median(fees), @ceiling)",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			expectMoneyError(t, source, bound, lang.ErrExtension, boundFees, boundAmount, candidates)
		})
	}
}

// The currency-less zero — what an empty sum produces, what {"currency": "",
// "minor": 0} reads as — goes with every currency.
func TestCurrencylessZeroGoesWithAnyCurrency(t *testing.T) {
	t.Parallel()
	with := func(fees ...any) map[string]any {
		return map[string]any{"fees": fees, "names": []any{"a", "b", "c"}[:len(fees)]}
	}
	checkMoney(t, []lang.ArgSpec{unknownFees, candidates}, []moneyCase{
		{"sum 零在前", "sum(fees)", with(zero, "EUR 0.50"), "{EUR 50}"},
		{"sum 零在两种位置都不挡币种", "sum(fees)", with("USD 1.00", zero, "USD 2.00"), "{USD 300}"},
		{"sum 全是零", "sum(fees)", with(zero, zero), "{ 0}"},
		{"max 取到零时带上列表的币种", "max(fees)", with(zero, "EUR -0.05"), "{EUR 0}"},
		{"max 零在后", "max(fees)", with("EUR -0.05", zero), "{EUR 0}"},
		{"min 全是零", "min(fees)", with(zero, zero), "{ 0}"},
		{"arg_max 指向零", "arg_max(fees)", with("EUR -0.05", zero), "1"},
		{"二元 min", "min(fees[0], fees[1])", with("EUR -0.05", zero), "{EUR -5}"},
		{"二元 max", "max(fees[0], fees[1])", with("EUR -0.05", zero), "{EUR 0}"},
		{"abs 零", "abs(fees[0])", with(zero), "{ 0}"},
		{"avg 零参与平均", "avg(fees)", with(zero, "EUR 0.05"), "{EUR 2}"},
		{"median 单个零", "median(fees)", with(zero), "{ 0}"},
		{"median 中间是零时带上列表的币种", "median(fees)", with("EUR -1.00", zero, "EUR 1.00"), "{EUR 0}"},
		{"cumsum 补上币种", "cumsum(fees)", with(zero, "EUR 1.00"), "[{EUR 0} {EUR 100}]"},
		{"deltas 补上币种", "deltas(fees)", with(zero, "EUR 1.00"), "[{EUR 100}]"},
		// sort hands the amounts back as they came: the zero keeps no
		// currency, which also shows the sort is stable.
		{"sort 稳定：EUR 0 在前", "sort(fees)", with("EUR 0.00", zero), "[{EUR 0} { 0}]"},
		{"sort 稳定：零在前", "sort(fees)", with(zero, "EUR 0.00"), "[{ 0} {EUR 0}]"},
		{"sort_desc 稳定：EUR 0 在前", "sort_desc(fees)", with("EUR 0.00", zero), "[{EUR 0} { 0}]"},
		{"sort_desc 稳定：零在前", "sort_desc(fees)", with(zero, "EUR 0.00"), "[{ 0} {EUR 0}]"},
		{"sort_by 零作键", "sort_by(names, fees)", with(zero, "EUR -1.00"), "[b a]"},
		{"top_k 零作键", "top_k(names, fees, 1)", with("EUR -1.00", zero), "[b]"},
	})
	// Under money<c> the zero takes the bound currency on the way out.
	checkMoney(t, []lang.ArgSpec{boundFees, boundAmount}, []moneyCase{
		{"money<c> sum", "sum(fees)", map[string]any{"fees": []any{zero, "EUR 0.50"}, "amount": "EUR 1.00"}, "{EUR 50}"},
		{"money<c> max 全是零", "max(fees)", map[string]any{"fees": []any{zero, zero}, "amount": "EUR 1.00"}, "{EUR 0}"},
		{"money<c> cumsum 全是零", "cumsum(fees)", map[string]any{"fees": []any{zero, zero}, "amount": "EUR 1.00"}, "[{EUR 0} {EUR 0}]"},
		{"money<c> sort 零", "sort(fees)", map[string]any{"fees": []any{zero}, "amount": "JPY 1"}, "[{JPY 0}]"},
		{"money<c> sort 零与 EUR 0", "sort(fees)", map[string]any{"fees": []any{"EUR 0.00", zero}, "amount": "EUR 1.00"}, "[{EUR 0} {EUR 0}]"},
	})
}

// Two currencies in one list have no sum, no order and no average; the zero
// between them does not hide the mismatch.
func TestMixedCurrenciesAreRefused(t *testing.T) {
	t.Parallel()
	inputs := []map[string]any{
		{"fees": []any{"EUR 0.05", "USD 0.01", "USD 0.02"}, "names": []any{"a", "b", "c"}},
		{"fees": []any{"EUR 0.05", zero, "USD 0.01"}, "names": []any{"a", "b", "c"}},
	}
	for _, source := range []string{
		"sum(fees)", "min(fees)", "max(fees)", "arg_min(fees)", "arg_max(fees)",
		"sort(fees)", "sort_desc(fees)", "cumsum(fees)", "deltas(fees)",
		"avg(fees)", "median(fees)", "avg(fees, @up)", "median(fees, @down)", "round(median(fees), @floor)",
		"min(fees[0], fees[2])", "max(fees[0], fees[2])",
		"sort_by(names, fees)", "sort_by_desc(names, fees)", "top_k(names, fees, 1)", "bottom_k(names, fees, 1)",
		"sum(fees) + like(fees[0], 1)",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			for _, args := range inputs {
				expectMoneyError(t, source, args, lang.ErrCurrency, unknownFees, candidates)
			}
		})
	}
	t.Run("结果再与别的币种相加", func(t *testing.T) {
		t.Parallel()
		args := map[string]any{"fees": []any{"USD 1.00"}, "amount": "EUR 1.00"}
		expectMoneyError(t, "sum(fees) + amount", args, lang.ErrCurrency, unknownFees, boundAmount)
	})
	t.Run("字面量里的两种币种在编译期报错", func(t *testing.T) {
		t.Parallel()
		if _, err := lang.CompileExpr("sum([USD 1, EUR 1])", moneyPack(t), lang.CompileOptions{}); !errors.Is(err, lang.ErrCurrency) {
			t.Fatalf("sum([USD 1, EUR 1]) compile error = %v, want ErrCurrency", err)
		}
	})
	t.Run("money<c> 的列表在边界上就被拒绝", func(t *testing.T) {
		t.Parallel()
		_, err := runMoney(t, "sum(fees)", map[string]any{"fees": []any{"EUR 1.00", "USD 1.00"}}, boundFees)
		if !errors.Is(err, lang.ErrCurrency) || !errors.Is(err, lang.ErrContract) {
			t.Fatalf("sum of EUR and USD under money<c>: error = %v, want ErrCurrency and ErrContract", err)
		}
	})
}

// A total that leaves int64 is an error, never a wrapped-around amount; the
// extremes themselves are fine.
func TestMoneyOverflowIsAnError(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, source string
		fees         []int64
	}{
		{"sum 上溢", "sum(fees)", []int64{math.MaxInt64, 1}},
		{"sum 下溢", "sum(fees)", []int64{math.MinInt64, -1}},
		// Adding step by step: the total would fit, the running total does
		// not. This records the current behavior.
		{"sum 中途溢出", "sum(fees)", []int64{math.MaxInt64, 1, -1}},
		{"cumsum 上溢", "cumsum(fees)", []int64{math.MaxInt64, 1}},
		{"cumsum 下溢", "cumsum(fees)", []int64{math.MinInt64, -1}},
		{"deltas 下溢", "deltas(fees)", []int64{1, math.MinInt64}},
		{"deltas 上溢", "deltas(fees)", []int64{-2, math.MaxInt64}},
		{"abs 最小值", "abs(fees[0])", []int64{math.MinInt64}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// An overflow is the data's, not a failing extension: it keeps
			// ErrArithmetic, and fallback does not take it.
			got, err := runMoney(t, test.source, map[string]any{"fees": euros(test.fees...)}, boundFees)
			if !errors.Is(err, lang.ErrArithmetic) || errors.Is(err, lang.ErrExtension) || !strings.Contains(fmt.Sprint(err), "overflow") {
				t.Fatalf("%s with %v = %s, error %v; want an ErrArithmetic overflow", test.source, test.fees, got, err)
			}
		})
	}
}

func TestMoneyAtTheEdgesOfInt64(t *testing.T) {
	t.Parallel()
	edges := func(cents ...int64) map[string]any { return map[string]any{"fees": euros(cents...)} }
	checkMoney(t, []lang.ArgSpec{boundFees}, []moneyCase{
		{"sum 两端相抵", "sum(fees)", edges(math.MaxInt64, math.MinInt64), "{EUR -1}"},
		{"sum 恰好到最大", "sum(fees)", edges(math.MaxInt64-1, 1), "{EUR 9223372036854775807}"},
		{"deltas 恰好到最小", "deltas(fees)", edges(math.MaxInt64, -1), "[{EUR -9223372036854775808}]"},
		{"abs 最小值加一", "abs(fees[0])", edges(math.MinInt64 + 1), "{EUR 9223372036854775807}"},
		{"max 两端", "max(fees)", edges(math.MinInt64, math.MaxInt64), "{EUR 9223372036854775807}"},
		{"arg_min 两端", "arg_min(fees)", edges(math.MaxInt64, math.MinInt64), "1"},
		{"sort 两端", "sort(fees)", edges(math.MaxInt64, math.MinInt64), "[{EUR -9223372036854775808} {EUR 9223372036854775807}]"},
		{"cumsum 两端", "cumsum(fees)", edges(math.MaxInt64, math.MinInt64), "[{EUR 9223372036854775807} {EUR -1}]"},
		{"avg 单个最大值", "avg(fees)", edges(math.MaxInt64), "{EUR 9223372036854775807}"},
		{"avg 和超出也能算", "avg(fees)", edges(math.MaxInt64, 1), "{EUR 4611686018427387904}"},
		{"avg 三个最小值", "avg(fees)", edges(math.MinInt64, math.MinInt64, math.MinInt64), "{EUR -9223372036854775808}"},
		{"median 奇数个两端", "median(fees)", edges(math.MaxInt64, math.MinInt64, 0), "{EUR 0}"},
	})
	// The median of an even count lies between its two middle amounts, so
	// it always fits in int64.
	for name, cents := range map[string][]int64{
		"两个最大值": {math.MaxInt64, math.MaxInt64},
		"两个最小值": {math.MinInt64, math.MinInt64},
	} {
		t.Run("median "+name, func(t *testing.T) {
			t.Parallel()
			want := fmt.Sprintf("{EUR %d}", cents[0])
			got, err := runMoney(t, "median(fees)", map[string]any{"fees": euros(cents...)}, boundFees)
			if err != nil || got != want {
				t.Fatalf("median(%v) = %s, %v; want %s", cents, got, err, want)
			}
		})
	}
}

// roundingModes is what each mode makes of five averages — 1.5, -1.5, 2.5,
// 4/3 and -4/3 cents — and of three medians — 1.5, -1.5 and 2.5 cents.
var roundingModes = []struct {
	mode   string
	avg    [5]int64
	median [3]int64
}{
	{"half_even", [5]int64{2, -2, 2, 1, -1}, [3]int64{2, -2, 2}},
	{"half_up", [5]int64{2, -2, 3, 1, -1}, [3]int64{2, -2, 3}},
	{"half_down", [5]int64{1, -1, 2, 1, -1}, [3]int64{1, -1, 2}},
	{"down", [5]int64{1, -1, 2, 1, -1}, [3]int64{1, -1, 2}},
	{"up", [5]int64{2, -2, 3, 2, -2}, [3]int64{2, -2, 3}},
	{"ceiling", [5]int64{2, -1, 3, 2, -1}, [3]int64{2, -1, 3}},
	{"floor", [5]int64{1, -2, 2, 1, -2}, [3]int64{1, -2, 2}},
}

var (
	averagedFees = [5][]any{euros(1, 2), euros(-1, -2), euros(2, 3), euros(1, 1, 2), euros(-1, -1, -2)}
	medianFees   = [3][]any{euros(2, 1), euros(-1, -2), euros(100, 2, 3, 1)}
)

// Each mode settles a tie its own way, whether it is written round(…, @mode)
// or passed straight to the variant.
func TestRoundedAggregatesSettleTiesByMode(t *testing.T) {
	t.Parallel()
	for _, mode := range roundingModes {
		t.Run(mode.mode, func(t *testing.T) {
			t.Parallel()
			registry := moneyPack(t)
			for i, fees := range averagedFees {
				args, want := map[string]any{"fees": fees}, fmt.Sprintf("{EUR %d}", mode.avg[i])
				expectMoney(t, registry, "round(avg(fees), @"+mode.mode+")", args, want, boundFees)
				expectMoney(t, registry, "avg(fees, @"+mode.mode+")", args, want, boundFees)
			}
			for i, fees := range medianFees {
				args, want := map[string]any{"fees": fees}, fmt.Sprintf("{EUR %d}", mode.median[i])
				expectMoney(t, registry, "round(median(fees), @"+mode.mode+")", args, want, boundFees)
				expectMoney(t, registry, "median(fees, @"+mode.mode+")", args, want, boundFees)
			}
		})
	}
}

// Without round(…) the registry's default rounding is the one that applies.
func TestUnroundedAggregatesTakeTheRegistryDefault(t *testing.T) {
	t.Parallel()
	for _, mode := range roundingModes {
		t.Run(mode.mode, func(t *testing.T) {
			t.Parallel()
			rounding, err := lang.ParseRounding(mode.mode)
			if err != nil {
				t.Fatal(err)
			}
			registry := moneyPackRounding(t, rounding)
			for i, fees := range averagedFees {
				expectMoney(t, registry, "avg(fees)", map[string]any{"fees": fees}, fmt.Sprintf("{EUR %d}", mode.avg[i]), boundFees)
			}
			for i, fees := range medianFees {
				expectMoney(t, registry, "median(fees)", map[string]any{"fees": fees}, fmt.Sprintf("{EUR %d}", mode.median[i]), boundFees)
			}
		})
	}
}

// round(…) reaches every rounding step inside it, and an inner round keeps
// its own mode.
func TestRoundingScopeReachesTheAggregates(t *testing.T) {
	t.Parallel()
	ties := map[string]any{"fees": euros(1, 2)}
	checkMoney(t, []lang.ArgSpec{boundFees}, []moneyCase{
		{"max 里的两个聚合", "round(max(avg(fees), median(fees)), @up)", ties, "{EUR 2}"},
		{"相加的两个聚合", "round(avg(fees) + median(fees), @floor)", ties, "{EUR 2}"},
		{"内层 round 保留自己的方式", "round(round(avg(fees), @down) + avg(fees), @up)", ties, "{EUR 3}"},
		{"与费率乘法同一作用域", "round(avg(fees) * 150%, @up)", ties, "{EUR 3}"},
		{"与费率乘法向下", "round(avg(fees) * 150%, @down)", ties, "{EUR 1}"},
		{"推导式里的聚合", "round(sum([avg(fees) for f in fees]), @up)", map[string]any{"fees": euros(2, 3)}, "{EUR 6}"},
		{"不写 round 走缺省", "sum([avg(fees) for f in fees])", map[string]any{"fees": euros(2, 3)}, "{EUR 4}"},
	})
	for _, source := range []string{"round(sum(fees), @up)", "round(avg(fees), @nope)", "round(max(fees), @down)"} {
		t.Run("编译错误 "+source, func(t *testing.T) {
			t.Parallel()
			if _, err := lang.CompileExpr(source, moneyPack(t), lang.CompileOptions{Args: []lang.ArgSpec{boundFees}}); err == nil {
				t.Fatalf("CompileExpr(%q) error = nil, want a compile error", source)
			}
		})
	}
}

// Every money overload is constexpr: a call on literals is done while the
// rule compiles.
func TestMoneyOverloadsFoldAtCompileTime(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"round(avg([USD 0.01, USD 0.02]), @down)":                           "{USD 1}",
		"sum([USD 0.01, USD 0.02]) + abs(USD -1) + max([USD 3, USD 4])":     "{USD 503}",
		"median([EUR 1, EUR 3, EUR 2]) + min(EUR 1, EUR 2)":                 "{EUR 300}",
		"cumsum(sort_desc([JPY 1, JPY 3]))[1]":                              "{JPY 4}",
		`first(top_k(["a", "b"], [EUR 1, EUR 2], 1))`:                       "b",
		"arg_max([KWD 0.001, KWD 0.003, KWD 0.002]) + len(deltas([USD 1]))": "1",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			registry := moneyPack(t)
			artifact, err := lang.CompileExpr(source, registry, lang.CompileOptions{})
			if err != nil {
				t.Fatalf("CompileExpr(%q): %v", source, err)
			}
			if artifact.InstructionCount() != 1 {
				t.Errorf("%s compiled to %d instructions, want one load", source, artifact.InstructionCount())
			}
			expectMoney(t, registry, source, nil, want)
		})
	}
}

// Money keys select candidates the way numbers do: stable, cut at k, and the
// two lists must match in length.
func TestMoneyKeysSelectCandidates(t *testing.T) {
	t.Parallel()
	four := map[string]any{"names": []any{"a", "b", "c", "d"}, "fees": euros(2, 1, 2, 1)}
	signed := map[string]any{"names": []any{"a", "b", "c"}, "fees": euros(-1, 1, 0)}
	checkMoney(t, []lang.ArgSpec{candidates, boundFees}, []moneyCase{
		{"sort_by 相等保持原顺序", "sort_by(names, fees)", four, "[b d a c]"},
		{"sort_by_desc 相等保持原顺序", "sort_by_desc(names, fees)", four, "[a c b d]"},
		{"bottom_k 并列按原顺序", "bottom_k(names, fees, 3)", four, "[b d a]"},
		{"top_k 并列按原顺序", "top_k(names, fees, 3)", four, "[a c b]"},
		{"top_k k 为 0", "top_k(names, fees, 0)", four, "[]"},
		{"bottom_k k 为 0", "bottom_k(names, fees, 0)", four, "[]"},
		{"top_k k 大于长度", "top_k(names, fees, 9)", four, "[a c b d]"},
		{"bottom_k k 大于长度", "bottom_k(names, fees, 9)", four, "[b d a c]"},
		{"top_k k 等于长度", "top_k(names, fees, 4)", four, "[a c b d]"},
		{"sort_by 负键", "sort_by(names, fees)", signed, "[a c b]"},
		{"sort_by_desc 负键", "sort_by_desc(names, fees)", signed, "[b c a]"},
		{"bottom_k 负键", "bottom_k(names, fees, 1)", signed, "[a]"},
		{"候选是数字", "sort_by([10, 20, 30], fees)", signed, "[10 30 20]"},
		{"arg_min 取候选", "names[arg_min(fees)]", signed, "a"},
		{"arg_max 取候选", "names[arg_max(fees)]", signed, "b"},
	})
	short := map[string]any{"names": []any{"a"}, "fees": euros(1, 2)}
	long := map[string]any{"names": []any{"a", "b", "c"}, "fees": euros(1, 2)}
	for _, source := range []string{
		"sort_by(names, fees)", "sort_by_desc(names, fees)", "top_k(names, fees, 1)", "bottom_k(names, fees, 1)",
		"top_k(names, fees, 0 - 1)",
	} {
		t.Run("报错 "+source, func(t *testing.T) {
			t.Parallel()
			for _, args := range []map[string]any{short, long} {
				expectMoneyError(t, source, args, lang.ErrExtension, candidates, boundFees)
			}
		})
	}
}

// Money through comprehensions, the everyday way to filter before a fold.
func TestMoneyComposesWithComprehensions(t *testing.T) {
	t.Parallel()
	groups := lang.ArgSpec{Name: "groups", Type: lang.ArrayOf(lang.ArrayOf(lang.MoneyOf("c")))}
	checkMoney(t, []lang.ArgSpec{boundFees}, []moneyCase{
		{"筛选后求和", "sum([x for x in fees if x > like(x, 100)])", map[string]any{"fees": euros(50, 150, 200)}, "{EUR 350}"},
		{"筛选后取最小", "min([x for x in fees if x > like(x, 0)])", map[string]any{"fees": euros(-5, 7, 3)}, "{EUR 3}"},
		{"绝对值后取最大", "max([abs(x) for x in fees])", map[string]any{"fees": euros(-9, 7)}, "{EUR 9}"},
		{"乘整数后平均", "avg([x * 2 for x in fees])", map[string]any{"fees": euros(1, 2)}, "{EUR 3}"},
		{"累加到上限为止", "take_while(fees, [s <= like(s, 300) for s in cumsum(fees)])", map[string]any{"fees": euros(100, 200, 50)}, "[{EUR 100} {EUR 200}]"},
		{"与 reduce 一致", "reduce(x in fees, acc = 0, acc + x) == sum(fees)", map[string]any{"fees": euros(3, -1, 8)}, "true"},
		{"筛空后求和再比较", "sum([x for x in fees if x < like(x, 0)]) == 0", map[string]any{"fees": euros(3)}, "true"},
	})
	checkMoney(t, []lang.ArgSpec{groups}, []moneyCase{
		{"多层推导展开后求和", "sum([x for g in groups for x in g])", map[string]any{"groups": []any{euros(100), euros(200, 50)}}, "{EUR 350}"},
		{"分组求和再求和", "sum([sum(g) for g in groups])", map[string]any{"groups": []any{euros(100), euros()}}, "{EUR 100}"},
		{"分组平均", "[avg(g) for g in groups]", map[string]any{"groups": []any{euros(1, 2), euros(3)}}, "[{EUR 2} {EUR 3}]"},
	})
}

func TestAListOfTwoCurrenciesIsRefused(t *testing.T) {
	t.Parallel()
	history := lang.ArgSpec{Name: "history", Type: lang.ArrayOf(lang.MoneyOf(""))}
	_, err := runMoney(t, "sum(history)", map[string]any{"history": []any{"USD 1.00", "EUR 1.00"}}, history)
	if !errors.Is(err, lang.ErrCurrency) {
		t.Fatalf("summing dollars and euros: error = %v, want ErrCurrency", err)
	}
}

// Orders in different currencies, each converted at its own rate, add up in
// one: each order's rate is handed to using, and -> converts at it.
func TestAMixedHistoryConvertsIntoOneCurrency(t *testing.T) {
	t.Parallel()
	order, err := lang.ParseType("record{amount: int, currency: currency<?>}")
	if err != nil {
		t.Fatal(err)
	}
	history := lang.ArgSpec{Name: "history", Type: lang.ArrayOf(order)}
	rates := lang.ArgSpec{Name: "rates", Type: lang.DictOf(lang.FxRateOf("", "USD"))}
	args := map[string]any{
		"history": []any{map[string]any{"amount": 1000, "currency": "EUR"}, map[string]any{"amount": 2000, "currency": "JPY"}},
		"rates": map[string]any{
			"EUR": map[string]any{"base": "EUR", "quote": "USD", "rate": "1.1"},
			"JPY": map[string]any{"base": "JPY", "quote": "USD", "rate": "0.0067"},
		},
	}
	got, err := runMoney(t, "sum([using(rates[o.currency], money(o.amount, o.currency) -> USD) for o in history])", args, history, rates)
	if err != nil || got != "{USD 2440}" {
		t.Fatalf("EUR 10.00 and JPY 2000 in dollars = %s, %v, want {USD 2440}", got, err)
	}
}

// Each name covers money too once money is declared, and only then.
func TestMoneyOverloadsArriveWithTheDeclaration(t *testing.T) {
	t.Parallel()
	overloads := map[string]int{}
	for _, function := range moneyPack(t).Catalog().Functions() {
		overloads[function.Name()]++
	}
	for name, want := range map[string]int{
		"sum": 3, "avg": 4, "median": 4, "min": 8, "max": 8, "sort": 4, "cumsum": 3, "sort_by": 4, "top_k": 4, "arg_min": 4,
	} {
		if got := overloads[name]; got != want {
			t.Errorf("%s has %d overloads with money declared, want %d", name, got, want)
		}
	}
}

// Declaring money after the pack is registered is accepted, and the kernel's
// money works, but the pack's money overloads are not there: the pack looked
// for the declaration when it registered. This records the current behavior
// (the documented order is DeclareMoney first).
func TestDeclaringMoneyAfterThePackAddsNoPackOverloads(t *testing.T) {
	t.Parallel()
	late := lang.CoreRegistry()
	if err := std.Register(late); err != nil {
		t.Fatal(err)
	}
	if err := late.DeclareMoney(lang.MoneySpec{Rounding: lang.RoundHalfEven, Currencies: std.ISO4217()}); err != nil {
		t.Fatalf("DeclareMoney after std.Register: %v, want it accepted", err)
	}
	if _, err := lang.CompileExpr("USD 1 + USD 2", late, lang.CompileOptions{}); err != nil {
		t.Fatalf("kernel money after a late declaration: %v", err)
	}
	for _, source := range []string{"sum(fees)", "avg(fees)", "sort(fees)", "abs(fees[0])", `sort_by(["a"], fees)`} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			_, err := lang.CompileExpr(source, late, lang.CompileOptions{Args: []lang.ArgSpec{unknownFees}})
			if err == nil || !strings.Contains(err.Error(), "no overload") {
				t.Fatalf("CompileExpr(%q) after a late declaration: error = %v, want no overload", source, err)
			}
		})
	}
}

// avg divides before it adds, so it is checked against exact arithmetic:
// random lists, some near the ends of int64, in every mode.
func TestAverageAgreesWithExactArithmetic(t *testing.T) {
	t.Parallel()
	for _, mode := range []lang.Rounding{
		lang.RoundHalfEven, lang.RoundHalfUp, lang.RoundHalfDown, lang.RoundDown, lang.RoundUp, lang.RoundCeiling, lang.RoundFloor,
	} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			random := rand.New(rand.NewPCG(3, uint64(mode)))
			for range 60 {
				cents := randomCents(random)
				want := fmt.Sprintf("{EUR %d}", exactAverage(cents, mode))
				expectMoney(t, moneyPack(t), "avg(fees, @"+mode.String()+")", map[string]any{"fees": euros(cents...)}, want, boundFees)
			}
		})
	}
}

// randomCents is one to five amounts of any size and sign.
func randomCents(random *rand.Rand) []int64 {
	cents := make([]int64, 1+random.IntN(5))
	for i := range cents {
		cents[i] = random.Int64() >> random.IntN(64)
		if random.IntN(2) == 0 {
			cents[i] = -cents[i]
		}
	}
	return cents
}

// exactAverage is the mean of cents rounded by mode, in big integers.
func exactAverage(cents []int64, mode lang.Rounding) int64 {
	total := new(big.Int)
	for _, cent := range cents {
		total.Add(total, big.NewInt(cent))
	}
	n := big.NewInt(int64(len(cents)))
	quotient, remainder := new(big.Int).QuoRem(total, n, new(big.Int))
	twice := new(big.Int).Abs(new(big.Int).Lsh(remainder, 1)).Cmp(n)
	if remainder.Sign() != 0 && roundsAwayFromZero(mode, twice, quotient.Bit(0) == 1, total.Sign()) {
		quotient.Add(quotient, big.NewInt(int64(total.Sign())))
	}
	return quotient.Int64()
}

// roundsAwayFromZero decides an inexact quotient: twice compares twice the
// remainder with the divisor, odd is the truncated quotient's parity.
func roundsAwayFromZero(mode lang.Rounding, twice int, odd bool, sign int) bool {
	switch mode {
	case lang.RoundUp:
		return true
	case lang.RoundCeiling:
		return sign > 0
	case lang.RoundFloor:
		return sign < 0
	case lang.RoundHalfUp:
		return twice >= 0
	case lang.RoundHalfDown:
		return twice > 0
	case lang.RoundHalfEven:
		return twice > 0 || (twice == 0 && odd)
	default:
		return false
	}
}
