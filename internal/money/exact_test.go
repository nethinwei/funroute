package money

import (
	"encoding/json"
	"errors"
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
