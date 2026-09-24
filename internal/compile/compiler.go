package compile

import (
	"errors"
	"fmt"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

func CompileExpr(source string, registry *machine.Registry, options CompileOptions) (*machine.Artifact, error) {
	expr, err := syntax.Parse(source)
	if err != nil {
		return nil, compileError(err)
	}
	return CompileAST(expr, registry, options)
}

// CompileJSON compiles a canonical ExprJSON document. Together with
// CompileExpr it is the whole compile surface a host needs: both meet at
// CompileAST, so a program can never be accepted through one door and refused
// at the other, and neither requires the host to touch the AST.
func CompileJSON(exprJSON []byte, registry *machine.Registry, options CompileOptions) (*machine.Artifact, error) {
	expr, err := syntax.ImportExprJSON(exprJSON)
	if err != nil {
		return nil, compileError(err)
	}
	return CompileAST(expr, registry, options)
}

// ParseToJSON parses source into canonical ExprJSON in one step, which is what
// a front end needs: it renders the document, never the AST.
func ParseToJSON(source string) ([]byte, error) {
	expr, err := syntax.Parse(source)
	if err != nil {
		return nil, compileError(err)
	}
	encoded, err := syntax.ExportExprJSON(expr)
	return encoded, compileError(err)
}

// CompileAST is the single compile path: source and ExprJSON both reach it, so
// there is no way for one to be accepted while the other is refused.
func CompileAST(expr syntax.Expr, registry *machine.Registry, options CompileOptions) (*machine.Artifact, error) {
	inferred, compiler, err := build(expr, registry, options)
	if err != nil {
		return nil, err
	}
	exprJSON, err := syntax.ExportExprJSON(expr)
	if err != nil {
		return nil, compileError(err)
	}
	artifact, err := sealArtifact(exprJSON, inferred, compiler)
	return artifact, compileError(err)
}

// build checks, infers and compiles expr: every step CompileAST takes before
// it seals an artifact, and every step Analyze takes. When inference got
// through, its result comes back even if a later step failed.
func build(expr syntax.Expr, registry *machine.Registry, options CompileOptions) (*inference, *bytecodeCompiler, error) {
	if registry == nil {
		return nil, nil, compileError(errors.New("registry is required"))
	}
	if err := validateForms(expr, registry); err != nil {
		return nil, nil, compileError(err)
	}
	if err := options.validate(expr); err != nil {
		return nil, nil, err
	}
	if err := validateMoneyContract(options, registry); err != nil {
		return nil, nil, err
	}
	inferred, err := inferProgram(expr, registry, options.argTypes(), options.argOrder(), options.Result)
	if err != nil {
		return nil, nil, compileError(err)
	}
	attachDocs(inferred.Params, options.Args)
	inferred.ResultDoc = options.ResultDoc
	if err := checkExact(expr, inferred, registry); err != nil {
		return inferred, nil, compileError(err)
	}
	compiler := newBytecodeCompiler(registry, inferred)
	compiler.readsArgument = argumentReaders(expr)
	if err := compiler.compile(expr); err != nil {
		return inferred, nil, compileError(err)
	}
	limit := options.MaxInstructions
	if limit == 0 {
		limit = 10_000
	}
	if len(compiler.instructions) > limit {
		return inferred, nil, compileError(fmt.Errorf("compiled program has %d instructions, limit is %d", len(compiler.instructions), limit))
	}
	return inferred, compiler, nil
}

func newBytecodeCompiler(registry *machine.Registry, inferred *inference) *bytecodeCompiler {
	compiler := &bytecodeCompiler{
		registry:   registry,
		inferred:   inferred,
		argIndex:   map[string]int{},
		localIndex: map[string][]int{},
		constIndex: map[string][]int{},
		callIndex:  map[string]int{},
	}
	for i, param := range inferred.Params {
		compiler.argIndex[param.Name()] = i
	}
	return compiler
}

func sealArtifact(exprJSON []byte, inferred *inference, compiler *bytecodeCompiler) (*machine.Artifact, error) {
	parts := compiler.artifact(inferred.Result)
	parts.ExprJSON = exprJSON
	parts.Args = inferred.Params
	parts.ResultDoc = inferred.ResultDoc
	return machine.SealArtifact(parts, compiler.registry)
}

type bytecodeCompiler struct {
	registry *machine.Registry
	inferred *inference
	// readsArgument is every node whose subtree reads a program argument:
	// one that never folds, known without walking it again.
	readsArgument map[int]bool
	argIndex      map[string]int
	localIndex    map[string][]int
	// constIndex holds the bindings that folded to a constant: they occupy a
	// constant-pool slot instead of a local one, so nothing is stored at run
	// time and every read is a single OpConstant.
	constIndex   map[string][]int
	nextLocal    int
	callIndex    map[string]int
	constants    []machine.Constant
	calls        []machine.CallReference
	instructions []machine.Instruction
	// folding is set on the nested compiler a fold uses, so a fold never
	// recurses into another fold. folded counts what was folded away.
	folding bool
	folded  int
	// inRound is set inside a round(…), where the steps that land between
	// minor units are exact; roundSteps counts the ones in the innermost.
	inRound    bool
	roundSteps int
	// usingDepth is how many using bodies the compiler is inside: a
	// conversion outside every one has no rates to convert at.
	usingDepth int
}

func (c *bytecodeCompiler) compile(expr syntax.Expr) error {
	folded, err := c.tryFold(expr)
	if err != nil {
		return err
	}
	if folded {
		return nil
	}
	switch node := expr.(type) {
	case *syntax.LiteralExpr, *syntax.EnumExpr, *syntax.MoneyExpr, *syntax.RatioExpr, *syntax.FxRateExpr, *syntax.CurrencyExpr:
		value, err := c.constantValue(node)
		if err != nil {
			return err
		}
		return c.emitConstant(value, c.inferred.NodeTypes[node.NodeID()])
	case *syntax.VariableExpr:
		return c.compileVariable(node)
	case *syntax.ArrayExpr:
		return c.compileArray(node)
	case *syntax.DictExpr:
		return c.compileDict(node)
	case *syntax.SwitchExpr:
		return c.compileSwitch(node)
	case *syntax.ForExpr:
		return c.compileFor(node)
	case *syntax.ReduceExpr:
		return c.compileReduce(node)
	case *syntax.LetExpr:
		return c.compileLet(node)
	case *syntax.UsingExpr:
		return c.compileUsing(node)
	case *syntax.RecordExpr:
		return c.compileRecord(node)
	case *syntax.FieldExpr:
		return c.compileField(node)
	case *syntax.RecordUpdateExpr:
		return c.compileRecordUpdate(node)
	case *syntax.CallExpr:
		return c.compileCall(node)
	default:
		return fmt.Errorf("unsupported expression %T", expr)
	}
}

func (c *bytecodeCompiler) compileAll(exprs []syntax.Expr) error {
	for _, expr := range exprs {
		if err := c.compile(expr); err != nil {
			return err
		}
	}
	return nil
}

// constantValue is the value a literal, @member or money literal stands for.
func (c *bytecodeCompiler) constantValue(expr syntax.Expr) (machine.Value, error) {
	switch node := expr.(type) {
	case *syntax.LiteralExpr:
		return c.literalValue(node)
	case *syntax.EnumExpr:
		return c.enumValue(node), nil
	}
	return moneyLiteral(expr, c.registry)
}

// emitConstant pushes value, of the static type typ, which a literal always
// has a constant form for.
func (c *bytecodeCompiler) emitConstant(value machine.Value, typ machine.Type) error {
	index, ok := c.addConstant(value, typ)
	if !ok {
		return fmt.Errorf("internal error: %v has no constant form as %s", value.Any(), typ)
	}
	c.emit(machine.Instruction{Op: machine.OpConstant, A: index})
	return nil
}

func (c *bytecodeCompiler) compileVariable(node *syntax.VariableExpr) error {
	if indexes := c.constIndex[node.Name]; len(indexes) > 0 {
		c.emit(machine.Instruction{Op: machine.OpConstant, A: indexes[len(indexes)-1]})
		return nil
	}
	if slots := c.localIndex[node.Name]; len(slots) > 0 {
		c.emit(machine.Instruction{Op: machine.OpLoadLocal, A: slots[len(slots)-1]})
		return nil
	}
	index, ok := c.argIndex[node.Name]
	if !ok {
		return fmt.Errorf("internal error: no argument index for %q", node.Name)
	}
	c.emit(machine.Instruction{Op: machine.OpLoadArg, A: index})
	return nil
}

func (c *bytecodeCompiler) compileArray(node *syntax.ArrayExpr) error {
	return c.compileMake(node.ID, machine.ArrayKind, node.Items,
		machine.Instruction{Op: machine.OpMakeArray, A: len(node.Items)}, "cannot compile array with unresolved type")
}

// compileDict packs the values in key order, which is the order the parser
// and the ExprJSON import keep a dictionary's entries in.
func (c *bytecodeCompiler) compileDict(node *syntax.DictExpr) error {
	keys := make([]string, len(node.Entries))
	for i, entry := range node.Entries {
		keys[i] = entry.Key
	}
	return c.compileMake(node.ID, machine.DictKind, dictValues(node),
		machine.Instruction{Op: machine.OpMakeDict, A: len(keys), Keys: keys}, "cannot compile dictionary with unresolved type")
}

// compileRecord evaluates the fields in the order the type declares and packs
// them; compileField turns the name into the index it resolved to, so reading
// a field is one instruction and no name survives into the bytecode.
func (c *bytecodeCompiler) compileRecord(node *syntax.RecordExpr) error {
	values := make([]syntax.Expr, len(node.Fields))
	for i, field := range node.Fields {
		values[i] = field.Value
	}
	return c.compileMake(node.ID, machine.RecordKind, values,
		machine.Instruction{Op: machine.OpMakeRecord, A: len(node.Fields)}, "cannot compile record with unresolved type")
}

// compileMake pushes values in order and packs them into one value of the
// node's type with instruction. A record's type is always known, so only a
// container's can be unresolved.
func (c *bytecodeCompiler) compileMake(id int, kind machine.Kind, values []syntax.Expr, instruction machine.Instruction, unresolved string) error {
	if err := c.compileAll(values); err != nil {
		return err
	}
	typ, ok := c.inferred.NodeTypes[id]
	if !ok || typ.Kind() != kind || (kind != machine.RecordKind && !hasElem(typ)) {
		return errors.New(unresolved)
	}
	instruction.Type = &typ
	c.emit(instruction)
	return nil
}

func (c *bytecodeCompiler) compileField(node *syntax.FieldExpr) error {
	// Inference reports a read of a field the record lacks, with its place,
	// so a field that does not resolve here is the compiler's own mistake.
	sourceType, ok := c.inferred.NodeTypes[node.Value.NodeID()]
	index := sourceType.FieldIndex(node.Field)
	if !ok || sourceType.Kind() != machine.RecordKind || index < 0 {
		return fmt.Errorf("internal error: field %q was not resolved by inference", node.Field)
	}
	if err := c.compile(node.Value); err != nil {
		return err
	}
	resultType := c.inferred.NodeTypes[node.ID]
	c.emit(machine.Instruction{Op: machine.OpField, A: index, Type: &resultType})
	return nil
}

func (c *bytecodeCompiler) compileCall(node *syntax.CallExpr) error {
	key, ok := c.inferred.Selections[node.ID]
	if !ok {
		return fmt.Errorf("internal error: no selected overload for %s", node.Name)
	}
	function, ok := c.registry.Resolve(key)
	if !ok {
		return fmt.Errorf("function disappeared during compilation: %s", key)
	}
	if function.IsLazyIf() {
		return c.compileIf(node)
	}
	if function.IsLazyFallback() {
		return c.compileFallback(node)
	}
	if function.ReadsRates() && c.usingDepth == 0 {
		return syntax.Around(node, "type error: %s reads exchange rates, which only a using has: write it inside using(150 JPY / USD, …), or using(rates, …) with the rates as an argument", operatorName(node))
	}
	if function.NeedsBoundedArgs() {
		if err := c.requireBoundedArgs(node); err != nil {
			return err
		}
	}
	if err := c.exactStep(node, function); err != nil {
		return err
	}
	if function.IsRoundingScope() {
		if err := c.compileRoundingScope(node); err != nil {
			return err
		}
	} else if err := c.compileAll(node.Args); err != nil {
		return err
	}
	callIndex, ok := c.callIndex[key]
	if !ok {
		callIndex = len(c.calls)
		c.callIndex[key] = callIndex
		c.calls = append(c.calls, machine.CallReference{Name: function.Name, Signature: key, Cost: function.Cost()})
	}
	resultType := c.inferred.NodeTypes[node.ID]
	c.emit(machine.Instruction{Op: machine.OpCall, A: callIndex, B: len(node.Args), Type: &resultType})
	return nil
}

func (c *bytecodeCompiler) compileSwitch(node *syntax.SwitchExpr) error {
	subjectSlot := -1
	if node.Value != nil {
		if err := c.compile(node.Value); err != nil {
			return err
		}
		subjectSlot = c.nextLocal
		c.nextLocal++
		c.emit(machine.Instruction{Op: machine.OpStoreLocal, A: subjectSlot})
	}
	var endJumps []int
	for i, item := range node.Cases {
		// Without an else the switch is exhaustive, so when no other branch
		// matched the last one does: it needs no test.
		if i == len(node.Cases)-1 && node.Default == nil {
			if err := c.compile(item.Result); err != nil {
				return err
			}
			break
		}
		hits, misses, err := c.compileBranchTest(subjectSlot, item.Match)
		if err != nil {
			return err
		}
		c.patch(hits, len(c.instructions))
		if err := c.compile(item.Result); err != nil {
			return err
		}
		endJumps = append(endJumps, c.emit(machine.Instruction{Op: machine.OpJump}))
		c.patch(misses, len(c.instructions))
	}
	if node.Default != nil {
		if err := c.compile(node.Default); err != nil {
			return err
		}
	}
	c.patch(endJumps, len(c.instructions))
	return nil
}

// requireBoundedArgs holds a BoundedArgs function to its promise: what it
// produces is sized by its arguments, so those arguments must have a size the
// inputs already bound. Three things do: a literal, the length of a container
// (that length *is* part of the input size), and the two combined by
// arithmetic. An argument that could be any scalar at run time does not, and
// that is the one docs/termination.md rules out.
func (c *bytecodeCompiler) requireBoundedArgs(node *syntax.CallExpr) error {
	for i, arg := range node.Args {
		// Only an integer can stand for a length; the amount allocate splits
		// or anything else of another type says nothing about the size.
		if typ, ok := c.inferred.NodeTypes[arg.NodeID()]; ok && typ.Kind() != machine.IntKind {
			continue
		}
		if c.boundedExpr(arg) {
			continue
		}
		return syntax.Around(arg,
			"%s needs arguments whose size the inputs already bound: a literal, len(...) of a container, or those combined by arithmetic — argument %d is neither",
			node.Name, i+1)
	}
	return nil
}

func (c *bytecodeCompiler) boundedExpr(expr syntax.Expr) bool {
	switch node := expr.(type) {
	case *syntax.LiteralExpr:
		return true
	case *syntax.VariableExpr:
		// A binding that folded to a constant is as good as a literal.
		return len(c.constIndex[node.Name]) > 0
	case *syntax.CallExpr:
		return c.boundedCall(node)
	}
	return false
}

// boundedCall knows two things about the kernel: len hands back a size the
// input already bound, and arithmetic on bounded sizes stays bounded (the
// polynomial in docs/termination.md is a polynomial for exactly this reason).
// Only the kernel's own functions count — a host may register another len.
func (c *bytecodeCompiler) boundedCall(node *syntax.CallExpr) bool {
	key, ok := c.inferred.Selections[node.ID]
	if !ok {
		return false
	}
	function, ok := c.registry.Resolve(key)
	if !ok || !function.IsBuiltin() {
		return false
	}
	switch function.Name {
	case "len":
		return true
	case "add", "sub", "mul", "div", "mod":
		for _, arg := range node.Args {
			if !c.boundedExpr(arg) {
				return false
			}
		}
		return true
	}
	return false
}

func (c *bytecodeCompiler) compileFallback(node *syntax.CallExpr) error {
	if len(node.Args) < 2 {
		return errors.New("fallback requires at least 2 arguments")
	}
	endJumps := make([]int, 0, len(node.Args)-1)
	for _, candidate := range node.Args[:len(node.Args)-1] {
		begin := c.emit(machine.Instruction{Op: machine.OpBeginFallback})
		if err := c.compile(candidate); err != nil {
			return err
		}
		c.emit(machine.Instruction{Op: machine.OpEndFallback})
		endJumps = append(endJumps, c.emit(machine.Instruction{Op: machine.OpJump}))
		c.instructions[begin].A = len(c.instructions)
	}
	if err := c.compile(node.Args[len(node.Args)-1]); err != nil {
		return err
	}
	c.patch(endJumps, len(c.instructions))
	return nil
}

// compileBranchTest emits the test for one branch. With a subject each match is
// compared with eq; without one the matches are conditions. Any match selects
// the branch, so all but the last jump forward on true.
func (c *bytecodeCompiler) compileBranchTest(subjectSlot int, matches []syntax.Expr) ([]int, []int, error) {
	var hits, misses []int
	for i, match := range matches {
		if err := c.compileMatch(subjectSlot, match); err != nil {
			return nil, nil, err
		}
		if i == len(matches)-1 {
			misses = append(misses, c.emit(machine.Instruction{Op: machine.OpJumpIfFalse}))
			break
		}
		tryNext := c.emit(machine.Instruction{Op: machine.OpJumpIfFalse})
		hits = append(hits, c.emit(machine.Instruction{Op: machine.OpJump}))
		c.instructions[tryNext].A = len(c.instructions)
	}
	return hits, misses, nil
}

func (c *bytecodeCompiler) compileMatch(subjectSlot int, match syntax.Expr) error {
	if subjectSlot < 0 {
		return c.compile(match)
	}
	c.emit(machine.Instruction{Op: machine.OpLoadLocal, A: subjectSlot})
	if err := c.compile(match); err != nil {
		return err
	}
	c.emit(machine.Instruction{Op: machine.OpEqual})
	return nil
}

func (c *bytecodeCompiler) patch(jumps []int, target int) {
	for _, jump := range jumps {
		c.instructions[jump].A = target
	}
}

func (c *bytecodeCompiler) compileFor(node *syntax.ForExpr) error {
	if err := c.compile(node.Source); err != nil {
		return err
	}
	resultType, ok := c.inferred.NodeTypes[node.ID]
	if !ok || !hasElem(resultType) ||
		(resultType.Kind() != machine.ArrayKind && resultType.Kind() != machine.DictKind) {
		return errors.New("cannot compile for with unresolved result type")
	}
	return c.compileLoop(node.KeyVariable, node.Variable, "", node.Where, &resultType, func() error {
		return c.compileYield(node, resultType)
	})
}

// compileLoop lays out a loop over the source on the stack: loop_init, the
// condition, body, loop_next. accumulator is the name a reduce folds into,
// and empty for a comprehension; a filtered item jumps straight to loop_next.
func (c *bytecodeCompiler) compileLoop(key, value, accumulator string, where syntax.Expr, resultType *machine.Type, body func() error) error {
	slot := c.bindLocal(value)
	defer c.unbindLocal(value)
	keySlot := c.bindLocal(key)
	defer c.unbindLocal(key)
	accSlot := c.bindLocal(accumulator)
	defer c.unbindLocal(accumulator)

	init := c.emit(machine.Instruction{Op: machine.OpLoopInit, B: slot, C: accSlot, D: keySlot, Type: resultType})
	loopStart := len(c.instructions)
	jumpFiltered := -1
	if where != nil {
		if err := c.compile(where); err != nil {
			return err
		}
		jumpFiltered = c.emit(machine.Instruction{Op: machine.OpJumpIfFalse})
	}
	if err := body(); err != nil {
		return err
	}
	next := c.emit(machine.Instruction{Op: machine.OpLoopNext, A: loopStart, Type: resultType})
	if jumpFiltered >= 0 {
		c.instructions[jumpFiltered].A = next
	}
	c.instructions[init].A = len(c.instructions)
	return nil
}

// compileYield emits the loop body and the instruction that takes its value.
// An outer clause of a nested comprehension spreads: what it yields is the
// inner loop's whole array, so the instruction checks it against the loop's own
// type and splices the elements.
func (c *bytecodeCompiler) compileYield(node *syntax.ForExpr, resultType machine.Type) error {
	// A dictionary comprehension pushes the key first, so loop_collect finds
	// the value on top and the key under it.
	pairs := 0
	if node.YieldKey != nil {
		if err := c.compile(node.YieldKey); err != nil {
			return err
		}
		pairs = 1
	}
	if err := c.compile(node.Yield); err != nil {
		return err
	}
	if node.Flatten {
		c.emit(machine.Instruction{Op: machine.OpLoopSpread, Type: &resultType})
		return nil
	}
	yieldType := elemOf(resultType)
	c.emit(machine.Instruction{Op: machine.OpLoopCollect, A: pairs, Type: &yieldType})
	return nil
}

// bindLocal reserves a local slot for a name bound by `for` or `reduce`. The
// slot is scoped: unbindLocal restores any outer binding of the same name. An
// empty name — the key of a loop over an array, the accumulator of a
// comprehension — takes no slot: it is NoKey, which is also NoAccumulator.
func (c *bytecodeCompiler) bindLocal(name string) int {
	if name == "" {
		return machine.NoKey
	}
	slot := c.nextLocal
	c.nextLocal++
	c.localIndex[name] = append(c.localIndex[name], slot)
	return slot
}

func (c *bytecodeCompiler) unbindLocal(name string) {
	if name == "" {
		return
	}
	slots := c.localIndex[name]
	c.localIndex[name] = slots[:len(slots)-1]
}

// compileReduce lays out: source, init, loop_init, condition, body,
// loop_collect, loop_next. It shares the loop opcodes with for; the
// accumulator slot in C is what makes it a fold instead of a mapping, and a
// filtered item jumps straight to loop_next, leaving the accumulator alone.
func (c *bytecodeCompiler) compileReduce(node *syntax.ReduceExpr) error {
	resultType, ok := c.inferred.NodeTypes[node.ID]
	if !ok || !resultType.IsConcrete() {
		return errors.New("cannot compile reduce with unresolved result type")
	}
	if err := c.compile(node.Source); err != nil {
		return err
	}
	if err := c.compile(node.Init); err != nil {
		return err
	}
	return c.compileLoop(node.KeyVariable, node.Variable, node.Accumulator, node.Where, &resultType, func() error {
		if err := c.compile(node.Body); err != nil {
			return err
		}
		c.emit(machine.Instruction{Op: machine.OpLoopCollect, Type: &resultType})
		return nil
	})
}

// compileLet evaluates each binding in order, so a later binding can read an
// earlier one, and keeps it bound through the body. A binding whose value is
// fixed at compile time becomes a constant rather than a local slot: nothing
// is computed and nothing is stored at run time.
func (c *bytecodeCompiler) compileLet(node *syntax.LetExpr) error {
	for _, binding := range node.Bindings {
		index, folded, err := c.foldBinding(binding.Value)
		if err != nil {
			return err
		}
		if folded {
			c.bindConstant(binding.Name, index)
			defer c.unbindConstant(binding.Name)
			c.folded++
			continue
		}
		if err := c.compile(binding.Value); err != nil {
			return err
		}
		slot := c.bindLocal(binding.Name)
		defer c.unbindLocal(binding.Name)
		c.emit(machine.Instruction{Op: machine.OpStoreLocal, A: slot})
	}
	return c.compile(node.Body)
}

func (c *bytecodeCompiler) bindConstant(name string, index int) {
	c.constIndex[name] = append(c.constIndex[name], index)
}

// unbindConstant undoes the bindConstant compileLet made.
func (c *bytecodeCompiler) unbindConstant(name string) {
	stack := c.constIndex[name]
	c.constIndex[name] = stack[:len(stack)-1]
}

func (c *bytecodeCompiler) compileIf(node *syntax.CallExpr) error {
	if len(node.Args) != 3 {
		return errors.New("if requires 3 arguments")
	}
	// The condition is a branch test of one match with no subject.
	_, misses, err := c.compileBranchTest(-1, node.Args[:1])
	if err != nil {
		return err
	}
	if err := c.compile(node.Args[1]); err != nil {
		return err
	}
	jumpEnd := c.emit(machine.Instruction{Op: machine.OpJump})
	c.patch(misses, len(c.instructions))
	if err := c.compile(node.Args[2]); err != nil {
		return err
	}
	c.instructions[jumpEnd].A = len(c.instructions)
	return nil
}

func (c *bytecodeCompiler) emit(instruction machine.Instruction) int {
	index := len(c.instructions)
	c.instructions = append(c.instructions, instruction)
	return index
}

// attachDocs copies the declaration prose onto the inferred parameters. A
// contract that declares arguments fixes the parameters to them, in its
// order — inferProgram takes argOrder as the argument order — so the i-th
// parameter is args[i]; one that declares none has no prose to copy.
func attachDocs(params []machine.Parameter, args []ArgSpec) {
	for i, arg := range args {
		params[i] = machine.NewParameter(params[i].Name(), params[i].Type(), arg.Doc)
	}
}
