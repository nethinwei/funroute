package lang

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
)

// ExprJSONVersion is 2 since switch cases carry a list of matches and the
// subject became optional; version 1 documents are rejected rather than guessed
// at.
const ExprJSONVersion = 2

type exprJSONDocument struct {
	Version int       `json:"version"`
	Expr    *exprJSON `json:"expr"`
}

type exprJSON struct {
	Node     string          `json:"node"`
	Int      *int64          `json:"int,omitempty"`
	Float    *string         `json:"float,omitempty"`
	String   *string         `json:"string,omitempty"`
	Bool     *bool           `json:"bool,omitempty"`
	Name     string          `json:"name,omitempty"`
	Items    []*exprJSON     `json:"items,omitempty"`
	Entries  []exprJSONEntry `json:"entries,omitempty"`
	Args     []*exprJSON     `json:"args,omitempty"`
	Value    *exprJSON       `json:"value,omitempty"`
	Cases    []exprJSONCase  `json:"cases,omitempty"`
	Default  *exprJSON       `json:"default,omitempty"`
	Source   *exprJSON       `json:"source,omitempty"`
	Variable string          `json:"variable,omitempty"`
	Where    *exprJSON       `json:"where,omitempty"`
	Yield    *exprJSON       `json:"yield,omitempty"`

	Accumulator string    `json:"accumulator,omitempty"`
	Init        *exprJSON `json:"init,omitempty"`
	Body        *exprJSON `json:"body,omitempty"`
}

type exprJSONEntry struct {
	Key   string    `json:"key"`
	Value *exprJSON `json:"value"`
}

type exprJSONCase struct {
	Match  []*exprJSON `json:"match"`
	Result *exprJSON   `json:"result"`
}

// ExportExprJSON produces canonical, tagged JSON. Dictionary entries are
// sorted because dictionary order has no language semantics.
func ExportExprJSON(expr Expr) ([]byte, error) {
	node, err := exportJSONNode(expr)
	if err != nil {
		return nil, err
	}
	return json.Marshal(exprJSONDocument{Version: ExprJSONVersion, Expr: node})
}

func exportJSONNode(expr Expr) (*exprJSON, error) {
	switch node := expr.(type) {
	case *LiteralExpr:
		return exportJSONLiteral(node)
	case *VariableExpr:
		return &exprJSON{Node: "var", Name: node.Name}, nil
	case *ArrayExpr:
		return exportJSONArray(node)
	case *DictExpr:
		return exportJSONDict(node)
	case *CallExpr:
		return exportJSONCall(node)
	case *SwitchExpr:
		return exportJSONSwitch(node)
	case *ForExpr:
		return exportJSONFor(node)
	case *ReduceExpr:
		return exportJSONReduce(node)
	default:
		return nil, fmt.Errorf("unsupported expression node %T", expr)
	}
}

func exportJSONNodes(exprs []Expr) ([]*exprJSON, error) {
	out := make([]*exprJSON, len(exprs))
	for i, expr := range exprs {
		encoded, err := exportJSONNode(expr)
		if err != nil {
			return nil, err
		}
		out[i] = encoded
	}
	return out, nil
}

func exportJSONLiteral(node *LiteralExpr) (*exprJSON, error) {
	switch node.Value.kind {
	case IntKind:
		value := node.Value.i
		return &exprJSON{Node: "int", Int: &value}, nil
	case FloatKind:
		value := strconv.FormatFloat(node.Value.f, 'g', -1, 64)
		return &exprJSON{Node: "float", Float: &value}, nil
	case StringKind:
		value := node.Value.s
		return &exprJSON{Node: "string", String: &value}, nil
	case BoolKind:
		value := node.Value.b
		return &exprJSON{Node: "bool", Bool: &value}, nil
	default:
		return nil, fmt.Errorf("literal node has unsupported value type %s", node.Value.Type())
	}
}

func exportJSONArray(node *ArrayExpr) (*exprJSON, error) {
	items, err := exportJSONNodes(node.Items)
	if err != nil {
		return nil, err
	}
	return &exprJSON{Node: "array", Items: items}, nil
}

