package machine

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"unicode/utf8"

	"github.com/nethinwei/funroute/internal/money"
)

const CatalogVersion = 1

// FunctionDescriptor is one entry of the catalog: the machine-readable half
// (types, signature, laziness) beside the host-written half. Everything here
// except Doc is derived from the registration, so nothing is written twice.
// Only Registry.Catalog makes one; a host reads it or sends it as JSON.
type FunctionDescriptor struct {
	name, signature string
	params          []Type
	result          Type
	special         string
	variadic        bool
	doc             Doc
}

// Name is the function's name, shared by its overloads.
func (d FunctionDescriptor) Name() string { return d.name }

// Signature is the function's full identity, name(params)->result.
func (d FunctionDescriptor) Signature() string { return d.signature }

// Params is a copy of the parameter types.
func (d FunctionDescriptor) Params() []Type { return slices.Clone(d.params) }

// Result is the result type.
func (d FunctionDescriptor) Result() Type { return d.result }

// Special names the kernel construct the function is (if, fallback, …), or is
// empty for an ordinary function.
func (d FunctionDescriptor) Special() string { return d.special }

// Variadic reports whether the function takes any number of arguments.
func (d FunctionDescriptor) Variadic() bool { return d.variadic }

// Doc is a copy of what the host wrote about the function.
func (d FunctionDescriptor) Doc() Doc { return cloneDoc(d.doc) }

type functionJSON struct {
	Name      string `json:"name"`
	Signature string `json:"signature"`
	Params    []Type `json:"params"`
	Result    Type   `json:"result"`
	Special   string `json:"special,omitempty"`
	Variadic  bool   `json:"variadic,omitempty"`
	Doc       Doc    `json:"doc"`
}

func (d FunctionDescriptor) MarshalJSON() ([]byte, error) {
	return json.Marshal(functionJSON{d.name, d.signature, d.params, d.result, d.special, d.variadic, d.doc})
}

// FormDescriptor is one form a program can use: its name, how it is written,
// and what the language says about it. A form is syntax, not a function, so
// it has no signature — only the shape of how it is spelled.
type FormDescriptor struct {
	name, syntax string
	doc          Doc
}

// Name is the form's name.
func (d FormDescriptor) Name() string { return d.name }

// Syntax is how the form is written.
func (d FormDescriptor) Syntax() string { return d.syntax }

// Doc is a copy of what the language says about the form.
func (d FormDescriptor) Doc() Doc { return cloneDoc(d.doc) }

type formJSON struct {
	Name   string `json:"name"`
	Syntax string `json:"syntax"`
	Doc    Doc    `json:"doc"`
}

func (d FormDescriptor) MarshalJSON() ([]byte, error) {
	return json.Marshal(formJSON{d.name, d.syntax, d.doc})
}

// LanguageCatalog is what a registry offers: its functions and its forms,
// with what the host and the language say about each. Registry.Catalog makes
// it; its JSON is what a front end reads.
type LanguageCatalog struct {
	functions []FunctionDescriptor
	forms     []FormDescriptor
	// money is the declared money feature, for completing a currency code
	// and explaining a literal's minor units; nil when none is declared.
	money *money.MoneySpec
}

// Functions is every visible function, ordered by signature.
func (c LanguageCatalog) Functions() []FunctionDescriptor { return slices.Clone(c.functions) }

// SpecialForms is every form the registry enables, then the ones it always has.
func (c LanguageCatalog) SpecialForms() []FormDescriptor { return slices.Clone(c.forms) }

// Money is the declared money feature, if there is one.
func (c LanguageCatalog) Money() (money.MoneySpec, bool) {
	if c.money == nil {
		return money.MoneySpec{}, false
	}
	spec := *c.money
	spec.Currencies = slices.Clone(spec.Currencies)
	return spec, true
}

type catalogJSON struct {
	Version         int                  `json:"version"`
	ArtifactVersion int                  `json:"artifact_version"`
	Functions       []FunctionDescriptor `json:"functions"`
	SpecialForms    []FormDescriptor     `json:"special_forms"`
	Money           *money.MoneySpec     `json:"money,omitempty"`
}

func (c LanguageCatalog) MarshalJSON() ([]byte, error) {
	return json.Marshal(catalogJSON{CatalogVersion, ArtifactVersion, c.functions, c.forms, c.money})
}

