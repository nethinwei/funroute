package machine

import (
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// unitFrame is a frame bound to a runtime whose arguments have the given
// types, for driving the boundary checks directly.
func unitFrame(t *testing.T, registry *Registry, params ...Parameter) *frame {
	t.Helper()
	artifact := &Artifact{parts: ArtifactParts{Args: params, Result: MoneyOf("c")}}
	runtime := &Runtime{artifact: artifact, registry: registry, money: newMoneyPlan(artifact, registry)}
	return &frame{runtime: runtime}
}

func TestTheBoundaryChecksEveryCurrencyAnArgumentHolds(t *testing.T) {
	t.Parallel()
	registry := declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2}, CurrencySpec{Code: "EUR", Digits: 2})
	params := []Parameter{{name: "amount", typ: MoneyOf("c")}, {name: "history", typ: ArrayOf(MoneyOf("c"))}}
	for name, test := range map[string]struct {
		args  []Value
		fails bool
	}{
		"one currency throughout": {[]Value{MoneyValue(1, "USD"), {kind: ArrayKind, box: []Money{{"USD", 2}, {"", 0}}}}, false},
		"an item in another":      {[]Value{MoneyValue(1, "USD"), {kind: ArrayKind, box: []Money{{"EUR", 2}}}}, true},
		"an undeclared currency":  {[]Value{MoneyValue(1, "GBP"), {kind: ArrayKind, box: []Money{}}}, true},
		"zero binds nothing":      {[]Value{MoneyValue(0, ""), {kind: ArrayKind, box: []Money{{"EUR", 2}}}}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := unitFrame(t, registry, params...).bindUnits(test.args)
			if test.fails != (err != nil) || (err != nil && (!errors.Is(err, ErrContract) || !errors.Is(err, ErrCurrency))) {
				t.Fatalf("bindUnits error = %v, want failure %v as ErrContract and ErrCurrency", err, test.fails)
			}
		})
	}
}

// A result's currency-less zero takes the currency its type names: what the
// run bound the variable to, and a container is copied only if it changes.
func TestAResultsZeroTakesItsCurrency(t *testing.T) {
	t.Parallel()
	registry := declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2})
	f := unitFrame(t, registry, Parameter{name: "amount", typ: MoneyOf("c")})
	if err := f.bindUnits([]Value{MoneyValue(5, "USD")}); err != nil {
		t.Fatal(err)
	}
	if filled, changed := f.fillUnits(MoneyValue(0, ""), MoneyOf("c")); !changed || filled.s != "USD" {
		t.Fatalf("zero in money<c> = %v, want USD 0", filled.Any())
	}
	proven := Value{kind: ArrayKind, box: []Money{{"USD", 1}}}
	if _, changed := f.fillUnits(proven, ArrayOf(MoneyOf("c"))); changed {
		t.Fatal("an array with nothing to fill was copied")
	}
	filled, _ := f.fillUnits(Value{kind: ArrayKind, box: []Money{{"", 0}}}, ArrayOf(MoneyOf("c")))
	if monies, _ := filled.box.([]Money); len(monies) != 1 || monies[0].currency != "USD" {
		t.Fatalf("filled array = %v, want [USD 0]", filled.Any())
	}
}

// A dictionary's currency-less zeros are filled like an array's.
func TestADictionarysZerosTakeTheirCurrency(t *testing.T) {
	t.Parallel()
	registry := declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2})
	f := unitFrame(t, registry)
	filled, changed := f.fillUnits(Value{kind: DictKind, box: map[string]Money{"a": {}}}, DictOf(MoneyOf("USD")))
	if monies, _ := filled.box.(map[string]Money); !changed || monies["a"].currency != "USD" {
		t.Fatalf("filled dictionary = %v, want a: USD 0", filled.Any())
	}
}

func moneyArray(items ...Money) Value { return Value{kind: ArrayKind, box: items} }

func moneyDict(entries map[string]Money) Value { return Value{kind: DictKind, box: entries} }

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

