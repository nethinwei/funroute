package lang

import "sort"

// Expr is deliberately small: literals, containers, free variables and calls.
// There are no statements, mutation, member access or implicit host access.
type Expr interface {
	exprNode()
	NodeID() int
	Position() int
}

type LiteralExpr struct {
	ID    int
	Pos   int
	Value Value
}

func (*LiteralExpr) exprNode()       {}
func (e *LiteralExpr) NodeID() int   { return e.ID }
func (e *LiteralExpr) Position() int { return e.Pos }

type VariableExpr struct {
	ID   int
	Pos  int
	Name string
}

func (*VariableExpr) exprNode()       {}
func (e *VariableExpr) NodeID() int   { return e.ID }
func (e *VariableExpr) Position() int { return e.Pos }

type ArrayExpr struct {
	ID    int
	Pos   int
	Items []Expr
}

func (*ArrayExpr) exprNode()       {}
func (e *ArrayExpr) NodeID() int   { return e.ID }
func (e *ArrayExpr) Position() int { return e.Pos }

type DictEntryExpr struct {
	Key   string
	Value Expr
}

type DictExpr struct {
	ID      int
	Pos     int
	Entries []DictEntryExpr
}

func (*DictExpr) exprNode()       {}
func (e *DictExpr) NodeID() int   { return e.ID }
func (e *DictExpr) Position() int { return e.Pos }

type CallExpr struct {
	ID   int
	Pos  int
	Name string
	Args []Expr
}

func (*CallExpr) exprNode()       {}
func (e *CallExpr) NodeID() int   { return e.ID }
func (e *CallExpr) Position() int { return e.Pos }

// SwitchCaseExpr holds one branch. Match lists the values (subject mode) or the
// conditions (subjectless mode) that select this branch; any of them matching
// is enough, like Go's `case a, b:`.
type SwitchCaseExpr struct {
	Match  []Expr
	Result Expr
}

// SwitchExpr is the typed representation of
// switch(value, match1, result1, ..., defaultResult).
// SwitchExpr is switch(...). A nil Value is the subjectless form: each branch's
// Match entries are boolean conditions, which replaces a chain of nested ifs.
type SwitchExpr struct {
	ID      int
	Pos     int
	Value   Expr
	Cases   []SwitchCaseExpr
	Default Expr
}

func (*SwitchExpr) exprNode()       {}
func (e *SwitchExpr) NodeID() int   { return e.ID }
func (e *SwitchExpr) Position() int { return e.Pos }

// ForExpr is the typed representation of
// for(source, item, [where], yield). The item name is locally bound and does
// not become a program argument.
type ForExpr struct {
	ID       int
	Pos      int
	Source   Expr
	Variable string
	Where    Expr
	Yield    Expr
}

func (*ForExpr) exprNode()       {}
func (e *ForExpr) NodeID() int   { return e.ID }
func (e *ForExpr) Position() int { return e.Pos }

// ReduceExpr is reduce(source, item, acc, init, body): body folds every item of
// source into the accumulator. Both item and acc are locally bound and never
// become program arguments.
type ReduceExpr struct {
	ID          int
	Pos         int
	Source      Expr
	Variable    string
	Accumulator string
	Init        Expr
	Body        Expr
}

func (*ReduceExpr) exprNode()       {}
func (e *ReduceExpr) NodeID() int   { return e.ID }
func (e *ReduceExpr) Position() int { return e.Pos }

// collectVariables returns the free variables in the order they first appear,
// which is the argument order of the compiled program.
func collectVariables(expr Expr) []string {
	collector := &variableCollector{seen: map[string]bool{}}
	collector.visit(expr, map[string]bool{})
	return collector.out
}

type variableCollector struct {
	seen map[string]bool
	out  []string
}

func (c *variableCollector) visit(current Expr, bound map[string]bool) {
	switch node := current.(type) {
	case *VariableExpr:
		if !bound[node.Name] && !c.seen[node.Name] {
			c.seen[node.Name] = true
			c.out = append(c.out, node.Name)
		}
	case *ArrayExpr:
		c.visitAll(node.Items, bound)
	case *DictExpr:
		c.visitDict(node, bound)
	case *CallExpr:
		c.visitAll(node.Args, bound)
	case *SwitchExpr:
		c.visitSwitch(node, bound)
	case *ForExpr:
		c.visitFor(node, bound)
	case *ReduceExpr:
		c.visitReduce(node, bound)
	}
}

func (c *variableCollector) visitAll(exprs []Expr, bound map[string]bool) {
	for _, expr := range exprs {
		c.visit(expr, bound)
	}
}

func (c *variableCollector) visitDict(node *DictExpr, bound map[string]bool) {
	entries := append([]DictEntryExpr(nil), node.Entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	for _, entry := range entries {
		c.visit(entry.Value, bound)
	}
}

func (c *variableCollector) visitSwitch(node *SwitchExpr, bound map[string]bool) {
	if node.Value != nil {
		c.visit(node.Value, bound)
	}
	for _, item := range node.Cases {
		c.visitAll(item.Match, bound)
		c.visit(item.Result, bound)
	}
	c.visit(node.Default, bound)
}

func (c *variableCollector) visitFor(node *ForExpr, bound map[string]bool) {
	c.visit(node.Source, bound)
	inner := bindNames(bound, node.Variable)
	if node.Where != nil {
		c.visit(node.Where, inner)
	}
	c.visit(node.Yield, inner)
}

func (c *variableCollector) visitReduce(node *ReduceExpr, bound map[string]bool) {
	c.visit(node.Source, bound)
	c.visit(node.Init, bound)
	c.visit(node.Body, bindNames(bound, node.Variable, node.Accumulator))
}

func bindNames(bound map[string]bool, names ...string) map[string]bool {
	inner := make(map[string]bool, len(bound)+len(names))
	for name, value := range bound {
		inner[name] = value
	}
	for _, name := range names {
		inner[name] = true
	}
	return inner
}
