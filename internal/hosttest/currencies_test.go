package hosttest

import (
	"errors"
	"slices"
	"testing"

	"github.com/nethinwei/funroute"
)

// threeCurrencies is a spec with a two-place, a zero-place and a three-place
// currency, listed out of order.
var threeCurrencies = funroute.MoneySpec{Currencies: []funroute.CurrencySpec{
	{Code: "USD", Digits: 2}, {Code: "KWD", Digits: 3}, {Code: "JPY", Digits: 0},
}}

func currencyTable(t *testing.T) *funroute.Currencies {
	t.Helper()
	table, err := funroute.NewCurrencies(threeCurrencies)
	if err != nil {
		t.Fatal(err)
	}
	return table
}

// A service that runs no rules reads and writes amounts the way the rules
// do: exactly, in each currency's places, refusing what does not fit.
func TestCurrenciesReadAmountsExactly(t *testing.T) {
	t.Parallel()
	table := currencyTable(t)
	for text, want := range map[string]funroute.Money{
		"USD 1.70":                  amount("USD", 170),
		"USD 1.7":                   amount("USD", 170),
		"USD -1.70":                 amount("USD", -170),
		"USD -92233720368547758.08": amount("USD", -1<<63),
		"  USD   2  ":               amount("USD", 200),
		"JPY 1234":                  amount("JPY", 1234),
		"KWD 1.234":                 amount("KWD", 1234),
		"KWD 0.001":                 amount("KWD", 1),
		"USD 0":                     amount("USD", 0),
		"USD 92233720368547758.07":  amount("USD", 9223372036854775807),
	} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			if got, err := table.Parse(text); err != nil || got != want {
				t.Errorf("Parse(%q) = %+v, %v, want %+v, nil", text, got, err, want)
			}
		})
	}
	if got, err := table.Of("KWD", "-0.5"); err != nil || got != (amount("KWD", -500)) {
		t.Errorf("Of(KWD, -0.5) = %+v, %v, want KWD -500 minor units", got, err)
	}
}

// What is not an amount in a declared currency is refused, never rounded; a
// currency the table does not have is an ErrCurrency.
func TestCurrenciesRefuseWhatIsNotAnAmount(t *testing.T) {
	t.Parallel()
	table := currencyTable(t)
	for text, currency := range map[string]bool{
		"USD 1.701": false, "JPY 1.5": false, "KWD 0.0001": false, "USD": false, "USD1.70": false,
		"USD 1e3": false, "USD 1_000": false, "USD 1.2.3": false, "USD ": false, "": false,
		"USD 92233720368547758.08": false, "EUR 1": true, "usd 1": true,
		// The literal's spelling is the language's, not the table's.
		"-USD 1.70": true, "-USD 1.7": true,
	} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			got, err := table.Parse(text)
			if err == nil || errors.Is(err, funroute.ErrCurrency) != currency {
				t.Errorf("Parse(%q) = %+v, %v, want an error that is ErrCurrency: %v", text, got, err, currency)
			}
		})
	}
	if _, err := table.Places("EUR"); !errors.Is(err, funroute.ErrCurrency) {
		t.Errorf("Places(EUR) error = %v, want ErrCurrency", err)
	}
}

// Format writes the figure in the currency's places, the sign on the figure;
// a currency-less zero is a bare 0.
func TestCurrenciesWriteAmountsInTheirPlaces(t *testing.T) {
	t.Parallel()
	table := currencyTable(t)
	for want, money := range map[string]funroute.Money{
		"USD 1.70": amount("USD", 170), "USD -0.05": amount("USD", -5),
		"USD 0.00": amount("USD", 0), "JPY -5": amount("JPY", -5), "KWD 1.234": amount("KWD", 1234),
		"KWD 0.001": amount("KWD", 1), "0": {},
	} {
		t.Run(want, func(t *testing.T) {
			t.Parallel()
			got, err := table.Format(money)
			if err != nil || got != want {
				t.Errorf("Format(%+v) = %q, %v, want %q", money, got, err, want)
			}
			if money.Currency() == "" {
				return
			}
			if back, err := table.Parse(got); err != nil || back != money {
				t.Errorf("Parse(Format(%+v)) = %+v, %v, want it back", money, back, err)
			}
		})
	}
	for _, places := range []struct {
		code string
		want int
	}{{"USD", 2}, {"JPY", 0}, {"KWD", 3}} {
		if got, err := table.Places(places.code); err != nil || got != places.want {
			t.Errorf("Places(%s) = %d, %v, want %d", places.code, got, err, places.want)
		}
	}
}

// The most negative amount a Money holds writes out and must read back.
func TestTheSmallestAmountReadsBack(t *testing.T) {
	t.Parallel()
	table := currencyTable(t)
	smallest := amount("USD", -1<<63)
	text, err := table.Format(smallest)
	if err != nil {
		t.Fatal(err)
	}
	if back, err := table.Parse(text); err != nil || back != smallest {
		t.Errorf("Parse(%q) = %+v, %v, want %+v", text, back, err, smallest)
	}
}

