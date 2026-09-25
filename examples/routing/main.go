// Command routing is a host that routes payments by a rule: the request and
// the decision are the host's own structs, the rule is text the host keeps
// with its version and approvals, and the risk engine is a function the host
// registers. It shows the whole round: bind the contract to the structs,
// compile and store the artifact, load it back, run it, and tell a rule's
// error from an engine that was not there.
//
//	go run ./examples/routing
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"github.com/nethinwei/funroute"
)

// Request is what the rule reads; each tagged field is an argument, in
// order, and the order is the artifact's ABI.
type Request struct {
	Country string `funroute:"country"`
	Amount  int64  `funroute:"amount"` // minor units
}

// Decision is what the rule returns: a record of these fields, in order.
type Decision struct {
	Channel string `funroute:"channel"`
	Fee     int64  `funroute:"fee"`
}

// rule sends Singapore to one acquirer, and the rest by how risky the engine
// finds them; when the engine is down it takes the conservative route
// rather than failing the payment.
const rule = `let(
  fee = amount * 29 / 1000 + 30,
  risky = fallback(risk.score_v1(country, amount) > 0.8, true),
  {channel: switch(case country == "SG" => "adyen", case risky => "manual_review", else => "stripe"), fee: fee})`

func main() {
	registry := funroute.CoreRegistry()
	if err := registry.EnableForm(funroute.SwitchForm); err != nil {
		log.Fatal(err)
	}
	if err := registry.Register(funroute.FunctionSpec{Name: "risk.score_v1", Doc: funroute.Doc{Cost: 10}, Go: score}); err != nil {
		log.Fatal(err)
	}
	binding, err := funroute.Bind[Request, Decision](registry)
	if err != nil {
		log.Fatal(err)
	}
	compiled, err := binding.Compile(rule)
	if err != nil {
		log.Fatal(err)
	}
	program, err := binding.Load(stored(compiled.Artifact()))
	if err != nil {
		log.Fatal(err)
	}
	for _, request := range []Request{{"SG", 120000}, {"US", 5000}, {"BR", 900000}, {"XX", 100}} {
		decision, err := program.Run(context.Background(), &request, funroute.RunOptions{})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s %7d → %-13s fee %d\n", request.Country, request.Amount, decision.Channel, decision.Fee)
	}
	ruleError(registry)
}

// score is the risk engine: large amounts are riskier, and it knows nothing
// of country XX — an error the rule's fallback takes as data not at hand.
func score(country string, amount int64) (float64, error) {
	if country == "XX" {
		return 0, errors.New("no model for this country")
	}
	return min(float64(amount)/1_000_000, 1), nil
}

// stored is the artifact written as the host stores it and read back, as a
// service that runs rules someone else compiled would.
func stored(artifact *funroute.Artifact) *funroute.Artifact {
	encoded, err := json.Marshal(artifact)
	if err != nil {
		log.Fatal(err)
	}
	var back funroute.Artifact
	if err := json.Unmarshal(encoded, &back); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("stored artifact %s, %d bytes\n", back.Digest()[:19], len(encoded))
	return &back
}

// ruleError shows that fallback does not swallow the rule's own mistakes:
// a division by zero is the rule's, not an engine's.
func ruleError(registry *funroute.Registry) {
	artifact, err := funroute.CompileExpr("fallback(amount / parts, 0)", registry, funroute.CompileOptions{Args: []funroute.ArgSpec{
		{Name: "amount", Type: funroute.IntType}, {Name: "parts", Type: funroute.IntType},
	}})
	if err != nil {
		log.Fatal(err)
	}
	runtime, err := funroute.Instantiate(artifact, registry)
	if err != nil {
		log.Fatal(err)
	}
	_, err = runtime.Run(context.Background(), map[string]any{"amount": 100, "parts": 0}, funroute.RunOptions{})
	fmt.Printf("fallback(amount / 0, 0): arithmetic=%v extension=%v (%v)\n", errors.Is(err, funroute.ErrArithmetic), errors.Is(err, funroute.ErrExtension), err)
}
