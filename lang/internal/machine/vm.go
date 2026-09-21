package machine

import (
	"encoding/json"
	"fmt"
	"sync"
)

type Runtime struct {
	artifact  *Artifact
	registry  *Registry
	constants []Value
	functions []*RegisteredFunction
	frames    sync.Pool
}

type RunOptions struct {
	Fuel     uint64
	MaxStack int
}

// Instantiate validates the artifact digest and binds its exact function
// signatures to the supplied registry.
func Instantiate(artifact *Artifact, registry *Registry) (*Runtime, error) {
	if artifact == nil || registry == nil {
		return nil, fmt.Errorf("artifact and registry are required")
	}
	snapshot, err := snapshotArtifact(artifact)
	if err != nil {
		return nil, err
	}
	if err := validateArtifact(snapshot); err != nil {
		return nil, err
	}
	constants, err := loadConstants(snapshot)
	if err != nil {
		return nil, err
	}
	functions, err := bindFunctions(snapshot, registry)
	if err != nil {
		return nil, err
	}
	return &Runtime{artifact: snapshot, registry: registry, constants: constants, functions: functions}, nil
}

// EvaluateClosed runs an artifact that takes no arguments and returns its
// value. It is how the compiler folds a closed subexpression: the real VM does
// the evaluation, so the codebase has exactly one set of semantics.
//
// It skips what Instantiate checks — the digest, and the JSON snapshot — because
// this artifact was built moments ago in this process and never left it. Its
// bytecode is still validated.
func EvaluateClosed(artifact *Artifact, registry *Registry, fuel uint64, maxStack int) (Value, error) {
	if artifact == nil || registry == nil {
		return Value{}, fmt.Errorf("artifact and registry are required")
	}
	for i, instruction := range artifact.Instructions {
		if err := validateInstruction(i, instruction, artifact); err != nil {
			return Value{}, err
		}
	}
	constants, err := loadConstants(artifact)
	if err != nil {
		return Value{}, err
	}
	functions, err := bindFunctions(artifact, registry)
	if err != nil {
		return Value{}, err
	}
	runtime := &Runtime{artifact: artifact, registry: registry, constants: constants, functions: functions}
	return runtime.execute(nil, &fuel, maxStack)
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
	if artifact.Version != ArtifactVersion {
		return fmt.Errorf("unsupported artifact version %d", artifact.Version)
	}
	expectedDigest, err := ArtifactDigest(artifact)
	if err != nil {
		return err
	}
	if artifact.Digest != expectedDigest {
		return fmt.Errorf("artifact digest mismatch: got %s, want %s", artifact.Digest, expectedDigest)
	}
	if !artifact.Result.IsConcrete() {
		return fmt.Errorf("artifact result type is not concrete: %s", artifact.Result)
	}
	for i, instruction := range artifact.Instructions {
		if err := validateInstruction(i, instruction, artifact); err != nil {
			return err
		}
	}
	return nil
}

func loadConstants(artifact *Artifact) ([]Value, error) {
	constants := make([]Value, len(artifact.Constants))
	for i, constant := range artifact.Constants {
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
	functions := make([]*RegisteredFunction, len(artifact.Calls))
	for i, call := range artifact.Calls {
		function, ok := registry.Resolve(call.Signature)
		if !ok {
			return nil, fmt.Errorf("required function is not registered: %s", call.Signature)
		}
		if function.Cost != call.Cost {
			return nil, fmt.Errorf("function cost changed for %s: artifact=%d registry=%d", call.Signature, call.Cost, function.Cost)
		}
		functions[i] = function
	}
	return functions, nil
}

type failFunc func(format string, args ...any) error

// validateInstruction checks one instruction against the artifact it belongs
// to, using the opcode table: there is no switch here to fall out of sync.
func validateInstruction(index int, instruction Instruction, artifact *Artifact) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("invalid instruction %d: %s", index, fmt.Sprintf(format, args...))
	}
	spec := instruction.Op.spec()
	if spec.name == opcodes[OpInvalid].name && instruction.Op != OpInvalid {
		return fail("unknown opcode %q", instruction.Op)
	}
	if instruction.Op == OpInvalid {
		return fail("unknown opcode %q", instruction.Op)
	}
	if spec.validate == nil {
		return nil
	}
	return spec.validate(instruction, artifact, fail)
}

func validateMakeInstruction(instruction Instruction, _ *Artifact, fail failFunc) error {
	if instruction.Op == OpMakeArray {
		if instruction.A < 0 || instruction.Type == nil || instruction.Type.Kind != ArrayKind {
			return fail("malformed array")
		}
		return nil
	}
	if instruction.A < 0 || len(instruction.Keys) != instruction.A || instruction.Type == nil || instruction.Type.Kind != DictKind {
		return fail("malformed dictionary")
	}
	return nil
}

