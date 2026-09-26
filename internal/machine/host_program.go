package machine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"

	"github.com/nethinwei/funroute/internal/kit"
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
	// plans is the crossing of the whole contract, planned once: what bind
	// uses for an artifact that declares the contract as it is, as every
	// one a Binding compiles does.
	plans contractPlans
}

type contractPlans struct {
	once   sync.Once
	args   *argsCodec
	result *codec
	err    error
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
	return c.bind(runtime)
}

// InstantiateCompiled is Instantiate for an artifact the caller has just
// compiled and keeps nothing of (InstantiateCompiled).
func (c *Codec[In, Out]) InstantiateCompiled(artifact *Artifact) (*Program[In, Out], error) {
	runtime, err := InstantiateCompiled(artifact, c.registry)
	if err != nil {
		return nil, err
	}
	return c.bind(runtime)
}

// bind plans the Go types' crossing for a loaded runtime.
func (c *Codec[In, Out]) bind(runtime *Runtime) (*Program[In, Out], error) {
	// Planned against what the runtime's own artifact declares — a snapshot,
	// or one its compiler handed over — or, when that is the contract as it
	// is, the contract's plans: their types are the Codec's own, which no
	// caller can change either.
	declared := runtime.artifact
	args, result, err := c.plansFor(declared)
	if err != nil {
		return nil, err
	}
	reads := argumentReads(declared, args, &runtime.reg)
	program := &Program[In, Out]{
		args: args, result: result, runtime: runtime, reads: reads, straight: straightProgram(runtime, result),
		hoisted: NewBatch(runtime, BatchOptions{}),
	}
	if program.straight {
		program.plain = plainLoadsOf(reads, runtime.reg.args)
	}
	return program, nil
}

// plansFor plans how In and Out carry what artifact declares: the plans of
// the whole contract, made once, when it declares the contract as it is, and
// plans of its own otherwise.
func (c *Codec[In, Out]) plansFor(artifact *Artifact) (*argsCodec, *codec, error) {
	if !slices.EqualFunc(artifact.parts.Args, c.params, sameParameter) || !artifact.parts.Result.Equal(c.result) {
		return planCrossing(c.registry, c.in, c.out, artifact.parts.Args, artifact.parts.Result)
	}
	c.plans.once.Do(func() {
		c.plans.args, c.plans.result, c.plans.err = planCrossing(c.registry, c.in, c.out, c.params, c.result)
	})
	return c.plans.args, c.plans.result, c.plans.err
}

func sameParameter(a, b Parameter) bool { return a.name == b.name && a.typ.Equal(b.typ) }

// planCrossing is the plans of the arguments params and the result.
func planCrossing(registry *Registry, in, out reflect.Type, params []Parameter, result Type) (*argsCodec, *codec, error) {
	args, err := newArgsCodec(registry, in, params)
	if err != nil {
		return nil, nil, kit.Classify(ErrContract, "", err)
	}
	plan, err := newCodecFor(registry, out, result)
	if err != nil {
		return nil, nil, kit.Classify(ErrContract, "result: ", err)
	}
	return args, plan, nil
}

// argumentReads lists the arguments the bytecode loads. An argument the
// program never reads is never converted either, and of a record whose
// fields are promoted only those fields are, so one host struct can serve
// many rules and each pays only for the fields it uses.
func argumentReads(artifact *Artifact, plan *argsCodec, reg *regProgram) []argRead {
	read := make([]bool, len(artifact.parts.Args))
	for _, instruction := range artifact.parts.Instructions {
		if instruction.Op == OpLoadArg {
			read[instruction.A] = true
		}
	}
	var reads []argRead
	for i, ok := range read {
		if !ok {
			continue
		}
		arg := newArgRead(i, plan, reg.fieldOnly[i], reg.viewOnly[i])
		for _, p := range reg.promotions {
			if int(p.arg) == i {
				arg.promoted = append(arg.promoted, p)
			}
		}
		reads = append(reads, arg)
	}
	return reads
}

