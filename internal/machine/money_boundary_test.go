package machine

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/money"
)

// unitRuntime is a runtime whose arguments have the given types, for driving
// the boundary checks directly.
func unitRuntime(t *testing.T, registry *Registry, params ...Parameter) *Runtime {
	t.Helper()
	artifact := &Artifact{parts: ArtifactParts{Args: params, Result: MoneyType}}
	return &Runtime{artifact: artifact, registry: registry, money: newMoneyPlan(artifact, registry)}
}

func TestTheBoundaryChecksEveryCurrencyAnArgumentHolds(t *testing.T) {
	t.Parallel()
	registry := declared(t, money.CurrencySpec{Code: "USD", Digits: 2}, money.CurrencySpec{Code: "EUR", Digits: 2})
	params := []Parameter{{name: "amount", typ: MoneyType}, {name: "history", typ: ArrayOf(MoneyType)}}
	for name, test := range map[string]struct {
		args  []Value
		fails bool
	}{
		"one currency throughout": {[]Value{MoneyValue(1, "USD"), arrayOf([]money.Money{money.Make("USD", 2), money.Make("", 0)})}, false},
		// money is any declared currency, each value its own: a list of
		// two is the aggregate's to refuse, not the boundary's.
		"two currencies":         {[]Value{MoneyValue(1, "USD"), arrayOf([]money.Money{money.Make("EUR", 2)})}, false},
		"an undeclared currency": {[]Value{MoneyValue(1, "GBP"), arrayOf([]money.Money{})}, true},
		"an undeclared item":     {[]Value{MoneyValue(0, ""), arrayOf([]money.Money{money.Make("GBP", 2)})}, true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := unitRuntime(t, registry, params...).checkUnits(test.args)
			if test.fails != (err != nil) || (err != nil && (!errors.Is(err, ErrContract) || !errors.Is(err, ErrCurrency))) {
				t.Fatalf("checkUnits error = %v, want failure %v as ErrContract and ErrCurrency", err, test.fails)
			}
		})
	}
}

func moneyArray(items ...money.Money) Value { return arrayOf(items) }

func moneyDict(entries map[string]money.Money) Value { return Value{kind: DictKind, box: entries} }

func record(t *testing.T, typ Type, fields ...Value) Value {
	t.Helper()
	value, err := Record(typ, fields)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func array(t *testing.T, elem Type, items ...Value) Value {
	t.Helper()
	value, err := Array(elem, items)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// The plan scans the arguments that hold a currency, a ratio being unitless,
// and fills in the result when it holds money.
func TestTheMoneyPlanMarksWhatHoldsACurrency(t *testing.T) {
	t.Parallel()
	registry := declared(t, money.CurrencySpec{Code: "USD", Digits: 2})
	artifact := &Artifact{parts: ArtifactParts{Args: []Parameter{
		{name: "n", typ: IntType},
		{name: "m", typ: MoneyType},
		{name: "rates", typ: ArrayOf(RatioType)},
		{name: "order", typ: RecordOf(Field{name: "cur", typ: CurrencyType})},
	}, Result: DictOf(MoneyType)}}
	plan := newMoneyPlan(artifact, registry)
	if !slices.Equal(plan.scan, []bool{false, true, false, true}) || !plan.any || plan.table == nil {
		t.Fatalf("plan = scan %v, any %v, want [false true false true], true", plan.scan, plan.any)
	}
	plain := newMoneyPlan(&Artifact{parts: ArtifactParts{Args: []Parameter{{name: "r", typ: RatioType}}, Result: RatioType}}, registry)
	if plain.any {
		t.Fatal("plan of a ratio scans it, want nothing scanned")
	}
}

func TestTheBoundaryWalksEveryShape(t *testing.T) {
	t.Parallel()
	registry := declared(t, money.CurrencySpec{Code: "USD", Digits: 2}, money.CurrencySpec{Code: "EUR", Digits: 2}, money.CurrencySpec{Code: "JPY", Digits: 0})
	for name, test := range boundaryCases(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := unitRuntime(t, registry, test.params...).checkUnits(test.args)
			expectBoundary(t, err, test.want)
		})
	}
}

// boundaryCase is a contract, arguments for it, and the text the error
// binding them must contain, "" when they bind.
type boundaryCase struct {
	params []Parameter
	args   []Value
	want   string
}

func expectBoundary(t *testing.T, err error, want string) {
	t.Helper()
	switch {
	case want == "" && err != nil:
		t.Fatalf("checkUnits error = %v, want none", err)
	case want == "":
	case err == nil:
		t.Fatalf("checkUnits succeeded, want an error containing %q", want)
	case !errors.Is(err, ErrContract) || !errors.Is(err, ErrCurrency) || !strings.Contains(err.Error(), want):
		t.Fatalf("checkUnits error = %v, want ErrContract and ErrCurrency containing %q", err, want)
	}
}

func one(name string, typ Type, value Value, want string) boundaryCase {
	return boundaryCase{[]Parameter{{name: name, typ: typ}}, []Value{value}, want}
}

