// Command batch is a host that scores payments with a model that is cheap
// per batch and dear per call. The model gives FunRoute both: Go for one
// request, GoBatch for many. A rule calls it as if one at a time; the host
// either runs requests it already holds in one synchronous batch, or lets
// goroutines submit their own and have them gathered for a moment.
//
//	go run ./examples/batch
package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nethinwei/funroute"
)

// Payment is what the rule reads; Verdict what it returns.
type Payment struct {
	Amount  float64 `funroute:"amount"`
	Country string  `funroute:"country"`
}

type Verdict struct {
	Score  float64 `funroute:"score"`
	Review bool    `funroute:"review"`
}

const rule = `let(score = model.fraud_v1(amount), {score: score, review: score > 0.5 || country == "XX"})`

// calls counts how often the engine ran, one request or a batch at a time.
var single, batched atomic.Int64

func main() {
	registry := funroute.CoreRegistry()
	err := registry.Register(funroute.FunctionSpec{
		Name: "model.fraud_v1", Doc: funroute.Doc{Cost: 20},
		Go:      func(amount float64) (float64, error) { single.Add(1); return fraud(amount), nil },
		GoBatch: func(amounts []float64) ([]float64, error) { batched.Add(1); return scores(amounts), nil },
	})
	if err != nil {
		log.Fatal(err)
	}
	binding, err := funroute.Bind[Payment, Verdict](registry)
	if err != nil {
		log.Fatal(err)
	}
	program, err := binding.Compile(rule)
	if err != nil {
		log.Fatal(err)
	}
	held(program)
	submitted(program)
}

// held is 100 requests the host already has: one call of the engine.
func held(program *funroute.Program[Payment, Verdict]) {
	payments := make([]Payment, 100)
	for i := range payments {
		payments[i] = Payment{Amount: float64(i * 100), Country: "SG"}
	}
	verdicts := make([]Verdict, len(payments))
	program.RunBatch(context.Background(), len(payments),
		func(i int) *Payment { return &payments[i] }, func(i int) *Verdict { return &verdicts[i] },
		funroute.RunOptions{}, func(i int, err error) { log.Printf("payment %d: %v", i, err) })
	fmt.Printf("held:      %d payments, engine ran %d batch(es) and %d single call(s); payment 99 → %+v\n",
		len(payments), batched.Swap(0), single.Swap(0), verdicts[99])
}

// submitted is 64 goroutines, each with its own payment, gathered for at
// most 2 ms into as few batches as the timing allows.
func submitted(program *funroute.Program[Payment, Verdict]) {
	batch := program.Batch(funroute.BatchOptions{MaxSize: 64, MaxWait: 2 * time.Millisecond})
	defer batch.Close()
	var group sync.WaitGroup
	for i := range 64 {
		group.Go(func() {
			if _, err := batch.Run(context.Background(), &Payment{Amount: float64(i * 1000), Country: "BR"}); err != nil {
				log.Print(err)
			}
		})
	}
	group.Wait()
	fmt.Printf("submitted: 64 goroutines, engine ran %d batch(es) and %d single call(s)\n", batched.Load(), single.Load())
}

// fraud is the model: the larger the amount, the likelier fraud.
func fraud(amount float64) float64 { return min(amount/50_000, 1) }

func scores(amounts []float64) []float64 {
	out := make([]float64, len(amounts))
	for i, amount := range amounts {
		out[i] = fraud(amount)
	}
	return out
}
