package machine

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/nethinwei/funroute/internal/kit"
)

type Runtime struct {
	artifact  *Artifact
	registry  *Registry
	constants []Value
	functions []*RegisteredFunction
	// updates holds, for each record_with, the indexes of the fields it
	// replaces, by program counter; nil when the program has none.
	updates [][]int
	// money is what the runtime knows about the currencies its arguments
	// carry; empty for a program without money.
	money moneyPlan
	// depth is how deep the stack gets, which loading proved.
	depth  int
	frames sync.Pool
}

type RunOptions struct {
	Fuel     uint64
	MaxStack int
	// prefetched holds results a Batch computed ahead of the program, keyed
	// by the call instruction's program counter. The program takes them
	// instead of calling. Only a Batch sets it: a host that could would skip
	// the real call.
	prefetched map[int]Prefetched
}

// Instantiate validates the artifact digest and binds its exact function
// signatures to the supplied registry.
func Instantiate(artifact *Artifact, registry *Registry) (*Runtime, error) {
	if artifact == nil || registry == nil {
		return nil, errors.New("artifact and registry are required")
	}
	snapshot, err := snapshotArtifact(artifact)
	if err != nil {
		return nil, err
	}
	if err := validateArtifact(snapshot); err != nil {
		return nil, err
	}
	runtime, err := newRuntime(snapshot, registry)
	if err != nil {
		return nil, err
	}
	if err := checkMoneyStamp(snapshot, registry); err != nil {
		return nil, err
	}
	if err := declaredConstants(runtime.constants, registry); err != nil {
		return nil, err
	}
	return runtime, nil
}

// newRuntime loads a validated artifact's constants and binds its calls.
func newRuntime(artifact *Artifact, registry *Registry) (*Runtime, error) {
	constants, err := loadConstants(artifact)
	if err != nil {
		return nil, err
	}
	functions, err := bindFunctions(artifact, registry)
	if err != nil {
		return nil, err
	}
	depth, err := verify(artifact, functions)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		artifact: artifact, registry: registry, constants: constants, functions: functions, depth: depth,
		updates: resolveUpdates(artifact), money: newMoneyPlan(artifact, registry),
	}, nil
}

// declaredConstants holds an artifact's constants to the registry's currency
// table as the boundary holds arguments: declared currencies, no amount
// without one but zero, exchange rates that keep the rules. The digest says
// the artifact is the one that was sealed, not that its sealer was honest.
func declaredConstants(constants []Value, registry *Registry) error {
	table := registry.currencies()
	if table == nil {
		return nil // checkMoneyStamp refused an artifact with money already
	}
	for i, constant := range constants {
		if err := declaredValue(table, constant, true); err != nil {
			return fmt.Errorf("constant %d: %w", i, err)
		}
	}
	return nil
}

// resolveUpdates turns each record_with's field names into the indexes the
// frame writes by, once, at load. The names stay in the instruction rather
// than an index list of its own because the interpreter copies an Instruction
// on every step: one more slice header in it made every program 7% slower,
// record updates or not.
func resolveUpdates(artifact *Artifact) [][]int {
	var updates [][]int
	for pc, instruction := range artifact.parts.Instructions {
		if instruction.Op != OpRecordWith {
			continue
		}
		if updates == nil {
			updates = make([][]int, len(artifact.parts.Instructions))
		}
		updates[pc] = kit.Map(instruction.Keys, instruction.Type.FieldIndex)
	}
	return updates
}

// EvaluateClosed runs an artifact that takes no arguments and returns its
// value. It is how the compiler folds a closed subexpression: the real VM does
// the evaluation, so the codebase has exactly one set of semantics.
//
// It skips what Instantiate checks — the digest, and the JSON snapshot — because
// this artifact was built moments ago in this process and never left it. Its
// bytecode is still validated. fuel and maxStack are the limits themselves:
// the compiler always passes both, so zero is not the default a run's is.
func EvaluateClosed(parts ArtifactParts, registry *Registry, fuel uint64, maxStack int) (Value, error) {
	if registry == nil {
		return Value{}, errors.New("a registry is required")
	}
	artifact := &Artifact{parts: parts}
	if err := validateInstructions(artifact); err != nil {
		return Value{}, err
	}
	runtime, err := newRuntime(artifact, registry)
	if err != nil {
		return Value{}, err
	}
	f := runtime.acquireFrame()
	return runtime.runFrame(context.Background(), f, nil, RunOptions{Fuel: fuel, MaxStack: maxStack})
}