func validateCallInstruction(instruction Instruction, artifact *Artifact, fail failFunc) error {
	if instruction.A < 0 || instruction.A >= len(artifact.Calls) || instruction.B < 0 || instruction.Type == nil {
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
	if instruction.A < 0 || instruction.A > len(artifact.Instructions) {
		return fail("malformed loop target %d", instruction.A)
	}
	if instruction.Op == OpLoopNext {
		return nil
	}
	if instruction.B < 0 || instruction.B >= artifact.Locals {
		return fail("malformed loop local %d", instruction.B)
	}
	if instruction.D != NoKey && (instruction.D < 0 || instruction.D >= artifact.Locals) {
		return fail("malformed loop key slot %d", instruction.D)
	}
	if instruction.C == NoAccumulator {
		if !isArrayType(instruction.Type) {
			return fail("malformed loop result type")
		}
		return nil
	}
	if instruction.C < 0 || instruction.C >= artifact.Locals {
		return fail("malformed loop accumulator %d", instruction.C)
	}
	return nil
}

func isArrayType(typ *Type) bool {
	return typ != nil && typ.Kind == ArrayKind && typ.Elem != nil
}

func (r *Runtime) Args() []Parameter {
	out := make([]Parameter, len(r.artifact.Args))
	for i, param := range r.artifact.Args {
		out[i] = Parameter{Name: param.Name, Type: CloneType(param.Type)}
	}
	return out
}

func (r *Runtime) ResultType() Type { return CloneType(r.artifact.Result) }

// Run binds the arguments and executes the program. The activation frame comes
// from a pool, and the arguments are bound inside it, so a call allocates only
// what the program itself builds.
// Run takes the arguments by name and converts them, which is what a console
// hands over when an operator fills a form.
//
// A caller that already holds typed values in ABI order should use RunValues:
// name lookup and conversion dominate the cost of a short routing decision,
// and the host owns the contract, so it knows the order.
func (r *Runtime) Run(rawArgs map[string]any, options RunOptions) (Value, error) {
	f := r.acquireFrame()
	args := f.argSpace(len(r.artifact.Args))
	if err := r.bindArgs(args, rawArgs); err != nil {
		r.releaseFrame(f)
		return Value{}, err
	}
	return r.runFrame(f, args, options)
}

// RunValues takes the arguments already typed, in the artifact's ABI order. It
// checks each against its declared type — a wrong type is a host bug, not
// something to trust — but does no name lookup and no conversion.
func (r *Runtime) RunValues(args []Value, options RunOptions) (Value, error) {
	if len(args) != len(r.artifact.Args) {
		return Value{}, fmt.Errorf("expected %d arguments, got %d", len(r.artifact.Args), len(args))
	}
	f := r.acquireFrame()
	space := f.argSpace(len(args))
	for i, param := range r.artifact.Args {
		if !args[i].hasType(param.Type) {
			r.releaseFrame(f)
			return Value{}, fmt.Errorf("argument %q: expected %s, got %s", param.Name, param.Type, args[i].Type())
		}
		space[i] = args[i]
	}
	return r.runFrame(f, space, options)
}

func (r *Runtime) runFrame(f *frame, args []Value, options RunOptions) (Value, error) {
	fuel := options.Fuel
	if fuel == 0 {
		fuel = 10_000
	}
	maxStack := options.MaxStack
	if maxStack == 0 {
		maxStack = 1_024
	}
	f.fuelCell = fuel
	f.reset(r, args, &f.fuelCell, maxStack)
	value, err := f.guardedRun()
	r.releaseFrame(f)
	return value, err
}

func (r *Runtime) bindArgs(args []Value, rawArgs map[string]any) error {
	for i, param := range r.artifact.Args {
		raw, ok := rawArgs[param.Name]
		if !ok {
			return fmt.Errorf("missing argument %q of type %s", param.Name, param.Type)
		}
		value, err := coerce(raw, param.Type)
		if err != nil {
			return fmt.Errorf("argument %q: %w", param.Name, err)
		}
		args[i] = value
	}
	if len(rawArgs) == len(args) {
		return nil
	}
	for name := range rawArgs {
		if !r.hasArg(name) {
			return fmt.Errorf("unknown argument %q", name)
		}
	}
	return nil
}

func (r *Runtime) hasArg(name string) bool {
	for _, param := range r.artifact.Args {
		if param.Name == name {
			return true
		}
	}
	return false
}

func (r *Runtime) execute(args []Value, fuel *uint64, maxStack int) (Value, error) {
	f := r.acquireFrame()
	f.reset(r, args, fuel, maxStack)
	value, err := f.guardedRun()
	r.releaseFrame(f)
	return value, err
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