func TestUnitVariablesAreSortedAndDistinct(t *testing.T) {
	t.Parallel()
	params := []Parameter{
		{name: "amount", typ: MoneyOf("c")},
		{name: "caps", typ: ArrayOf(MoneyOf("a"))},
		{name: "order", typ: RecordOf(Field{name: "cur", typ: CurrencyOf("b")}, Field{name: "fee", typ: MoneyOf("USD")})},
		{name: "loose", typ: MoneyOf("")},
		{name: "count", typ: IntType},
	}
	if got := UnitVariables(params); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("UnitVariables = %v, want [a b c]", got)
	}
	if got := UnitVariables([]Parameter{{name: "fee", typ: MoneyOf("USD")}}); got != nil {
		t.Fatalf("UnitVariables of a code = %v, want none", got)
	}
}

// The plan scans the arguments that hold a currency, a rate being unitless,
// and fills in the result when it holds money.
func TestTheMoneyPlanMarksWhatHoldsACurrency(t *testing.T) {
	t.Parallel()
	registry := declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2})
	artifact := &Artifact{parts: ArtifactParts{Args: []Parameter{
		{name: "n", typ: IntType},
		{name: "m", typ: MoneyOf("")},
		{name: "rates", typ: ArrayOf(RateType)},
		{name: "order", typ: RecordOf(Field{name: "cur", typ: CurrencyOf("")})},
	}, Result: DictOf(MoneyOf("USD"))}}
	plan := newMoneyPlan(artifact, registry)
	if !slices.Equal(plan.scan, []bool{false, true, false, true}) || !plan.any || !plan.result || plan.table == nil {
		t.Fatalf("plan = scan %v, any %v, result %v, want [false true false true], true, true", plan.scan, plan.any, plan.result)
	}
	plain := newMoneyPlan(&Artifact{parts: ArtifactParts{Args: []Parameter{{name: "r", typ: RateType}}, Result: RateType}}, registry)
	if plain.any || plain.result {
		t.Fatalf("plan of a rate and a rate result = any %v, result %v, want neither", plain.any, plain.result)
	}
}

func TestTheBoundaryWalksEveryShape(t *testing.T) {
	t.Parallel()
	registry := declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2}, CurrencySpec{Code: "EUR", Digits: 2}, CurrencySpec{Code: "JPY", Digits: 0})
	for name, test := range boundaryCases(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := unitFrame(t, registry, test.params...).bindUnits(test.args)
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
		t.Fatalf("bindUnits error = %v, want none", err)
	case want == "":
	case err == nil:
		t.Fatalf("bindUnits succeeded, want an error containing %q", want)
	case !errors.Is(err, ErrContract) || !errors.Is(err, ErrCurrency) || !strings.Contains(err.Error(), want):
		t.Fatalf("bindUnits error = %v, want ErrContract and ErrCurrency containing %q", err, want)
	}
}

func one(name string, typ Type, value Value, want string) boundaryCase {
	return boundaryCase{[]Parameter{{name: name, typ: typ}}, []Value{value}, want}
}

func boundaryCases(t *testing.T) map[string]boundaryCase {
	t.Helper()
	order := RecordOf(Field{name: "amount", typ: MoneyOf("c")}, Field{name: "cur", typ: CurrencyOf("c")})
	nested := RecordOf(Field{name: "fees", typ: DictOf(MoneyOf("c"))})
	usd, eur := MoneyValue(1, "USD"), MoneyValue(1, "EUR")
	pair := []Parameter{{name: "amount", typ: MoneyOf("c")}, {name: "cap", typ: MoneyOf("c")}}
	return map[string]boundaryCase{
		"the code it names":         one("m", MoneyOf("USD"), usd, ""),
		"another code":              one("m", MoneyOf("USD"), eur, `argument "m"`),
		"a zero in a code":          one("m", MoneyOf("USD"), MoneyValue(0, ""), ""),
		"no currency, not zero":     one("m", MoneyOf(""), MoneyValue(5, ""), "5 minor units with no currency"),
		"an undeclared currency":    one("m", MoneyOf(""), MoneyValue(5, "GBP"), `"GBP" is not declared`),
		"a currency of its code":    one("c", CurrencyOf("USD"), CurrencyValue("USD"), ""),
		"a currency of another":     one("c", CurrencyOf("USD"), CurrencyValue("EUR"), "EUR where USD is declared"),
		"an empty currency":         one("c", CurrencyOf(""), CurrencyValue(""), "not declared"),
		"an item in another":        one("xs", ArrayOf(MoneyOf("USD")), moneyArray(Money{"USD", 1}, Money{"EUR", 1}), "item 1"),
		"an entry in another":       one("d", DictOf(MoneyOf("USD")), moneyDict(map[string]Money{"x": {"EUR", 1}}), `entry "x"`),
		"a record in one currency":  one("o", order, record(t, order, usd, CurrencyValue("USD")), ""),
		"a record in two":           one("o", order, record(t, order, usd, CurrencyValue("EUR")), `field "cur"`),
		"deep in records and dicts": one("xs", ArrayOf(nested), array(t, nested, record(t, nested, moneyDict(map[string]Money{"a": {"USD", 1}, "b": {"GBP", 1}}))), `entry "b"`),
		"rates are not scanned":     one("rs", ArrayOf(RateType), Value{kind: ArrayKind, box: []Rate{newRate(1)}}, ""),
		"a variable agreed":         {pair, []Value{usd, usd}, ""},
		"a variable and a zero":     {pair, []Value{MoneyValue(0, ""), eur}, ""},
		"a variable in conflict":    {pair, []Value{usd, eur}, "c is EUR here but USD elsewhere"},
		"an empty array":            one("xs", ArrayOf(MoneyOf("USD")), moneyArray(), ""),
	}
}

