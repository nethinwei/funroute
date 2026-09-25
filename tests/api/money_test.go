package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/nethinwei/funroute"
	"github.com/nethinwei/funroute/extensions/std"
)

// testTable is the currencies the helpers make values in; a code it does not
// declare is made with looseMoney, as a host's JSON would bring it.
var testTable = func() *funroute.Currencies {
	table, err := funroute.NewCurrencies(funroute.MoneySpec{Currencies: []funroute.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "EUR", Digits: 2}, {Code: "JPY", Digits: 0}, {Code: "KWD", Digits: 3},
	}})
	if err != nil {
		panic(err)
	}
	return table
}()

// amount is minor units in a declared currency.
func amount(code string, minor int64) funroute.Money {
	m, err := testTable.Minor(code, minor)
	if err != nil {
		panic(err)
	}
	return m
}

// looseMoney is money in the undeclared currency XXX, read from JSON, which
// checks only its shape: the way an undeclared currency reaches a rule's
// boundary.
func looseMoney(minor int64) funroute.Money {
	var m funroute.Money
	if err := json.Unmarshal(fmt.Appendf(nil, `{"currency":"XXX","minor":%d}`, minor), &m); err != nil {
		panic(err)
	}
	return m
}

// ratioText is a ratio the test writes as its exact text.
func ratioText(text string) funroute.Ratio {
	r, err := funroute.ParseRatio(text)
	if err != nil {
		panic(err)
	}
	return r
}

// currency is a currency as a value; an undeclared code comes from JSON.
func currency(code string) funroute.Currency {
	var c funroute.Currency
	if err := json.Unmarshal(fmt.Appendf(nil, "%q", code), &c); err != nil {
		panic(err)
	}
	return c
}

// valueOf is ToValue for a value that always converts.
func valueOf[T any](x T) funroute.Value {
	v, err := funroute.ToValue(x)
	if err != nil {
		panic(err)
	}
	return v
}

// FeeIn is a payment as a host holds it: the amount in its currency, and the
// cap in the same one.
type FeeIn struct {
	Amount funroute.Money `funroute:"amount"`
	Cap    funroute.Money `funroute:"cap"`
}

type FeeOut struct {
	Fee funroute.Money `funroute:"fee"`
}