// snapshotArtifact round-trips the artifact through JSON so the runtime owns an
// immutable copy that the caller cannot mutate afterwards.
func snapshotArtifact(artifact *Artifact) (*Artifact, error) {
	encoded, err := json.Marshal(artifact)
	if err != nil {
		return nil, fmt.Errorf("snapshot artifact: %w", err)
	}
	var snapshot Artifact
	if err := json.Unmarshal(encoded, &snapshot); err != nil {
		return nil, fmt.Errorf("snapshot artifact: %w", err)
	}
	return &snapshot, nil
}

func validateArtifact(artifact *Artifact) error {
	if artifact.parts.Version != ArtifactVersion {
		return fmt.Errorf("unsupported artifact version %d", artifact.parts.Version)
	}
	expectedDigest, err := ArtifactDigest(artifact)
	if err != nil {
		return err
	}
	if artifact.parts.Digest != expectedDigest {
		return fmt.Errorf("artifact digest mismatch: got %s, want %s", artifact.parts.Digest, expectedDigest)
	}
	if !artifact.parts.Result.IsConcrete() {
		return fmt.Errorf("artifact result type is not concrete: %s", artifact.parts.Result)
	}
	for _, param := range artifact.parts.Args {
		if !IsValidVariableName(param.name) || IsReservedName(param.name) || !param.typ.IsConcrete() {
			return fmt.Errorf("artifact argument is invalid: %s:%s", param.name, param.typ)
		}
	}
	// A frame makes its locals before the program runs, so their count is
	// bounded here: every slot is written by an instruction, and none writes
	// more than a loop's variable, key and accumulator.
	if artifact.parts.Locals < 0 || artifact.parts.Locals > 3*len(artifact.parts.Instructions) {
		return fmt.Errorf("artifact claims %d locals for %d instructions", artifact.parts.Locals, len(artifact.parts.Instructions))
	}
	return validateInstructions(artifact)
}

// validateInstructions checks each instruction against the artifact it
// belongs to, using the opcode table: there is no switch here to fall out of
// sync.
func validateInstructions(artifact *Artifact) error {
	for i, instruction := range artifact.parts.Instructions {
		if err := validateInstruction(i, instruction, artifact); err != nil {
			return err
		}
	}
	return nil
}

func validateInstruction(index int, instruction Instruction, artifact *Artifact) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("invalid instruction %d: %s", index, fmt.Sprintf(format, args...))
	}
	spec := instruction.Op.spec()
	if instruction.Op == OpInvalid || spec.name == opcodes[OpInvalid].name {
		return fail("unknown opcode %q", instruction.Op)
	}
	if spec.validate == nil {
		return nil
	}
	return spec.validate(instruction, artifact, fail)
}

func loadConstants(artifact *Artifact) ([]Value, error) {
	constants := make([]Value, len(artifact.parts.Constants))
	for i, constant := range artifact.parts.Constants {
		value, err := constant.value()
		if err != nil {
			return nil, fmt.Errorf("constant %d: %w", i, err)
		}
		constants[i] = value
	}
	return constants, nil
}

// bindFunctions refuses to load an artifact whose registry drifted: the exact
// signature must still exist and keep its fuel cost.
func bindFunctions(artifact *Artifact, registry *Registry) ([]*RegisteredFunction, error) {
	functions := make([]*RegisteredFunction, len(artifact.parts.Calls))
	for i, call := range artifact.parts.Calls {
		function, ok := registry.Resolve(call.Signature)
		if !ok {
			return nil, fmt.Errorf("required function is not registered: %s", call.Signature)
		}
		if function.Doc.Cost != call.Cost {
			return nil, fmt.Errorf("function cost changed for %s: artifact=%d registry=%d", call.Signature, call.Cost, function.Doc.Cost)
		}
		functions[i] = function
	}
	return functions, nil
}

type failFunc func(format string, args ...any) error

