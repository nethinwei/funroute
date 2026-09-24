package lang_test

import (
	"errors"
	"slices"
	"testing"

	"funroute/lang"
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

func routeBinding(t *testing.T) *lang.Binding[RouteIn, RouteOut] {
	t.Helper()
	registry := lang.CoreRegistry()
	if err := registry.EnableForm(lang.ForForm); err != nil {
		t.Fatal(err)
	}
	binding, err := lang.Bind[RouteIn, RouteOut](registry)
	if err != nil {
		t.Fatal(err)
	}
	return binding
}

// The contract is the two Go types, and a program runs on them directly.
func TestBindingRunsOnHostTypes(t *testing.T) {
	t.Parallel()
	binding := routeBinding(t)
	names := []string{}
	for _, arg := range binding.Options().Args {
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
	out, err := program.Run(t.Context(), &in, lang.RunOptions{})
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
func consoleArtifact(t *testing.T, source string, args []lang.ArgSpec, result lang.Type) *lang.Artifact {
	t.Helper()
	registry := lang.CoreRegistry()
	if err := registry.EnableForm(lang.ForForm); err != nil {
		t.Fatal(err)
	}
	artifact, err := lang.CompileExpr(source, registry, lang.CompileOptions{Args: args, Result: &result})
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

// An artifact loads by name: fewer arguments, another order, a record that
// declares fewer fields, a result that fills only part of Out.
func TestBindingLoadsWhatAnArtifactDeclares(t *testing.T) {
	t.Parallel()
	artifact := consoleArtifact(t, `{net: order.amount - amount}`, []lang.ArgSpec{
		{Name: "amount", Type: lang.IntType},
		{Name: "order", Type: lang.RecordOf(lang.FieldOf("amount", lang.IntType))},
	}, lang.RecordOf(lang.FieldOf("net", lang.IntType)))
	program, err := routeBinding(t).Load(artifact)
	if err != nil {
		t.Fatal(err)
	}
	in := RouteIn{Amount: 30, Order: Order{Amount: 1000, CurrencyCode: "SGD"}}
	out, err := program.Run(t.Context(), &in, lang.RunOptions{})
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
	net := lang.RecordOf(lang.FieldOf("net", lang.IntType))
	for name, artifact := range map[string]*lang.Artifact{
		"retyped argument": consoleArtifact(t, `{net: 1}`, []lang.ArgSpec{{Name: "amount", Type: lang.FloatType}}, net),
		"unknown argument": consoleArtifact(t, `{net: fee}`, []lang.ArgSpec{{Name: "fee", Type: lang.IntType}}, net),
		"unknown field": consoleArtifact(t, `{net: order.discount}`, []lang.ArgSpec{{Name: "order", Type: lang.RecordOf(
			lang.FieldOf("discount", lang.IntType))}}, net),
		"unknown result field": consoleArtifact(t, `{bonus: 1}`, nil, lang.RecordOf(lang.FieldOf("bonus", lang.IntType))),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := routeBinding(t).Load(artifact); !errors.Is(err, lang.ErrContract) {
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
	channel := lang.EnumOf("channel", "adyen", "stripe")
	artifact := consoleArtifact(t, `if(channel == @adyen, @stripe, @adyen)`,
		[]lang.ArgSpec{{Name: "channel", Type: channel}}, channel)
	binding, err := lang.Bind[ChannelIn, string](lang.CoreRegistry())
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Load(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := program.Run(t.Context(), &ChannelIn{Channel: "adyen"}, lang.RunOptions{}); err != nil || out != "stripe" {
		t.Fatalf("Run(channel=adyen) = %q, %v; want \"stripe\"", out, err)
	}
	if out, err := program.Run(t.Context(), &ChannelIn{Channel: "paypal"}, lang.RunOptions{}); !errors.Is(err, lang.ErrContract) || out != "" {
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
	binding, err := lang.Bind[BasketIn, Basket](lang.CoreRegistry())
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
	out, err := program.Run(t.Context(), &in, lang.RunOptions{})
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
	binding, err := lang.Bind[OrderIn, Order](lang.CoreRegistry())
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(`order with {amount: order.amount - 30}`)
	if err != nil {
		t.Fatal(err)
	}
	in := OrderIn{Order: Order{Amount: 1200, CurrencyCode: "SGD", Tags: []string{"vip"}}}
	out, err := program.Run(t.Context(), &in, lang.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Amount != 1170 || out.CurrencyCode != "SGD" || &out.Tags[0] != &in.Order.Tags[0] {
		t.Fatalf("out = %+v, want Amount 1170, CurrencyCode SGD and the input's own Tags", out)
	}
}

// The two batch shapes, as a host names them.
func TestBindingRunsBatches(t *testing.T) {
	t.Parallel()
	program, err := routeBinding(t).Compile(`{channel: country, net: amount, score: 0.5, skus: [country]}`)
	if err != nil {
		t.Fatal(err)
	}
	requests := []RouteIn{{Country: "SG", Amount: 1}, {Country: "HK", Amount: 2}}
	outs := program.RunBatch(t.Context(), requests, lang.RunOptions{}, func(i int, err error) {
		t.Errorf("request %d failed: %v", i, err)
	})
	if outs[0].Channel != "SG" || outs[1].Net != 2 {
		t.Fatalf("outs = %+v, want channel SG first and net 2 second", outs)
	}
	decisions := []*RouteOut{{}, {}}
	program.RunBatchInto(t.Context(), requests, decisions, lang.RunOptions{}, func(i int, err error) {
		t.Errorf("request %d failed: %v", i, err)
	})
	if decisions[0].Channel != "SG" || decisions[1].Channel != "HK" {
		t.Fatalf("decisions = %+v %+v, want channels SG and HK", *decisions[0], *decisions[1])
	}
	nets := make([]int32, len(requests))
	program.RunBatchFunc(t.Context(), len(requests), func(i int) *RouteIn { return &requests[i] },
		func(i int) *RouteOut { return decisions[i] }, lang.RunOptions{}, func(i int, err error) {
			t.Errorf("request %d failed: %v", i, err)
		})
	for i, decision := range decisions {
		nets[i] = decision.Net
	}
	if nets[0] != 1 || nets[1] != 2 {
		t.Fatalf("nets = %v, want [1 2]", nets)
	}
	batch := program.Batch(lang.BatchOptions{MaxSize: 1})
	defer batch.Close()
	if out, err := batch.Run(t.Context(), &requests[1]); err != nil || out.Channel != "HK" {
		t.Fatalf("batch.Run(requests[1]) = %+v, %v; want channel HK", out, err)
	}
}