// normalizeDoc fills in what a host left out and refuses what it got wrong.
// Parameter labels are the one thing reflection cannot recover — Go drops
// parameter names — so a miscount is an error rather than a padded "参数 2".
func normalizeDoc(spec *FunctionSpec) error {
	doc := &spec.Doc
	if err := validDocText(spec.Name, doc); err != nil {
		return err
	}
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

// validDocText refuses prose that is not UTF-8. It reaches a person through
// the catalog, the hover and the JSON a front end parses, and a string cut
// by bytes — label[:2] on 左侧补齐 — is the easy way to break it.
func validDocText(name string, doc *Doc) error {
	texts := append([]string{doc.Label, doc.Description, doc.Category, doc.Result}, doc.Params...)
	for _, example := range doc.Examples {
		texts = append(texts, example.Source, example.Result)
	}
	for _, text := range texts {
		if !utf8.ValidString(text) {
			return fmt.Errorf("function %s: its doc has text that is not valid UTF-8: %q", name, text)
		}
	}
	return nil
}

// Catalog describes exactly what this registry allows: its visible functions
// and the special forms it enables.
func (r *Registry) Catalog() LanguageCatalog {
	functions := r.visibleFunctions()
	sortFunctionDescriptors(functions)
	catalog := LanguageCatalog{functions: functions, forms: r.specialForms()}
	if amount, ok := r.Money(); ok {
		catalog.money = &amount
	}
	return catalog
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
		name:      function.Name,
		signature: signature,
		params:    params,
		result:    CloneType(function.Result),
		special:   function.special.String(),
		variadic:  variadic,
		doc:       cloneDoc(function.Doc),
	}
}

// sortFunctionDescriptors orders the functions by signature, so a catalog is
// the same bytes however the registry was filled.
func sortFunctionDescriptors(functions []FunctionDescriptor) {
	sort.Slice(functions, func(i, j int) bool { return functions[i].signature < functions[j].signature })
}

// formDescriptors are the forms a registry can switch on.
var formDescriptors = map[Form]FormDescriptor{
	SwitchForm: {
		name: "switch", syntax: "switch(value, case match => result, else => otherwise)",
		doc: Doc{Label: "多分支选择", Examples: formExamples["switch"], Category: "控制", Result: "所选结果",
			Description: "按顺序匹配值并返回第一个结果；通常需要默认结果，枚举成员全部覆盖时可省略。所有结果必须同类型。"},
	},
	ForForm: {
		name: "for", syntax: "[result for item in source if condition]",
		doc: Doc{Label: "列表推导", Examples: formExamples["for"], Category: "控制", Result: "结果数组",
			Description: "遍历数组，按可选条件筛选并产出新数组；item 是局部名称，不会成为外部参数。写法是 [产出 for item in 输入 if 条件]。"},
	},
	ReduceForm: {
		name: "reduce", syntax: "reduce(item in source if condition, acc = init, body)",
		doc: Doc{Label: "逐项折叠", Examples: formExamples["reduce"], Category: "控制", Result: "折叠结果",
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
	{name: "let", syntax: "let(name = value, body)", doc: Doc{Label: "局部绑定", Examples: formExamples["let"], Category: "控制", Result: "主体的值",
		Description: "按顺序给名字绑定值，后面的绑定与主体可以引用前面的名字；名字不会成为程序参数。"}},
	{name: "and", syntax: "a && b", doc: Doc{Label: "逻辑与", Examples: formExamples["and"], Category: "逻辑", Result: "判断结果",
		Description: "两个条件同时成立。展开为 if(a,b,false)，因此右侧只在左侧成立时才求值。"}},
	{name: "or", syntax: "a || b", doc: Doc{Label: "逻辑或", Examples: formExamples["or"], Category: "逻辑", Result: "判断结果",
		Description: "任一条件成立。展开为 if(a,true,b)，因此右侧只在左侧不成立时才求值。"}},
	{name: "not", syntax: "!a", doc: Doc{Label: "逻辑非", Examples: formExamples["not"], Category: "逻辑", Result: "判断结果",
		Description: "条件取反。展开为 if(a,false,true)。"}},
}

// usingForm is there wherever money is: it quotes rates for -> to convert
// at, and needs no switch of its own.
var usingForm = FormDescriptor{name: "using", syntax: "using(rate, …, body)", doc: Doc{Label: "局部汇率", Examples: formExamples["using"], Category: "金额", Result: "主体的值",
	Description: "主体里的 -> 只按这里列出的汇率换汇，外层的 using 不参与，所以规则用哪条汇率看 using 本身就知道；-> 与 fx 只能写在 using 里。" +
		"每个汇率是 fxrate 或 array<fxrate>：宿主作为参数传入的报价（[]FxRate 零拷贝）、两笔不同币种金额隐含的成交汇率（implied(settled, paid)，精确不舍入）、字面量 150 JPY / USD（1 美元换 150 日元），或从外层读出的 fx(USD, JPY)——要沿用外层的某个汇率就这样写进来，还能加点：fx(USD, JPY) * 102%。" +
		"同一货币对写了两次时后写的为准，反方向的报价按倒数用；同币种汇率恒为 1；没有这一对是 ErrNoFxRate，fallback 可以兜底。"}}

func (r *Registry) specialForms() []FormDescriptor {
	enabled := r.EnabledForms()
	out := make([]FormDescriptor, 0, len(enabled)+len(alwaysForms)+1)
	for _, form := range enabled {
		out = append(out, formDescriptors[form])
	}
	out = append(out, alwaysForms...)
	if _, declared := r.Money(); declared {
		out = append(out, usingForm)
	}
	return out
}
