package api

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/nethinwei/funroute"
)

// RouteIn is a request as a host holds it: the tagged fields are the
// arguments, in this order, and Note is not part of the contract.
type RouteIn struct {
	Country string    `funroute:"country"`
	Amount  int64     `funroute:"amount"`
	Order   Order     `funroute:"order"`
	Scores  []float64 `funroute:"scores"`
	Lines   []Line    `funroute:"lines"`
	Note    string
}

type RouteOut struct {
	Channel string   `funroute:"channel"`
	Net     int32    `funroute:"net"`
	Score   float64  `funroute:"score"`
	SKUs    []string `funroute:"skus"`
}

const routeSource = `{channel: if(country == "SG", "adyen", order.currency_code), net: order.amount - amount, score: scores[1], skus: [l.sku for l in lines]}`

func routeBinding(t *testing.T) *funroute.Binding[RouteIn, RouteOut] {
	t.Helper()
	registry := funroute.CoreRegistry()
	if err := registry.EnableForm(funroute.ForForm); err != nil {
		t.Fatal(err)
	}
	binding, err := funroute.Bind[RouteIn, RouteOut](registry)
	if err != nil {
		t.Fatal(err)
	}
	return binding
}

// The contract is the two Go types, and a program runs on them directly.
func TestBindingRunsOnHostTypes(t *testing.T) {
	t.Parallel()
	binding := routeBinding(t)
	args := binding.Options().Args
	names := make([]string, 0, len(args))
	for _, arg := range args {
		names = append(names, arg.Name)
	}
	if !slices.Equal(names, []string{"country", "amount", "order", "scores", "lines"}) {
		t.Fatalf("arguments = %v, want [country amount order scores lines]", names)
	}
	program, err := binding.Compile(routeSource)
	if err != nil {
		t.Fatal(err)
	}
	in := RouteIn{
		Country: "HK", Amount: 30,
		Order:  Order{Amount: 1000, CurrencyCode: "HKD"},
		Scores: []float64{0.1, 0.7},
		Lines:  []Line{{SKU: "a", Amount: 1}, {SKU: "b", Amount: 2}},
	}
	out, err := program.Run(t.Context(), &in, funroute.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := RouteOut{Channel: "HKD", Net: 970, Score: 0.7, SKUs: []string{"a", "b"}}
	if out.Channel != want.Channel || out.Net != want.Net || out.Score != want.Score || !slices.Equal(out.SKUs, want.SKUs) {
		t.Fatalf("out = %+v, want %+v", out, want)
	}
}

// A binding compiles against its whole contract: a program with another
// result does not compile.
func TestBindingCompilesOnlyItsContract(t *testing.T) {
	t.Parallel()
	if _, err := routeBinding(t).Compile(`amount`); err == nil {
		t.Fatal("a program returning int compiled against a record result")
	}
}

// consoleArtifact compiles the way a console does: the contract declares only
// what the rule reads, in its own order.
func consoleArtifact(t *testing.T, source string, args []funroute.ArgSpec, result funroute.Type) *funroute.Artifact {
	t.Helper()
	registry := funroute.CoreRegistry()
	if err := registry.EnableForm(funroute.ForForm); err != nil {
		t.Fatal(err)
	}
	artifact, err := funroute.CompileExpr(source, registry, funroute.CompileOptions{Args: args, Result: &result})
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

// An artifact loads by name: fewer arguments, another order, a record that
// declares fewer fields, a result that fills only part of Out.
func TestBindingLoadsWhatAnArtifactDeclares(t *testing.T) {
	t.Parallel()
	artifact := consoleArtifact(t, `{net: order.amount - amount}`, []funroute.ArgSpec{
		{Name: "amount", Type: funroute.IntType},
		{Name: "order", Type: funroute.RecordOf(funroute.FieldOf("amount", funroute.IntType))},
	}, funroute.RecordOf(funroute.FieldOf("net", funroute.IntType)))
	program, err := routeBinding(t).Load(artifact)
	if err != nil {
		t.Fatal(err)
	}
	in := RouteIn{Amount: 30, Order: Order{Amount: 1000, CurrencyCode: "SGD"}}
	out, err := program.Run(t.Context(), &in, funroute.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Net != 970 || out.Channel != "" || out.Score != 0 || out.SKUs != nil {
		t.Fatalf("out = %+v, want only Net 970", out)
	}
	if program.Artifact().Digest() != artifact.Digest() {
		t.Fatalf("program.Artifact().Digest() = %s, want the loaded %s", program.Artifact().Digest(), artifact.Digest())
	}
}

// What an artifact declares must be there with that type, by name.
func TestBindingRefusesWhatItCannotCarry(t *testing.T) {
	t.Parallel()
	net := funroute.RecordOf(funroute.FieldOf("net", funroute.IntType))
	for name, artifact := range map[string]*funroute.Artifact{
		"retyped argument": consoleArtifact(t, `{net: 1}`, []funroute.ArgSpec{{Name: "amount", Type: funroute.FloatType}}, net),
		"unknown argument": consoleArtifact(t, `{net: fee}`, []funroute.ArgSpec{{Name: "fee", Type: funroute.IntType}}, net),
		"unknown field": consoleArtifact(t, `{net: order.discount}`, []funroute.ArgSpec{{Name: "order", Type: funroute.RecordOf(
			funroute.FieldOf("discount", funroute.IntType))}}, net),
		"unknown result field": consoleArtifact(t, `{bonus: 1}`, nil, funroute.RecordOf(funroute.FieldOf("bonus", funroute.IntType))),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := routeBinding(t).Load(artifact); !errors.Is(err, funroute.ErrContract) {
				t.Errorf("Load(the %q artifact) error = %v, want ErrContract", name, err)
			}
		})
	}
}

type ChannelIn struct {
	Channel string `funroute:"channel"`
}

// A Go string carries an enum the artifact declares, and only its members.
func TestBindingCarriesEnumsAsStrings(t *testing.T) {
	t.Parallel()
	channel := funroute.EnumOf("channel", "adyen", "stripe")
	artifact := consoleArtifact(t, `if(channel == @adyen, @stripe, @adyen)`,
		[]funroute.ArgSpec{{Name: "channel", Type: channel}}, channel)
	binding, err := funroute.Bind[ChannelIn, string](funroute.CoreRegistry())
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Load(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := program.Run(t.Context(), &ChannelIn{Channel: "adyen"}, funroute.RunOptions{}); err != nil || out != "stripe" {
		t.Fatalf("Run(channel=adyen) = %q, %v; want \"stripe\"", out, err)
	}
	if out, err := program.Run(t.Context(), &ChannelIn{Channel: "paypal"}, funroute.RunOptions{}); !errors.Is(err, funroute.ErrContract) || out != "" {
		t.Fatalf("Run(channel=paypal) = %q, %v; want \"\" and ErrContract: a non-member entered", out, err)
	}
}

type BasketIn struct {
	Basket Basket `funroute:"basket"`
}

// Records nest through a binding in both directions: a struct inside a
// struct, and a slice of structs, in and back out.
func TestBindingCarriesNestedRecords(t *testing.T) {
	t.Parallel()
	binding, err := funroute.Bind[BasketIn, Basket](funroute.CoreRegistry())
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(`basket`)
	if err != nil {
		t.Fatal(err)
	}
	in := BasketIn{Basket: Basket{
		Customer: Order{Amount: 1200, CurrencyCode: "SGD", Tags: []string{"vip"}},
		Lines:    []Line{{SKU: "a", Amount: 10}, {SKU: "b", Amount: 20}},
	}}
	out, err := program.Run(t.Context(), &in, funroute.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Customer.CurrencyCode != "SGD" || out.Customer.Amount != 1200 || !slices.Equal(out.Customer.Tags, []string{"vip"}) ||
		!slices.Equal(out.Lines, in.Basket.Lines) {
		t.Fatalf("out = %+v, want %+v", out, in.Basket)
	}
}

type OrderIn struct {
	Order Order `funroute:"order"`
}

// The update's type is the record's, so a rule can take an Order and give an
// Order back — and the fields it did not touch are the host's own backings.
func TestBindingUpdatesARecordItWasGiven(t *testing.T) {
	t.Parallel()
	binding, err := funroute.Bind[OrderIn, Order](funroute.CoreRegistry())
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(`order with {amount: order.amount - 30}`)
	if err != nil {
		t.Fatal(err)
	}
	in := OrderIn{Order: Order{Amount: 1200, CurrencyCode: "SGD", Tags: []string{"vip"}}}
	out, err := program.Run(t.Context(), &in, funroute.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Amount != 1170 || out.CurrencyCode != "SGD" || &out.Tags[0] != &in.Order.Tags[0] {
		t.Fatalf("out = %+v, want Amount 1170, CurrencyCode SGD and the input's own Tags", out)
	}
}

// A batch reads each request where the host keeps it and writes each result
// where the host wants it: here, a slice in and each request's own response.
func TestBindingRunsBatches(t *testing.T) {
	t.Parallel()
	program, err := routeBinding(t).Compile(`{channel: country, net: amount, score: 0.5, skus: [country]}`)
	if err != nil {
		t.Fatal(err)
	}
	requests := []RouteIn{{Country: "SG", Amount: 1}, {Country: "HK", Amount: 2}}
	decisions := []*RouteOut{{}, {}}
	program.RunBatch(t.Context(), len(requests), func(i int) *RouteIn { return &requests[i] },
		func(i int) *RouteOut { return decisions[i] }, funroute.RunOptions{}, func(i int, err error) {
			t.Errorf("request %d failed: %v", i, err)
		})
	if decisions[0].Channel != "SG" || decisions[1].Channel != "HK" || decisions[0].Net != 1 || decisions[1].Net != 2 {
		t.Fatalf("decisions = %+v %+v, want SG with net 1 and HK with net 2", *decisions[0], *decisions[1])
	}
	batch := program.Batch(funroute.BatchOptions{MaxSize: 1})
	defer batch.Close()
	if out, err := batch.Run(t.Context(), &requests[1]); err != nil || out.Channel != "HK" {
		t.Fatalf("batch.Run(requests[1]) = %+v, %v; want channel HK", out, err)
	}
}

// A binding compiles the document a front end edits as it compiles text, and
// refuses one of another contract or one that is not a document at all.
func TestBindingCompilesADocument(t *testing.T) {
	t.Parallel()
	binding := routeBinding(t)
	document, err := funroute.ParseToJSON(routeSource)
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.CompileJSON(document)
	if err != nil {
		t.Fatalf("CompileJSON(%s) error = %v", document, err)
	}
	in := RouteIn{Country: "SG", Amount: 30, Order: Order{Amount: 1000, CurrencyCode: "SGD"}, Scores: []float64{0.1, 0.7}}
	out, err := program.Run(t.Context(), &in, funroute.RunOptions{})
	if err != nil || out.Channel != "adyen" || out.Net != 970 || out.Score != 0.7 || len(out.SKUs) != 0 {
		t.Fatalf("Run = %+v, %v, want channel adyen, net 970, score 0.7, no skus", out, err)
	}
	other, err := funroute.ParseToJSON(`amount`)
	if err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		document []byte
		want     string
	}{
		"another result": {other, "the contract returns record"},
		"not a document": {[]byte(`{`), "decode expression JSON"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := binding.CompileJSON(test.document); !errors.Is(err, funroute.ErrCompile) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("CompileJSON(%s) error = %v, want ErrCompile containing %q", test.document, err, test.want)
			}
		})
	}
}

// A program hands out the runtime it runs on, for a host that also runs it
// on values it did not bind.
func TestProgramHandsOutItsRuntime(t *testing.T) {
	t.Parallel()
	binding, err := funroute.Bind[OrderIn, Order](funroute.CoreRegistry())
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(`order with {amount: order.amount - 30}`)
	if err != nil {
		t.Fatal(err)
	}
	runtime := program.Runtime()
	if got, want := runtime.ResultType().String(), binding.Options().Result.String(); got != want {
		t.Fatalf("Runtime().ResultType() = %s, want %s", got, want)
	}
	value, err := runtime.Run(t.Context(), map[string]any{"order": Order{Amount: 1200, CurrencyCode: "SGD", Tags: []string{"vip"}}}, funroute.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(value)
	if want := `{"amount":1170,"currency_code":"SGD","tags":["vip"]}`; err != nil || string(encoded) != want {
		t.Fatalf("Runtime().Run = %s, %v, want %s", encoded, err, want)
	}
}

// A dictionary of exchange rates crosses a binding both ways intact: no
// backing holds exchange rates by key, so each one is carried as a value.
func TestBindingCarriesADictionaryOfRates(t *testing.T) {
	t.Parallel()
	registry := funroute.CoreRegistry()
	if err := registry.DeclareMoney(funroute.MoneySpec{Currencies: []funroute.CurrencySpec{{Code: "USD", Digits: 2}, {Code: "JPY", Digits: 0}}}); err != nil {
		t.Fatal(err)
	}
	type in struct {
		Rates map[string]funroute.FxRate `funroute:"rates"`
	}
	binding, err := funroute.Bind[in, map[string]funroute.FxRate](registry)
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(`rates`)
	if err != nil {
		t.Fatal(err)
	}
	table, _ := registry.Currencies()
	rate, err := table.FxRate("USD", "JPY", "150.5")
	if err != nil {
		t.Fatal(err)
	}
	got, err := program.Run(t.Context(), &in{Rates: map[string]funroute.FxRate{"card": rate}}, funroute.RunOptions{})
	if err != nil || len(got) != 1 {
		t.Fatalf("rates round trip = %v, %v, want one rate", got, err)
	}
	if order, err := got["card"].Cmp(rate); err != nil || order != 0 || got["card"].Base() != "USD" {
		t.Fatalf("rates round trip gave card %v (%v), want %v", got["card"], err, rate)
	}
}

// FeesIn and FeesOut are a host's request and result for a rule that
// answers an array.
type FeesIn struct {
	Fees []int64 `funroute:"fees"`
}

type FeesOut struct {
	Doubled []int64 `funroute:"doubled"`
}

// A host that keeps its result between requests hands it to RunInto, which
// builds the next answer in the memory the result already has.
func TestProgramRunsIntoTheHostsResult(t *testing.T) {
	t.Parallel()
	registry := funroute.CoreRegistry()
	if err := registry.EnableForm(funroute.ForForm); err != nil {
		t.Fatal(err)
	}
	binding, err := funroute.Bind[FeesIn, FeesOut](registry)
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(`{doubled: [fee * 2 for fee in fees]}`)
	if err != nil {
		t.Fatal(err)
	}
	out := FeesOut{Doubled: make([]int64, 0, 8)}
	for _, fees := range [][]int64{{1, 2}, {3, 4, 5}} {
		if err := program.RunInto(t.Context(), &FeesIn{Fees: fees}, &out, funroute.RunOptions{}); err != nil {
			t.Fatal(err)
		}
		if len(out.Doubled) != len(fees) || out.Doubled[0] != fees[0]*2 || cap(out.Doubled) != 8 {
			t.Fatalf("RunInto(%v) = %v with room for %d, want the doubles in the same 8", fees, out.Doubled, cap(out.Doubled))
		}
	}
}