func moneyConsole(t *testing.T) *funroute.Registry {
	t.Helper()
	registry := funroute.CoreRegistry()
	err := registry.DeclareMoney(funroute.MoneySpec{Currencies: []funroute.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "EUR", Digits: 2}, {Code: "JPY", Digits: 0},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

// A host declares its currencies, writes a contract of any currency and runs
// a fee rule on its own Go values; the result comes back in the currency the
// arguments brought, and a cap in another currency is refused where the rule
// compares them.
func TestHostRunsAFeeRuleInAnyCurrency(t *testing.T) {
	t.Parallel()
	registry := moneyConsole(t)
	options := funroute.CompileOptions{Args: []funroute.ArgSpec{
		{Name: "amount", Type: funroute.MoneyType}, {Name: "cap", Type: funroute.MoneyType},
	}}
	artifact, err := funroute.CompileExpr("if(round(amount * 0.029, @half_even) + money(30, currency(amount)) < cap, round(amount * 0.029, @half_even) + money(30, currency(amount)), cap)", registry, options)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := funroute.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	args := []funroute.Value{valueOf(amount("EUR", 10_000)), valueOf(amount("EUR", 500))}
	value, err := runtime.RunValues(t.Context(), args, funroute.RunOptions{})
	if money, _ := value.Money(); err != nil || money != (amount("EUR", 320)) {
		t.Fatalf("fee on EUR 100.00 = %v, %v, want EUR 3.20", value.Any(), err)
	}
	encoded, err := registry.EncodeJSON(value)
	if err != nil || string(encoded) != `"EUR 3.20"` {
		t.Fatalf("EncodeJSON = %s, %v, want \"EUR 3.20\"", encoded, err)
	}
	args[1] = valueOf(amount("USD", 500))
	if _, err := runtime.RunValues(t.Context(), args, funroute.RunOptions{}); !errors.Is(err, funroute.ErrCurrency) {
		t.Fatalf("a dollar cap on a euro payment: error = %v, want ErrCurrency", err)
	}
}

// A bound Go type carries money in funroute.Money fields, both ways.
func TestBindingCarriesMoney(t *testing.T) {
	t.Parallel()
	binding, err := funroute.Bind[FeeIn, FeeOut](moneyConsole(t))
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile("{fee: if(round(amount * 0.029, @half_even) < cap, round(amount * 0.029, @half_even), cap)}")
	if err != nil {
		t.Fatal(err)
	}
	in := FeeIn{Amount: amount("JPY", 10_000), Cap: amount("JPY", 1_000)}
	out, err := program.Run(t.Context(), &in, funroute.RunOptions{})
	if err != nil || out.Fee != (amount("JPY", 290)) {
		t.Fatalf("fee on JPY 10000 = %v, %v, want JPY 290", out.Fee, err)
	}
}

// A bound result declares its money without a currency, and a rule may
// still fill it with an amount it writes in one.
func TestABindingTakesAFixedAmountInItsRecord(t *testing.T) {
	t.Parallel()
	binding, err := funroute.Bind[FeeIn, FeeOut](moneyConsole(t))
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile("if(amount > cap, {fee: USD 0.30}, {fee: amount})")
	if err != nil {
		t.Fatal(err)
	}
	in := FeeIn{Amount: amount("EUR", 900), Cap: amount("EUR", 500)}
	out, err := program.Run(t.Context(), &in, funroute.RunOptions{})
	if err != nil || out.Fee != (amount("USD", 30)) {
		t.Fatalf("fee over the cap = %v, %v, want USD 0.30", out.Fee, err)
	}
}

// The operators' arithmetic, for a host: the currency is kept, a
// currency-less zero goes with any currency, two currencies are an
// ErrCurrency, and past int64 is an error rather than a wrap.
func TestMoneyArithmeticKeepsTheCurrency(t *testing.T) {
	t.Parallel()
	usd := func(minor int64) funroute.Money { return amount("USD", minor) }
	checkMoney := func(name string, got funroute.Money, err error, want funroute.Money) {
		t.Helper()
		if err != nil || got != want {
			t.Errorf("%s = %+v, %v, want %+v", name, got, err, want)
		}
	}
	sum, err := usd(170).Add(usd(30))
	checkMoney("USD 1.70 + USD 0.30", sum, err, usd(200))
	difference, err := usd(170).Sub(usd(200))
	checkMoney("USD 1.70 - USD 2.00", difference, err, usd(-30))
	zero, err := funroute.Money{}.Add(usd(5))
	checkMoney("0 + USD 0.05", zero, err, usd(5))
	negated, err := usd(5).Neg()
	checkMoney("-(USD 0.05)", negated, err, usd(-5))
	absolute, err := usd(-5).Abs()
	checkMoney("abs(USD -0.05)", absolute, err, usd(5))
	times, err := usd(333).MulInt(3)
	checkMoney("USD 3.33 × 3", times, err, usd(999))
	fee, err := usd(1000).MulRatio(ratioText("0.029"), funroute.RoundHalfUp)
	checkMoney("USD 10.00 × 2.9%", fee, err, usd(29))
	gross, err := usd(971).DivRatio(ratioText("0.971"), funroute.RoundHalfUp)
	checkMoney("USD 9.71 ÷ 97.1%", gross, err, usd(1000))
	if ratio, err := usd(29).Ratio(usd(1000)); err != nil || ratio != ratioText("0.029") {
		t.Errorf("USD 0.29 / USD 10.00 = %v, %v, want 0.029", ratio, err)
	}
	if order, err := usd(1).Cmp(usd(2)); err != nil || order != -1 {
		t.Errorf("Cmp(USD 0.01, USD 0.02) = %d, %v, want -1", order, err)
	}
	if order, err := (funroute.Money{}).Cmp(usd(0)); err != nil || order != 0 {
		t.Errorf("Cmp(0, USD 0.00) = %d, %v, want 0", order, err)
	}
	if usd(-3).Sign() != -1 || usd(0).Sign() != 0 || !usd(0).IsZero() || usd(1).IsZero() {
		t.Error("Sign and IsZero disagree with the minor units")
	}
}

func TestMoneyArithmeticRefusesWhatWouldBeWrong(t *testing.T) {
	t.Parallel()
	usd, jpy := amount("USD", 1), amount("JPY", 1)
	largest := amount("USD", 1<<63-1)
	for name, test := range map[string]struct {
		run      func() error
		currency bool
	}{
		"add":        {func() error { _, err := usd.Add(jpy); return err }, true},
		"sub":        {func() error { _, err := usd.Sub(jpy); return err }, true},
		"cmp":        {func() error { _, err := usd.Cmp(jpy); return err }, true},
		"ratio":      {func() error { _, err := usd.Ratio(jpy); return err }, true},
		"add past":   {func() error { _, err := largest.Add(usd); return err }, false},
		"times past": {func() error { _, err := largest.MulInt(2); return err }, false},
		"negate min": {func() error { _, err := amount("USD", -1<<63).Neg(); return err }, false},
		"abs min":    {func() error { _, err := amount("USD", -1<<63).Abs(); return err }, false},
		"rate past":  {func() error { _, err := largest.MulRatio(ratioText("2"), funroute.RoundHalfUp); return err }, false},
		"by zero":    {func() error { _, err := usd.DivRatio(ratioText("0"), funroute.RoundHalfUp); return err }, false},
		"ratio zero": {func() error { _, err := usd.Ratio(amount("USD", 0)); return err }, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := test.run()
			if err == nil || errors.Is(err, funroute.ErrCurrency) != test.currency {
				t.Errorf("%s error = %v, want an error that is ErrCurrency: %v", name, err, test.currency)
			}
		})
	}
}

// Allocate loses no minor unit: the shares add up to the amount, the
// remainder going one unit at a time to the largest weights, the earlier on
// a tie — for refunds as for payments.
func TestAllocateLosesNoMinorUnit(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		minor   int64
		weights []int64
		want    []int64
	}{
		"thirds":            {100, []int64{1, 1, 1}, []int64{34, 33, 33}},
		"largest weight":    {100, []int64{1, 2}, []int64{33, 67}},
		"tie goes earlier":  {5, []int64{1, 2, 2}, []int64{1, 2, 2}},
		"largest remainder": {101, []int64{1, 3, 3}, []int64{15, 43, 43}},
		"negative":          {-100, []int64{1, 1, 1}, []int64{-34, -33, -33}},
		"a zero weight":     {10, []int64{0, 1}, []int64{0, 10}},
		"less than a unit":  {1, []int64{1, 1, 1}, []int64{1, 0, 0}},
		"nothing to share":  {0, []int64{3, 1}, []int64{0, 0}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			shares, err := amount("JPY", test.minor).Allocate(test.weights...)
			if err != nil {
				t.Fatalf("Allocate(JPY %d, %v) error = %v", test.minor, test.weights, err)
			}
			checkShares(t, shares, test.want)
		})
	}
	split, err := amount("USD", 1000).Split(3)
	if err != nil {
		t.Fatal(err)
	}
	checkShares(t, split, []int64{334, 333, 333})
	for name, weights := range map[string][]int64{"no weights": nil, "all zero": {0, 0}, "negative": {2, -1}} {
		if _, err := (amount("USD", 1)).Allocate(weights...); err == nil {
			t.Errorf("Allocate with %s error = nil, want one", name)
		}
	}
	if _, err := (amount("USD", 1)).Split(0); err == nil {
		t.Error("Split(0) error = nil, want one")
	}
}