// A Program is an artifact bound to Go types: Run takes the host's struct and
// returns the host's result type, with nothing converted by name in between.
type Program[In, Out any] struct {
	args    *argsCodec
	result  *codec
	runtime *Runtime
	reads   []argRead
	// straight is set for a program that runs the shorter way
	// (host_straight.go), and plain, for one of those, when every argument it
	// reads is a plain field.
	straight bool
	plain    *plainLoads
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
	// A Binding compiles without the digest, which only what leaves the
	// process needs: it is computed here, as SealArtifact would have.
	if artifact.parts.Digest == "" {
		if artifact.parts.Digest, err = ArtifactDigest(artifact); err != nil {
			panic(err) // it was just encoded to be copied
		}
	}
	return artifact
}

// Run reads the arguments out of in, runs the program and writes the result
// into an Out. Scalars and strings cost no allocation; a slice or map field is
// wrapped, not copied; a record argument the program only reads is read in
// place. A container in the result is the program's backing and is
// read-only. in is only read, and only during the call.
func (p *Program[In, Out]) Run(ctx context.Context, in *In) (Out, error) {
	var out Out
	if err := p.run(ctx, in, &out, false); err != nil {
		var zero Out
		return zero, err
	}
	return out, nil
}

// RunInto is Run writing the result into *out, and building it in what out
// already holds: an array answered — the whole result, or a field of the
// record answered — goes into the memory of the slice out has there, so a
// host that runs a program again with the same out allocates nothing for
// it. The slices in out must not be the arguments' memory; one that is, is
// not built into. On a failure *out is left in no particular state.
func (p *Program[In, Out]) RunInto(ctx context.Context, in *In, out *Out) error {
	if out == nil {
		return fmt.Errorf("%w: the result's place is nil", ErrContract)
	}
	return p.run(ctx, in, out, true)
}

func (p *Program[In, Out]) run(ctx context.Context, in *In, out *Out, reuse bool) error {
	if in == nil {
		return fmt.Errorf("%w: arguments are nil", ErrContract)
	}
	return p.runIn(ctx, p.runtime.acquireFrame(), in, out, reuse)
}

// runIn runs the program in f, a frame of the pool's or a Session's.
func (p *Program[In, Out]) runIn(ctx context.Context, f *frame, in *In, out *Out, reuse bool) error {
	if p.straight {
		return p.runStraight(ctx, f, in, out)
	}
	r := p.runtime
	args := f.argSpace(len(p.args.params))
	f.onlyReads = true
	if err := encodeArgs(p.args, p.reads, args, in, f); err != nil {
		// Nothing ran, so the frame would not clear what was written.
		clear(args)
		r.releaseFrame(f)
		return kit.Classify(ErrContract, "", err)
	}
	return r.runProgram(ctx, f, args, p.result, placeOf(out), reuse)
}

// decodeInto is the result as an Out, or the zero Out with the error — never
// an Out half written.
func decodeInto[Out any](plan *codec, value Value) (Out, error) {
	var out Out
	if err := decodeResult(plan, value, &out); err != nil {
		var zero Out
		return zero, kit.Classify(ErrContract, "result: ", err)
	}
	return out, nil
}

// A Session runs a Program on one goroutine at a time, in a frame of its own:
// what the pool of frames costs every run of Program.Run — taking a frame and
// giving it back — it pays once, as a Lua state or a V8 isolate is one
// thread's. Program.Run may be called from any number of goroutines at once;
// a Session may not, so each goroutine that wants one takes its own.
type Session[In, Out any] struct {
	program *Program[In, Out]
	frame   *frame
}

// Session is a new Session of the program.
func (p *Program[In, Out]) Session() *Session[In, Out] {
	f := p.runtime.newFrame()
	f.owned = true
	return &Session[In, Out]{program: p, frame: f}
}

// Run is Program.Run in the session's frame.
func (s *Session[In, Out]) Run(ctx context.Context, in *In) (Out, error) {
	var out Out
	if err := s.run(ctx, in, &out, false); err != nil {
		var zero Out
		return zero, err
	}
	return out, nil
}

// RunInto is Program.RunInto in the session's frame.
func (s *Session[In, Out]) RunInto(ctx context.Context, in *In, out *Out) error {
	if out == nil {
		return fmt.Errorf("%w: the result's place is nil", ErrContract)
	}
	return s.run(ctx, in, out, true)
}

func (s *Session[In, Out]) run(ctx context.Context, in *In, out *Out, reuse bool) error {
	if in == nil {
		return fmt.Errorf("%w: arguments are nil", ErrContract)
	}
	return s.program.runIn(ctx, s.frame, in, out, reuse)
}
