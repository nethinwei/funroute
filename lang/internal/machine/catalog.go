package machine

import (
	"fmt"
	"sort"
)

const CatalogVersion = 1

// FunctionDescriptor is one entry of the catalog: the machine-readable half
// (types, signature, laziness) beside the host-written half. Everything here
// except Doc is derived from the registration, so nothing is written twice.
type FunctionDescriptor struct {
	Name      string `json:"name"`
	Signature string `json:"signature"`
	Params    []Type `json:"params"`
	Result    Type   `json:"result"`
	Special   string `json:"special,omitempty"`
	Variadic  bool   `json:"variadic,omitempty"`
	Doc       Doc    `json:"doc"`
}

type ValueTypeDescriptor struct {
	Type        Type   `json:"type"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type LanguageCatalog struct {
	Version         int                   `json:"version"`
	ArtifactVersion int                   `json:"artifact_version"`
	Functions       []FunctionDescriptor  `json:"functions"`
	SpecialForms    []FunctionDescriptor  `json:"special_forms"`
	ValueTypes      []ValueTypeDescriptor `json:"value_types"`
	Source          SourceSyntax          `json:"source"`
	// Nodes describes the shape of every ExprJSON node. The syntax layer fills
	// it in, from the same definitions its importer reads, so a front end that
	// builds nodes from it cannot disagree with the compiler.
	Nodes []NodeSchema `json:"nodes,omitempty"`
}

// SourceSyntax is the source-language contract a headless editor consumes.
// It is produced by the parser's own operator definitions, so a browser never
// needs a second hand-maintained precedence or desugaring table.
type SourceSyntax struct {
	ExprJSONVersion     int                        `json:"expr_json_version"`
	VariableNamePattern string                     `json:"variable_name_pattern"`
	Keywords            []string                   `json:"keywords"`
	Operators           []SourceOperatorDescriptor `json:"operators"`
}

// SourceOperatorDescriptor describes one source spelling and the canonical
// ExprJSON tree it expands into. Placeholders in Template are named operands.
type SourceOperatorDescriptor struct {
	Token         string             `json:"token"`
	Fixity        string             `json:"fixity"`
	Associativity string             `json:"associativity,omitempty"`
	Precedence    int                `json:"precedence"`
	Form          string             `json:"form,omitempty"`
	Operands      []string           `json:"operands"`
	Template      ExpressionTemplate `json:"template"`
}

// ExpressionTemplate is a small, typed pattern language for canonical
// ExprJSON. It intentionally covers only operator expansions.
type ExpressionTemplate struct {
	Placeholder string               `json:"$,omitempty"`
	Node        string               `json:"node,omitempty"`
	Name        string               `json:"name,omitempty"`
	Args        []ExpressionTemplate `json:"args,omitempty"`
	Int         *int64               `json:"int,omitempty"`
	Bool        *bool                `json:"bool,omitempty"`
}

// NodeSchema is one ExprJSON node as a front end needs to know it: its tag,
// the lazy form it belongs to (if the registry can switch it off), and its
// fields.
type NodeSchema struct {
	Node   string        `json:"node"`
	Form   string        `json:"form,omitempty"`
	Fields []FieldSchema `json:"fields"`
}

// FieldSchema is one field of a node. Kind is expr, exprs, name, text, list,
// or the literal kinds int, float, string and bool. A list's items have their
// own Fields. Binds exposes the parser's scope rule to headless editors, so a
// variable picker cannot drift from the compiler's understanding of locals.
type FieldSchema struct {
	Name     string        `json:"name"`
	Kind     string        `json:"kind"`
	Optional bool          `json:"optional,omitempty"`
	Role     string        `json:"role,omitempty"`
	Binds    []string      `json:"binds,omitempty"`
	Default  string        `json:"default,omitempty"`
	Min      int           `json:"min,omitempty"`
	Fields   []FieldSchema `json:"fields,omitempty"`
}

// normalizeDoc fills in what a host left out and refuses what it got wrong.
// Parameter labels are the one thing reflection cannot recover — Go drops
// parameter names — so a miscount is an error rather than a padded "参数 2".
func normalizeDoc(spec *FunctionSpec) error {
	doc := &spec.Doc
	if doc.Label == "" {
		doc.Label = spec.Name
	}
	if doc.Category == "" {
		doc.Category = namespaceOf(spec.Name)
	}
	if len(doc.Params) != 0 && len(doc.Params) != len(spec.Params) {
		return fmt.Errorf("function %s has %d parameter labels but %d typed parameters", spec.Name, len(doc.Params), len(spec.Params))
	}
	labels := make([]string, len(spec.Params))
	copy(labels, doc.Params)
	for i := range labels {
		if labels[i] == "" {
			labels[i] = fmt.Sprintf("参数 %d", i+1)
		}
	}
	doc.Params = labels
	if doc.Result == "" {
		doc.Result = "结果"
	}
	return nil
}

// Catalog describes exactly what this registry allows: its visible functions
// and the special forms it enables.
func (r *Registry) Catalog() LanguageCatalog {
	functions := r.visibleFunctions()
	sortFunctionDescriptors(functions)
	return LanguageCatalog{
		Version:         CatalogVersion,
		ArtifactVersion: ArtifactVersion,
		Functions:       functions,
		SpecialForms:    r.specialForms(),
		ValueTypes:      append(coreValueTypes(), r.handleValueTypes()...),
	}
}

// handleValueTypes lists the host's opaque types, so a console can show what
// its model functions pass between them.
func (r *Registry) handleValueTypes() []ValueTypeDescriptor {
	handles := r.Handles()
	out := make([]ValueTypeDescriptor, len(handles))
	for i, handle := range handles {
		out[i] = ValueTypeDescriptor{Type: handle, Label: handle.String(), Description: "宿主的不透明值，只能在函数之间传递"}
	}
	return out
}

func (r *Registry) visibleFunctions() []FunctionDescriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	functions := make([]FunctionDescriptor, 0, len(r.byKey))
	for _, function := range r.byKey {
		functions = append(functions, describeFunction(function))
	}
	return functions
}

func describeFunction(function *RegisteredFunction) FunctionDescriptor {
	params := make([]Type, len(function.Params))
	for i := range function.Params {
		params[i] = CloneType(function.Params[i])
	}
	special := ""
	signature := function.key
	variadic := false
	switch function.special {
	case specialIf:
		special = "if"
	case specialFallback:
		special = "fallback"
		signature = "fallback(T,T,...)->T"
		variadic = true
	}
	doc := function.Doc
	doc.Params = append([]string(nil), function.Doc.Params...)
	return FunctionDescriptor{
		Name:      function.Name,
		Signature: signature,
		Params:    params,
		Result:    CloneType(function.Result),
		Special:   special,
		Variadic:  variadic,
		Doc:       doc,
	}
}

func sortFunctionDescriptors(functions []FunctionDescriptor) {
	sort.Slice(functions, func(i, j int) bool {
		left, right := functions[i].Doc, functions[j].Doc
		if left.Category != right.Category {
			return left.Category < right.Category
		}
		if left.Label != right.Label {
			return left.Label < right.Label
		}
		return functions[i].Signature < functions[j].Signature
	})
}

func coreValueTypes() []ValueTypeDescriptor {
	return []ValueTypeDescriptor{
		{Type: BoolType, Label: "布尔值", Description: "true 或 false"},
		{Type: IntType, Label: "整数", Description: "有符号 64 位整数"},
		{Type: FloatType, Label: "浮点数", Description: "有限 float64"},
		{Type: StringType, Label: "字符串", Description: "UTF-8 字符串"},
		{Type: ArrayOf(TypeVar("T")), Label: "数组", Description: "元素必须同型"},
		{Type: DictOf(TypeVar("T")), Label: "字典", Description: "string key、value 必须同型"},
	}
}

var formDescriptors = map[Form]func() FunctionDescriptor{
	SwitchForm: switchSpecialForm,
	ForForm:    forSpecialForm,
	ReduceForm: reduceSpecialForm,
}

func (r *Registry) specialForms() []FunctionDescriptor {
	enabled := r.EnabledForms()
	out := make([]FunctionDescriptor, 0, len(enabled)+len(derivedForms))
	for _, form := range enabled {
		out = append(out, formDescriptors[form]())
	}
	// let and the derived forms need no switch: let binds names and the
	// derived forms expand to if, which the kernel always has. They are listed
	// so a console can offer them as first-class cards.
	out = append(out, letSpecialForm())
	for _, derived := range derivedForms {
		out = append(out, derived())
	}
	return out
}

var derivedForms = []func() FunctionDescriptor{andDerivedForm, orDerivedForm, notDerivedForm}

func letSpecialForm() FunctionDescriptor {
	return FunctionDescriptor{
		Name:      "let",
		Signature: "let(name = value, ..., body)",
		Special:   "let",
		Doc: Doc{
			Label:       "局部绑定",
			Description: "按顺序给名字绑定值，后面的绑定与主体可以引用前面的名字；名字不会成为程序参数。",
			Category:    "控制",
			Result:      "主体的值",
		},
	}
}

// andDerivedForm and friends are derived expressions in the sense of Scheme
// R7RS: the language defines them by their expansion into if, so they add no
// node type, no opcode and no inference rule, while staying named constructs
// that documentation and the drag-and-drop catalog can show.
func andDerivedForm() FunctionDescriptor {
	return derivedForm(
		"and", "and(bool,bool)->bool", "逻辑与",
		"两个条件同时成立。展开为 if(a,b,false)，因此右侧只在左侧成立时才求值。",
		[]string{"左条件", "右条件"},
	)
}

func orDerivedForm() FunctionDescriptor {
	return derivedForm(
		"or", "or(bool,bool)->bool", "逻辑或",
		"任一条件成立。展开为 if(a,true,b)，因此右侧只在左侧不成立时才求值。",
		[]string{"左条件", "右条件"},
	)
}

func notDerivedForm() FunctionDescriptor {
	return derivedForm(
		"not", "not(bool)->bool", "逻辑非",
		"条件取反。展开为 if(a,false,true)。",
		[]string{"条件"},
	)
}

func derivedForm(name, signature, label, description string, params []string) FunctionDescriptor {
	types := make([]Type, len(params))
	for i := range types {
		types[i] = BoolType
	}
	return FunctionDescriptor{
		Name:      name,
		Signature: signature,
		Params:    types,
		Result:    BoolType,
		Special:   name,
		Doc: Doc{
			Cost:        1,
			Label:       label,
			Description: description,
			Category:    "逻辑",
			Result:      "判断结果",
		},
	}
}

func reduceSpecialForm() FunctionDescriptor {
	t := TypeVar("T")
	r := TypeVar("R")
	return FunctionDescriptor{
		Name:      "reduce",
		Signature: "reduce(item in array<T> if condition, acc = R, R) -> R",
		Params:    []Type{ArrayOf(t), t, BoolType, r, r},
		Result:    r,
		Special:   "reduce",
		Doc: Doc{
			Cost:  1,
			Label: "逐项折叠",
			Description: "按顺序把每个元素并进累加器：每一步用当前元素和当前累加器算出下一个累加器，" +
				"所以它不止能求和 —— 取最大、计数、拼接都是它。可选的 if 条件先筛掉元素，被跳过的元素不改变累加器。" +
				"item 和 acc 是局部名称，不会成为外部参数。遍历次数有限，不引入递归。",
			Category: "控制",
			Result:   "折叠结果",
		},
	}
}

func switchSpecialForm() FunctionDescriptor {
	t := TypeVar("T")
	r := TypeVar("R")
	return FunctionDescriptor{
		Name:      "switch",
		Signature: "switch(value,case...[,default])->R",
		Params:    []Type{t, t, r, r},
		Result:    r,
		Variadic:  true,
		Special:   "switch",
		Doc: Doc{
			Cost:        1,
			Label:       "多分支选择",
			Description: "按顺序匹配值并返回第一个结果；通常需要默认结果，枚举成员全部覆盖时可省略。所有结果必须同类型。",
			Category:    "控制",
			Result:      "所选结果",
		},
	}
}

func forSpecialForm() FunctionDescriptor {
	t := TypeVar("T")
	r := TypeVar("R")
	return FunctionDescriptor{
		Name:      "for",
		Signature: "[result for item in array<T> if condition] -> array<R>",
		Params:    []Type{ArrayOf(t), t, BoolType, r},
		Result:    ArrayOf(r),
		Special:   "for",
		Doc: Doc{
			Cost:        1,
			Label:       "列表推导",
			Description: "遍历数组，按可选条件筛选并产出新数组；item 是局部名称，不会成为外部参数。写法是 [产出 for item in 输入 if 条件]。",
			Category:    "控制",
			Result:      "结果数组",
		},
	}
}