func validateMakeInstruction(instruction Instruction, _ *Artifact, fail failFunc) error {
	if instruction.Op == OpMakeArray {
		if instruction.A < 0 || instruction.Type == nil || instruction.Type.kind != ArrayKind {
			return fail("malformed array")
		}
		return nil
	}
	if instruction.Op == OpMakeRecord {
		if instruction.Type == nil || instruction.Type.kind != RecordKind || instruction.A != len(instruction.Type.fields) {
			return fail("malformed record")
		}
		return nil
	}
	if instruction.A < 0 || len(instruction.Keys) != instruction.A || instruction.Type == nil || instruction.Type.kind != DictKind {
		return fail("malformed dictionary")
	}
	return nil
}

func validateFieldInstruction(instruction Instruction, _ *Artifact, fail failFunc) error {
	if instruction.A < 0 || instruction.Type == nil {
		return fail("malformed field access")
	}
	return nil
}

// validateRecordWith requires distinct fields of the record type, so the
// indexes resolveUpdates finds for them can be used unchecked.
func validateRecordWith(instruction Instruction, _ *Artifact, fail failFunc) error {
	if instruction.Type == nil || instruction.Type.kind != RecordKind || len(instruction.Keys) == 0 {
		return fail("malformed record update")
	}
	seen := make([]bool, len(instruction.Type.fields))
	for _, name := range instruction.Keys {
		index := instruction.Type.FieldIndex(name)
		if index < 0 || seen[index] {
			return fail("record update field %q", name)
		}
		seen[index] = true
	}
	return nil
}

func validateCallInstruction(instruction Instruction, artifact *Artifact, fail failFunc) error {
	if instruction.A < 0 || instruction.A >= len(artifact.parts.Calls) || instruction.B < 0 || instruction.Type == nil {
		return fail("malformed call")
	}
	return nil
}

// validateLoopInstruction covers both loop shapes: C is the accumulator slot of
// a fold, or NoAccumulator when the loop maps into an array.
func validateLoopInstruction(instruction Instruction, artifact *Artifact, fail failFunc) error {
	if instruction.Type == nil || !instruction.Type.IsConcrete() {
		return fail("malformed loop type")
	}
	if instruction.Op == OpLoopCollect {
		return nil
	}
	if instruction.A < 0 || instruction.A > len(artifact.parts.Instructions) {
		return fail("malformed loop target %d", instruction.A)
	}
	if instruction.Op == OpLoopNext {
		return nil
	}
	if instruction.B < 0 || instruction.B >= artifact.parts.Locals {
		return fail("malformed loop local %d", instruction.B)
	}
	if instruction.D != NoKey && (instruction.D < 0 || instruction.D >= artifact.parts.Locals) {
		return fail("malformed loop key slot %d", instruction.D)
	}
	if instruction.C == NoAccumulator {
		// A comprehension builds an array, or a dictionary when it has a key.
		if kind := instruction.Type.kind; instruction.Type.elem == nil || kind != ArrayKind && kind != DictKind {
			return fail("malformed loop result type")
		}
		return nil
	}
	if instruction.C < 0 || instruction.C >= artifact.parts.Locals {
		return fail("malformed loop accumulator %d", instruction.C)
	}
	return nil
}

func (r *Runtime) Args() []Parameter {
	return cloneParameters(r.artifact.parts.Args)
}

func (r *Runtime) ResultType() Type { return r.artifact.parts.Result }

// Run binds the arguments and executes the program. The activation frame comes
// from a pool, and the arguments are bound inside it, so a call allocates only
// what the program itself builds.
// Run takes the arguments by name and converts them, which is what a console
// hands over when an operator fills a form.
//
// A caller that already holds typed values in ABI order should use RunValues:
// name lookup and conversion dominate the cost of a short routing decision,
// and the host owns the contract, so it knows the order.
//
// ctx is the request's budget: every extension call sees its deadline, and a
// program stops at the next call once it has passed (ErrDeadline). The pure
// part of a program is not interrupted; it is nanoseconds.
func (r *Runtime) Run(ctx context.Context, rawArgs map[string]any, options RunOptions) (Value, error) {
	f := r.acquireFrame()
	args := f.argSpace(len(r.artifact.parts.Args))
	if err := r.bindArgs(args, rawArgs); err != nil {
		// Nothing ran, so the frame would not clear what was bound.
		clear(args)
		r.releaseFrame(f)
		return Value{}, kit.Classify(ErrContract, "", err)
	}
	return r.runFrame(ctx, f, args, options)
}

