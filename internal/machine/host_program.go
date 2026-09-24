package machine

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/nethinwei/funroute/internal/money"
)

// A Codec is a contract read off two Go types: the tagged fields of In are the
// arguments, in declaration order, and Out is the result. That contract is
// what Bind compiles against. An artifact compiled elsewhere may declare less
// — the arguments and record fields its rule reads — and Instantiate plans the
// crossing for exactly what it declares, matching by name.
type Codec[In, Out any] struct {
	registry *Registry
	in, out  reflect.Type
	params   []Parameter
	result   Type
}

// NewCodec reads the contract off In and Out. In must be a struct; its
// `funroute:"name"` fields are the arguments, and a struct with none — struct{}
// — is a rule that takes none. Out is any type a Go function could return.
// registry names the handles either may hold.
func NewCodec[In, Out any](registry *Registry) (*Codec[In, Out], error) {
	if registry == nil {
		return nil, errors.New("a registry is required")
	}
	c := &Codec[In, Out]{registry: registry, in: reflect.TypeFor[In](), out: reflect.TypeFor[Out]()}
	if c.in.Kind() != reflect.Struct {
		return nil, fmt.Errorf("arguments must be a struct, got %s", c.in)
	}
	declared, _, err := taggedFields(registry, c.in)
	if err != nil {
		return nil, fmt.Errorf("arguments: %w", err)
	}
	for _, field := range declared {
		c.params = append(c.params, Parameter{name: field.name, typ: field.typ})
	}
	if c.result, err = reflectType(registry, c.out); err != nil {
		return nil, fmt.Errorf("result: %w", err)
	}
	return c, nil
}

// Parameters is the contract's argument list, in ABI order.
func (c *Codec[In, Out]) Parameters() []Parameter {
	return cloneParameters(c.params)
}

func (c *Codec[In, Out]) Result() Type { return c.result }

// Instantiate binds an artifact to the Go types. Each argument it declares
// must be a field of In, by name, able to carry the declared type; its result
// must fit Out the same way. What In and Out carry beyond that is ignored. A
// mismatch is an ErrContract, found before anything runs.
func (c *Codec[In, Out]) Instantiate(artifact *Artifact) (*Program[In, Out], error) {
	runtime, err := Instantiate(artifact, c.registry)
	if err != nil {
		return nil, err
	}
	// Planned against the runtime's own snapshot, so the types the codecs
	// share belong to it and no caller can change them.
	declared := runtime.artifact
	args, err := newArgsCodec(c.registry, c.in, declared.parts.Args)
	if err != nil {
		return nil, money.Classify(ErrContract, "", err)
	}
	result, err := newCodecFor(c.registry, c.out, declared.parts.Result)
	if err != nil {
		return nil, money.Classify(ErrContract, "result: ", err)
	}
	return &Program[In, Out]{
		args: args, result: result, runtime: runtime, reads: argumentReads(declared),
		hoisted: NewBatch(runtime, BatchOptions{}),
	}, nil
}

// argumentReads lists the arguments the bytecode loads. An argument the
// program never reads is never converted either, so one host struct can serve
// many rules and each pays only for the fields it uses.
func argumentReads(artifact *Artifact) []int {
	read := make([]bool, len(artifact.parts.Args))
	for _, instruction := range artifact.parts.Instructions {
		if instruction.Op == OpLoadArg {
			read[instruction.A] = true
		}
	}
	var reads []int
	for i, ok := range read {
		if ok {
			reads = append(reads, i)
		}
	}
	return reads
}

// A Program is an artifact bound to Go types: Run takes the host's struct and
// returns the host's result type, with nothing converted by name in between.
type Program[In, Out any] struct {
	args    *argsCodec
	result  *codec
	runtime *Runtime
	reads   []int
	// hoisted is the program's batchable calls, found once. RunBatch executes
	// through it directly; it never queues and never starts a timer.
	hoisted *Batch
}

// Runtime is the untyped runtime underneath, for what a Program does not do
// itself — a Batch, or RunValues with values built ahead of time.
func (p *Program[In, Out]) Runtime() *Runtime { return p.runtime }

// Artifact is a copy of the artifact the program runs — what a host stores and
// ships after compiling through a Binding.
func (p *Program[In, Out]) Artifact() *Artifact {
	artifact, err := snapshotArtifact(p.runtime.artifact)
	if err != nil {
		panic(err) // it was decoded from this very encoding when it was loaded
	}
	return artifact
}

// Run reads the arguments out of in, runs the program and writes the result
// into an Out. Scalars and strings cost no allocation; a slice or map field is
// wrapped, not copied; a record argument costs its fields. A container in the
// result is the program's backing and is read-only. in is only read, and only
// during the call.
func (p *Program[In, Out]) Run(ctx context.Context, in *In, options RunOptions) (Out, error) {
	var zero Out
	if in == nil {
		return zero, fmt.Errorf("%w: arguments are nil", ErrContract)
	}
	r := p.runtime
	f := r.acquireFrame()
	args := f.argSpace(len(p.args.params))
	if err := encodeArgs(p.args, p.reads, args, in); err != nil {
		// Nothing ran, so the frame would not clear what was written.
		clearValues(args)
		r.releaseFrame(f)
		return zero, money.Classify(ErrContract, "", err)
	}
	value, err := r.runFrame(ctx, f, args, options)
	if err != nil {
		return zero, err
	}
	return decodeInto[Out](p.result, value)
}

// decodeInto is the result as an Out, or the zero Out with the error — never
// an Out half written.
func decodeInto[Out any](plan *codec, value Value) (Out, error) {
	var out Out
	if err := decodeResult(plan, value, &out); err != nil {
		var zero Out
		return zero, money.Classify(ErrContract, "result: ", err)
	}
	return out, nil
}
