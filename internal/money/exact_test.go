package money

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
)

// Exact money rounds once: five cents at 50% twice is exactly 1.25 cents,
// which half up is one cent, where rounding each step would make two.
func TestExactMoneyRoundsOnce(t *testing.T) {
	t.Parallel()
	half := ratio(1, 2)
	exact, err := Money{currency: "USD", minor: 5}.Exact().MulRatio(half)
	if err == nil {
		exact, err = exact.MulRatio(half)
	}
	if err != nil || exact.Minor() != ratio(5, 4) || exact.Currency() != "USD" {
		t.Fatalf("USD 0.05 × 50%% × 50%% = %s, %v, want 5/4 minor units", exact, err)
	}
	once, err := exact.Round(RoundHalfUp)
	if err != nil || once != (Money{currency: "USD", minor: 1}) {
		t.Fatalf("Round(half_up) = %v, %v, want USD 0.01", once, err)
	}
	step, _ := Money{currency: "USD", minor: 5}.MulRatio(half, RoundHalfUp)
	twice, _ := step.MulRatio(half, RoundHalfUp)
	if twice.minor != 2 {
		t.Fatalf("rounding each step = %v, want USD 0.02: the case would show nothing", twice)
	}
}

// Exact money adds, subtracts, counts, compares and converts as money does,
// in one currency; the currency-less zero goes with any.
func TestExactMoneyComputesAsMoneyDoes(t *testing.T) {
	t.Parallel()
	third, _ := Money{currency: "USD", minor: 1}.Exact().DivRatio(ratio(3, 1))
	for name, test := range map[string]struct {
		run  func() (ExactMoney, error)
		want string
	}{
		"a sum":            {func() (ExactMoney, error) { return third.Add(third) }, "USD 2/3 minor units"},
		"a difference":     {func() (ExactMoney, error) { return third.Sub(Money{currency: "USD", minor: 1}.Exact()) }, "USD -2/3 minor units"},
		"a negation":       {third.Neg, "USD -1/3 minor units"},
		"a count":          {func() (ExactMoney, error) { return third.MulInt(3) }, "USD 1 minor units"},
		"with no currency": {func() (ExactMoney, error) { return ExactMoney{}.Add(third) }, "USD 1/3 minor units"},
	} {
		if got, err := test.run(); err != nil || got.String() != test.want {
			t.Errorf("%s = %s, %v, want %s", name, got, err, test.want)
		}
	}
	// Exact money is as wide as money: the smallest amount taken from five
	// cents overflows as it does for Money, and taken from itself is zero.
	smallest := Money{currency: "USD", minor: math.MinInt64}.Exact()
	if got, err := (Money{currency: "USD", minor: 5}).Exact().Sub(smallest); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("USD 0.05 - the smallest amount = %s, %v, want ErrArithmetic", got, err)
	}
	if got, err := smallest.Sub(smallest); err != nil || got.Sign() != 0 {
		t.Fatalf("the smallest amount less itself = %s, %v, want zero", got, err)
	}
	if _, err := third.Add(Money{currency: "EUR", minor: 1}.Exact()); !errors.Is(err, ErrCurrency) {
		t.Fatalf("dollars plus euros: error = %v, want ErrCurrency", err)
	}
	if order, err := third.Cmp(Money{currency: "USD", minor: 1}.Exact()); err != nil || order != -1 {
		t.Fatalf("a third of a cent against a cent = %d, %v, want -1", order, err)
	}
	encoded, err := json.Marshal(third)
	if err != nil || string(encoded) != `{"currency":"USD","minor":"1/3"}` {
		t.Fatalf("json.Marshal(a third of a cent) = %s, %v", encoded, err)
	}
}

// ConvertExact converts at a rate without rounding: USD 1.00 at 150.5 JPY
// / USD is 150.5 yen, rounded only where the rule rounds it.
func TestConvertExactLeavesTheRounding(t *testing.T) {
	t.Parallel()
	table := rateCurrencies(t)
	fx := fxRate(t, "USD", "JPY", "150.5")
	yen, err := table.ConvertExact(Money{currency: "USD", minor: 100}.Exact(), fx)
	if err != nil || yen.String() != "JPY 150.5 minor units" {
		t.Fatalf("ConvertExact(USD 1.00) = %s, %v, want 150.5 yen", yen, err)
	}
	if _, err := table.ConvertExact(Money{currency: "EUR", minor: 100}.Exact(), fx); !errors.Is(err, ErrCurrency) {
		t.Fatalf("ConvertExact of euros at a dollar rate: error = %v, want ErrCurrency", err)
	}
	// The rate in minor units, 10^-20, fits no ratio; the amount cancels it.
	tiny, err := table.ConvertExact(Money{currency: "USD", minor: 100}.Exact(), fxRate(t, "USD", "JPY", "0.000000000000000001"))
	if err != nil || tiny.Minor() != ratio(1, 1_000_000_000_000_000_000) {
		t.Fatalf("ConvertExact(USD 1.00) at 10^-18 = %s, %v, want 10^-18 yen", tiny, err)
	}
}

// MulPart is prorate's exact step: the proportion is never a ratio of its
// own, so a whole of MinInt64 — 1/MinInt64 fits no ratio — takes a share.
func TestExactMoneyTakesAPart(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		e           ExactMoney
		part, whole int64
		want        Ratio
	}{
		"a third":            {usd(100).Exact(), 1, 3, ratio(100, 3)},
		"a negative whole":   {usd(100).Exact(), 1, -4, ratio(-25, 1)},
		"zero of MinInt64":   {usd(0).Exact(), 1, math.MinInt64, Ratio{}},
		"MinInt64 of itself": {usd(5).Exact(), math.MinInt64, math.MinInt64, ratio(5, 1)},
		"half of MinInt64":   {usd(math.MinInt64).Exact(), 1, 2, ratio(math.MinInt64/2, 1)},
	} {
		if got, err := test.e.MulPart(test.part, test.whole); err != nil || got.Minor() != test.want {
			t.Errorf("%s: (%s).MulPart(%d, %d) = %s, %v, want %s", name, test.e, test.part, test.whole, got, err, test.want)
		}
	}
	if _, err := usd(1).Exact().MulPart(1, 0); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("MulPart(1, 0) error = %v, want ErrArithmetic", err)
	}
}

// The sign of exact money is the sign of its minor units, however small a
// part of one they are.
func TestExactMoneySign(t *testing.T) {
	t.Parallel()
	third, _ := Money{currency: "USD", minor: 1}.Exact().DivRatio(ratio(3, 1))
	minusThird, _ := third.Neg()
	for _, test := range []struct {
		name string
		e    ExactMoney
		want int
	}{
		{"a third of a cent", third, 1},
		{"minus a third of a cent", minusThird, -1},
		{"the currency-less zero", ExactMoney{}, 0},
		{"a dollar's zero", Money{currency: "USD"}.Exact(), 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.e.Sign(); got != test.want {
				t.Fatalf("(%s).Sign() = %d, want %d", test.e, got, test.want)
			}
		})
	}
}
