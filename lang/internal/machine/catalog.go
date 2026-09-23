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

// FormDescriptor is one form a program can use: its name, how it is written,
// and what the language says about it. A form is syntax, not a function, so
// it has no signature — only the shape of how it is spelled.
type FormDescriptor struct {
	Name   string `json:"name"`
	Syntax string `json:"syntax"`
	Doc    Doc    `json:"doc"`
}

// LanguageCatalog is what a registry offers: its functions and its forms,
// with what the host and the language say about each.
type LanguageCatalog struct {
	Version         int                  `json:"version"`
	ArtifactVersion int                  `json:"artifact_version"`
	Functions       []FunctionDescriptor `json:"functions"`
	SpecialForms    []FormDescriptor     `json:"special_forms"`
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
	}
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
	// The catalog is read by people, so fallback shows the arity it takes
	// rather than the one registered key that stands for all of them.
	signature, variadic := function.key, function.special == specialFallback
	if variadic {
		signature = "fallback(T,T,...)->T"
	}
	return FunctionDescriptor{
		Name:      function.Name,
		Signature: signature,
		Params:    params,
		Result:    CloneType(function.Result),
		Special:   function.special.String(),
		Variadic:  variadic,
		Doc:       cloneDoc(function.Doc),
	}
}

// sortFunctionDescriptors orders the functions by signature, so a catalog is
// the same bytes however the registry was filled.
func sortFunctionDescriptors(functions []FunctionDescriptor) {
	sort.Slice(functions, func(i, j int) bool { return functions[i].Signature < functions[j].Signature })
}

// formDescriptors are the forms a registry can switch on.
var formDescriptors = map[Form]FormDescriptor{
	SwitchForm: {
		Name: "switch", Syntax: "switch(value, case match => result, else otherwise)",
		Doc: Doc{Label: "多分支选择", Category: "控制", Result: "所选结果",
			Description: "按顺序匹配值并返回第一个结果；通常需要默认结果，枚举成员全部覆盖时可省略。所有结果必须同类型。"},
	},
	ForForm: {
		Name: "for", Syntax: "[result for item in source if condition]",
		Doc: Doc{Label: "列表推导", Category: "控制", Result: "结果数组",
			Description: "遍历数组，按可选条件筛选并产出新数组；item 是局部名称，不会成为外部参数。写法是 [产出 for item in 输入 if 条件]。"},
	},
	ReduceForm: {
		Name: "reduce", Syntax: "reduce(item in source if condition, acc = init, body)",
		Doc: Doc{Label: "逐项折叠", Category: "控制", Result: "折叠结果",
			Description: "按顺序把每个元素并进累加器：每一步用当前元素和当前累加器算出下一个累加器，" +
				"所以它不止能求和 —— 取最大、计数、拼接都是它。可选的 if 条件先筛掉元素，被跳过的元素不改变累加器。" +
				"item 和 acc 是局部名称，不会成为外部参数。遍历次数有限，不引入递归。"},
	},
}

// alwaysForms need no switch: let only binds names, and and, or and not are
// derived expressions in the sense of Scheme R7RS — the language defines them
// by their expansion into if, which the kernel always has, so they add no
// node, opcode or inference rule while staying named constructs a reader can
// look up.
var alwaysForms = []FormDescriptor{
	{Name: "let", Syntax: "let(name = value, body)", Doc: Doc{Label: "局部绑定", Category: "控制", Result: "主体的值",
		Description: "按顺序给名字绑定值，后面的绑定与主体可以引用前面的名字；名字不会成为程序参数。"}},
	{Name: "and", Syntax: "a && b", Doc: Doc{Label: "逻辑与", Category: "逻辑", Result: "判断结果",
		Description: "两个条件同时成立。展开为 if(a,b,false)，因此右侧只在左侧成立时才求值。"}},
	{Name: "or", Syntax: "a || b", Doc: Doc{Label: "逻辑或", Category: "逻辑", Result: "判断结果",
		Description: "任一条件成立。展开为 if(a,true,b)，因此右侧只在左侧不成立时才求值。"}},
	{Name: "not", Syntax: "!a", Doc: Doc{Label: "逻辑非", Category: "逻辑", Result: "判断结果",
		Description: "条件取反。展开为 if(a,false,true)。"}},
}

func (r *Registry) specialForms() []FormDescriptor {
	enabled := r.EnabledForms()
	out := make([]FormDescriptor, 0, len(enabled)+len(alwaysForms))
	for _, form := range enabled {
		out = append(out, formDescriptors[form])
	}
	return append(out, alwaysForms...)
}