// A spec is checked where it is declared, whichever door it comes through.
func TestACurrencySpecIsCheckedWhereItIsDeclared(t *testing.T) {
	t.Parallel()
	usd := funroute.CurrencySpec{Code: "USD", Digits: 2}
	for name, spec := range map[string]funroute.MoneySpec{
		"no currency":     {},
		"lower-case code": {Currencies: []funroute.CurrencySpec{{Code: "usd", Digits: 2}}},
		"two-letter code": {Currencies: []funroute.CurrencySpec{{Code: "US", Digits: 2}}},
		"nine places":     {Currencies: []funroute.CurrencySpec{{Code: "USD", Digits: 9}}},
		"negative places": {Currencies: []funroute.CurrencySpec{{Code: "USD", Digits: -1}}},
		"declared twice":  {Currencies: []funroute.CurrencySpec{usd, usd}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := funroute.NewCurrencies(spec); err == nil {
				t.Errorf("NewCurrencies(%+v) error = nil, want one", spec)
			}
			if err := funroute.CoreRegistry().DeclareMoney(spec); err == nil {
				t.Errorf("DeclareMoney(%+v) error = nil, want one", spec)
			}
		})
	}
	if _, err := funroute.NewCurrencies(funroute.MoneySpec{Currencies: []funroute.CurrencySpec{{Code: "ABCDEFGH", Digits: 8}}}); err != nil {
		t.Errorf("NewCurrencies of an eight-letter code with eight places error = %v, want nil", err)
	}
}

// A registry's table is the one its rules use; declaring is once only, and
// a registry that declared nothing says so.
func TestRegistryCurrenciesIsWhatWasDeclared(t *testing.T) {
	t.Parallel()
	registry := funroute.CoreRegistry()
	if _, declared := registry.Currencies(); declared {
		t.Error("Currencies() of a registry that declared nothing reports a table")
	}
	if _, declared := registry.Money(); declared {
		t.Error("Money() of a registry that declared nothing reports a spec")
	}
	if err := registry.DeclareMoney(threeCurrencies); err != nil {
		t.Fatal(err)
	}
	table, declared := registry.Currencies()
	if !declared {
		t.Fatalf("Currencies() = %v, %v, want the declared table", table, declared)
	}
	if got := table.Spec().Codes(); !slices.Equal(got, []string{"JPY", "KWD", "USD"}) {
		t.Errorf("Spec().Codes() = %v, want [JPY KWD USD]: sorted by code", got)
	}
	spec := table.Spec()
	spec.Currencies[0].Digits = 5
	if again, _ := registry.Currencies(); again.Spec().Currencies[0].Digits != 0 {
		t.Error("changing a Spec() changed the registry's table")
	}
	if err := registry.DeclareMoney(threeCurrencies); err == nil {
		t.Error("DeclareMoney a second time error = nil, want one")
	}
}

// A host splits an amount as a rule does, by the same strategy: the units
// rounding leaves go where the strategy names, and the name reads back.
func TestAHostAllocatesByAStrategy(t *testing.T) {
	t.Parallel()
	currencies, err := funroute.NewCurrencies(funroute.MoneySpec{Currencies: []funroute.CurrencySpec{{Code: "USD", Digits: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	amount, err := currencies.Parse("USD 0.10")
	if err != nil {
		t.Fatal(err)
	}
	strategy, err := funroute.ParseAllocation("all_last")
	if err != nil || strategy != funroute.AllocateAllLast {
		t.Fatalf("ParseAllocation(all_last) = %v, %v", strategy, err)
	}
	shares, err := amount.AllocateBy(strategy, 1, 1, 1)
	if err != nil || len(shares) != 3 || shares[2].Minor() != 4 || shares[0].Minor() != 3 {
		t.Fatalf("USD 0.10 in three, all last = %v, %v, want 3, 3, 4 cents", shares, err)
	}
	if even, err := amount.SplitBy(funroute.AllocateReverseOrder, 3); err != nil || even[2].Minor() != 4 {
		t.Fatalf("USD 0.10 split in three, reverse order = %v, %v, want the last share 4 cents", even, err)
	}
}

// A host computes beside a rule with the same Go methods: the average and
// the median of amounts in one currency, an amount like another, a rate a
// table takes as a value, and each rounding mode by name.
func TestAHostComputesBesideTheRule(t *testing.T) {
	t.Parallel()
	table, err := funroute.NewCurrencies(funroute.MoneySpec{Currencies: []funroute.CurrencySpec{{Code: "USD", Digits: 2}, {Code: "JPY", Digits: 0}}})
	if err != nil {
		t.Fatal(err)
	}
	var amounts []funroute.Money
	for _, text := range []string{"USD 1.00", "USD 2.00", "USD 4.00"} {
		amount, err := table.Parse(text)
		if err != nil {
			t.Fatal(err)
		}
		amounts = append(amounts, amount)
	}
	for name, test := range map[string]struct {
		got  func() (funroute.Money, error)
		want int64
	}{
		"average, half down": {func() (funroute.Money, error) { return funroute.AverageMoney(amounts, funroute.RoundHalfDown) }, 233},
		"average, up":        {func() (funroute.Money, error) { return funroute.AverageMoney(amounts, funroute.RoundUp) }, 234},
		"median, floor":      {func() (funroute.Money, error) { return funroute.MedianMoney(amounts, funroute.RoundFloor) }, 200},
	} {
		if got, err := test.got(); err != nil || got.Minor() != test.want || got.Currency() != "USD" {
			t.Errorf("%s = %v, %v, want USD %d minor units", name, got, err, test.want)
		}
	}
	fx, err := table.FxRate("USD", "JPY", "150")
	if err != nil {
		t.Fatalf("FxRate(150 JPY / USD): %v", err)
	}
	if yen, err := table.Convert(amounts[0], fx, funroute.RoundHalfUp); err != nil || yen.Minor() != 150 {
		t.Fatalf("USD 1.00 at 150 JPY / USD = %v, %v, want JPY 150", yen, err)
	}
}