func boundaryCases(t *testing.T) map[string]boundaryCase {
	t.Helper()
	order := RecordOf(Field{name: "amount", typ: MoneyType}, Field{name: "cur", typ: CurrencyType})
	nested := RecordOf(Field{name: "fees", typ: DictOf(MoneyType)})
	usd, eur := MoneyValue(1, "USD"), MoneyValue(1, "EUR")
	pair := []Parameter{{name: "amount", typ: MoneyType}, {name: "cap", typ: MoneyType}}
	return map[string]boundaryCase{
		"a declared currency":       one("m", MoneyType, usd, ""),
		"a currency-less zero":      one("m", MoneyType, MoneyValue(0, ""), ""),
		"no currency, not zero":     one("m", MoneyType, MoneyValue(5, ""), "5 minor units with no currency"),
		"an undeclared currency":    one("m", MoneyType, MoneyValue(5, "GBP"), `"GBP" is not declared`),
		"a declared currency value": one("c", CurrencyType, CurrencyValue("EUR"), ""),
		"an empty currency":         one("c", CurrencyType, CurrencyValue(""), "not declared"),
		"items in two currencies":   one("xs", ArrayOf(MoneyType), moneyArray(money.Make("USD", 1), money.Make("EUR", 1)), ""),
		"an undeclared item":        one("xs", ArrayOf(MoneyType), moneyArray(money.Make("USD", 1), money.Make("GBP", 1)), "item 1"),
		"an undeclared entry":       one("d", DictOf(MoneyType), moneyDict(map[string]money.Money{"x": money.Make("GBP", 1)}), `entry "x"`),
		"a record in two":           one("o", order, record(t, order, usd, CurrencyValue("EUR")), ""),
		"an undeclared field":       one("o", order, record(t, order, usd, CurrencyValue("GBP")), `field "cur"`),
		"deep in records and dicts": one("xs", ArrayOf(nested), array(t, nested, record(t, nested, moneyDict(map[string]money.Money{"a": money.Make("USD", 1), "b": money.Make("GBP", 1)}))), `entry "b"`),
		"ratios are not scanned":    one("rs", ArrayOf(RatioType), Value{kind: ArrayKind, box: []money.Ratio{ratio(1, 10_000_000_000)}}, ""),
		"two amounts, one currency": {pair, []Value{usd, usd}, ""},
		"two amounts, two":          {pair, []Value{usd, eur}, ""},
		"an empty array":            one("xs", ArrayOf(MoneyType), moneyArray(), ""),
	}
}

// An artifact with money cannot load into a registry without it; if one ran
// there anyway, the boundary would refuse its currencies rather than guess.
func TestTheBoundaryNeedsATable(t *testing.T) {
	t.Parallel()
	err := unitRuntime(t, CoreRegistry(), Parameter{name: "m", typ: MoneyType}).checkUnits([]Value{MoneyValue(1, "USD")})
	expectBoundary(t, err, "declares no money")
}

// An exchange rate's two currencies are checked like an amount's,
// and a rate a host built in Go is held to the rules on the way in.
func TestTheBoundaryChecksAnExchangeRate(t *testing.T) {
	t.Parallel()
	registry := declared(t, money.CurrencySpec{Code: "USD", Digits: 2}, money.CurrencySpec{Code: "EUR", Digits: 2}, money.CurrencySpec{Code: "JPY", Digits: 0})
	fx := func(base, quote string, r money.Ratio) Value {
		return FxRateValue(money.FxRateFrom(&money.Pair{Base: base, Quote: quote}, r))
	}
	usdJPY := fx("USD", "JPY", ratio(150, 1))
	for name, test := range map[string]struct {
		boundaryCase
		also error
	}{
		"the pair it names":     {boundaryCase: one("fx", FxRateType, usdJPY, "")},
		"an undeclared quote":   {boundaryCase: one("fx", FxRateType, fx("USD", "GBP", ratio(4, 5)), `"GBP" is not declared`)},
		"in an array":           {boundaryCase: one("fxs", ArrayOf(FxRateType), array(t, FxRateType, usdJPY, fx("USD", "GBP", ratio(4, 5))), "item 1")},
		"the zero money.FxRate": {boundaryCase: one("fx", FxRateType, FxRateValue(money.FxRate{}), "exchange rate needs")},
		"a non-code":            {boundaryCase: one("fx", FxRateType, fx("usd", "JPY", ratio(150, 1)), "exchange rate needs")},
		"a negative rate":       {boundaryCase: one("fx", FxRateType, fx("USD", "JPY", ratio(-150, 1)), ""), also: ErrArithmetic},
		"itself, not 1":         {boundaryCase: one("fx", FxRateType, fx("USD", "USD", ratio(2, 1)), ""), also: ErrArithmetic},
		"itself, 1":             {boundaryCase: one("fx", FxRateType, fx("USD", "USD", ratio(1, 1)), "")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := unitRuntime(t, registry, test.params...).checkUnits(test.args)
			if test.also == nil {
				expectBoundary(t, err, test.want)
			} else if !errors.Is(err, ErrContract) || !errors.Is(err, test.also) {
				t.Fatalf("checkUnits(%v) error = %v, want ErrContract and %v", test.args[0].Any(), err, test.also)
			}
		})
	}
}
