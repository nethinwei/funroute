package lang_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"

	"funroute/extensions/std"
	"funroute/lang"
)

// rateScale is how many of the machine's rate units make 1; the tests below
// were written in those units, and rateUnits turns one back into a Rate.
const rateScale = 10_000_000_000

// testTable is the currencies the helpers make values in; a code it does not
// declare is made with looseMoney, as a host's JSON would bring it.
var testTable = func() *lang.Currencies {
	table, err := lang.NewCurrencies(lang.MoneySpec{Rounding: lang.RoundHalfUp, Currencies: []lang.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "EUR", Digits: 2}, {Code: "JPY", Digits: 0}, {Code: "KWD", Digits: 3},
	}})
	if err != nil {
		panic(err)
	}
	return table
}()

// amount is minor units in a declared currency.
func amount(code string, minor int64) lang.Money {
	m, err := testTable.Minor(code, minor)
	if err != nil {
		panic(err)
	}
	return m
}

// looseMoney is money read from JSON, which checks only its shape: the way
// an undeclared currency reaches a rule's boundary.
func looseMoney(code string, minor int64) lang.Money {
	var m lang.Money
	if err := json.Unmarshal(fmt.Appendf(nil, `{"currency":%q,"minor":%d}`, code, minor), &m); err != nil {
		panic(err)
	}
	return m
}

// rateUnits is a rate given in the machine's units of 1e-10.
func rateUnits(n int64) lang.Rate {
	sign := ""
	if n < 0 {
		sign, n = "-", -n
	}
	r, err := lang.ParseRate(fmt.Sprintf("%s%d.%010d", sign, n/rateScale, n%rateScale))
	if err != nil {
		panic(err)
	}
	return r
}

// yenRates is a rate table over registry's currencies where a dollar buys
// 150 yen.
func yenRates(t *testing.T, registry *lang.Registry) *lang.Rates {
	t.Helper()
	table, declared := registry.Currencies()
	if !declared {
		t.Fatal("the registry declares no money")
	}
	rates := table.NewRates()
	if err := rates.Add("USD", "JPY", "150"); err != nil {
		t.Fatal(err)
	}
	return rates
}

// currency is a currency as a value; an undeclared code comes from JSON.
func currency(code string) lang.Currency {
	var c lang.Currency
	if err := json.Unmarshal(fmt.Appendf(nil, "%q", code), &c); err != nil {
		panic(err)
	}
	return c
}

// valueOf is ToValue for a value that always converts.
func valueOf[T any](x T) lang.Value {
	v, err := lang.ToValue(x)
	if err != nil {
		panic(err)
	}
	return v
}

// FeeIn is a payment as a host holds it: the amount in its currency, and the
// cap in the same one.
type FeeIn struct {
	Amount lang.Money `funroute:"amount"`
	Cap    lang.Money `funroute:"cap"`
}

type FeeOut struct {
	Fee lang.Money `funroute:"fee"`
}

