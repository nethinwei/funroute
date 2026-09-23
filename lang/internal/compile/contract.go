package compile

import (
	"fmt"

	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
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
}

func (o CompileOptions) argTypes() map[string]machine.Type {
	if len(o.Args) == 0 {
		return nil
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
	names := make([]string, len(o.Args))
	for i, arg := range o.Args {
		names[i] = arg.Name
	}
	return names
}

func (o CompileOptions) argDocs() map[string]string {
	docs := make(map[string]string, len(o.Args))
	for _, arg := range o.Args {
		if arg.Doc != "" {
			docs[arg.Name] = arg.Doc
		}
	}
	return docs
}

// ValidateContract checks a host contract without needing an expression. This
// lets consoles put contract authoring before expression authoring and reject a
// malformed ABI before an operator starts writing policy logic.
func ValidateContract(options CompileOptions) error {
	declared := make(map[string]bool, len(options.Args))
	for _, arg := range options.Args {
		if !machine.IsValidVariableName(arg.Name) || machine.IsReservedName(arg.Name) {
			return contractErrorf("invalid argument name %q", arg.Name)
		}
		if declared[arg.Name] {
			return contractErrorf("argument %q is declared twice", arg.Name)
		}
		if !arg.Type.IsConcrete() {
			return contractErrorf("argument %q has a non-concrete type: %s", arg.Name, arg.Type)
		}
		declared[arg.Name] = true
	}
	if options.Result != nil && !options.Result.IsConcrete() {
		return contractErrorf("the declared result type is not concrete: %s", *options.Result)
	}
	return nil
}

// validate checks the declared contract against the names the expression
// actually reads. An empty argument list still means inference for the library
// API; products that require a contract validate that policy at their edge.
func (o CompileOptions) validate(expr syntax.Expr) error {
	if err := ValidateContract(o); err != nil {
		return err
	}
	if len(o.Args) == 0 {
		return nil
	}
	declared := make(map[string]bool, len(o.Args))
	for _, arg := range o.Args {
		declared[arg.Name] = true
	}
	for _, read := range syntax.FirstReads(expr) {
		if !declared[read.Name] {
			return fmt.Errorf("%w: %w", machine.ErrContract,
				syntax.Around(read, "the expression reads %q but the contract does not declare it", read.Name))
		}
	}
	return nil
}
