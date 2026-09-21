package lang

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

type CompileOptions struct {
	ArgTypes        map[string]Type
	MaxInstructions int
}

func CompileExpr(source string, registry *Registry, options CompileOptions) (*Artifact, error) {
	expr, err := Parse(source)
	if err != nil {
		return nil, err
	}
	return CompileAST(expr, registry, options)
}

func CompileAST(expr Expr, registry *Registry, options CompileOptions) (*Artifact, error) {
	if registry == nil {
		return nil, fmt.Errorf("registry is required")
	}
	if err := validateForms(expr, registry); err != nil {
		return nil, err
	}
	inferred, err := inferProgram(expr, registry, options.ArgTypes)
	if err != nil {
		return nil, err
	}
	exprJSON, err := ExportExprJSON(expr)
	if err != nil {
		return nil, err
	}
	compiler := newBytecodeCompiler(registry, inferred)
	compiler.markTail(expr)
	if err := compiler.compile(expr); err != nil {
		return nil, err
	}
	limit := options.MaxInstructions
	if limit == 0 {
		limit = 10_000
	}
	if len(compiler.instructions) > limit {
		return nil, fmt.Errorf("compiled program has %d instructions, limit is %d", len(compiler.instructions), limit)
	}
	return sealArtifact(exprJSON, inferred, compiler)
}

func newBytecodeCompiler(registry *Registry, inferred *inference) *bytecodeCompiler {
	compiler := &bytecodeCompiler{
		registry:   registry,
		inferred:   inferred,
		tail:       map[int]bool{},
		argIndex:   map[string]int{},
		localIndex: map[string][]int{},
		callIndex:  map[string]int{},
	}
	for i, param := range inferred.Params {
		compiler.argIndex[param.Name] = i
	}
	return compiler
}

// markTail flags the expressions whose value is the value of the whole program.
// A recur in one of those positions is a jump back to the start, so the VM can
// reuse its frame instead of nesting one.
func (c *bytecodeCompiler) markTail(expr Expr) {
	c.tail[expr.NodeID()] = true
	switch node := expr.(type) {
	case *CallExpr:
		if c.isIf(node) {
			c.markTail(node.Args[1])
			c.markTail(node.Args[2])
		}
	case *SwitchExpr:
		for _, item := range node.Cases {
			c.markTail(item.Result)
		}
		c.markTail(node.Default)
	}
}

func (c *bytecodeCompiler) isIf(node *CallExpr) bool {
	key, ok := c.inferred.Selections[node.ID]
	if !ok || len(node.Args) != 3 {
		return false
	}
	function, ok := c.registry.resolve(key)
	return ok && function.special == specialIf
}

// sealArtifact freezes the compiled program and stamps its digest.
func sealArtifact(exprJSON []byte, inferred *inference, compiler *bytecodeCompiler) (*Artifact, error) {
	artifact := &Artifact{
		Version:      ArtifactVersion,
		ExprJSON:     exprJSON,
		Args:         inferred.Params,
		Result:       inferred.Result,
		Constants:    compiler.constants,
		Calls:        compiler.calls,
		Locals:       compiler.nextLocal,
		Instructions: compiler.instructions,
	}
	digest, err := artifactDigest(artifact)
	if err != nil {
		return nil, err
	}
	artifact.Digest = digest
	return artifact, nil
}

type bytecodeCompiler struct {
	registry     *Registry
	inferred     *inference
	tail         map[int]bool
	argIndex     map[string]int
	localIndex   map[string][]int
	nextLocal    int
	callIndex    map[string]int
	constants    []Constant
	calls        []CallReference
	instructions []Instruction
}

func (c *bytecodeCompiler) compile(expr Expr) error {
	switch node := expr.(type) {
	case *LiteralExpr:
		return c.compileLiteral(node)
	case *VariableExpr:
		return c.compileVariable(node)
	case *ArrayExpr:
		return c.compileArray(node)
	case *DictExpr:
		return c.compileDict(node)
	case *SwitchExpr:
		return c.compileSwitch(node)
	case *ForExpr:
		return c.compileFor(node)
	case *ReduceExpr:
		return c.compileReduce(node)
	case *CallExpr:
		return c.compileCall(node)
	default:
		return fmt.Errorf("unsupported expression %T", expr)
	}
}

func (c *bytecodeCompiler) compileAll(exprs []Expr) error {
	for _, expr := range exprs {
		if err := c.compile(expr); err != nil {
			return err
		}
	}
	return nil
}

