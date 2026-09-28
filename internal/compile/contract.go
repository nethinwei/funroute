package compile

import (
	"github.com/nethinwei/funroute/internal/kit"
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// The contract — which arguments a program takes, in which order, with which
// types and prose, and what it returns — belongs to the host, not to the
// expression text. A payment console already stores a rule's metadata (id,
// version, effective window, approver, rollout, rollback pointer); argument
// types and documentation are the same kind of information, and keeping a
// second copy inside the source would be two parallel stores to drift apart.
//
// So the contract arrives as CompileOptions, and the expression stays an
// expression.

// ArgSpec is one declared argument. Order is the ABI: the compiled artifact
// takes its arguments in the order given here.
//
// An argument may be declared and never used by the expression, which is how a
// caller's ABI outlives the expression's need for the value. The reverse is an
// error: a name the expression reads but nothing declares has no type.
type ArgSpec struct {
	Name string
	Type machine.Type
	// Doc is prose for the console. It never reaches the bytecode and is
	// excluded from the artifact digest, so rewording it does not invalidate a
	// deployed artifact.
	Doc string
}

// CompileOptions carries the contract plus the compile-time limits.
type CompileOptions struct {
	// Args, when non-empty, fixes the arguments and their order. Leave it
	// empty to infer both: convenient for trying an expression out, but then
	// the same text compiled against different types is a different program.
	Args []ArgSpec

	// Result, when set, is unified with the expression's result rather than
	// compared to it afterwards, so it also settles ambiguities: a declared
	// array<string> is what makes a bare `[]` compilable.
	Result *machine.Type

	// ResultDoc is prose for the console, excluded from the digest like Doc.
	ResultDoc string

	MaxInstructions int

	// plain compiles every call as written: no aggregate fused, no source
	// hoisted — for the tests that hold the two to one answer.
	plain bool
	// hints is argTypes worked out once, read-only: by Bind for every
	// compile of the binding, and by build for the steps of one compile.
	hints map[string]machine.Type
	// unsealed leaves the digest out, for a Binding that loads the artifact
	// at once and computes the digest only when asked (Program.Artifact).
	unsealed bool
}

func (o CompileOptions) argTypes() map[string]machine.Type {
	if o.hints != nil || len(o.Args) == 0 {
		return o.hints
	}
	types := make(map[string]machine.Type, len(o.Args))
	for _, arg := range o.Args {
		types[arg.Name] = arg.Type
	}
	return types
}

// argOrder returns the declared order, or nil to use appearance order.
func (o CompileOptions) argOrder() []string {
	if len(o.Args) == 0 {
		return nil
	}
	return kit.Map(o.Args, func(arg ArgSpec) string { return arg.Name })
}

// ValidateContract checks a host contract without needing an expression. This
// lets consoles put contract authoring before expression authoring and reject a
// malformed ABI before an operator starts writing policy logic.
func ValidateContract(options CompileOptions) error {
	declared := make(map[string]bool, len(options.Args))
	for _, arg := range options.Args {
		if !machine.IsValidVariableName(arg.Name) || machine.IsReservedName(arg.Name) {
			return kit.Errorf(machine.ErrContract, "invalid argument name %q", arg.Name)
		}
		if declared[arg.Name] {
			return kit.Errorf(machine.ErrContract, "argument %q is declared twice", arg.Name)
		}
		if !arg.Type.IsConcrete() {
			return kit.Errorf(machine.ErrContract, "argument %q has a non-concrete type: %s", arg.Name, arg.Type)
		}
		declared[arg.Name] = true
	}
	if options.Result != nil && !options.Result.IsConcrete() {
		return kit.Errorf(machine.ErrContract, "the declared result type is not concrete: %s", *options.Result)
	}
	if options.MaxInstructions < 0 {
		return kit.Errorf(machine.ErrContract, "the instruction limit is %d; 0 means the default", options.MaxInstructions)
	}
	return nil
}

// validate checks the declared contract against the names the expression
// actually reads. An empty argument list still means inference for the library
// API; products that require a contract validate that policy at their edge.
func (o CompileOptions) validate(program programSurvey) error {
	if err := ValidateContract(o); err != nil {
		return err
	}
	if len(o.Args) == 0 {
		return nil
	}
	declared := o.argTypes()
	for _, read := range program.first {
		if _, ok := declared[read.Name]; !ok {
			return kit.Classify(machine.ErrContract, "",
				syntax.Around(read, "the expression reads %q but the contract does not declare it", read.Name))
		}
	}
	return nil
}

// programSurvey is what one walk of a program finds before it is typed:
// where each free variable is first read, in that order, every node whose
// subtree reads one, and how many nodes there are.
type programSurvey struct {
	first   []*syntax.VariableExpr
	readers map[int]bool
	nodes   int
	// selects says a selector is in the program, to be expanded before
	// anything else reads it.
	selects bool
}

// survey walks the program once, and rejects one that uses a special form its
// registry does not enable. It runs on the AST, so source and ExprJSON go
// through the same check and a console cannot smuggle a form in as JSON.
//
// Which nodes are forms is declared on the nodes themselves (syntax.Form), and
// the walk is the generic one, so a new form needs nothing here.
func survey(expr syntax.Expr, registry *machine.Registry) (programSurvey, error) {
	var disabled syntax.Expr
	nodes, selects := 0, false
	first, readers := syntax.Survey(expr, func(node syntax.Expr) {
		nodes++
		_, selector := node.(*syntax.SelectorExpr)
		selects = selects || selector
		if form, ok := syntax.FormOf(node); disabled == nil && ok && !registry.FormEnabled(form) {
			disabled = node
		}
	})
	if disabled != nil {
		form, _ := syntax.FormOf(disabled)
		return programSurvey{}, syntax.Around(disabled, "%s is not enabled in this registry", string(form))
	}
	return programSurvey{first: first, readers: readers, nodes: nodes, selects: selects}, nil
}

// expanded is expr with its selectors written out (syntax.ExpandSelectors),
// and the survey of what it became: the comprehensions a selector stands for
// are a form, which the registry must enable.
func expanded(expr syntax.Expr, registry *machine.Registry) (syntax.Expr, programSurvey, error) {
	surveyed, err := survey(expr, registry)
	if err != nil || !surveyed.selects {
		return expr, surveyed, err
	}
	if expr, err = syntax.ExpandSelectors(expr); err != nil {
		return nil, programSurvey{}, err
	}
	surveyed, err = survey(expr, registry)
	return expr, surveyed, err
}

// names is the free variables, in the order they are first read: the
// arguments of a program no contract orders.
func (p programSurvey) names() []string {
	return kit.Map(p.first, func(read *syntax.VariableExpr) string { return read.Name })
}
