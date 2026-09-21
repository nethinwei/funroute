package lang

import (
	"encoding/json"
	"fmt"
	"sync"
)

type Runtime struct {
	artifact  *Artifact
	registry  *Registry
	constants []Value
	functions []*registeredFunction
	frames    sync.Pool
}

type RunOptions struct {
	Fuel         uint64
	MaxStack     int
	MaxRecursion int
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
	expectedDigest, err := artifactDigest(artifact)
	if err != nil {
		return err
	}
	if artifact.Digest != expectedDigest {
		return fmt.Errorf("artifact digest mismatch: got %s, want %s", artifact.Digest, expectedDigest)
	}
	if !artifact.Result.IsConcrete() {
		return fmt.Errorf("artifact result type is not concrete: %s", artifact.Result)
	}
	if _, err := ImportExprJSON(artifact.ExprJSON); err != nil {
		return fmt.Errorf("artifact expression JSON: %w", err)
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
func bindFunctions(artifact *Artifact, registry *Registry) ([]*registeredFunction, error) {
	functions := make([]*registeredFunction, len(artifact.Calls))
	for i, call := range artifact.Calls {
		function, ok := registry.resolve(call.Signature)
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

func validateInstruction(index int, instruction Instruction, artifact *Artifact) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("invalid instruction %d: %s", index, fmt.Sprintf(format, args...))
	}
	switch instruction.Op {
	case OpConstant:
		if instruction.A < 0 || instruction.A >= len(artifact.Constants) {
			return fail("constant index %d", instruction.A)
		}
	case OpLoadArg:
		if instruction.A < 0 || instruction.A >= len(artifact.Args) {
			return fail("argument index %d", instruction.A)
		}
	case OpLoadLocal:
		if instruction.A < 0 || instruction.A >= artifact.Locals {
			return fail("local index %d", instruction.A)
		}
	case OpMakeArray, OpMakeDict:
		return validateMakeInstruction(instruction, fail)
	case OpEqual:
	case OpLoopInit, OpLoopCollect, OpLoopNext:
		return validateLoopInstruction(instruction, artifact, fail)
	case OpCall, OpRecur:
		return validateCallInstruction(instruction, artifact, fail)
	case OpJumpIfFalse, OpJump:
		if instruction.A < 0 || instruction.A > len(artifact.Instructions) {
			return fail("jump target %d", instruction.A)
		}
	default:
		return fail("unknown opcode %q", instruction.Op)
	}
	return nil
}

func validateMakeInstruction(instruction Instruction, fail failFunc) error {
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
	if instruction.Op == OpCall {
		if instruction.A < 0 || instruction.A >= len(artifact.Calls) || instruction.B < 0 || instruction.Type == nil {
			return fail("malformed call")
		}
		return nil
	}
	if instruction.B != len(artifact.Args) || instruction.Type == nil {
		return fail("malformed recursion")
	}
	return nil
}

// validateLoopInstruction covers both loop shapes: C is the accumulator slot of
// a fold, or noAccumulator when the loop maps into an array.
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
	if instruction.C == noAccumulator {
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
		out[i] = Parameter{Name: param.Name, Type: cloneType(param.Type)}
	}
	return out
}

func (r *Runtime) ResultType() Type { return cloneType(r.artifact.Result) }

// Run binds the arguments and executes the program. The activation frame comes
// from a pool, and the arguments are bound inside it, so a call allocates only
// what the program itself builds.
func (r *Runtime) Run(rawArgs map[string]any, options RunOptions) (Value, error) {
	fuel := options.Fuel
	if fuel == 0 {
		fuel = 10_000
	}
	maxStack := options.MaxStack
	if maxStack == 0 {
		maxStack = 1_024
	}
	maxRecursion := options.MaxRecursion
	if maxRecursion == 0 {
		maxRecursion = 128
	}
	f := r.acquireFrame()
	args := f.argSpace(len(r.artifact.Args))
	if err := r.bindArgs(args, rawArgs); err != nil {
		r.releaseFrame(f)
		return Value{}, err
	}
	f.fuelCell = fuel
	f.reset(r, args, &f.fuelCell, maxStack, maxRecursion, 0)
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

func (r *Runtime) execute(args []Value, fuel *uint64, maxStack, maxRecursion, depth int) (Value, error) {
	if depth > maxRecursion {
		return Value{}, fmt.Errorf("recursion limit %d exceeded", maxRecursion)
	}
	f := r.acquireFrame()
	f.reset(r, args, fuel, maxStack, maxRecursion, depth)
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