// checkShares fails unless shares are want in minor units, each in JPY or
// USD as the first one is.
func checkShares(t *testing.T, shares []funroute.Money, want []int64) {
	t.Helper()
	got := make([]int64, len(shares))
	for i, share := range shares {
		got[i] = share.Minor()
		if share.Currency() != shares[0].Currency() || share.Currency() == "" {
			t.Errorf("share %d is in %q, want the amount's currency", i, share.Currency())
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("shares = %v, want %v", got, want)
	}
}

// Ledger is a host type that uses every money type: the language reads each
// one as its own kind, not as a record or a string.
type Ledger struct {
	Amount funroute.Money            `funroute:"amount"`
	Fee    funroute.Ratio            `funroute:"fee"`
	Fx     funroute.FxRate           `funroute:"fx"`
	Payout funroute.Currency         `funroute:"payout"`
	Lines  []funroute.Money          `funroute:"lines"`
	Caps   map[string]funroute.Money `funroute:"caps"`
}

type Settlement struct {
	Fee     funroute.Money    `funroute:"fee"`
	Settled funroute.Money    `funroute:"settled"`
	Total   funroute.Money    `funroute:"total"`
	Cap     funroute.Money    `funroute:"cap"`
	Payout  funroute.Currency `funroute:"payout"`
	Double  funroute.Ratio    `funroute:"double"`
	Quoted  funroute.Money    `funroute:"quoted"`
	Fx      funroute.FxRate   `funroute:"fx"`
	Lines   []funroute.Money  `funroute:"lines"`
}

const settlementSource = `{fee: round(amount * fee, @half_even), settled: using(150 JPY / USD, round(amount -> JPY, @half_even)), total: sum(lines), cap: caps["card"], payout: payout,
double: fee + fee, quoted: using(fx, round(amount -> JPY, @half_even)), fx: fx, lines: [line * 2 for line in lines]}`

// fullConsole declares money before the standard pack, so the pack's money
// overloads (sum over money among them) are there.
func fullConsole(t *testing.T) *funroute.Registry {
	t.Helper()
	registry := moneyConsole(t)
	if err := registry.EnableForm(funroute.SwitchForm, funroute.ForForm, funroute.ReduceForm); err != nil {
		t.Fatal(err)
	}
	if err := std.Register(registry); err != nil {
		t.Fatal(err)
	}
	return registry
}

func ledger() Ledger {
	return Ledger{
		Amount: amount("USD", 1000), Fee: ratioText("0.029"),
		Fx: agreedRate(), Payout: currency("EUR"),
		Lines: []funroute.Money{amount("USD", 1), amount("USD", 2)},
		Caps:  map[string]funroute.Money{"card": amount("EUR", 5)},
	}
}

// Bind reads Money, Ratio and Currency fields as their kinds —
// containers of money too — and a program runs on them in both directions.
// Reflection cannot see a currency, so the contract is money, not money<c>.
func TestBindingCarriesEveryMoneyType(t *testing.T) {
	t.Parallel()
	registry := fullConsole(t)
	binding, err := funroute.Bind[Ledger, Settlement](registry)
	if err != nil {
		t.Fatal(err)
	}
	args := binding.Options().Args
	types := make([]string, 0, len(args))
	for _, arg := range args {
		types = append(types, arg.Name+":"+arg.Type.String())
	}
	want := []string{"amount:money", "fee:ratio", "fx:fxrate", "payout:currency", "lines:array<money>", "caps:dict<money>"}
	if !slices.Equal(types, want) {
		t.Fatalf("Bind[Ledger] arguments = %v, want %v", types, want)
	}
	program, err := binding.Compile(settlementSource)
	if err != nil {
		t.Fatal(err)
	}
	in := ledger()
	out, err := program.Run(t.Context(), &in, funroute.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	wantOut := Settlement{
		Fee: amount("USD", 29), Settled: amount("JPY", 1500),
		Total: amount("USD", 3), Cap: amount("EUR", 5), Payout: currency("EUR"),
		Double: ratioText("0.058"), Quoted: amount("JPY", 1600), Fx: in.Fx, Lines: []funroute.Money{amount("USD", 2), amount("USD", 4)},
	}
	if !equalSettlements(out, wantOut) {
		t.Errorf("Run(ledger) = %+v, want %+v", out, wantOut)
	}
}

// agreedRate is 160 JPY / USD, the rate a Ledger carries into a rule.
func agreedRate() funroute.FxRate {
	fx, err := testTable.FxRate("USD", "JPY", "160")
	if err != nil {
		panic(err)
	}
	return fx
}

// sameRate compares two rates, the zero one included.
func sameRate(a, b funroute.FxRate) bool {
	if a.Base() == "" || b.Base() == "" {
		return a.Base() == b.Base()
	}
	order, err := a.Cmp(b)
	return err == nil && order == 0
}

func equalSettlements(a, b Settlement) bool {
	return a.Fee == b.Fee && a.Settled == b.Settled && a.Total == b.Total && a.Cap == b.Cap && a.Payout == b.Payout &&
		a.Double == b.Double && a.Quoted == b.Quoted && sameRate(a.Fx, b.Fx) && slices.Equal(a.Lines, b.Lines)
}

// A bound run checks every currency it is handed: an undeclared one is
// refused at the boundary, and two in one list meet in the rule.
func TestBindingRefusesCurrenciesThatDoNotFit(t *testing.T) {
	t.Parallel()
	registry := fullConsole(t)
	binding, err := funroute.Bind[Ledger, Settlement](registry)
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(settlementSource)
	if err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		edit     func(*Ledger)
		contract bool
	}{
		"undeclared payout":       {func(in *Ledger) { in.Payout = currency("XXX") }, true},
		"undeclared amount":       {func(in *Ledger) { in.Amount = looseMoney(1000) }, true},
		"undeclared line":         {func(in *Ledger) { in.Lines[1] = looseMoney(2) }, true},
		"undeclared cap":          {func(in *Ledger) { in.Caps["card"] = looseMoney(1) }, true},
		"two currencies in lines": {func(in *Ledger) { in.Lines[1] = amount("JPY", 2) }, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := ledger()
			test.edit(&in)
			out, err := program.Run(t.Context(), &in, funroute.RunOptions{})
			if !errors.Is(err, funroute.ErrCurrency) || errors.Is(err, funroute.ErrContract) != test.contract {
				t.Errorf("Run(%s) error = %v, want ErrCurrency, ErrContract: %v", name, err, test.contract)
			}
			if !equalSettlements(out, Settlement{}) {
				t.Errorf("Run(%s) = %+v, want the zero value on failure", name, out)
			}
		})
	}
}

