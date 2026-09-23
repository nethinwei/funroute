package lang_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"funroute/lang"
)

// A rule is compiled against the contract the host declares, bound to the
// registry it may call, and then run: by name, the way a form or a JSON
// request arrives, or in ABI order on the hot path.
func ExampleCompileExpr() {
	registry := lang.CoreRegistry()
	artifact, err := lang.CompileExpr(`if(vip, amount * 15 / 10000, amount * 25 / 10000)`, registry, lang.CompileOptions{
		Args: []lang.ArgSpec{
			{Name: "vip", Type: lang.BoolType, Doc: "是否大客户"},
			{Name: "amount", Type: lang.IntType, Doc: "订单金额，单位：分"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	runtime, err := lang.Instantiate(artifact, registry)
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	options := lang.RunOptions{Fuel: lang.DefaultFuel}

	fee, err := runtime.Run(ctx, map[string]any{"vip": false, "amount": 120000}, options)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("by name:", fee.Any())

	fee, err = runtime.RunValues(ctx, []lang.Value{lang.Bool(true), lang.Int(120000)}, options)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("in order:", fee.Any())
	// Output:
	// by name: 300
	// in order: 180
}

// A domain function is registered by its Go signature; the Doc says only what
// the signature cannot, and a rule calls the function by its name.
func ExampleLogic() {
	registry := lang.CoreRegistry()
	err := lang.Logic(registry, "risk.score_v1", lang.Doc{
		Label:  "风险评分",
		Cost:   25,
		Params: []string{"国家", "金额"},
	}, func(country string, amount int64) (float64, error) {
		if country == "SG" && amount < 1_000_000 {
			return 0.2, nil
		}
		return 0.9, nil
	})
	if err != nil {
		log.Fatal(err)
	}
	artifact, err := lang.CompileExpr(`if(risk.score_v1(country, amount) > 0.5, "review", "accept")`, registry, lang.CompileOptions{
		Args: []lang.ArgSpec{
			{Name: "country", Type: lang.StringType},
			{Name: "amount", Type: lang.IntType},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	runtime, err := lang.Instantiate(artifact, registry)
	if err != nil {
		log.Fatal(err)
	}
	for _, country := range []string{"SG", "BR"} {
		decision, err := runtime.Run(context.Background(), map[string]any{"country": country, "amount": 5000}, lang.RunOptions{})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(country, decision.Any())
	}
	// Output:
	// SG accept
	// BR review
}

// The contract is two Go types: the tagged fields of the request are the
// arguments, in their order, and the result is written straight into the
// response. RunBatch runs the requests a host already holds, and reports a
// failure by the index of the request that failed.
func ExampleBind() {
	type Request struct {
		Country string `funroute:"country"`
		Amount  int64  `funroute:"amount"`
	}
	type Route struct {
		Channel string `funroute:"channel"`
		Fee     int64  `funroute:"fee"`
	}
	binding, err := lang.Bind[Request, Route](lang.CoreRegistry())
	if err != nil {
		log.Fatal(err)
	}
	program, err := binding.Compile(`{channel: if(country == "SG", "adyen", "stripe"), fee: amount * 25 / 10000}`)
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()

	route, err := program.Run(ctx, &Request{Country: "SG", Amount: 120000}, lang.RunOptions{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%+v\n", route)

	requests := []Request{{Country: "SG", Amount: 40000}, {Country: "BR", Amount: 80000}}
	routes := program.RunBatch(ctx, requests, lang.RunOptions{}, func(i int, err error) {
		log.Printf("request %d: %v", i, err)
	})
	for i, route := range routes {
		fmt.Printf("%d: %+v\n", i, route)
	}
	// Output:
	// {Channel:adyen Fee:300}
	// 0: {Channel:adyen Fee:100}
	// 1: {Channel:stripe Fee:200}
}

// A console compiles a rule against only what it reads. A binding loads that
// artifact by name: the request's other fields are ignored, and the response
// fields the rule does not write stay zero.
func ExampleBinding_Load() {
	type Request struct {
		Country string `funroute:"country"`
		Amount  int64  `funroute:"amount"`
	}
	type Route struct {
		Channel string `funroute:"channel"`
		Fee     int64  `funroute:"fee"`
	}
	registry := lang.CoreRegistry()
	stored, err := lang.CompileExpr(`{fee: amount * 25 / 10000}`, registry, lang.CompileOptions{
		Args: []lang.ArgSpec{{Name: "amount", Type: lang.IntType}},
	})
	if err != nil {
		log.Fatal(err)
	}
	binding, err := lang.Bind[Request, Route](registry)
	if err != nil {
		log.Fatal(err)
	}
	program, err := binding.Load(stored)
	if err != nil {
		log.Fatal(err)
	}
	route, err := program.Run(context.Background(), &Request{Country: "SG", Amount: 120000}, lang.RunOptions{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%+v\n", route)
	// Output:
	// {Channel: Fee:300}
}

// A record update gives back the record it was given with some fields
// replaced. The contract is written as text, the way a console stores it, and
// the complex type is declared once by name.
func ExampleCompileExpr_recordUpdate() {
	contract := lang.TextContract{
		Types:  map[string]string{"Order": "record{amount: int, currency: string}"},
		Args:   []lang.TextArg{{Name: "order", Type: "Order", Doc: "订单"}},
		Result: &lang.TextResult{Type: "Order", Doc: "扣掉优惠后的订单"},
	}
	options, err := contract.Options()
	if err != nil {
		log.Fatal(err)
	}
	registry := lang.CoreRegistry()
	artifact, err := lang.CompileExpr(`{...order, amount: order.amount - 30}`, registry, options)
	if err != nil {
		log.Fatal(err)
	}
	runtime, err := lang.Instantiate(artifact, registry)
	if err != nil {
		log.Fatal(err)
	}
	order := map[string]any{"amount": 1200, "currency": "SGD"}
	discounted, err := runtime.Run(context.Background(), map[string]any{"order": order}, lang.RunOptions{})
	if err != nil {
		log.Fatal(err)
	}
	// A record marshals in its type's field order.
	encoded, err := json.Marshal(discounted)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(encoded))
	// Output:
	// {"amount":1170,"currency":"SGD"}
}

// Format lays a program out the way the language prints it; the result parses
// back to the same program.
func ExampleFormat() {
	formatted, err := lang.Format(`let(rate=250,cap=5000,min(amount*rate/10000,cap))`)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(formatted)
	// Output:
	// let(rate = 250, cap = 5000, min(amount * rate / 10000, cap))
}
