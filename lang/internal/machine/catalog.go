package machine

import (
	"fmt"
	"regexp"
	"sort"
)

const CatalogVersion = 1

// FunctionDisplay contains presentation-only metadata. It never affects type
// inference, artifact identity or runtime evaluation.
type FunctionDisplay struct {
	Label       string             `json:"label"`
	Description string             `json:"description,omitempty"`
	Category    string             `json:"category"`
	Color       string             `json:"color,omitempty"`
	Icon        string             `json:"icon,omitempty"`
	DocsURL     string             `json:"docs_url,omitempty"`
	Keywords    []string           `json:"keywords,omitempty"`
	Parameters  []ParameterDisplay `json:"parameters,omitempty"`
	Result      ResultDisplay      `json:"result,omitempty"`
	Examples    []FunctionExample  `json:"examples,omitempty"`
	Hidden      bool               `json:"hidden,omitempty"`
	Order       int                `json:"order,omitempty"`
}

type ParameterDisplay struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
}

type ResultDisplay struct {
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
}

type FunctionExample struct {
	Title      string `json:"title"`
	Expression string `json:"expression"`
}

type FunctionDescriptor struct {
	Name      string          `json:"name"`
	Signature string          `json:"signature"`
	Params    []Type          `json:"params"`
	Result    Type            `json:"result"`
	Cost      uint64          `json:"cost"`
	Special   string          `json:"special,omitempty"`
	Variadic  bool            `json:"variadic,omitempty"`
	Display   FunctionDisplay `json:"display"`
}