// everyKind compiles a record of every money kind from its arguments, for
// the tests of Run's JSON shapes.
func everyKind(t *testing.T) (*funroute.Registry, *funroute.Runtime) {
	t.Helper()
	registry := moneyConsole(t)
	artifact, err := funroute.CompileExpr(`{fee: round(amount * fee, @half_even), made: money(100, payout)}`, registry, funroute.CompileOptions{Args: []funroute.ArgSpec{
		{Name: "amount", Type: funroute.MoneyType}, {Name: "fee", Type: funroute.RatioType}, {Name: "payout", Type: funroute.CurrencyType},
	}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := funroute.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	return registry, runtime
}

// Run takes money as "USD 1.70" or as {"currency", "minor"}, a ratio as a
// decimal string or a number and a currency as its code — and the plain integer 0 as the currency-less zero.
func TestRunReadsEveryMoneyShape(t *testing.T) {
	t.Parallel()
	registry, runtime := everyKind(t)
	for name, test := range map[string]struct{ args, want string }{
		"text":          {`{"amount":"USD 10.00","fee":"0.029","payout":"EUR"}`, `{"fee":"USD 0.29","made":"EUR 1.00"}`},
		"minor units":   {`{"amount":{"currency":"USD","minor":1000},"fee":0.029,"payout":"JPY"}`, `{"fee":"USD 0.29","made":"JPY 100"}`},
		"negative":      {`{"amount":"USD -10.00","fee":"1","payout":"USD"}`, `{"fee":"USD -10.00","made":"USD 1.00"}`},
		"integer rate":  {`{"amount":"JPY 7","fee":2,"payout":"USD"}`, `{"fee":"JPY 14","made":"USD 1.00"}`},
		"zero as 0":     {`{"amount":0,"fee":"0.5","payout":"USD"}`, `{"fee":0,"made":"USD 1.00"}`},
		"zero, no code": {`{"amount":{"currency":"","minor":0},"fee":"0.5","payout":"USD"}`, `{"fee":0,"made":"USD 1.00"}`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			args, err := funroute.DecodeArgs([]byte(test.args))
			if err != nil {
				t.Fatal(err)
			}
			value, err := runtime.Run(t.Context(), args, funroute.RunOptions{})
			if err != nil {
				t.Fatalf("Run(%s) error = %v", test.args, err)
			}
			if encoded, err := registry.EncodeJSON(value); err != nil || string(encoded) != test.want {
				t.Errorf("Run(%s) = %s, %v, want %s", test.args, encoded, err, test.want)
			}
		})
	}
}

// What is not money in a declared currency is refused at the boundary as a
// contract failure, and as a currency failure when a currency is what is
// wrong.
func TestRunRefusesMoneyThatDoesNotFit(t *testing.T) {
	t.Parallel()
	_, runtime := everyKind(t)
	valid := map[string]string{"amount": `"USD 1"`, "fee": `"0.029"`, "payout": `"EUR"`}
	for name, test := range map[string]struct {
		key, value string
		currency   bool
	}{
		"too many places":       {"amount", `"USD 1.001"`, false},
		"no space":              {"amount", `"USD1"`, false},
		"an exponent":           {"amount", `"USD 1e3"`, false},
		"a bare number":         {"amount", `170`, false},
		"minor as text":         {"amount", `{"currency":"USD","minor":"170"}`, false},
		"minor with a fraction": {"amount", `{"currency":"USD","minor":1.5}`, false},
		"no currency field":     {"amount", `{"minor":170}`, false},
		"undeclared, object":    {"amount", `{"currency":"XXX","minor":1}`, true},
		"rate over zero":        {"fee", `"1/0"`, false},
		"rate not a decimal":    {"fee", `"abc"`, false},
		"rate as an object":     {"fee", `{}`, false},
		"undeclared payout":     {"payout", `"XXX"`, true},
		"payout as a number":    {"payout", `840`, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := runWith(t, runtime, valid, test.key, test.value)
			if !errors.Is(err, funroute.ErrContract) || errors.Is(err, funroute.ErrCurrency) != test.currency {
				t.Errorf("Run with %s = %s: error = %v, want ErrContract, ErrCurrency: %v", test.key, test.value, err, test.currency)
			}
		})
	}
}

