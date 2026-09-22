package syntax

import (
	"fmt"

	"funroute/lang/internal/machine"
)

// Expr is deliberately small: literals, containers, free variables and calls.
// There are no statements, mutation, member access or implicit host access.
//
// Each node type below is the single description of that node. Its struct tags
// say how it is exchanged as ExprJSON, which names it binds and where they are
// visible, and what a front end needs to build one. The walker in walk.go
// reads the tags, so importing, exporting, collecting free variables and
// describing the node to a front end all follow from the definition — there is
// no second list to keep in step.
//
//	json:"name"        the ExprJSON field, with omitempty for an optional one
//	role:"..."         what a string field holds: var, fn, local or text
//	binds:"a,b"        the fields (of this node) in which a local name is visible;
//	                   "@rest" means the later items of the list it belongs to
//	default:"item"     the value a front end puts in a fresh node
//	min:"1"            the fewest items a list may hold
type Expr interface {
	exprNode()
	NodeID() int
	Position() int
	// kind is the ExprJSON node tag.
	kind() string
}

// checker is implemented by a node that has rules its tags cannot say; the
// parser and the importer both call it, so source and JSON obey the same ones.
type checker interface {
	check() error
}

// former is implemented by a node that is a lazy form the registry can turn
// off. The compiler refuses the node when its form is not enabled.
type former interface {
	Form() machine.Form
}

// LiteralExpr is exchanged as {"node":"int","int":1} and the like: the node
// tag is the value's kind and the field carries the value, so it is the one
// node the walker does not describe from tags.
type LiteralExpr struct {
	ID    int
	Pos   int
	Value machine.Value
}

func (*LiteralExpr) exprNode()       {}
func (e *LiteralExpr) NodeID() int   { return e.ID }
func (e *LiteralExpr) Position() int { return e.Pos }
func (e *LiteralExpr) kind() string  { return e.Value.Kind().String() }

type VariableExpr struct {
	ID   int    `json:"-"`
	Pos  int    `json:"-"`
	Name string `json:"name" role:"var" default:"value"`
}

func (*VariableExpr) exprNode()       {}
func (e *VariableExpr) NodeID() int   { return e.ID }
func (e *VariableExpr) Position() int { return e.Pos }
func (*VariableExpr) kind() string    { return "var" }

// EnumExpr is a member of a host-declared enum, written @adyen, or
// @channel.adyen when the member name alone is ambiguous. The member set lives
// in the contract, so the node carries names only; the compiler resolves which
// enum it belongs to and emits the member string as a constant.
type EnumExpr struct {
	ID     int    `json:"-"`
	Pos    int    `json:"-"`
	Enum   string `json:"enum,omitempty" role:"text"`
	Member string `json:"member" role:"text" default:"member"`
}

func (*EnumExpr) exprNode()       {}
func (e *EnumExpr) NodeID() int   { return e.ID }
func (e *EnumExpr) Position() int { return e.Pos }
func (*EnumExpr) kind() string    { return "enum" }

// Source reprints the reference the way it was written.
func (e *EnumExpr) Source() string {
	if e.Enum == "" {
		return "@" + e.Member
	}
	return "@" + e.Enum + "." + e.Member
}

func (e *EnumExpr) check() error {
	if !machine.IsValidVariableName(e.Member) || machine.IsReservedName(e.Member) {
		return fmt.Errorf("invalid enum member %q", e.Member)
	}
	if e.Enum != "" && !machine.IsValidVariableName(e.Enum) {
		return fmt.Errorf("invalid enum name %q", e.Enum)
	}
	return nil
}

type ArrayExpr struct {
	ID    int    `json:"-"`
	Pos   int    `json:"-"`
	Items []Expr `json:"items,omitempty"`
}

func (*ArrayExpr) exprNode()       {}
func (e *ArrayExpr) NodeID() int   { return e.ID }
func (e *ArrayExpr) Position() int { return e.Pos }
func (*ArrayExpr) kind() string    { return "array" }

type DictEntryExpr struct {
	Key   string `json:"key" role:"text" default:"key"`
	Value Expr   `json:"value"`
}

// DictExpr keeps its entries sorted by key, because dictionary order has no
// language semantics and the canonical JSON must not depend on how the
// program was written.
type DictExpr struct {
	ID      int             `json:"-"`
	Pos     int             `json:"-"`
	Entries []DictEntryExpr `json:"entries,omitempty" sort:"key"`
}

func (*DictExpr) exprNode()       {}
func (e *DictExpr) NodeID() int   { return e.ID }
func (e *DictExpr) Position() int { return e.Pos }
func (*DictExpr) kind() string    { return "dict" }

func (e *DictExpr) check() error {
	seen := make(map[string]bool, len(e.Entries))
	for _, entry := range e.Entries {
		if seen[entry.Key] {
			return fmt.Errorf("duplicate dictionary key %q", entry.Key)
		}
		seen[entry.Key] = true
	}
	return nil
}

type CallExpr struct {
	ID   int    `json:"-"`
	Pos  int    `json:"-"`
	Name string `json:"name" role:"fn"`
	Args []Expr `json:"args,omitempty"`
}

