package compile

import (
	"fmt"
	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
	"sort"
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
	if registry == nil {
		return nil, compileError(fmt.Errorf("registry is required"))
	}
	if err := validateForms(expr, registry); err != nil {
		return nil, compileError(err)
	}
	if err := options.validate(syntax.FreeVariables(expr)); err != nil {
		return nil, err
	}
	inferred, err := inferProgram(expr, registry, options.argTypes(), options.argOrder(), options.Result)
	if err != nil {
		return nil, compileError(err)
	}
	attachDocs(inferred.Params, options.argDocs())
	inferred.ResultDoc = options.ResultDoc
	exprJSON, err := syntax.ExportExprJSON(expr)
	if err != nil {
		return nil, compileError(err)
	}
	compiler := newBytecodeCompiler(registry, inferred)
	if err := compiler.compile(expr); err != nil {
		return nil, compileError(err)
	}
	limit := options.MaxInstructions
	if limit == 0 {
		limit = 10_000
	}
	if len(compiler.instructions) > limit {
		return nil, compileError(fmt.Errorf("compiled program has %d instructions, limit is %d", len(compiler.instructions), limit))
	}
	artifact, err := sealArtifact(exprJSON, inferred, compiler)
	return artifact, compileError(err)
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
		compiler.argIndex[param.Name] = i
	}
	return compiler
}

func sealArtifact(exprJSON []byte, inferred *inference, compiler *bytecodeCompiler) (*machine.Artifact, error) {
	artifact := &machine.Artifact{
		Version:      machine.ArtifactVersion,
		ExprJSON:     exprJSON,
		Args:         inferred.Params,
		Result:       inferred.Result,
		ResultDoc:    inferred.ResultDoc,
		Constants:    compiler.constants,
		Calls:        compiler.calls,
		Locals:       compiler.nextLocal,
		MaxStack:     maxStackDepth(compiler.instructions),
		Instructions: compiler.instructions,
	}
	digest, err := machine.ArtifactDigest(artifact)
	if err != nil {
		return nil, err
	}
	artifact.Digest = digest
	return artifact, nil
}