// Five currency variables are more than the frame keeps inline; they bind
// all the same, in their sorted order.
func TestTheBoundaryBindsManyVariables(t *testing.T) {
	t.Parallel()
	registry := declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2}, CurrencySpec{Code: "EUR", Digits: 2}, CurrencySpec{Code: "JPY", Digits: 0})
	var params []Parameter
	var args []Value
	for i, code := range []string{"USD", "EUR", "JPY", "USD", "EUR"} {
		name := string(rune('e' - i))
		params = append(params, Parameter{name: "m" + name, typ: MoneyOf(name)})
		args = append(args, MoneyValue(1, code))
	}
	f := unitFrame(t, registry, params...)
	if err := f.bindUnits(args); err != nil {
		t.Fatal(err)
	}
	if want := []string{"EUR", "USD", "JPY", "EUR", "USD"}; !slices.Equal(f.units, want) {
		t.Fatalf("units a–e = %v, want %v", f.units, want)
	}
	// The next run starts from nothing: another currency binds.
	args[0] = MoneyValue(1, "JPY")
	if err := f.bindUnits(args); err != nil || f.units[4] != "JPY" {
		t.Fatalf("a second run: %v, units %v, want e bound to JPY", err, f.units)
	}
}

// An artifact with money cannot load into a registry without it; if one ran
// there anyway, the boundary would refuse its currencies rather than guess.
func TestTheBoundaryNeedsATable(t *testing.T) {
	t.Parallel()
	err := unitFrame(t, CoreRegistry(), Parameter{name: "m", typ: MoneyOf("")}).bindUnits([]Value{MoneyValue(1, "USD")})
	expectBoundary(t, err, "declares no money")
}