func exportJSONDict(node *DictExpr) (*exprJSON, error) {
	entries := append([]DictEntryExpr(nil), node.Entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	out := &exprJSON{Node: "dict", Entries: make([]exprJSONEntry, len(entries))}
	for i, entry := range entries {
		encoded, err := exportJSONNode(entry.Value)
		if err != nil {
			return nil, err
		}
		out.Entries[i] = exprJSONEntry{Key: entry.Key, Value: encoded}
	}
	return out, nil
}

func exportJSONCall(node *CallExpr) (*exprJSON, error) {
	args, err := exportJSONNodes(node.Args)
	if err != nil {
		return nil, err
	}
	return &exprJSON{Node: "call", Name: node.Name, Args: args}, nil
}

func exportJSONSwitch(node *SwitchExpr) (*exprJSON, error) {
	fallback, err := exportJSONNode(node.Default)
	if err != nil {
		return nil, err
	}
	out := &exprJSON{Node: "switch", Default: fallback, Cases: make([]exprJSONCase, len(node.Cases))}
	if node.Value != nil {
		if out.Value, err = exportJSONNode(node.Value); err != nil {
			return nil, err
		}
	}
	for i, item := range node.Cases {
		matches, err := exportJSONNodes(item.Match)
		if err != nil {
			return nil, err
		}
		result, err := exportJSONNode(item.Result)
		if err != nil {
			return nil, err
		}
		out.Cases[i] = exprJSONCase{Match: matches, Result: result}
	}
	return out, nil
}

func exportJSONFor(node *ForExpr) (*exprJSON, error) {
	source, err := exportJSONNode(node.Source)
	if err != nil {
		return nil, err
	}
	yield, err := exportJSONNode(node.Yield)
	if err != nil {
		return nil, err
	}
	out := &exprJSON{Node: "for", Source: source, Variable: node.Variable, Yield: yield}
	if node.Where != nil {
		out.Where, err = exportJSONNode(node.Where)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func exportJSONReduce(node *ReduceExpr) (*exprJSON, error) {
	parts, err := exportJSONNodes([]Expr{node.Source, node.Init, node.Body})
	if err != nil {
		return nil, err
	}
	return &exprJSON{
		Node: "reduce", Source: parts[0], Init: parts[1], Body: parts[2],
		Variable: node.Variable, Accumulator: node.Accumulator,
	}, nil
}

// ImportExprJSON validates and imports a canonical expression document.
func ImportExprJSON(data []byte) (Expr, error) {
	var document exprJSONDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode expression JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("decode expression JSON: trailing JSON value")
		}
		return nil, fmt.Errorf("decode expression JSON: %w", err)
	}
	if document.Version != ExprJSONVersion {
		return nil, fmt.Errorf("unsupported expression JSON version %d", document.Version)
	}
	if document.Expr == nil {
		return nil, fmt.Errorf("expression JSON is missing expr")
	}
	nextID := 1
	return importJSONNode(document.Expr, &nextID)
}

func importJSONNode(node *exprJSON, nextID *int) (Expr, error) {
	if node == nil {
		return nil, fmt.Errorf("expression JSON contains a null node")
	}
	id := *nextID
	(*nextID)++
	switch node.Node {
	case "int", "float", "string", "bool":
		return importJSONLiteral(node, id)
	case "var":
		return importJSONVariable(node, id)
	case "array":
		return importJSONArray(node, id, nextID)
	case "dict":
		return importJSONDict(node, id, nextID)
	case "call":
		return importJSONCall(node, id, nextID)
	case "switch":
		return importJSONSwitch(node, id, nextID)
	case "for":
		return importJSONFor(node, id, nextID)
	case "reduce":
		return importJSONReduce(node, id, nextID)
	default:
		return nil, fmt.Errorf("unknown expression node %q", node.Node)
	}
}

func importJSONLiteral(node *exprJSON, id int) (Expr, error) {
	switch node.Node {
	case "int":
		if node.Int == nil {
			return nil, fmt.Errorf("int node is missing int")
		}
		return &LiteralExpr{ID: id, Value: Int(*node.Int)}, nil
	case "float":
		return importJSONFloat(node, id)
	case "string":
		if node.String == nil {
			return nil, fmt.Errorf("string node is missing string")
		}
		return &LiteralExpr{ID: id, Value: String(*node.String)}, nil
	default:
		if node.Bool == nil {
			return nil, fmt.Errorf("bool node is missing bool")
		}
		return &LiteralExpr{ID: id, Value: Bool(*node.Bool)}, nil
	}
}

func importJSONFloat(node *exprJSON, id int) (Expr, error) {
	if node.Float == nil {
		return nil, fmt.Errorf("float node is missing float")
	}
	parsed, err := strconv.ParseFloat(*node.Float, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid float %q", *node.Float)
	}
	value, err := checkedFloat(parsed)
	if err != nil {
		return nil, err
	}
	return &LiteralExpr{ID: id, Value: value}, nil
}

func importJSONVariable(node *exprJSON, id int) (Expr, error) {
	if !functionNamePattern.MatchString(node.Name) || node.Name == "true" || node.Name == "false" {
		return nil, fmt.Errorf("invalid variable name %q", node.Name)
	}
	return &VariableExpr{ID: id, Name: node.Name}, nil
}

func importJSONArray(node *exprJSON, id int, nextID *int) (Expr, error) {
	items := make([]Expr, len(node.Items))
	for i, item := range node.Items {
		decoded, err := importJSONNode(item, nextID)
		if err != nil {
			return nil, fmt.Errorf("array item %d: %w", i, err)
		}
		items[i] = decoded
	}
	return &ArrayExpr{ID: id, Items: items}, nil
}

func importJSONDict(node *exprJSON, id int, nextID *int) (Expr, error) {
	entries := make([]DictEntryExpr, len(node.Entries))
	seen := map[string]bool{}
	for i, entry := range node.Entries {
		if seen[entry.Key] {
			return nil, fmt.Errorf("duplicate dictionary key %q", entry.Key)
		}
		seen[entry.Key] = true
		decoded, err := importJSONNode(entry.Value, nextID)
		if err != nil {
			return nil, fmt.Errorf("dictionary entry %q: %w", entry.Key, err)
		}
		entries[i] = DictEntryExpr{Key: entry.Key, Value: decoded}
	}
	return &DictExpr{ID: id, Entries: entries}, nil
}

func importJSONCall(node *exprJSON, id int, nextID *int) (Expr, error) {
	if !functionNamePattern.MatchString(node.Name) {
		return nil, fmt.Errorf("invalid function name %q", node.Name)
	}
	args := make([]Expr, len(node.Args))
	for i, arg := range node.Args {
		decoded, err := importJSONNode(arg, nextID)
		if err != nil {
			return nil, fmt.Errorf("call argument %d: %w", i, err)
		}
		args[i] = decoded
	}
	return &CallExpr{ID: id, Name: node.Name, Args: args}, nil
}

func importJSONSwitch(node *exprJSON, id int, nextID *int) (Expr, error) {
	if node.Default == nil || len(node.Cases) == 0 {
		return nil, fmt.Errorf("switch node requires at least one case and a default")
	}
	// A missing value is the subjectless form, whose matches are conditions.
	var value Expr
	var err error
	if node.Value != nil {
		if value, err = importJSONNode(node.Value, nextID); err != nil {
			return nil, fmt.Errorf("switch value: %w", err)
		}
	}
	cases, err := importJSONCases(node.Cases, nextID)
	if err != nil {
		return nil, err
	}
	fallback, err := importJSONNode(node.Default, nextID)
	if err != nil {
		return nil, fmt.Errorf("switch default: %w", err)
	}
	return &SwitchExpr{ID: id, Value: value, Cases: cases, Default: fallback}, nil
}

func importJSONCases(items []exprJSONCase, nextID *int) ([]SwitchCaseExpr, error) {
	cases := make([]SwitchCaseExpr, len(items))
	for i, item := range items {
		if len(item.Match) == 0 {
			return nil, fmt.Errorf("switch case %d has no match", i)
		}
		matches := make([]Expr, len(item.Match))
		for j, match := range item.Match {
			decoded, err := importJSONNode(match, nextID)
			if err != nil {
				return nil, fmt.Errorf("switch case %d match %d: %w", i, j, err)
			}
			matches[j] = decoded
		}
		result, err := importJSONNode(item.Result, nextID)
		if err != nil {
			return nil, fmt.Errorf("switch case %d result: %w", i, err)
		}
		cases[i] = SwitchCaseExpr{Match: matches, Result: result}
	}
	return cases, nil
}

func importJSONFor(node *exprJSON, id int, nextID *int) (Expr, error) {
	if node.Source == nil || node.Yield == nil {
		return nil, fmt.Errorf("for node requires source and yield")
	}
	if err := validLocalName(node.Variable); err != nil {
		return nil, fmt.Errorf("for variable: %w", err)
	}
	source, err := importJSONNode(node.Source, nextID)
	if err != nil {
		return nil, fmt.Errorf("for source: %w", err)
	}
	var where Expr
	if node.Where != nil {
		where, err = importJSONNode(node.Where, nextID)
		if err != nil {
			return nil, fmt.Errorf("for condition: %w", err)
		}
	}
	yield, err := importJSONNode(node.Yield, nextID)
	if err != nil {
		return nil, fmt.Errorf("for yield: %w", err)
	}
	return &ForExpr{ID: id, Source: source, Variable: node.Variable, Where: where, Yield: yield}, nil
}

func importJSONReduce(node *exprJSON, id int, nextID *int) (Expr, error) {
	if node.Source == nil || node.Init == nil || node.Body == nil {
		return nil, fmt.Errorf("reduce node requires source, init and body")
	}
	if err := validLocalName(node.Variable); err != nil {
		return nil, fmt.Errorf("reduce item: %w", err)
	}
	if err := validLocalName(node.Accumulator); err != nil {
		return nil, fmt.Errorf("reduce accumulator: %w", err)
	}
	if node.Variable == node.Accumulator {
		return nil, fmt.Errorf("reduce item and accumulator must have different names")
	}
	source, err := importJSONNode(node.Source, nextID)
	if err != nil {
		return nil, fmt.Errorf("reduce source: %w", err)
	}
	init, err := importJSONNode(node.Init, nextID)
	if err != nil {
		return nil, fmt.Errorf("reduce init: %w", err)
	}
	body, err := importJSONNode(node.Body, nextID)
	if err != nil {
		return nil, fmt.Errorf("reduce body: %w", err)
	}
	return &ReduceExpr{
		ID: id, Source: source, Variable: node.Variable,
		Accumulator: node.Accumulator, Init: init, Body: body,
	}, nil
}

func validLocalName(name string) error {
	if !functionNamePattern.MatchString(name) || reservedNames[name] {
		return fmt.Errorf("invalid local variable name %q", name)
	}
	return nil
}
