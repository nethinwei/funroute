package lang

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
		ValueTypes:   coreValueTypes(),
	}
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

func describeFunction(function *registeredFunction) FunctionDescriptor {
	params := make([]Type, len(function.Params))
	for i := range function.Params {
		params[i] = cloneType(function.Params[i])
	}
	special := ""
	if function.special == specialIf {
		special = "if"
	}
	return FunctionDescriptor{
		Name:      function.Name,
		Signature: function.key,
		Params:    params,
		Result:    cloneType(function.Result),
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
	RecurForm:  recurSpecialForm,
}

func (r *Registry) specialForms() []FunctionDescriptor {
	enabled := r.EnabledForms()
	out := make([]FunctionDescriptor, 0, len(enabled))
	for _, form := range enabled {
		out = append(out, formDescriptors[form]())
	}
	return out
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

func recurSpecialForm() FunctionDescriptor {
	r := TypeVar("R")
	return FunctionDescriptor{
		Name:      "recur",
		Signature: "recur(args...)->R",
		Params:    []Type{r},
		Result:    r,
		Variadic:  true,
		Special:   "recur",
		Cost:      1,
		Display: FunctionDisplay{
			Label:       "自递归",
			Description: "用新的实参重新进入整个表达式，实参个数和类型必须与 args 一致。不终止的程序由 fuel 与递归深度限制拦截。",
			Category:    "工程师",
			Color:       "#B91C1C",
			Icon:        "↻",
			Parameters:  []ParameterDisplay{{Name: "args", Label: "新实参", Description: "与 args 同序同型"}},
			Result:      ResultDisplay{Label: "递归结果", Description: "与整体结果同型"},
			Examples:    []FunctionExample{{Title: "累加到 0", Expression: `if(eq(n,0),acc,recur(sub(n,1),add(acc,n)))`}},
			Order:       20,
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
		Signature: "for(array<T>,item,[condition],result)->array<R>",
		Params:    []Type{ArrayOf(t), t, BoolType, r},
		Result:    ArrayOf(r),
		Special:   "for",
		Cost:      1,
		Display: FunctionDisplay{
			Label:       "遍历 / 筛选",
			Description: "遍历数组，可选过滤条件并生成新数组；item 是局部名称，不会成为外部参数。",
			Category:    "控制",
			Color:       "#7C3AED",
			Icon:        "∀",
			Parameters: []ParameterDisplay{
				{Name: "source", Label: "输入数组"},
				{Name: "item", Label: "局部名称", Placeholder: "item"},
				{Name: "condition", Label: "过滤条件", Description: "可省略"},
				{Name: "result", Label: "生成结果"},
			},
			Result:   ResultDisplay{Label: "结果数组", Description: "array<R>"},
			Examples: []FunctionExample{{Title: "筛选健康渠道", Expression: `for(channels,channel,route.is_healthy@1(channel),channel)`}},
			Order:    40,
		},
	}
}