// runWith runs with the valid arguments, key replaced by value, and returns
// the error.
func runWith(t *testing.T, runtime *funroute.Runtime, valid map[string]string, key, value string) error {
	t.Helper()
	document := "{"
	for _, name := range []string{"amount", "fee", "payout"} {
		text := valid[name]
		if name == key {
			text = value
		}
		if len(document) > 1 {
			document += ","
		}
		document += `"` + name + `":` + text
	}
	args, err := funroute.DecodeArgs([]byte(document + "}"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Run(t.Context(), args, funroute.RunOptions{})
	return err
}

// "XXX 1" names a currency the registry does not have, the same as
// {"currency": "XXX", "minor": 1}, and minor units with no currency are money
// in no currency: each is an ErrCurrency, as its message says.
func TestAnUndeclaredCurrencyInTextIsACurrencyError(t *testing.T) {
	t.Parallel()
	_, runtime := everyKind(t)
	valid := map[string]string{"amount": `"USD 1"`, "fee": `"0.029"`, "payout": `"EUR"`}
	for _, amount := range []string{`"XXX 1"`, `{"currency":"","minor":5}`} {
		if err := runWith(t, runtime, valid, "amount", amount); !errors.Is(err, funroute.ErrCurrency) || !errors.Is(err, funroute.ErrContract) {
			t.Errorf("Run with amount %s error = %v, want ErrCurrency and ErrContract", amount, err)
		}
	}
}

// ErrCurrency tells a rule meeting two currencies apart from a contract
// broken at the door; an argument in another currency than the contract's
// code is both.
func TestCurrencyErrorsAreTyped(t *testing.T) {
	t.Parallel()
	registry := moneyConsole(t)
	for name, test := range map[string]struct {
		unit     string
		contract bool
	}{"two currencies meet": {"", false}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			artifact, err := funroute.CompileExpr(`a + b`, registry, funroute.CompileOptions{Args: []funroute.ArgSpec{
				{Name: "a", Type: funroute.MoneyType}, {Name: "b", Type: funroute.MoneyType},
			}})
			if err != nil {
				t.Fatal(err)
			}
			runtime, err := funroute.Instantiate(artifact, registry)
			if err != nil {
				t.Fatal(err)
			}
			args := []funroute.Value{valueOf(amount("USD", 1)), valueOf(amount("EUR", 1))}
			_, err = runtime.RunValues(t.Context(), args, funroute.RunOptions{})
			if !errors.Is(err, funroute.ErrCurrency) || errors.Is(err, funroute.ErrContract) != test.contract || errors.Is(err, funroute.ErrExtension) {
				t.Errorf("USD + EUR (%s) error = %v, want ErrCurrency, ErrContract: %v, not ErrExtension", name, err, test.contract)
			}
		})
	}
}

