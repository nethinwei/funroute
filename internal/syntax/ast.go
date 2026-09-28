package syntax

import (
	"fmt"
	"slices"
	"strings"

	"github.com/nethinwei/funroute/internal/kit"
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
//	kind:"name"        on the embedded Node: the ExprJSON node tag, with
//	                   ",form" for a form — a node that branches, binds names
//	                   or scopes what is inside it — and ",optional" for a
//	                   form a registry can turn off
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
}

// Node is what every node carries besides its own fields: its ID, where a
// message about it points and the source it was read from. Every node embeds
// one, untagged for ExprJSON, so ExprJSON and the digest never see it; its
// kind tag names the node.
type Node struct {
	ID  int `json:"-"`
	Pos int `json:"-"`
	Span
}

func (*Node) exprNode() {}

// NodeID is the node's ID, unique within one parse or import.
func (n *Node) NodeID() int { return n.ID }

// Position is where a message about the node points.
func (n *Node) Position() int { return n.Pos }

// Span is the source a node was read from: bytes Start up to End, the
// parentheses around it included. Position is where a message about the node
// points; the span is all of it. A node imported from ExprJSON has no source,
// and its span is empty.
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

// LiteralExpr is exchanged as {"node":"int","int":1} and the like: the node
// tag is the value's kind and the field carries the value, so it is the one
// node the walker does not describe from tags.
//
// A decimal is the number it was written as, Decimal, exactly; Value is the
// float64 nearest to it. Which one the literal is depends on what its
// context reads it as: a ratio is the decimal, and a float is Value only when
// Value is the decimal (Float), so no literal is ever rounded on its way to
// being a float or a ratio.
type LiteralExpr struct {
	Node
	Value   machine.Value
	Decimal money.Decimal
}

type VariableExpr struct {
	Node `kind:"var"`
	Name string `json:"name" role:"var"`
}

// EnumExpr is a member of a host-declared enum, written @adyen, or
// @channel.adyen when the member name alone is ambiguous. The member set lives
// in the contract, so the node carries names only; the compiler resolves which
// enum it belongs to and emits the member string as a constant.
type EnumExpr struct {
	Node   `kind:"enum"`
	Enum   string `json:"enum,omitempty" role:"text"`
	Member string `json:"member" role:"text"`
}

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
	Node  `kind:"array"`
	Items []Expr `json:"items,omitempty"`
}

type DictEntryExpr struct {
	Key   string `json:"key" role:"text"`
	Value Expr   `json:"value"`
}

// DictExpr keeps its entries sorted by key, because dictionary order has no
// language semantics and the canonical JSON must not depend on how the
// program was written.
type DictExpr struct {
	Node    `kind:"dict"`
	Entries []DictEntryExpr `json:"entries,omitempty" sort:"key"`
}

func (e *DictExpr) check() error {
	if key, twice := kit.Repeated(e.Entries, func(entry DictEntryExpr) string { return entry.Key }); twice {
		return fmt.Errorf("duplicate dictionary key %q", key)
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
	Node   `kind:"record"`
	Fields []RecordFieldExpr `json:"fields" min:"1"`
}

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
	Node   `kind:"record_update"`
	Base   Expr              `json:"base"`
	Fields []RecordFieldExpr `json:"fields" min:"1"`
}

func (e *RecordUpdateExpr) check() error { return checkRecordFields(e.Fields) }

// FieldExpr is r.field. The field name is resolved to a position in the
// record's type at compile time, so nothing is looked up while the rule runs.
type FieldExpr struct {
	Node  `kind:"field"`
	Value Expr   `json:"value"`
	Field string `json:"field" role:"text"`
}

func (e *FieldExpr) check() error {
	if !machine.IsValidFieldName(e.Field) {
		return fmt.Errorf("invalid record field name %q", e.Field)
	}
	return nil
}

// SelectorExpr is .field or .a.b, written as an argument after the first of
// a call: sort_by(channels, .fee) keys each item of the first argument by the
// fields it reads. It is shorthand for [e.fee for e in channels], and the
// compiler reads it as that (ExpandSelectors), so it needs no rules of its own.
type SelectorExpr struct {
	Node `kind:"selector"`
	Path string `json:"path" role:"text"`
}

func (e *SelectorExpr) check() error {
	for field := range strings.SplitSeq(e.Path, ".") {
		if !machine.IsValidFieldName(field) {
			return fmt.Errorf("invalid record field name %q", field)
		}
	}
	return nil
}