// RunValues takes the arguments already typed, in the artifact's ABI order. It
// checks each against its declared type — a wrong type is a host bug, not
// something to trust — but does no name lookup and no conversion.
func (r *Runtime) RunValues(ctx context.Context, args []Value, options RunOptions) (Value, error) {
	// No invariant re-check here. A Value cannot hold a NaN in the first
	// place: every public constructor rejects one where it enters —
	// funroute.Float is CheckedFloat, ToValue goes through checkFloats, and
	// Array/Dict/Record validate what they pack. Scanning again on every
	// call made a 65536-element vector cost 16µs instead of 200ns, and it
	// never protected the one case it could not see anyway — a host that
	// mutates a backing it promised to treat as read-only can do that just
	// as well after this line.
	if err := r.checkKinds(args, false); err != nil {
		return Value{}, err
	}
	return r.runTyped(ctx, args, options)
}

// admit checks a request's arguments as a run would — their kinds unless a
// Codec built them, their currencies always — without running it: a Batch
// asks before it hands the arguments to an engine, which must never see what
// the program itself would refuse.
func (r *Runtime) admit(args []Value, typed bool) error {
	if err := r.checkKinds(args, typed); err != nil {
		return err
	}
	if !r.money.any {
		return nil
	}
	return r.checkUnits(args)
}

// checkKinds holds typed arguments to the contract: their number, and each
// one's type unless a Codec built them.
func (r *Runtime) checkKinds(args []Value, typed bool) error {
	params := r.artifact.parts.Args
	if len(args) != len(params) {
		return fmt.Errorf("%w: expected %d arguments, got %d", ErrContract, len(params), len(args))
	}
	for i := 0; !typed && i < len(params); i++ {
		if !args[i].hasType(params[i].typ) {
			return argumentError(params[i], args[i])
		}
	}
	return nil
}

// argumentError is a typed argument that is not its parameter's type. Money
// of the right kind in the wrong currency is also an ErrCurrency, as the same
// amount is when it comes through Run as JSON.
func argumentError(param Parameter, value Value) error {
	err := fmt.Errorf("argument %q: expected %s, got %s", param.name, param.typ.Summary(), value.Type().Summary())
	if value.kind == param.typ.kind && IsUnitKind(value.kind) {
		err = kit.Classify(ErrCurrency, "", err)
	}
	return kit.Classify(ErrContract, "", err)
}

// runTyped runs arguments a Codec produced. They are typed by construction,
// and the ones the program never reads are left empty — which RunValues would
// rightly refuse from a host, so the check is not repeated here.
func (r *Runtime) runTyped(ctx context.Context, args []Value, options RunOptions) (Value, error) {
	f := r.acquireFrame()
	space := f.argSpace(len(args))
	copy(space, args)
	return r.runFrame(ctx, f, space, options)
}

func (r *Runtime) runFrame(ctx context.Context, f *frame, args []Value, options RunOptions) (Value, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	f.fuelCell = cmp.Or(options.Fuel, DefaultFuel)
	f.reset(r, args, &f.fuelCell, cmp.Or(options.MaxStack, 1_024))
	f.ctx = ctx
	f.deadline = ctx.Done() != nil
	f.prefetched = options.prefetched
	if r.money.any {
		if err := r.checkUnits(args); err != nil {
			r.releaseFrame(f)
			return Value{}, err
		}
	}
	value, err := f.guardedRun()
	r.releaseFrame(f)
	return value, err
}

func (r *Runtime) bindArgs(args []Value, rawArgs map[string]any) error {
	for i, param := range r.artifact.parts.Args {
		raw, ok := rawArgs[param.name]
		if !ok {
			return fmt.Errorf("missing argument %q of type %s", param.name, param.typ)
		}
		value, err := coerceWith(raw, param.typ, r.money.table)
		if err != nil {
			return fmt.Errorf("argument %q: %w", param.name, err)
		}
		args[i] = value
	}
	if len(rawArgs) == len(args) {
		return nil
	}
	for name := range rawArgs {
		if !slices.ContainsFunc(r.artifact.parts.Args, func(param Parameter) bool { return param.name == name }) {
			return fmt.Errorf("unknown argument %q", name)
		}
	}
	return nil
}

func (r *Runtime) acquireFrame() *frame {
	if f, ok := r.frames.Get().(*frame); ok {
		return f
	}
	return &frame{}
}

func (r *Runtime) releaseFrame(f *frame) {
	f.release()
	r.frames.Put(f)
}