type bytecodeCompiler struct {
	registry   *machine.Registry
	inferred   *inference
	argIndex   map[string]int
	localIndex map[string][]int
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
	case *syntax.LiteralExpr:
		return c.compileLiteral(node)
	case *syntax.EnumExpr:
		return c.compileLiteral(&syntax.LiteralExpr{ID: node.ID, Pos: node.Pos, Value: machine.String(node.Member)})
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
	case *syntax.RecordExpr:
		return c.compileRecord(node)
	case *syntax.FieldExpr:
		return c.compileField(node)
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

func (c *bytecodeCompiler) compileLiteral(node *syntax.LiteralExpr) error {
	constant, err := machine.ConstantFromValue(node.Value)
	if err != nil {
		return err
	}
	index := len(c.constants)
	c.constants = append(c.constants, constant)
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
	if err := c.compileAll(node.Items); err != nil {
		return err
	}
	typ, ok := c.inferred.NodeTypes[node.ID]
	if !ok || typ.Kind != machine.ArrayKind || typ.Elem == nil {
		return fmt.Errorf("cannot compile array with unresolved type")
	}
	c.emit(machine.Instruction{Op: machine.OpMakeArray, A: len(node.Items), Type: &typ})
	return nil
}

func (c *bytecodeCompiler) compileDict(node *syntax.DictExpr) error {
	entries := append([]syntax.DictEntryExpr(nil), node.Entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	keys := make([]string, len(entries))
	for i, entry := range entries {
		keys[i] = entry.Key
		if err := c.compile(entry.Value); err != nil {
			return err
		}
	}
	typ, ok := c.inferred.NodeTypes[node.ID]
	if !ok || typ.Kind != machine.DictKind || typ.Elem == nil {
		return fmt.Errorf("cannot compile dictionary with unresolved type")
	}
	c.emit(machine.Instruction{Op: machine.OpMakeDict, A: len(entries), Keys: keys, Type: &typ})
	return nil
}

// compileRecord evaluates the fields in the order the type declares and packs
// them; compileField turns the name into the index it resolved to, so reading
// a field is one instruction and no name survives into the bytecode.
func (c *bytecodeCompiler) compileRecord(node *syntax.RecordExpr) error {
	resultType, ok := c.inferred.NodeTypes[node.ID]
	if !ok || resultType.Kind != machine.RecordKind {
		return fmt.Errorf("cannot compile record with unresolved type")
	}
	for _, field := range node.Fields {
		if err := c.compile(field.Value); err != nil {
			return err
		}
	}
	c.emit(machine.Instruction{Op: machine.OpMakeRecord, A: len(node.Fields), Type: &resultType})
	return nil
}

func (c *bytecodeCompiler) compileField(node *syntax.FieldExpr) error {
	sourceType, ok := c.inferred.NodeTypes[node.Value.NodeID()]
	if !ok || sourceType.Kind != machine.RecordKind {
		return fmt.Errorf("cannot compile field access on a non-record")
	}
	index := sourceType.FieldIndex(node.Field)
	if index < 0 {
		return fmt.Errorf("%s has no field %q", sourceType.Summary(), node.Field)
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
	if function.NeedsBoundedArgs() {
		if err := c.requireBoundedArgs(node); err != nil {
			return err
		}
	}
	if err := c.compileAll(node.Args); err != nil {
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
	for _, item := range node.Cases {
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
		if c.boundedExpr(arg) {
			continue
		}
		return fmt.Errorf(
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
		return fmt.Errorf("fallback requires at least 2 arguments")
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
	if !ok || resultType.Elem == nil ||
		(resultType.Kind != machine.ArrayKind && resultType.Kind != machine.DictKind) {
		return fmt.Errorf("cannot compile for with unresolved result type")
	}
	slot := c.bindLocal(node.Variable)
	defer c.unbindLocal(node.Variable)
	keySlot := c.bindLoopKey(node.KeyVariable)
	defer c.unbindLoopKey(node.KeyVariable)

	init := c.emit(machine.Instruction{Op: machine.OpLoopInit, B: slot, C: machine.NoAccumulator, D: keySlot, Type: &resultType})
	loopStart := len(c.instructions)
	jumpFiltered := -1
	if node.Where != nil {
		if err := c.compile(node.Where); err != nil {
			return err
		}
		jumpFiltered = c.emit(machine.Instruction{Op: machine.OpJumpIfFalse})
	}
	if err := c.compileYield(node, resultType); err != nil {
		return err
	}
	next := c.emit(machine.Instruction{Op: machine.OpLoopNext, A: loopStart, Type: &resultType})
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
		spliced := machine.CloneType(resultType)
		c.emit(machine.Instruction{Op: machine.OpLoopSpread, Type: &spliced})
		return nil
	}
	yieldType := machine.CloneType(*resultType.Elem)
	c.emit(machine.Instruction{Op: machine.OpLoopCollect, A: pairs, Type: &yieldType})
	return nil
}

// bindLocal reserves a local slot for a name bound by `for` or `reduce`. The
// slot is scoped: unbindLocal restores any outer binding of the same name.
func (c *bytecodeCompiler) bindLocal(name string) int {
	slot := c.nextLocal
	c.nextLocal++
	c.localIndex[name] = append(c.localIndex[name], slot)
	return slot
}

// bindLoopKey reserves a slot for a dictionary walk's key variable, or reports
// noKey when the loop walks an array.
func (c *bytecodeCompiler) bindLoopKey(name string) int {
	if name == "" {
		return machine.NoKey
	}
	return c.bindLocal(name)
}

func (c *bytecodeCompiler) unbindLoopKey(name string) {
	if name != "" {
		c.unbindLocal(name)
	}
}

func (c *bytecodeCompiler) unbindLocal(name string) {
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
		return fmt.Errorf("cannot compile reduce with unresolved result type")
	}
	if err := c.compile(node.Source); err != nil {
		return err
	}
	if err := c.compile(node.Init); err != nil {
		return err
	}
	itemSlot := c.bindLocal(node.Variable)
	defer c.unbindLocal(node.Variable)
	keySlot := c.bindLoopKey(node.KeyVariable)
	defer c.unbindLoopKey(node.KeyVariable)
	accSlot := c.bindLocal(node.Accumulator)
	defer c.unbindLocal(node.Accumulator)

	init := c.emit(machine.Instruction{Op: machine.OpLoopInit, B: itemSlot, C: accSlot, D: keySlot, Type: &resultType})
	loopStart := len(c.instructions)
	jumpFiltered := -1
	if node.Where != nil {
		if err := c.compile(node.Where); err != nil {
			return err
		}
		jumpFiltered = c.emit(machine.Instruction{Op: machine.OpJumpIfFalse})
	}
	if err := c.compile(node.Body); err != nil {
		return err
	}
	c.emit(machine.Instruction{Op: machine.OpLoopCollect, Type: &resultType})
	next := c.emit(machine.Instruction{Op: machine.OpLoopNext, A: loopStart, Type: &resultType})
	if jumpFiltered >= 0 {
		c.instructions[jumpFiltered].A = next
	}
	c.instructions[init].A = len(c.instructions)
	return nil
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

func (c *bytecodeCompiler) unbindConstant(name string) {
	stack := c.constIndex[name]
	if len(stack) == 0 {
		return
	}
	c.constIndex[name] = stack[:len(stack)-1]
}

func (c *bytecodeCompiler) compileIf(node *syntax.CallExpr) error {
	if len(node.Args) != 3 {
		return fmt.Errorf("if requires 3 arguments")
	}
	if err := c.compile(node.Args[0]); err != nil {
		return err
	}
	jumpFalse := c.emit(machine.Instruction{Op: machine.OpJumpIfFalse})
	if err := c.compile(node.Args[1]); err != nil {
		return err
	}
	jumpEnd := c.emit(machine.Instruction{Op: machine.OpJump})
	c.instructions[jumpFalse].A = len(c.instructions)
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

// maxStackDepth walks the instructions once and reports the deepest the
// operand stack gets. Branches are followed linearly, which can overestimate
// where an `if` rejoins — the frame then reserves a little more than it needs,
// which is harmless, while an underestimate only costs the per-push check.
func maxStackDepth(instructions []machine.Instruction) int {
	depth, deepest := 0, 0
	for _, instruction := range instructions {
		depth += machine.StackEffect(instruction)
		if depth < 0 {
			depth = 0
		}
		if depth > deepest {
			deepest = depth
		}
	}
	return deepest
}

// attachDocs copies the declaration prose onto the inferred parameters.
func attachDocs(params []machine.Parameter, docs map[string]string) {
	for i := range params {
		params[i].Doc = docs[params[i].Name]
	}
}