type CallExpr struct {
	Node `kind:"call"`
	Name string `json:"name" role:"fn"`
	Args []Expr `json:"args,omitempty"`
}

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
	Node    `kind:"switch,form,optional"`
	Value   Expr             `json:"value,omitempty"`
	Cases   []SwitchCaseExpr `json:"cases" min:"1"`
	Default Expr             `json:"default,omitempty"`
}

// ForExpr is [yield for variable in source if where], or
// {yield_key: yield for variable in source if where} when it builds a
// dictionary. The names are locally bound and do not become program arguments;
// KeyVariable is set for a dictionary walk ([e for k, v in d]) and empty for
// an array.
type ForExpr struct {
	Node        `kind:"for,form,optional"`
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

func (e *ForExpr) check() error { return distinctNames(e.KeyVariable, e.Variable) }

// ReduceExpr is reduce(variable in source if where, accumulator = init, body):
// body folds every item the filter keeps into the accumulator. All three names
// are locally bound and never become program arguments. Where is optional and
// sees the loop variables but not the accumulator, because an item is filtered
// before it is folded.
type ReduceExpr struct {
	Node        `kind:"reduce,form,optional"`
	Source      Expr   `json:"source"`
	Variable    string `json:"variable" role:"local" binds:"where,body"`
	KeyVariable string `json:"key_variable,omitempty" role:"local" binds:"where,body"`
	Where       Expr   `json:"where,omitempty"`
	Accumulator string `json:"accumulator" role:"local" binds:"body"`
	Init        Expr   `json:"init"`
	Body        Expr   `json:"body"`
}

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
	Node     `kind:"let,form"`
	Bindings []LetBinding `json:"bindings" min:"1"`
	Body     Expr         `json:"body"`
}

func (e *LetExpr) check() error {
	return distinctNames(kit.Map(e.Bindings, func(binding LetBinding) string { return binding.Name })...)
}

// distinctNames rejects two locals of one node sharing a name. An empty name
// is an absent optional one and does not count.
func distinctNames(names ...string) error {
	present := slices.DeleteFunc(names, func(name string) bool { return name == "" })
	if name, twice := kit.Repeated(present, kit.Identity[string]); twice {
		return fmt.Errorf("the name %q is bound twice", name)
	}
	return nil
}

// MoneyExpr is an amount written in its currency: USD 1.70, EUR -0.05. The
// amount stays the decimal it was written as — how many minor units it is
// depends on the currency's places, which are the registry's, and ExprJSON
// must not depend on a registry.
type MoneyExpr struct {
	Node     `kind:"money"`
	Currency string `json:"currency" role:"text"`
	Amount   string `json:"amount" role:"text"`
}

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
	Node  `kind:"ratio"`
	Value string `json:"value" role:"text"`
	Unit  string `json:"unit" role:"text"`
}

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
	return text != "" && kit.IsDigits(text)
}

// CurrencyExpr is a currency, written as its code: USD. Currencies are the
// language's own, not an enum a contract brings, so a name shaped like a code
// is always one, and no variable may take such a name.
type CurrencyExpr struct {
	Node `kind:"currency"`
	Code string `json:"code" role:"text"`
}

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
	Node  `kind:"fxrate"`
	Rate  string `json:"rate" role:"text"`
	Quote string `json:"quote" role:"text"`
	Base  string `json:"base" role:"text"`
}

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
	Node `kind:"using,form"`
	// Quotes are the exchange rates, in the order written.
	Quotes []Expr `json:"quotes" min:"1"`
	Body   Expr   `json:"body"`
}

// nodeTypes lists every node the walker knows, by its ExprJSON tag. A literal
// has four tags, one per value kind; the others have one each.
var nodeTypes = []Expr{
	&LiteralExpr{}, &VariableExpr{}, &EnumExpr{}, &ArrayExpr{}, &DictExpr{}, &CallExpr{},
	&RecordExpr{}, &FieldExpr{}, &SwitchExpr{}, &ForExpr{}, &ReduceExpr{}, &LetExpr{},
	&RecordUpdateExpr{}, &MoneyExpr{}, &RatioExpr{}, &UsingExpr{}, &FxRateExpr{}, &CurrencyExpr{},
	&SelectorExpr{},
}