// boundFrame is a frame whose run bound c to USD.
func boundFrame(t *testing.T) *frame {
	t.Helper()
	registry := declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2}, CurrencySpec{Code: "EUR", Digits: 2})
	f := unitFrame(t, registry, Parameter{name: "amount", typ: MoneyOf("c")}, Parameter{name: "other", typ: MoneyOf("d")})
	if err := f.bindUnits([]Value{MoneyValue(5, "USD"), MoneyValue(0, "")}); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestFillingGivesZerosTheirCurrency(t *testing.T) {
	t.Parallel()
	fee := RecordOf(Field{name: "fee", typ: MoneyOf("c")}, Field{name: "count", typ: IntType})
	nested := RecordOf(Field{name: "fees", typ: ArrayOf(MoneyOf("EUR"))})
	for name, test := range map[string]struct {
		value   func(t *testing.T) Value
		typ     Type
		want    string
		changed bool
	}{
		"a code":                   {func(*testing.T) Value { return MoneyValue(0, "") }, MoneyOf("EUR"), "{EUR 0}", true},
		"a bound variable":         {func(*testing.T) Value { return MoneyValue(0, "") }, MoneyOf("c"), "{USD 0}", true},
		"an unbound variable":      {func(*testing.T) Value { return MoneyValue(0, "") }, MoneyOf("d"), "{ 0}", false},
		"a variable not in a run":  {func(*testing.T) Value { return MoneyValue(0, "") }, MoneyOf("z"), "{ 0}", false},
		"an unknown unit":          {func(*testing.T) Value { return MoneyValue(0, "") }, MoneyOf(""), "{ 0}", false},
		"money with its currency":  {func(*testing.T) Value { return MoneyValue(0, "EUR") }, MoneyOf("c"), "{EUR 0}", false},
		"an int":                   {func(*testing.T) Value { return Int(0) }, IntType, "0", false},
		"a currency":               {func(*testing.T) Value { return CurrencyValue("USD") }, CurrencyOf("c"), "USD", false},
		"an array of zeros":        {func(*testing.T) Value { return moneyArray(Money{}, Money{"USD", 1}) }, ArrayOf(MoneyOf("c")), "[{USD 0} {USD 1}]", true},
		"a dictionary of zeros":    {func(*testing.T) Value { return moneyDict(map[string]Money{"a": {}}) }, DictOf(MoneyOf("c")), "map[a:{USD 0}]", true},
		"a record's zero":          {func(t *testing.T) Value { return record(t, fee, MoneyValue(0, ""), Int(2)) }, fee, "map[count:2 fee:{USD 0}]", true},
		"zeros deep in records":    {func(t *testing.T) Value { return array(t, nested, record(t, nested, moneyArray(Money{}))) }, ArrayOf(nested), "[map[fees:[{EUR 0}]]]", true},
		"arrays in a dictionary":   {func(t *testing.T) Value { return dictOfArrays(t, Money{}) }, DictOf(ArrayOf(MoneyOf("c"))), "map[a:[{USD 0}]]", true},
		"an array of rates":        {func(*testing.T) Value { return Value{kind: ArrayKind, box: []Rate{newRate(0)}} }, ArrayOf(RateType), "[0]", false},
		"an array without a zero":  {func(*testing.T) Value { return moneyArray(Money{"USD", 1}) }, ArrayOf(MoneyOf("c")), "[{USD 1}]", false},
		"an unbound array of zero": {func(*testing.T) Value { return moneyArray(Money{}) }, ArrayOf(MoneyOf("d")), "[{ 0}]", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			filled, changed := boundFrame(t).fillUnits(test.value(t), test.typ)
			if got := fmt.Sprint(filled.Any()); got != test.want || changed != test.changed {
				t.Fatalf("fillUnits(%s) = %s, changed %v, want %s, changed %v", test.typ, got, changed, test.want, test.changed)
			}
		})
	}
}