func (c *bytecodeCompiler) compileLiteral(node *LiteralExpr) error {
	constant, err := constantFromValue(node.Value)
	if err != nil {
		return err
	}
	index := len(c.constants)
	c.constants = append(c.constants, constant)
	c.emit(Instruction{Op: OpConstant, A: index})
	return nil
}

func (c *bytecodeCompiler) compileVariable(node *VariableExpr) error {
	if slots := c.localIndex[node.Name]; len(slots) > 0 {
		c.emit(Instruction{Op: OpLoadLocal, A: slots[len(slots)-1]})
		return nil
	}
	index, ok := c.argIndex[node.Name]
	if !ok {
		return fmt.Errorf("internal error: no argument index for %q", node.Name)
	}
	c.emit(Instruction{Op: OpLoadArg, A: index})
	return nil
}

func (c *bytecodeCompiler) compileArray(node *ArrayExpr) error {
	if err := c.compileAll(node.Items); err != nil {
		return err
	}
	typ, ok := c.inferred.NodeTypes[node.ID]
	if !ok || typ.Kind != ArrayKind || typ.Elem == nil {
		return fmt.Errorf("cannot compile array with unresolved type")
	}
	c.emit(Instruction{Op: OpMakeArray, A: len(node.Items), Type: &typ})
	return nil
}