func moneyConsole(t *testing.T) *lang.Registry {
	t.Helper()
	registry := lang.CoreRegistry()
	err := registry.DeclareMoney(lang.MoneySpec{Rounding: lang.RoundHalfUp, Currencies: []lang.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "EUR", Digits: 2}, {Code: "JPY", Digits: 0},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

// A host declares its currencies, writes a contract in one currency variable
// and runs a fee rule on its own Go values; the result comes back in the
// currency the arguments brought.
func TestHostRunsAFeeRuleInAnyCurrency(t *testing.T) {
	t.Parallel()
	registry := moneyConsole(t)
	options := lang.CompileOptions{Args: []lang.ArgSpec{
		{Name: "amount", Type: lang.MoneyOf("c")}, {Name: "cap", Type: lang.MoneyOf("c")},
	}}
	artifact, err := lang.CompileExpr("if(amount * 0.029 + like(amount, 30) < cap, amount * 0.029 + like(amount, 30), cap)", registry, options)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := lang.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	args := []lang.Value{valueOf(amount("EUR", 10_000)), valueOf(amount("EUR", 500))}
	value, err := runtime.RunValues(t.Context(), args, lang.RunOptions{})
	if money, _ := value.Money(); err != nil || money != (amount("EUR", 320)) {
		t.Fatalf("fee on EUR 100.00 = %v, %v, want EUR 3.20", value.Any(), err)
	}
	encoded, err := registry.EncodeJSON(value)
	if err != nil || string(encoded) != `"EUR 3.20"` {
		t.Fatalf("EncodeJSON = %s, %v, want \"EUR 3.20\"", encoded, err)
	}
	args[1] = valueOf(amount("USD", 500))
	if _, err := runtime.RunValues(t.Context(), args, lang.RunOptions{}); !errors.Is(err, lang.ErrCurrency) {
		t.Fatalf("a dollar cap on a euro payment: error = %v, want ErrCurrency", err)
	}
}

// A bound Go type carries money in lang.Money fields, both ways.
func TestBindingCarriesMoney(t *testing.T) {
	t.Parallel()
	binding, err := lang.Bind[FeeIn, FeeOut](moneyConsole(t))
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile("{fee: if(amount * 0.029 < cap, amount * 0.029, cap)}")
	if err != nil {
		t.Fatal(err)
	}
	in := FeeIn{Amount: amount("JPY", 10_000), Cap: amount("JPY", 1_000)}
	out, err := program.Run(t.Context(), &in, lang.RunOptions{})
	if err != nil || out.Fee != (amount("JPY", 290)) {
		t.Fatalf("fee on JPY 10000 = %v, %v, want JPY 290", out.Fee, err)
	}
}

// A bound result declares its money without a currency, and a rule may
// still fill it with an amount it writes in one.
func TestABindingTakesAFixedAmountInItsRecord(t *testing.T) {
	t.Parallel()
	binding, err := lang.Bind[FeeIn, FeeOut](moneyConsole(t))
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile("if(amount > cap, {fee: USD 0.30}, {fee: amount})")
	if err != nil {
		t.Fatal(err)
	}
	in := FeeIn{Amount: amount("EUR", 900), Cap: amount("EUR", 500)}
	out, err := program.Run(t.Context(), &in, lang.RunOptions{})
	if err != nil || out.Fee != (amount("USD", 30)) {
		t.Fatalf("fee over the cap = %v, %v, want USD 0.30", out.Fee, err)
	}
}

// The operators' arithmetic, for a host: the currency is kept, a
// currency-less zero goes with any currency, two currencies are an
// ErrCurrency, and past int64 is an error rather than a wrap.
func TestMoneyArithmeticKeepsTheCurrency(t *testing.T) {
	t.Parallel()
	usd := func(minor int64) lang.Money { return amount("USD", minor) }
	checkMoney := func(name string, got lang.Money, err error, want lang.Money) {
		t.Helper()
		if err != nil || got != want {
			t.Errorf("%s = %+v, %v, want %+v", name, got, err, want)
		}
	}
	sum, err := usd(170).Add(usd(30))
	checkMoney("USD 1.70 + USD 0.30", sum, err, usd(200))
	difference, err := usd(170).Sub(usd(200))
	checkMoney("USD 1.70 - USD 2.00", difference, err, usd(-30))
	zero, err := lang.Money{}.Add(usd(5))
	checkMoney("0 + USD 0.05", zero, err, usd(5))
	negated, err := usd(5).Neg()
	checkMoney("-(USD 0.05)", negated, err, usd(-5))
	absolute, err := usd(-5).Abs()
	checkMoney("abs(USD -0.05)", absolute, err, usd(5))
	times, err := usd(333).MulInt(3)
	checkMoney("USD 3.33 × 3", times, err, usd(999))
	fee, err := usd(1000).MulRate(rateUnits(290_000_000), lang.RoundHalfUp)
	checkMoney("USD 10.00 × 2.9%", fee, err, usd(29))
	gross, err := usd(971).DivRate(rateUnits(9_710_000_000), lang.RoundHalfUp)
	checkMoney("USD 9.71 ÷ 97.1%", gross, err, usd(1000))
	if ratio, err := usd(29).Ratio(usd(1000)); err != nil || ratio != rateUnits(290_000_000) {
		t.Errorf("USD 0.29 / USD 10.00 = %v, %v, want 0.029", ratio, err)
	}
	if order, err := usd(1).Cmp(usd(2)); err != nil || order != -1 {
		t.Errorf("Cmp(USD 0.01, USD 0.02) = %d, %v, want -1", order, err)
	}
	if order, err := (lang.Money{}).Cmp(usd(0)); err != nil || order != 0 {
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
		"rate past":  {func() error { _, err := largest.MulRate(rateUnits(2*rateScale), lang.RoundHalfUp); return err }, false},
		"by zero":    {func() error { _, err := usd.DivRate(rateUnits(0), lang.RoundHalfUp); return err }, false},
		"ratio zero": {func() error { _, err := usd.Ratio(amount("USD", 0)); return err }, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := test.run()
			if err == nil || errors.Is(err, lang.ErrCurrency) != test.currency {
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
func checkShares(t *testing.T, shares []lang.Money, want []int64) {
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
	Amount lang.Money            `funroute:"amount"`
	Fee    lang.Rate             `funroute:"fee"`
	Fx     lang.FxRate           `funroute:"fx"`
	Payout lang.Currency         `funroute:"payout"`
	Lines  []lang.Money          `funroute:"lines"`
	Caps   map[string]lang.Money `funroute:"caps"`
}

type Settlement struct {
	Fee     lang.Money    `funroute:"fee"`
	Settled lang.Money    `funroute:"settled"`
	Total   lang.Money    `funroute:"total"`
	Cap     lang.Money    `funroute:"cap"`
	Payout  lang.Currency `funroute:"payout"`
	Double  lang.Rate     `funroute:"double"`
	Quoted  lang.Money    `funroute:"quoted"`
	Fx      lang.FxRate   `funroute:"fx"`
	Lines   []lang.Money  `funroute:"lines"`
}

const settlementSource = `{fee: amount * fee, settled: amount -> JPY, total: sum(lines), cap: caps["card"], payout: payout,
double: fee + fee, quoted: using(fx, amount -> JPY), fx: fx, lines: [line * 2 for line in lines]}`

// fullConsole declares money before the standard pack, so the pack's money
// overloads (sum over money among them) are there.
func fullConsole(t *testing.T) *lang.Registry {
	t.Helper()
	registry := moneyConsole(t)
	if err := registry.EnableForm(lang.SwitchForm, lang.ForForm, lang.ReduceForm); err != nil {
		t.Fatal(err)
	}
	if err := std.Register(registry); err != nil {
		t.Fatal(err)
	}
	return registry
}

func ledger() Ledger {
	return Ledger{
		Amount: amount("USD", 1000), Fee: rateUnits(290_000_000),
		Fx: agreedRate(), Payout: currency("EUR"),
		Lines: []lang.Money{amount("USD", 1), amount("USD", 2)},
		Caps:  map[string]lang.Money{"card": amount("EUR", 5)},
	}
}

// Bind reads Money, Rate and Currency fields as their kinds —
// containers of money too — and a program runs on them in both directions.
// Reflection cannot see a currency, so the contract is money, not money<c>.
func TestBindingCarriesEveryMoneyType(t *testing.T) {
	t.Parallel()
	registry := fullConsole(t)
	binding, err := lang.Bind[Ledger, Settlement](registry)
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, arg := range binding.Options().Args {
		types = append(types, arg.Name+":"+arg.Type.String())
	}
	want := []string{"amount:money<?>", "fee:rate", "fx:fxrate<?,?>", "payout:currency<?>", "lines:array<money<?>>", "caps:dict<money<?>>"}
	if !slices.Equal(types, want) {
		t.Fatalf("Bind[Ledger] arguments = %v, want %v", types, want)
	}
	program, err := binding.Compile(settlementSource)
	if err != nil {
		t.Fatal(err)
	}
	in := ledger()
	out, err := program.Run(t.Context(), &in, lang.RunOptions{Rates: yenRates(t, registry)})
	if err != nil {
		t.Fatal(err)
	}
	wantOut := Settlement{
		Fee: amount("USD", 29), Settled: amount("JPY", 1500),
		Total: amount("USD", 3), Cap: amount("EUR", 5), Payout: currency("EUR"),
		Double: rateUnits(580_000_000), Quoted: amount("JPY", 1600), Fx: in.Fx, Lines: []lang.Money{amount("USD", 2), amount("USD", 4)},
	}
	if !equalSettlements(out, wantOut) {
		t.Errorf("Run(ledger) = %+v, want %+v", out, wantOut)
	}
}

// agreedRate is USD/JPY 160, the rate a Ledger carries into a rule.
func agreedRate() lang.FxRate {
	fx, err := testTable.FxRate("USD", "JPY", "160")
	if err != nil {
		panic(err)
	}
	return fx
}

// sameRate compares two rates, the zero one included.
func sameRate(a, b lang.FxRate) bool {
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
	binding, err := lang.Bind[Ledger, Settlement](registry)
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
		"undeclared amount":       {func(in *Ledger) { in.Amount = looseMoney("XXX", 1000) }, true},
		"undeclared line":         {func(in *Ledger) { in.Lines[1] = looseMoney("XXX", 2) }, true},
		"undeclared cap":          {func(in *Ledger) { in.Caps["card"] = looseMoney("XXX", 1) }, true},
		"two currencies in lines": {func(in *Ledger) { in.Lines[1] = amount("JPY", 2) }, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := ledger()
			test.edit(&in)
			out, err := program.Run(t.Context(), &in, lang.RunOptions{Rates: yenRates(t, registry)})
			if !errors.Is(err, lang.ErrCurrency) || errors.Is(err, lang.ErrContract) != test.contract {
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
func everyKind(t *testing.T) (*lang.Registry, *lang.Runtime) {
	t.Helper()
	registry := moneyConsole(t)
	artifact, err := lang.CompileExpr(`{fee: amount * fee, made: money(100, payout)}`, registry, lang.CompileOptions{Args: []lang.ArgSpec{
		{Name: "amount", Type: lang.MoneyOf("")}, {Name: "fee", Type: lang.RateType}, {Name: "payout", Type: lang.CurrencyOf("")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := lang.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	return registry, runtime
}

// Run takes money as "USD 1.70" or as {"currency", "minor"}, a rate as a
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
			args, err := lang.DecodeArgs([]byte(test.args))
			if err != nil {
				t.Fatal(err)
			}
			value, err := runtime.Run(t.Context(), args, lang.RunOptions{})
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
		"rate past ten places":  {"fee", `"0.00000000001"`, false},
		"rate not a decimal":    {"fee", `"abc"`, false},
		"rate as an object":     {"fee", `{}`, false},
		"undeclared payout":     {"payout", `"XXX"`, true},
		"payout as a number":    {"payout", `840`, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := runWith(t, runtime, valid, test.key, test.value)
			if !errors.Is(err, lang.ErrContract) || errors.Is(err, lang.ErrCurrency) != test.currency {
				t.Errorf("Run with %s = %s: error = %v, want ErrContract, ErrCurrency: %v", test.key, test.value, err, test.currency)
			}
		})
	}
}

// runWith runs with the valid arguments, key replaced by value, and returns
// the error.
func runWith(t *testing.T, runtime *lang.Runtime, valid map[string]string, key, value string) error {
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
	args, err := lang.DecodeArgs([]byte(document + "}"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Run(t.Context(), args, lang.RunOptions{})
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
		if err := runWith(t, runtime, valid, "amount", amount); !errors.Is(err, lang.ErrCurrency) || !errors.Is(err, lang.ErrContract) {
			t.Errorf("Run with amount %s error = %v, want ErrCurrency and ErrContract", amount, err)
		}
	}
}

// ErrCurrency tells a rule meeting two currencies apart from a contract
// broken at the door; a currency variable bound two ways is both.
func TestCurrencyErrorsAreTyped(t *testing.T) {
	t.Parallel()
	registry := moneyConsole(t)
	for name, test := range map[string]struct {
		unit     string
		contract bool
	}{"two unknown currencies meet": {"", false}, "one variable bound twice": {"c", true}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			artifact, err := lang.CompileExpr(`a + b`, registry, lang.CompileOptions{Args: []lang.ArgSpec{
				{Name: "a", Type: lang.MoneyOf(test.unit)}, {Name: "b", Type: lang.MoneyOf(test.unit)},
			}})
			if err != nil {
				t.Fatal(err)
			}
			runtime, err := lang.Instantiate(artifact, registry)
			if err != nil {
				t.Fatal(err)
			}
			args := []lang.Value{valueOf(amount("USD", 1)), valueOf(amount("EUR", 1))}
			_, err = runtime.RunValues(t.Context(), args, lang.RunOptions{})
			if !errors.Is(err, lang.ErrCurrency) || errors.Is(err, lang.ErrContract) != test.contract || errors.Is(err, lang.ErrExtension) {
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
	artifact, err := lang.CompileExpr(`fallback(a + b, a)`, registry, lang.CompileOptions{Args: []lang.ArgSpec{
		{Name: "a", Type: lang.MoneyOf("")}, {Name: "b", Type: lang.MoneyOf("")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := lang.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(t.Context(), map[string]any{"a": "USD 1", "b": "EUR 1"}, lang.RunOptions{})
	if !errors.Is(err, lang.ErrCurrency) {
		t.Errorf("fallback(USD 1 + EUR 1, USD 1) = %v, %v, want ErrCurrency", value.Any(), err)
	}
}

// A manifest carries the money feature: a registry that applies it declares
// the same currencies and rounding, so money programs compile there to the
// host's artifact; a registry that declared other money refuses it.
func TestManifestCarriesTheMoneyFeature(t *testing.T) {
	t.Parallel()
	host := fullConsole(t)
	if err := lang.Logic(host, "ledger.fee_v1", lang.Doc{Cost: 5}, func(amount lang.Money) (lang.Money, error) { return amount, nil }); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(host.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	var manifest lang.Manifest
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		t.Fatal(err)
	}
	declared, declares := manifest.Money()
	if spec, _ := host.Money(); !declares || !slices.Equal(declared.Currencies, spec.Currencies) || declared.Rounding != spec.Rounding {
		t.Fatalf("manifest money = %+v, %v, want the host's %+v", declared, declares, spec)
	}
	// A tool declares the manifest's money before the standard pack, as the
	// CLI's language server does, then applies the rest.
	browser := lang.CoreRegistry()
	if err := browser.DeclareMoney(declared); err != nil {
		t.Fatal(err)
	}
	if err := browser.EnableForm(lang.SwitchForm, lang.ForForm, lang.ReduceForm); err != nil {
		t.Fatal(err)
	}
	if err := std.Register(browser); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Apply(browser); err != nil {
		t.Fatalf("Apply(manifest with money) error = %v", err)
	}
	checkSameArtifact(t, host, browser)
	bare := lang.CoreRegistry()
	if err := moneyOnlyManifest(t, declared).Apply(bare); err != nil {
		t.Fatalf("Apply(money alone) to a bare registry error = %v", err)
	}
	if _, declared := bare.Currencies(); !declared {
		t.Error("applying a manifest's money to a bare registry declared none")
	}
	other := lang.CoreRegistry()
	if err := other.DeclareMoney(lang.MoneySpec{Rounding: lang.RoundHalfEven, Currencies: declared.Currencies}); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Apply(other); err == nil {
		t.Error("Apply(manifest) to a registry rounding half even error = nil, want a refusal")
	}
}

// checkSameArtifact compiles a money program on both registries and wants one
// digest, and the host's artifact to load on the other.
// moneyOnlyManifest is a manifest that declares money and nothing else, as a
// tool would write one by hand in JSON.
func moneyOnlyManifest(t *testing.T, spec lang.MoneySpec) lang.Manifest {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{"version": lang.ManifestVersion, "forms": []string{}, "functions": []any{}, "money": spec})
	if err != nil {
		t.Fatal(err)
	}
	var manifest lang.Manifest
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func checkSameArtifact(t *testing.T, host, browser *lang.Registry) {
	t.Helper()
	source := `ledger.fee_v1(sum([line * 2.9% for line in lines])) + USD 0.30`
	options := lang.CompileOptions{Args: []lang.ArgSpec{{Name: "lines", Type: lang.ArrayOf(lang.MoneyOf("USD"))}}}
	atHost, err := lang.CompileExpr(source, host, options)
	if err != nil {
		t.Fatal(err)
	}
	inBrowser, err := lang.CompileExpr(source, browser, options)
	if err != nil {
		t.Fatalf("CompileExpr(%q) against the manifest error = %v", source, err)
	}
	if inBrowser.Digest() != atHost.Digest() {
		t.Errorf("against the manifest %q compiles to digest %s, want the host's %s", source, inBrowser.Digest(), atHost.Digest())
	}
	if _, err := lang.Instantiate(atHost, browser); err != nil {
		t.Errorf("Instantiate(host artifact) against the manifest error = %v", err)
	}
}

// A money value's JSON is the shape it had when its fields were public, and
// reads back to the same value.
func TestMoneyValuesKeepTheirJSONShape(t *testing.T) {
	t.Parallel()
	card, _ := lang.ParseRate("0.029")
	for want, value := range map[string]any{
		`{"currency":"USD","minor":170}`: amount("USD", 170),
		`"USD"`:                          currency("USD"),
		`"0.029"`:                        card,
		`[{"currency":"","minor":0},{"currency":"JPY","minor":5}]`: []lang.Money{{}, amount("JPY", 5)},
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
	contract := lang.CompileOptions{Args: []lang.ArgSpec{
		{Name: "fx", Type: lang.FxRateOf("USD", "JPY")}, {Name: "amount", Type: lang.MoneyOf("USD")},
	}}
	artifact, err := lang.CompileExpr("using(fx, amount -> JPY)", registry, contract)
	if err != nil {
		t.Fatal(err)
	}
	runtime, _ := lang.Instantiate(artifact, registry)
	value, err := runtime.Run(t.Context(), map[string]any{"fx": agreed, "amount": amount("USD", 200)}, lang.RunOptions{})
	if yen, _ := value.Money(); err != nil || yen != amount("JPY", 301) {
		t.Fatalf("using(fx, USD 2.00 -> JPY) at %s = %v, %v, want JPY 301 (300.5, half up)", agreed, value.Any(), err)
	}
	implied, err := lang.CompileExpr("settled / paid", registry, lang.CompileOptions{Args: []lang.ArgSpec{
		{Name: "settled", Type: lang.MoneyOf("JPY")}, {Name: "paid", Type: lang.MoneyOf("USD")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, _ = lang.Instantiate(implied, registry)
	value, err = runtime.Run(t.Context(), map[string]any{"settled": "JPY 30050", "paid": "USD 200.00"}, lang.RunOptions{})
	back, converted := lang.FromValue[lang.FxRate](value)
	if order, _ := back.Cmp(agreed); err != nil || converted != nil || order != 0 {
		t.Fatalf("JPY 30050 / USD 200.00 = %v (%v, %v), want %s", back, err, converted, agreed)
	}
}