// fallback does not catch a currency error: two currencies meeting is a
// fault in the rule or the data, not a missing value.
func TestFallbackDoesNotCatchACurrencyError(t *testing.T) {
	t.Parallel()
	registry := moneyConsole(t)
	artifact, err := funroute.CompileExpr(`fallback(a + b, a)`, registry, funroute.CompileOptions{Args: []funroute.ArgSpec{
		{Name: "a", Type: funroute.MoneyType}, {Name: "b", Type: funroute.MoneyType},
	}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := funroute.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(t.Context(), map[string]any{"a": "USD 1", "b": "EUR 1"}, funroute.RunOptions{})
	if !errors.Is(err, funroute.ErrCurrency) {
		t.Errorf("fallback(USD 1 + EUR 1, USD 1) = %v, %v, want ErrCurrency", value.Any(), err)
	}
}

// A manifest carries the money feature: a registry that applies it declares
// the same currencies and rounding, so money programs compile there to the
// host's artifact; a registry that declared other money refuses it.
func TestManifestCarriesTheMoneyFeature(t *testing.T) {
	t.Parallel()
	host := fullConsole(t)
	fee := func(amount funroute.Money) funroute.Money { return amount }
	if err := host.Register(funroute.FunctionSpec{Name: "ledger.fee_v1", Doc: funroute.Doc{Cost: 5}, Go: fee}); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(host.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	var manifest funroute.Manifest
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		t.Fatal(err)
	}
	declared, declares := manifest.Money()
	if spec, _ := host.Money(); !declares || !slices.Equal(declared.Currencies, spec.Currencies) {
		t.Fatalf("manifest money = %+v, %v, want the host's %+v", declared, declares, spec)
	}
	// A tool declares the manifest's money before the standard pack, as the
	// CLI's language server does, then applies the rest.
	browser := funroute.CoreRegistry()
	if err := browser.DeclareMoney(declared); err != nil {
		t.Fatal(err)
	}
	if err := browser.EnableForm(funroute.SwitchForm, funroute.ForForm, funroute.ReduceForm); err != nil {
		t.Fatal(err)
	}
	if err := std.Register(browser); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Apply(browser); err != nil {
		t.Fatalf("Apply(manifest with money) error = %v", err)
	}
	checkSameArtifact(t, host, browser)
	bare := funroute.CoreRegistry()
	if err := moneyOnlyManifest(t, declared).Apply(bare); err != nil {
		t.Fatalf("Apply(money alone) to a bare registry error = %v", err)
	}
	if _, declared := bare.Currencies(); !declared {
		t.Error("applying a manifest's money to a bare registry declared none")
	}
	other := funroute.CoreRegistry()
	if err := other.DeclareMoney(funroute.MoneySpec{Currencies: append(slices.Clone(declared.Currencies), funroute.CurrencySpec{Code: "GBP", Digits: 2})}); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Apply(other); err == nil {
		t.Error("Apply(manifest) to a registry with other currencies error = nil, want a refusal")
	}
}

// checkSameArtifact compiles a money program on both registries and wants one
// digest, and the host's artifact to load on the other.
// moneyOnlyManifest is a manifest that declares money and nothing else, as a
// tool would write one by hand in JSON.
func moneyOnlyManifest(t *testing.T, spec funroute.MoneySpec) funroute.Manifest {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{"version": funroute.ManifestVersion, "forms": []string{}, "functions": []any{}, "money": spec})
	if err != nil {
		t.Fatal(err)
	}
	var manifest funroute.Manifest
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func checkSameArtifact(t *testing.T, host, browser *funroute.Registry) {
	t.Helper()
	source := `ledger.fee_v1(sum([round(line * 2.9%, @half_even) for line in lines])) + USD 0.30`
	options := funroute.CompileOptions{Args: []funroute.ArgSpec{{Name: "lines", Type: funroute.ArrayOf(funroute.MoneyType)}}}
	atHost, err := funroute.CompileExpr(source, host, options)
	if err != nil {
		t.Fatal(err)
	}
	inBrowser, err := funroute.CompileExpr(source, browser, options)
	if err != nil {
		t.Fatalf("CompileExpr(%q) against the manifest error = %v", source, err)
	}
	if inBrowser.Digest() != atHost.Digest() {
		t.Errorf("against the manifest %q compiles to digest %s, want the host's %s", source, inBrowser.Digest(), atHost.Digest())
	}
	if _, err := funroute.Instantiate(atHost, browser); err != nil {
		t.Errorf("Instantiate(host artifact) against the manifest error = %v", err)
	}
}

// A money value's JSON is the shape it had when its fields were public, and
// reads back to the same value.
func TestMoneyValuesKeepTheirJSONShape(t *testing.T) {
	t.Parallel()
	card, _ := funroute.ParseRatio("0.029")
	for want, value := range map[string]any{
		`{"currency":"USD","minor":170}`: amount("USD", 170),
		`"USD"`:                          currency("USD"),
		`"0.029"`:                        card,
		`[{"currency":"","minor":0},{"currency":"JPY","minor":5}]`: []funroute.Money{{}, amount("JPY", 5)},
	} {
		encoded, err := json.Marshal(value)
		if err != nil || string(encoded) != want {
			t.Errorf("json.Marshal(%v) = %s, %v, want %s", value, encoded, err, want)
		}
	}
}

// A host hands a rule exchange rates as FxRate values and gets them back: a
// rate it passes goes to using, and one the rule works out from two amounts
// comes back exact.
func TestHostAndRuleTradeExchangeRates(t *testing.T) {
	t.Parallel()
	registry := moneyConsole(t)
	table, _ := registry.Currencies()
	agreed, err := table.FxRate("USD", "JPY", "150.25")
	if err != nil {
		t.Fatal(err)
	}
	contract := funroute.CompileOptions{Args: []funroute.ArgSpec{
		{Name: "fx", Type: funroute.FxRateType}, {Name: "amount", Type: funroute.MoneyType},
	}}
	artifact, err := funroute.CompileExpr("using(fx, round(amount -> JPY, @half_up))", registry, contract)
	if err != nil {
		t.Fatal(err)
	}
	runtime, _ := funroute.Instantiate(artifact, registry)
	value, err := runtime.Run(t.Context(), map[string]any{"fx": agreed, "amount": amount("USD", 200)}, funroute.RunOptions{})
	if yen, _ := value.Money(); err != nil || yen != amount("JPY", 301) {
		t.Fatalf("using(fx, USD 2.00 -> JPY) at %s = %v, %v, want JPY 301 (300.5, half up)", agreed, value.Any(), err)
	}
	implied, err := funroute.CompileExpr("implied(settled, paid)", registry, funroute.CompileOptions{Args: []funroute.ArgSpec{
		{Name: "settled", Type: funroute.MoneyType}, {Name: "paid", Type: funroute.MoneyType},
	}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, _ = funroute.Instantiate(implied, registry)
	value, err = runtime.Run(t.Context(), map[string]any{"settled": "JPY 30050", "paid": "USD 200.00"}, funroute.RunOptions{})
	back, converted := funroute.FromValue[funroute.FxRate](value)
	if order, _ := back.Cmp(agreed); err != nil || converted != nil || order != 0 {
		t.Fatalf("implied(JPY 30050, USD 200.00) = %v (%v, %v), want %s", back, err, converted, agreed)
	}
}

// The strategy constants are the members of the enum a rule names them in.
func TestAllocationConstantsAreTheEnumsMembers(t *testing.T) {
	t.Parallel()
	enum := funroute.AllocationEnumType()
	strategies := []funroute.AllocationStrategy{
		funroute.AllocateLargestRemainder, funroute.AllocateLargestWeight, funroute.AllocateInOrder,
		funroute.AllocateReverseOrder, funroute.AllocateAllFirst, funroute.AllocateAllLast,
	}
	names := make([]string, 0, len(strategies))
	for _, strategy := range strategies {
		parsed, err := funroute.ParseAllocation(strategy.String())
		if err != nil || parsed != strategy {
			t.Fatalf("ParseAllocation(%q) = %v, %v, want %v", strategy.String(), parsed, err, strategy)
		}
		names = append(names, strategy.String())
	}
	slices.Sort(names)
	if enum.Name() != "allocation" || !slices.Equal(enum.Values(), names) {
		t.Fatalf("AllocationEnumType() = %s, want enum<allocation> of %v", enum, names)
	}
}