func (*CallExpr) exprNode()       {}
func (e *CallExpr) NodeID() int   { return e.ID }
func (e *CallExpr) Position() int { return e.Pos }
func (*CallExpr) kind() string    { return "call" }

// SwitchCaseExpr holds one branch. Match lists the values (subject mode) or the
// conditions (subjectless mode) that select this branch; any of them matching
// is enough, like Go's `case a, b:`.
type SwitchCaseExpr struct {
	Match  []Expr `json:"match" min:"1"`
	Result Expr   `json:"result"`
}

// SwitchExpr is switch(...). A nil Value is the subjectless form: each branch's
// Match entries are boolean conditions, which replaces a chain of nested ifs.
type SwitchExpr struct {
	ID      int              `json:"-"`
	Pos     int              `json:"-"`
	Value   Expr             `json:"value,omitempty"`
	Cases   []SwitchCaseExpr `json:"cases" min:"1"`
	Default Expr             `json:"default,omitempty"`
}

func (*SwitchExpr) exprNode()          {}
func (e *SwitchExpr) NodeID() int      { return e.ID }
func (e *SwitchExpr) Position() int    { return e.Pos }
func (*SwitchExpr) kind() string       { return "switch" }
func (*SwitchExpr) Form() machine.Form { return machine.SwitchForm }

// ForExpr is [yield for variable in source if where]. The names are locally
// bound and do not become program arguments; KeyVariable is set for a
// dictionary walk ([e for k, v in d]) and empty for an array.
type ForExpr struct {
	ID          int    `json:"-"`
	Pos         int    `json:"-"`
	Source      Expr   `json:"source"`
	Variable    string `json:"variable" role:"local" binds:"where,yield" default:"item"`
	KeyVariable string `json:"key_variable,omitempty" role:"local" binds:"where,yield"`
	Where       Expr   `json:"where,omitempty"`
	Yield       Expr   `json:"yield"`
}

func (*ForExpr) exprNode()          {}
func (e *ForExpr) NodeID() int      { return e.ID }
func (e *ForExpr) Position() int    { return e.Pos }
func (*ForExpr) kind() string       { return "for" }
func (*ForExpr) Form() machine.Form { return machine.ForForm }

func (e *ForExpr) check() error { return distinctNames(e.KeyVariable, e.Variable) }

// ReduceExpr is reduce(variable in source, accumulator from init, body): body
// folds every item of source into the accumulator. All three names are locally
// bound and never become program arguments.
type ReduceExpr struct {
	ID          int    `json:"-"`
	Pos         int    `json:"-"`
	Source      Expr   `json:"source"`
	Variable    string `json:"variable" role:"local" binds:"body" default:"item"`
	KeyVariable string `json:"key_variable,omitempty" role:"local" binds:"body"`
	Accumulator string `json:"accumulator" role:"local" binds:"body" default:"acc"`
	Init        Expr   `json:"init"`
	Body        Expr   `json:"body"`
}

func (*ReduceExpr) exprNode()          {}
func (e *ReduceExpr) NodeID() int      { return e.ID }
func (e *ReduceExpr) Position() int    { return e.Pos }
func (*ReduceExpr) kind() string       { return "reduce" }
func (*ReduceExpr) Form() machine.Form { return machine.ReduceForm }

func (e *ReduceExpr) check() error {
	return distinctNames(e.KeyVariable, e.Variable, e.Accumulator)
}

// LetBinding is one name = value pair. The name is visible to the bindings
// after it and to the body, like Scheme's let*.
type LetBinding struct {
	Name  string `json:"name" role:"local" binds:"@rest,body" default:"x"`
	Value Expr   `json:"value"`
}

// LetExpr is let(x = e1, y = e2, body). The names are local and never become
// program arguments.
type LetExpr struct {
	ID       int          `json:"-"`
	Pos      int          `json:"-"`
	Bindings []LetBinding `json:"bindings" min:"1"`
	Body     Expr         `json:"body"`
}

func (*LetExpr) exprNode()       {}
func (e *LetExpr) NodeID() int   { return e.ID }
func (e *LetExpr) Position() int { return e.Pos }
func (*LetExpr) kind() string    { return "let" }

func (e *LetExpr) check() error {
	names := make([]string, len(e.Bindings))
	for i, binding := range e.Bindings {
		names[i] = binding.Name
	}
	return distinctNames(names...)
}

// distinctNames rejects two locals of one node sharing a name. An empty name
// is an absent optional one and does not count.
func distinctNames(names ...string) error {
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if name == "" {
			continue
		}
		if seen[name] {
			return fmt.Errorf("the name %q is bound twice", name)
		}
		seen[name] = true
	}
	return nil
}

// nodeTypes lists every node the walker knows, by its ExprJSON tag. A literal
// has four tags, one per value kind; the others have one each.
var nodeTypes = []Expr{
	&LiteralExpr{}, &VariableExpr{}, &EnumExpr{}, &ArrayExpr{}, &DictExpr{}, &CallExpr{},
	&SwitchExpr{}, &ForExpr{}, &ReduceExpr{}, &LetExpr{},
}