func (c *bytecodeCompiler) compileDict(node *DictExpr) error {
	entries := append([]DictEntryExpr(nil), node.Entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	keys := make([]string, len(entries))
	for i, entry := range entries {
		keys[i] = entry.Key
		if err := c.compile(entry.Value); err != nil {
			return err
		}
	}
	typ, ok := c.inferred.NodeTypes[node.ID]
	if !ok || typ.Kind != DictKind || typ.Elem == nil {
		return fmt.Errorf("cannot compile dictionary with unresolved type")
	}
	c.emit(Instruction{Op: OpMakeDict, A: len(entries), Keys: keys, Type: &typ})
	return nil
}

func (c *bytecodeCompiler) compileCall(node *CallExpr) error {
	key, ok := c.inferred.Selections[node.ID]
	if !ok {
		return fmt.Errorf("internal error: no selected overload for %s", node.Name)
	}
	if key == "$recur" {
		return c.compileRecur(node)
	}
	function, ok := c.registry.resolve(key)
	if !ok {
		return fmt.Errorf("function disappeared during compilation: %s", key)
	}
	if function.special == specialIf {
		return c.compileIf(node)
	}
	if err := c.compileAll(node.Args); err != nil {
		return err
	}
	callIndex, ok := c.callIndex[key]
	if !ok {
		callIndex = len(c.calls)
		c.callIndex[key] = callIndex
		c.calls = append(c.calls, CallReference{Name: function.Name, Signature: key, Cost: function.Cost})
	}
	resultType := c.inferred.NodeTypes[node.ID]
	c.emit(Instruction{Op: OpCall, A: callIndex, B: len(node.Args), Type: &resultType})
	return nil
}

func (c *bytecodeCompiler) compileRecur(node *CallExpr) error {
	if err := c.compileAll(node.Args); err != nil {
		return err
	}
	resultType := c.inferred.NodeTypes[node.ID]
	position := 0
	if c.tail[node.ID] {
		position = tailCall
	}
	c.emit(Instruction{Op: OpRecur, B: len(node.Args), C: position, Type: &resultType})
	return nil
}

func (c *bytecodeCompiler) compileSwitch(node *SwitchExpr) error {
	var endJumps []int
	for _, item := range node.Cases {
		hits, misses, err := c.compileBranchTest(node.Value, item.Match)
		if err != nil {
			return err
		}
		c.patch(hits, len(c.instructions))
		if err := c.compile(item.Result); err != nil {
			return err
		}
		endJumps = append(endJumps, c.emit(Instruction{Op: OpJump}))
		c.patch(misses, len(c.instructions))
	}
	if err := c.compile(node.Default); err != nil {
		return err
	}
	c.patch(endJumps, len(c.instructions))
	return nil
}

// compileBranchTest emits the test for one branch. With a subject each match is
// compared with eq; without one the matches are conditions. Any match selects
// the branch, so all but the last jump forward on true.
func (c *bytecodeCompiler) compileBranchTest(subject Expr, matches []Expr) ([]int, []int, error) {
	var hits, misses []int
	for i, match := range matches {
		if err := c.compileMatch(subject, match); err != nil {
			return nil, nil, err
		}
		if i == len(matches)-1 {
			misses = append(misses, c.emit(Instruction{Op: OpJumpIfFalse}))
			break
		}
		tryNext := c.emit(Instruction{Op: OpJumpIfFalse})
		hits = append(hits, c.emit(Instruction{Op: OpJump}))
		c.instructions[tryNext].A = len(c.instructions)
	}
	return hits, misses, nil
}

func (c *bytecodeCompiler) compileMatch(subject Expr, match Expr) error {
	if subject == nil {
		return c.compile(match)
	}
	if err := c.compile(subject); err != nil {
		return err
	}
	if err := c.compile(match); err != nil {
		return err
	}
	c.emit(Instruction{Op: OpEqual})
	return nil
}

func (c *bytecodeCompiler) patch(jumps []int, target int) {
	for _, jump := range jumps {
		c.instructions[jump].A = target
	}
}

func (c *bytecodeCompiler) compileFor(node *ForExpr) error {
	if err := c.compile(node.Source); err != nil {
		return err
	}
	resultType, ok := c.inferred.NodeTypes[node.ID]
	if !ok || resultType.Kind != ArrayKind || resultType.Elem == nil {
		return fmt.Errorf("cannot compile for with unresolved result type")
	}
	slot := c.bindLocal(node.Variable)
	defer c.unbindLocal(node.Variable)

	init := c.emit(Instruction{Op: OpLoopInit, B: slot, C: noAccumulator, Type: &resultType})
	loopStart := len(c.instructions)
	jumpFiltered := -1
	if node.Where != nil {
		if err := c.compile(node.Where); err != nil {
			return err
		}
		jumpFiltered = c.emit(Instruction{Op: OpJumpIfFalse})
	}
	if err := c.compile(node.Yield); err != nil {
		return err
	}
	yieldType := cloneType(*resultType.Elem)
	c.emit(Instruction{Op: OpLoopCollect, Type: &yieldType})
	next := c.emit(Instruction{Op: OpLoopNext, A: loopStart, Type: &resultType})
	if jumpFiltered >= 0 {
		c.instructions[jumpFiltered].A = next
	}
	c.instructions[init].A = len(c.instructions)
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

func (c *bytecodeCompiler) unbindLocal(name string) {
	slots := c.localIndex[name]
	c.localIndex[name] = slots[:len(slots)-1]
}

// compileReduce lays out: source, init, loop_init, body, loop_collect,
// loop_next. It shares the loop opcodes with for; the accumulator slot in C is
// what makes it a fold instead of a mapping.
func (c *bytecodeCompiler) compileReduce(node *ReduceExpr) error {
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
	accSlot := c.bindLocal(node.Accumulator)
	defer c.unbindLocal(node.Accumulator)

	init := c.emit(Instruction{Op: OpLoopInit, B: itemSlot, C: accSlot, Type: &resultType})
	loopStart := len(c.instructions)
	if err := c.compile(node.Body); err != nil {
		return err
	}
	c.emit(Instruction{Op: OpLoopCollect, Type: &resultType})
	c.emit(Instruction{Op: OpLoopNext, A: loopStart, Type: &resultType})
	c.instructions[init].A = len(c.instructions)
	return nil
}

func (c *bytecodeCompiler) compileIf(node *CallExpr) error {
	if len(node.Args) != 3 {
		return fmt.Errorf("if requires 3 arguments")
	}
	if err := c.compile(node.Args[0]); err != nil {
		return err
	}
	jumpFalse := c.emit(Instruction{Op: OpJumpIfFalse})
	if err := c.compile(node.Args[1]); err != nil {
		return err
	}
	jumpEnd := c.emit(Instruction{Op: OpJump})
	c.instructions[jumpFalse].A = len(c.instructions)
	if err := c.compile(node.Args[2]); err != nil {
		return err
	}
	c.instructions[jumpEnd].A = len(c.instructions)
	return nil
}

func (c *bytecodeCompiler) emit(instruction Instruction) int {
	index := len(c.instructions)
	c.instructions = append(c.instructions, instruction)
	return index
}

func artifactDigest(artifact *Artifact) (string, error) {
	copyArtifact := *artifact
	copyArtifact.Digest = ""
	encoded, err := json.Marshal(copyArtifact)
	if err != nil {
		return "", fmt.Errorf("encode artifact digest: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