func dictOfArrays(t *testing.T, items ...Money) Value {
	t.Helper()
	value, err := Dict(ArrayOf(MoneyOf("")), map[string]Value{"a": moneyArray(items...)})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// Filling copies a container only when something in it changes, and never
// writes into the one it was handed: the host may still hold it.
func TestFillingCopiesOnlyWhatChanges(t *testing.T) {
	t.Parallel()
	f := boundFrame(t)
	proven := []Money{{"USD", 1}}
	if filled, _ := f.fillUnits(moneyArray(proven...), ArrayOf(MoneyOf("c"))); &filled.box.([]Money)[0] != &proven[0] {
		t.Fatal("an array with nothing to fill was copied")
	}
	entries := map[string]Money{"a": {"USD", 1}}
	if filled, _ := f.fillUnits(moneyDict(entries), DictOf(MoneyOf("c"))); reflect.ValueOf(filled.box).Pointer() != reflect.ValueOf(entries).Pointer() {
		t.Fatal("a dictionary with nothing to fill was copied")
	}
	fee := RecordOf(Field{name: "fee", typ: MoneyOf("c")})
	full := record(t, fee, MoneyValue(1, "USD"))
	if filled, _ := f.fillUnits(full, fee); filled.box != full.box {
		t.Fatal("a record with nothing to fill was copied")
	}
	zeros := []Money{{}}
	f.fillUnits(moneyArray(zeros...), ArrayOf(MoneyOf("c")))
	zeroEntries := map[string]Money{"a": {}}
	f.fillUnits(moneyDict(zeroEntries), DictOf(MoneyOf("c")))
	empty := record(t, fee, MoneyValue(0, ""))
	f.fillUnits(empty, fee)
	if zeros[0].currency != "" || zeroEntries["a"].currency != "" || empty.Field(0).s != "" {
		t.Fatalf("filling wrote into its input: %v, %v, %v", zeros, zeroEntries, empty.Any())
	}
}

// A program's result is filled on its way out: the zero a run computed is
// in the currency its contract binds.
func TestAResultIsFilledOnItsWayOut(t *testing.T) {
	t.Parallel()
	f := boundFrame(t)
	f.runtime.artifact.parts.Result = ArrayOf(MoneyOf("c"))
	f.runtime.money = newMoneyPlan(f.runtime.artifact, f.runtime.registry)
	f.stack = []Value{moneyArray(Money{}, Money{"USD", 3})}
	result, err := f.result()
	if got := fmt.Sprint(result.Any()); err != nil || got != "[{USD 0} {USD 3}]" {
		t.Fatalf("result() = %s, %v, want [{USD 0} {USD 3}]", got, err)
	}
}

// An exchange rate's two currencies bind and are checked like an amount's,
// and a rate a host built in Go is held to the rules on the way in.
func TestTheBoundaryChecksAnExchangeRate(t *testing.T) {
	t.Parallel()
	registry := declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2}, CurrencySpec{Code: "EUR", Digits: 2}, CurrencySpec{Code: "JPY", Digits: 0})
	fx := func(base, quote string, rate *big.Rat) Value {
		return FxRateValue(FxRate{base: base, quote: quote, rate: rate})
	}
	usdJPY := fx("USD", "JPY", big.NewRat(150, 1))
	withAmount := []Parameter{{name: "fx", typ: FxRateOf("a", "b")}, {name: "m", typ: MoneyOf("b")}}
	for name, test := range map[string]struct {
		boundaryCase
		also error
	}{
		"the pair it names":     {boundaryCase: one("fx", FxRateOf("USD", "JPY"), usdJPY, "")},
		"another base":          {boundaryCase: one("fx", FxRateOf("USD", "JPY"), fx("EUR", "JPY", big.NewRat(160, 1)), "EUR where USD is declared")},
		"another quote":         {boundaryCase: one("fx", FxRateOf("USD", "JPY"), fx("USD", "EUR", big.NewRat(9, 10)), "EUR where JPY is declared")},
		"an undeclared quote":   {boundaryCase: one("fx", FxRateOf("", ""), fx("USD", "GBP", big.NewRat(4, 5)), `"GBP" is not declared`)},
		"variables agreed":      {boundaryCase: boundaryCase{withAmount, []Value{usdJPY, MoneyValue(100, "JPY")}, ""}},
		"variables in conflict": {boundaryCase: boundaryCase{withAmount, []Value{usdJPY, MoneyValue(100, "EUR")}, "b is EUR here but JPY elsewhere"}},
		"in an array":           {boundaryCase: one("fxs", ArrayOf(FxRateOf("USD", "")), array(t, FxRateOf("", ""), usdJPY, fx("EUR", "USD", big.NewRat(11, 10))), "item 1")},
		"the zero FxRate":       {boundaryCase: one("fx", FxRateOf("", ""), FxRateValue(FxRate{}), "exchange rate needs")},
		"a non-code":            {boundaryCase: one("fx", FxRateOf("", ""), fx("usd", "JPY", big.NewRat(150, 1)), "exchange rate needs")},
		"a negative rate":       {boundaryCase: one("fx", FxRateOf("", ""), fx("USD", "JPY", big.NewRat(-150, 1)), ""), also: ErrArithmetic},
		"itself, not 1":         {boundaryCase: one("fx", FxRateOf("", ""), fx("USD", "USD", big.NewRat(2, 1)), ""), also: ErrArithmetic},
		"itself, 1":             {boundaryCase: one("fx", FxRateOf("", ""), fx("USD", "USD", big.NewRat(1, 1)), "")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := unitFrame(t, registry, test.params...).bindUnits(test.args)
			if test.also == nil {
				expectBoundary(t, err, test.want)
			} else if !errors.Is(err, ErrContract) || !errors.Is(err, test.also) {
				t.Fatalf("bindUnits(%v) error = %v, want ErrContract and %v", test.args[0].Any(), err, test.also)
			}
		})
	}
}