type ValueTypeDescriptor struct {
	Type        Type   `json:"type"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Color       string `json:"color"`
}

type LanguageCatalog struct {
	Version      int                   `json:"version"`
	Functions    []FunctionDescriptor  `json:"functions"`
	SpecialForms []FunctionDescriptor  `json:"special_forms"`
	ValueTypes   []ValueTypeDescriptor `json:"value_types"`
	// Nodes describes the shape of every ExprJSON node. The syntax layer fills
	// it in, from the same definitions its importer reads, so a front end that
	// builds nodes from it cannot disagree with the compiler.
	Nodes []NodeSchema `json:"nodes,omitempty"`
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
// own Fields.
type FieldSchema struct {
	Name     string        `json:"name"`
	Kind     string        `json:"kind"`
	Optional bool          `json:"optional,omitempty"`
	Role     string        `json:"role,omitempty"`
	Default  string        `json:"default,omitempty"`
	Min      int           `json:"min,omitempty"`
	Fields   []FieldSchema `json:"fields,omitempty"`
}

var displayColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func normalizeFunctionDisplay(spec *FunctionSpec) error {
	display := &spec.Display
	if display.Label == "" {
		display.Label = spec.Name
	}
	if display.Category == "" {
		display.Category = "其他"
	}
	if display.Color == "" {
		display.Color = "#64748B"
	}
	if !displayColorPattern.MatchString(display.Color) {
		return fmt.Errorf("function %s display color must be #RRGGBB", spec.Name)
	}
	if len(display.Parameters) > len(spec.Params) {
		return fmt.Errorf("function %s has %d display parameters but only %d typed parameters", spec.Name, len(display.Parameters), len(spec.Params))
	}
	parameters := make([]ParameterDisplay, len(spec.Params))
	copy(parameters, display.Parameters)
	for i := range parameters {
		if parameters[i].Name == "" {
			parameters[i].Name = fmt.Sprintf("arg%d", i+1)
		}
		if parameters[i].Label == "" {
			parameters[i].Label = parameters[i].Name
		}
	}
	display.Parameters = parameters
	return nil
}

func cloneFunctionDisplay(in FunctionDisplay) FunctionDisplay {
	out := in
	out.Keywords = append([]string(nil), in.Keywords...)
	out.Parameters = append([]ParameterDisplay(nil), in.Parameters...)
	out.Examples = append([]FunctionExample(nil), in.Examples...)
	return out
}

// Catalog describes exactly what this registry allows: its visible functions
// and the special forms it enables.
func (r *Registry) Catalog() LanguageCatalog {
	functions := r.visibleFunctions()
	sortFunctionDescriptors(functions)
	return LanguageCatalog{
		Version:      CatalogVersion,
		Functions:    functions,
		SpecialForms: r.specialForms(),
		ValueTypes:   append(coreValueTypes(), r.handleValueTypes()...),
	}
}

// handleValueTypes lists the host's opaque types, so a console can show what
// its model functions pass between them.
func (r *Registry) handleValueTypes() []ValueTypeDescriptor {
	handles := r.Handles()
	out := make([]ValueTypeDescriptor, len(handles))
	for i, handle := range handles {
		out[i] = ValueTypeDescriptor{Type: handle, Label: handle.String(), Description: "宿主的不透明值，只能在函数之间传递", Color: "#7C3AED"}
	}
	return out
}

func (r *Registry) visibleFunctions() []FunctionDescriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	functions := make([]FunctionDescriptor, 0, len(r.byKey))
	for _, function := range r.byKey {
		if function.Display.Hidden {
			continue
		}
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
	if function.special == specialIf {
		special = "if"
	}
	return FunctionDescriptor{
		Name:      function.Name,
		Signature: function.key,
		Params:    params,
		Result:    CloneType(function.Result),
		Cost:      function.Cost,
		Special:   special,
		Display:   cloneFunctionDisplay(function.Display),
	}
}

func sortFunctionDescriptors(functions []FunctionDescriptor) {
	sort.Slice(functions, func(i, j int) bool {
		left, right := functions[i].Display, functions[j].Display
		if left.Category != right.Category {
			return left.Category < right.Category
		}
		if left.Order != right.Order {
			return left.Order < right.Order
		}
		if left.Label != right.Label {
			return left.Label < right.Label
		}
		return functions[i].Signature < functions[j].Signature
	})
}

func coreValueTypes() []ValueTypeDescriptor {
	return []ValueTypeDescriptor{
		{Type: BoolType, Label: "布尔值", Description: "true 或 false", Color: "#0EA5E9"},
		{Type: IntType, Label: "整数", Description: "有符号 64 位整数", Color: "#2563EB"},
		{Type: FloatType, Label: "浮点数", Description: "有限 float64", Color: "#0891B2"},
		{Type: StringType, Label: "字符串", Description: "UTF-8 字符串", Color: "#059669"},
		{Type: ArrayOf(TypeVar("T")), Label: "数组", Description: "元素必须同型", Color: "#D97706"},
		{Type: DictOf(TypeVar("T")), Label: "字典", Description: "string key、value 必须同型", Color: "#EA580C"},
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
		Display: FunctionDisplay{
			Label:       "局部绑定",
			Description: "按顺序给名字绑定值，后面的绑定与主体可以引用前面的名字；名字不会成为程序参数。",
			Category:    "控制",
			Color:       "#7C3AED",
			Icon:        "≔",
			Parameters: []ParameterDisplay{
				{Name: "bindings", Label: "绑定", Description: "name = value，可多个"},
				{Name: "body", Label: "主体", Description: "整体结果"},
			},
			Result: ResultDisplay{Label: "主体的值"},
			Order:  15,
		},
	}
}

// andDerivedForm and friends are derived expressions in the sense of Scheme
// R7RS: the language defines them by their expansion into if, so they add no
// node type, no opcode and no inference rule, while staying named constructs
// that documentation and the drag-and-drop catalog can show.
func andDerivedForm() FunctionDescriptor {
	return derivedForm(
		"and", "and(bool,bool)->bool", "逻辑与", "∧", 2,
		"两个条件同时成立。展开为 if(a,b,false)，因此右侧只在左侧成立时才求值。",
		[]ParameterDisplay{{Name: "left", Label: "左条件", Description: "bool"}, {Name: "right", Label: "右条件", Description: "bool"}},
	)
}

func orDerivedForm() FunctionDescriptor {
	return derivedForm(
		"or", "or(bool,bool)->bool", "逻辑或", "∨", 3,
		"任一条件成立。展开为 if(a,true,b)，因此右侧只在左侧不成立时才求值。",
		[]ParameterDisplay{{Name: "left", Label: "左条件", Description: "bool"}, {Name: "right", Label: "右条件", Description: "bool"}},
	)
}

func notDerivedForm() FunctionDescriptor {
	return derivedForm(
		"not", "not(bool)->bool", "逻辑非", "¬", 4,
		"条件取反。展开为 if(a,false,true)。",
		[]ParameterDisplay{{Name: "condition", Label: "条件", Description: "bool"}},
	)
}

func derivedForm(name, signature, label, icon string, order int, description string, params []ParameterDisplay) FunctionDescriptor {
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
		Cost:      1,
		Display: FunctionDisplay{
			Label:       label,
			Description: description,
			Category:    "逻辑",
			Color:       "#4338CA",
			Icon:        icon,
			Parameters:  params,
			Result:      ResultDisplay{Label: "判断结果", Description: "bool"},
			Order:       order,
		},
	}
}

func reduceSpecialForm() FunctionDescriptor {
	t := TypeVar("T")
	r := TypeVar("R")
	return FunctionDescriptor{
		Name:      "reduce",
		Signature: "reduce(array<T>,item,acc,R,R)->R",
		Params:    []Type{ArrayOf(t), t, r, r, r},
		Result:    r,
		Special:   "reduce",
		Cost:      1,
		Display: FunctionDisplay{
			Label:       "遍历累加",
			Description: "把数组元素逐个折叠进累加器；item 和 acc 都是局部名称，不会成为外部参数。遍历次数有限，不引入递归。",
			Category:    "控制",
			Color:       "#7C3AED",
			Icon:        "Σ",
			Parameters: []ParameterDisplay{
				{Name: "source", Label: "输入数组"},
				{Name: "item", Label: "元素局部名", Placeholder: "item"},
				{Name: "acc", Label: "累加器局部名", Placeholder: "acc"},
				{Name: "init", Label: "初始值"},
				{Name: "body", Label: "累加表达式", Description: "必须返回累加器类型"},
			},
			Result:   ResultDisplay{Label: "累加结果", Description: "R"},
			Examples: []FunctionExample{{Title: "金额求和", Expression: `reduce(prices,price,total,0,add(total,price))`}},
			Order:    50,
		},
	}
}

func switchSpecialForm() FunctionDescriptor {
	t := TypeVar("T")
	r := TypeVar("R")
	return FunctionDescriptor{
		Name:      "switch",
		Signature: "switch(value,match,result,...,default)->R",
		Params:    []Type{t, t, r, r},
		Result:    r,
		Variadic:  true,
		Special:   "switch",
		Cost:      1,
		Display: FunctionDisplay{
			Label:       "多分支选择",
			Description: "按顺序匹配值并返回第一个结果；最后一个参数是默认结果。所有结果必须同类型。",
			Category:    "控制",
			Color:       "#7C3AED",
			Icon:        "≡",
			Parameters: []ParameterDisplay{
				{Name: "value", Label: "待匹配值"},
				{Name: "match", Label: "匹配值"},
				{Name: "result", Label: "匹配结果"},
				{Name: "default", Label: "默认结果"},
			},
			Result:   ResultDisplay{Label: "所选结果", Description: "R"},
			Examples: []FunctionExample{{Title: "按国家路由", Expression: `switch(country,"SG","adyen","MY","stripe","fallback")`}},
			Order:    30,
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
		Cost:      1,
		Display: FunctionDisplay{
			Label:       "列表推导",
			Description: "遍历数组，按可选条件筛选并产出新数组；item 是局部名称，不会成为外部参数。写法是 [产出 for item in 输入 if 条件]。",
			Category:    "控制",
			Color:       "#7C3AED",
			Icon:        "∀",
			Parameters: []ParameterDisplay{
				{Name: "result", Label: "产出表达式"},
				{Name: "item", Label: "局部名称", Placeholder: "item"},
				{Name: "source", Label: "输入数组"},
				{Name: "condition", Label: "筛选条件", Description: "可省略"},
			},
			Result:   ResultDisplay{Label: "结果数组", Description: "array<R>"},
			Examples: []FunctionExample{{Title: "筛选健康渠道", Expression: `[channel for channel in channels if route.is_healthy_v1(channel)]`}},
			Order:    40,
		},
	}
}
