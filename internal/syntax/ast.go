package syntax

import (
	"fmt"
	"strings"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/money"
)

// Expr is deliberately small: literals, containers, free variables and calls.
// There are no statements, mutation, member access or implicit host access.
//
// Each node type below is the single description of that node. Its struct tags
// say how it is exchanged as ExprJSON and which names it binds and where they
// are visible. The walker in walk.go reads the tags, so importing, exporting,
// collecting free variables, scope and the syntax tree all follow from the
// definition — there is no second list to keep in step.
//
//	json:"name"        the ExprJSON field, with omitempty for an optional one
//	role:"..."         what a string field holds: var, fn, local or text
//	binds:"a,b"        the fields (of this node) in which a local name is visible;
//	                   "@rest" means the later items of the list it belongs to
//	min:"1"            the fewest items a list may hold
//	sort:"key"         a list kept in key order, so one dictionary has one form
type Expr interface {
	exprNode()
	NodeID() int
	Position() int
	// Extent is the source the node was read from.
	Extent() Span
	setExtent(Span)
	// kind is the ExprJSON node tag.
	kind() string
}

// Span is the source a node was read from: bytes Start up to End, the
// parentheses around it included. Position is where a message about the node
// points; the span is all of it. A node imported from ExprJSON has no source,
// and its span is empty. Every node embeds one, untagged, so ExprJSON and the
// digest never see it.
type Span struct {
	Start, End int
}

func (s Span) Extent() Span { return s }

// HoldsCharacter reports whether the character starting at offset is in the
// span, as a hover asks: the span's end is the first byte after it.
func (s Span) HoldsCharacter(offset int) bool { return s.Start <= offset && offset < s.End }

// HoldsCursor reports whether a cursor at offset is in the span, as scope and
// completion ask: a cursor right after the last character is still in it.
func (s Span) HoldsCursor(offset int) bool { return s.Start <= offset && offset <= s.End }

func (s *Span) setExtent(extent Span) { *s = extent }

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
	ID  int
	Pos int
	Span
	Value machine.Value
}

func (*LiteralExpr) exprNode()       {}
func (e *LiteralExpr) NodeID() int   { return e.ID }
func (e *LiteralExpr) Position() int { return e.Pos }
func (e *LiteralExpr) kind() string  { return e.Value.Kind().String() }

type VariableExpr struct {
	ID  int `json:"-"`
	Pos int `json:"-"`
	Span
	Name string `json:"name" role:"var"`
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
	ID  int `json:"-"`
	Pos int `json:"-"`
	Span
	Enum   string `json:"enum,omitempty" role:"text"`
	Member string `json:"member" role:"text"`
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
	// A member follows @, so it may be shaped like a code: @CARD is no
	// currency.
	if !machine.IsValidFieldName(e.Member) {
		return fmt.Errorf("invalid enum member %q", e.Member)
	}
	if e.Enum != "" && !machine.IsValidFieldName(e.Enum) {
		return fmt.Errorf("invalid enum name %q", e.Enum)
	}
	return nil
}

type ArrayExpr struct {
	ID  int `json:"-"`
	Pos int `json:"-"`
	Span
	Items []Expr `json:"items,omitempty"`
}

func (*ArrayExpr) exprNode()       {}
func (e *ArrayExpr) NodeID() int   { return e.ID }
func (e *ArrayExpr) Position() int { return e.Pos }
func (*ArrayExpr) kind() string    { return "array" }

type DictEntryExpr struct {
	Key   string `json:"key" role:"text"`
	Value Expr   `json:"value"`
}

// DictExpr keeps its entries sorted by key, because dictionary order has no
// language semantics and the canonical JSON must not depend on how the
// program was written.
type DictExpr struct {
	ID  int `json:"-"`
	Pos int `json:"-"`
	Span
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

// RecordFieldExpr is one field of a record literal. Unlike a dictionary entry
// the name is an identifier, and unlike a dictionary the order is kept: it is
// part of the record's type.
type RecordFieldExpr struct {
	Name  string `json:"name" role:"text"`
	Value Expr   `json:"value"`
}

// RecordExpr is {amount: 1200, currency: "SGD"}: a fixed set of named fields,
// each with its own type. A dictionary literal writes its keys as strings
// ({"a": 1}) and every value shares one type; a record writes them as names
// and every field has its own.
type RecordExpr struct {
	ID  int `json:"-"`
	Pos int `json:"-"`
	Span
	Fields []RecordFieldExpr `json:"fields" min:"1"`
}

func (*RecordExpr) exprNode()       {}
func (e *RecordExpr) NodeID() int   { return e.ID }
func (e *RecordExpr) Position() int { return e.Pos }
func (*RecordExpr) kind() string    { return "record" }

func (e *RecordExpr) check() error { return checkRecordFields(e.Fields) }

func checkRecordFields(fields []RecordFieldExpr) error {
	seen := make(map[string]bool, len(fields))
	for _, field := range fields {
		if !machine.IsValidFieldName(field.Name) {
			return fmt.Errorf("invalid record field name %q", field.Name)
		}
		if seen[field.Name] {
			return fmt.Errorf("duplicate record field %q", field.Name)
		}
		seen[field.Name] = true
	}
	return nil
}

// RecordUpdateExpr is order with {amount: 1}: the record Base with some of its
// fields replaced. It cannot be sugar for a record literal, because which
// fields Base has is known only once its type is. The result has Base's type:
// every field named must be one of Base's, with a value of that field's type.
// A nested field is replaced by nesting: b with {customer: b.customer with {amount: 1}}.
type RecordUpdateExpr struct {
	ID  int `json:"-"`
	Pos int `json:"-"`
	Span
	Base   Expr              `json:"base"`
	Fields []RecordFieldExpr `json:"fields" min:"1"`
}

func (*RecordUpdateExpr) exprNode()       {}
func (e *RecordUpdateExpr) NodeID() int   { return e.ID }
func (e *RecordUpdateExpr) Position() int { return e.Pos }
func (*RecordUpdateExpr) kind() string    { return "record_update" }
func (e *RecordUpdateExpr) check() error  { return checkRecordFields(e.Fields) }

// FieldExpr is r.field. The field name is resolved to a position in the
// record's type at compile time, so nothing is looked up while the rule runs.
type FieldExpr struct {
	ID  int `json:"-"`
	Pos int `json:"-"`
	Span
	Value Expr   `json:"value"`
	Field string `json:"field" role:"text"`
}

func (e *FieldExpr) check() error {
	if !machine.IsValidFieldName(e.Field) {
		return fmt.Errorf("invalid record field name %q", e.Field)
	}
	return nil
}

func (*FieldExpr) exprNode()       {}
func (e *FieldExpr) NodeID() int   { return e.ID }
func (e *FieldExpr) Position() int { return e.Pos }
func (*FieldExpr) kind() string    { return "field" }

type CallExpr struct {
	ID  int `json:"-"`
	Pos int `json:"-"`
	Span
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
	ID  int `json:"-"`
	Pos int `json:"-"`
	Span
	Value   Expr             `json:"value,omitempty"`
	Cases   []SwitchCaseExpr `json:"cases" min:"1"`
	Default Expr             `json:"default,omitempty"`
}

func (*SwitchExpr) exprNode()          {}
func (e *SwitchExpr) NodeID() int      { return e.ID }
func (e *SwitchExpr) Position() int    { return e.Pos }
func (*SwitchExpr) kind() string       { return "switch" }
func (*SwitchExpr) Form() machine.Form { return machine.SwitchForm }

// ForExpr is [yield for variable in source if where], or
// {yield_key: yield for variable in source if where} when it builds a
// dictionary. The names are locally bound and do not become program arguments;
// KeyVariable is set for a dictionary walk ([e for k, v in d]) and empty for
// an array.
type ForExpr struct {
	ID  int `json:"-"`
	Pos int `json:"-"`
	Span
	Source      Expr   `json:"source"`
	Variable    string `json:"variable" role:"local" binds:"where,yield_key,yield"`
	KeyVariable string `json:"key_variable,omitempty" role:"local" binds:"where,yield_key,yield"`
	Where       Expr   `json:"where,omitempty"`
	// YieldKey turns the comprehension into a dictionary one: with it the
	// result is dict<V> keyed by this expression, without it array<V>.
	YieldKey Expr `json:"yield_key,omitempty"`
	Yield    Expr `json:"yield"`
	// Flatten says this loop's yield is itself an array to be spliced, which
	// is how [f(x, y) for x in xs for y in ys] nests: every clause but the
	// innermost carries it, so the result is one flat array instead of an
	// array of arrays. Set only by the parser (and by ExprJSON that says so).
	Flatten bool `json:"flatten,omitempty"`
}

func (*ForExpr) exprNode()          {}
func (e *ForExpr) NodeID() int      { return e.ID }
func (e *ForExpr) Position() int    { return e.Pos }
func (*ForExpr) kind() string       { return "for" }
func (*ForExpr) Form() machine.Form { return machine.ForForm }

func (e *ForExpr) check() error { return distinctNames(e.KeyVariable, e.Variable) }

// ReduceExpr is reduce(variable in source if where, accumulator = init, body):
// body folds every item the filter keeps into the accumulator. All three names
// are locally bound and never become program arguments. Where is optional and
// sees the loop variables but not the accumulator, because an item is filtered
// before it is folded.
type ReduceExpr struct {
	ID  int `json:"-"`
	Pos int `json:"-"`
	Span
	Source      Expr   `json:"source"`
	Variable    string `json:"variable" role:"local" binds:"where,body"`
	KeyVariable string `json:"key_variable,omitempty" role:"local" binds:"where,body"`
	Where       Expr   `json:"where,omitempty"`
	Accumulator string `json:"accumulator" role:"local" binds:"body"`
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
	Name  string `json:"name" role:"local" binds:"@rest,body"`
	Value Expr   `json:"value"`
}

// LetExpr is let(x = e1, y = e2, body). The names are local and never become
// program arguments.
type LetExpr struct {
	ID  int `json:"-"`
	Pos int `json:"-"`
	Span
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

// MoneyExpr is an amount written in its currency: USD 1.70, EUR -0.05. The
// amount stays the decimal it was written as — how many minor units it is
// depends on the currency's places, which are the registry's, and ExprJSON
// must not depend on a registry.
type MoneyExpr struct {
	ID  int `json:"-"`
	Pos int `json:"-"`
	Span
	Currency string `json:"currency" role:"text"`
	Amount   string `json:"amount" role:"text"`
}

func (*MoneyExpr) exprNode()       {}
func (e *MoneyExpr) NodeID() int   { return e.ID }
func (e *MoneyExpr) Position() int { return e.Pos }
func (*MoneyExpr) kind() string    { return "money" }

func (e *MoneyExpr) check() error {
	if !money.IsCurrencyCode(e.Currency) {
		return fmt.Errorf("invalid currency code %q", e.Currency)
	}
	digits, negative := strings.CutPrefix(e.Amount, "-")
	if !plainDecimal(digits) {
		return fmt.Errorf("invalid amount %q: write a plain decimal such as 1.70", e.Amount)
	}
	if negative && strings.Trim(digits, "0.") == "" {
		return fmt.Errorf("invalid amount %q: an amount has no negative zero", e.Amount)
	}
	return nil
}

// RatioExpr is a ratio written with its unit: 2.9% or 25bps. Like money it
// keeps the decimal it was written as.
type RatioExpr struct {
	ID  int `json:"-"`
	Pos int `json:"-"`
	Span
	Value string `json:"value" role:"text"`
	Unit  string `json:"unit" role:"text"`
}

func (*RatioExpr) exprNode()       {}
func (e *RatioExpr) NodeID() int   { return e.ID }
func (e *RatioExpr) Position() int { return e.Pos }
func (*RatioExpr) kind() string    { return "ratio" }

func (e *RatioExpr) check() error {
	if e.Unit != "%" && e.Unit != "bps" {
		return fmt.Errorf("invalid ratio unit %q: write %% or bps", e.Unit)
	}
	if !plainDecimal(e.Value) {
		return fmt.Errorf("invalid ratio %q: write a plain decimal such as 2.9", e.Value)
	}
	return nil
}

// Scale is the power of ten the ratio's value is divided by: 2 for a percent,
// 4 for a basis point.
func (e *RatioExpr) Scale() int {
	if e.Unit == "bps" {
		return 4
	}
	return 2
}

// plainDecimal is digits with at most one point between them.
func plainDecimal(text string) bool {
	whole, fraction, hasPoint := strings.Cut(text, ".")
	return digitsOnly(whole) && (!hasPoint || digitsOnly(fraction))
}

func digitsOnly(text string) bool {
	if text == "" {
		return false
	}
	for i := 0; i < len(text); i++ {
		if text[i] < '0' || text[i] > '9' {
			return false
		}
	}
	return true
}

// CurrencyExpr is a currency, written as its code: USD. Currencies are the
// language's own, not an enum a contract brings, so a name shaped like a code
// is always one, and no variable may take such a name.
type CurrencyExpr struct {
	ID  int `json:"-"`
	Pos int `json:"-"`
	Span
	Code string `json:"code" role:"text"`
}

func (*CurrencyExpr) exprNode()       {}
func (e *CurrencyExpr) NodeID() int   { return e.ID }
func (e *CurrencyExpr) Position() int { return e.Pos }
func (*CurrencyExpr) kind() string    { return "currency" }

func (e *CurrencyExpr) check() error {
	if !money.IsCurrencyCode(e.Code) {
		return fmt.Errorf("invalid currency code %q", e.Code)
	}
	return nil
}

// FxRateExpr is an exchange rate written the way a market writes one, 150
// JPY / USD: one USD buys 150 JPY. It is a rate, not two amounts, so its
// figure takes as many places as the quote has.
type FxRateExpr struct {
	ID  int `json:"-"`
	Pos int `json:"-"`
	Span
	Rate  string `json:"rate" role:"text"`
	Quote string `json:"quote" role:"text"`
	Base  string `json:"base" role:"text"`
}

func (*FxRateExpr) exprNode()       {}
func (e *FxRateExpr) NodeID() int   { return e.ID }
func (e *FxRateExpr) Position() int { return e.Pos }
func (*FxRateExpr) kind() string    { return "fxrate" }

func (e *FxRateExpr) check() error {
	if !money.IsCurrencyCode(e.Quote) || !money.IsCurrencyCode(e.Base) {
		return fmt.Errorf("invalid currency codes %q / %q", e.Quote, e.Base)
	}
	// plainDecimal, as for money and ratios: digits on both sides of a point,
	// which is what the printer writes back and the parser reads.
	if !plainDecimal(e.Rate) {
		return fmt.Errorf("an exchange rate's figure is a plain positive decimal, not %q", e.Rate)
	}
	return nil
}

// UsingExpr is using(settlement, implied(settled, paid), body): body runs
// with the quoted exchange rates and no others, so -> in it converts at them
// and nothing else — the rates a rule converts at are the ones it names. A
// quote is an fxrate or an array<fxrate>, a later one over an earlier. A
// rate from an outer using is carried in by reading it: using(fx(USD, JPY), …).
type UsingExpr struct {
	ID  int `json:"-"`
	Pos int `json:"-"`
	Span
	// Quotes are the exchange rates, in the order written.
	Quotes []Expr `json:"quotes" min:"1"`
	Body   Expr   `json:"body"`
}

func (*UsingExpr) exprNode()       {}
func (e *UsingExpr) NodeID() int   { return e.ID }
func (e *UsingExpr) Position() int { return e.Pos }
func (*UsingExpr) kind() string    { return "using" }

// nodeTypes lists every node the walker knows, by its ExprJSON tag. A literal
// has four tags, one per value kind; the others have one each.
var nodeTypes = []Expr{
	&LiteralExpr{}, &VariableExpr{}, &EnumExpr{}, &ArrayExpr{}, &DictExpr{}, &CallExpr{},
	&RecordExpr{}, &FieldExpr{}, &SwitchExpr{}, &ForExpr{}, &ReduceExpr{}, &LetExpr{},
	&RecordUpdateExpr{}, &MoneyExpr{}, &RatioExpr{}, &UsingExpr{}, &FxRateExpr{}, &CurrencyExpr{},
}
