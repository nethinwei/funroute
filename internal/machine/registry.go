package machine

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/nethinwei/funroute/internal/money"
)

// EvalFunc receives immutable values in a slice that is only valid for the
// duration of the call; an implementation that needs to keep an argument must
// copy it out. The context carries the request's deadline, capped by the
// function's own Timeout; the kernel's functions ignore it.
type EvalFunc func(ctx context.Context, args []Value) (Value, error)

type specialForm uint8

const (
	specialNone specialForm = iota
	specialIf
	specialFallback
)

// String is the lazy function's name, the one the catalog and the manifest
// both write; an ordinary function has none.
func (f specialForm) String() string {
	return [...]string{specialNone: "", specialIf: "if", specialFallback: "fallback"}[f]
}

// cloneDoc copies a Doc so a description handed out cannot reach the
// registry's own through its slice.
func cloneDoc(doc Doc) Doc {
	doc.Params = append([]string(nil), doc.Params...)
	doc.Examples = append([]Example(nil), doc.Examples...)
	return doc
}

// BatchEvalFunc evaluates one function for many argument lists at once — one
// engine call for a whole batch of requests. calls[i] is the i-th request's
// arguments; the result has one value per request, in the same order.
type BatchEvalFunc func(ctx context.Context, calls [][]Value) ([]Value, error)

// FunctionSpec defines a pure host function. The evaluator receives the values
// themselves — read-only, never copied — and no ambient runtime capabilities.
// EvalBatch is optional: a Batch runs it for the calls it can hoist out of the
// per-request programs, and falls back to Eval for the rest.
type FunctionSpec struct {
	Name      string
	Params    []Type
	Result    Type
	Eval      EvalFunc
	EvalBatch BatchEvalFunc
	// Doc is everything a host says about this function: what to call it, what
	// it costs, how long one call may take. It is also what the catalog hands
	// a front end, so there is one structure rather than an input shape and a
	// parallel output shape that have to be kept in step.
	Doc Doc

	special specialForm
	builtin bool
	// readsRun marks a kernel function whose answer depends on the run, not
	// only on its arguments — convert reads the quotes of its using — so
	// folding must not call it.
	readsRun bool
	// roundingScope marks round(expr, mode): the scope a step that lands
	// between minor units is written in. Not a special form — nothing about
	// it is lazy, and a front end draws it as a call.
	roundingScope bool
	// exactStep marks an operation that lands between minor units, without
	// a mode: exact, and written only inside a round.
	exactStep bool
	// takesExact marks a kernel operation that computes with exact money as
	// it does with whole minor units: sums, counts, comparisons, the rounding
	// variants. Exact money reaches no other function.
	takesExact bool
}

func (s FunctionSpec) Signature() string {
	params := make([]string, len(s.Params))
	for i := range s.Params {
		params[i] = s.Params[i].String()
	}
	return fmt.Sprintf("%s(%s)->%s", s.Name, strings.Join(params, ","), s.Result)
}

// RegisteredFunction is a function as the registry holds it: its spec plus the
// key that identifies the exact signature a call is bound to. The compiler
// reads it; nothing outside this module sees it.
type RegisteredFunction struct {
	FunctionSpec
	key string
}

// Key is the signature key an artifact records, so a registry that drifts
// fails to bind rather than binding the wrong function.
func (f *RegisteredFunction) Key() string { return f.key }

// NeedsBoundedArgs reports whether the compiler must refuse a call whose
// arguments could be any size at run time.
func (f *RegisteredFunction) NeedsBoundedArgs() bool { return f.Doc.BoundedArgs }

// IsConstexpr reports whether folding may call this function. Everything the
// kernel registers is, except what reads the run; a host function says so for
// itself.
func (f *RegisteredFunction) IsConstexpr() bool { return (f.builtin && !f.readsRun) || f.Doc.Constexpr }

// ReadsRates reports a kernel function that reads the exchange rates of the
// using it runs in — convert (->) and fx — which only a using's body may
// call: there are no rates outside one.
func (f *RegisteredFunction) ReadsRates() bool { return f.readsRun }

// IsLazyIf reports whether this is the kernel's `if`, which the compiler emits
// as jumps instead of a call so the untaken branch is never evaluated.
func (f *RegisteredFunction) IsLazyIf() bool { return f.special == specialIf }

// IsLazyFallback reports whether this is the kernel's error boundary. Every
// candidate participates in type inference, but bytecode advances only when
// the previous expression returns ErrExtension or ErrDeadline.
func (f *RegisteredFunction) IsLazyFallback() bool { return f.special == specialFallback }

// IsRoundingScope reports round(expr, mode), the scope exact steps are
// written in.
func (f *RegisteredFunction) IsRoundingScope() bool { return f.roundingScope }

// IsExactStep reports an operation that lands between minor units written
// without a mode: it computes exactly, and only a round(…) around it may
// take what it makes.
func (f *RegisteredFunction) IsExactStep() bool { return f.exactStep }

// TakesExact reports a function exact money may be handed to: an exact step,
// round, or a kernel operation that computes with it.
func (f *RegisteredFunction) TakesExact() bool { return f.takesExact || f.exactStep || f.roundingScope }

// Cost is what one call charges the fuel budget. It lives in Doc because a
// host states it once, beside what the function is for.
func (f *RegisteredFunction) Cost() uint64 { return f.Doc.Cost }

// IsBuiltin distinguishes trusted kernel functions from host extensions.
// Their ordinary domain errors (division by zero, head of an empty array)
// are expression errors and must never become catchable ErrExtension values.
func (f *RegisteredFunction) IsBuiltin() bool { return f.builtin }

// Registry is immutable from the point of view of a running VM. Registration
// is synchronized so applications can build a registry during startup.
type Registry struct {
	mu       sync.RWMutex
	byName   map[string][]*RegisteredFunction
	byKey    map[string]*RegisteredFunction
	forms    map[Form]bool
	handles  map[reflect.Type]string
	byHandle map[string]reflect.Type
	// money is the declared money feature, nil until DeclareMoney.
	money *money.Currencies
}

func NewRegistry() *Registry {
	return &Registry{
		byName:   map[string][]*RegisteredFunction{},
		byKey:    map[string]*RegisteredFunction{},
		forms:    map[Form]bool{},
		handles:  map[reflect.Type]string{},
		byHandle: map[string]reflect.Type{},
	}
}

// DefineHandle declares that Go values of type T cross the boundary as
// handle<name>: functions registered with Logic and Model then accept and
// return T directly, wrapping and unwrapping the payload without copying it.
// The name has the shape of a function name so it can carry a namespace and a
// version.
func DefineHandle[T any](registry *Registry, name string) error {
	if !IsValidFunctionName(name) {
		return fmt.Errorf("invalid handle name %q", name)
	}
	typ := reflect.TypeFor[T]()
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if existing, ok := registry.handles[typ]; ok && existing != name {
		return fmt.Errorf("type %s is already handle<%s>", typ, existing)
	}
	if existing, ok := registry.byHandle[name]; ok && existing != typ {
		return fmt.Errorf("handle<%s> is already Go type %s", name, existing)
	}
	registry.handles[typ] = name
	registry.byHandle[name] = typ
	return nil
}

// handleName is the handle type a Go type was defined as, if any.
func (r *Registry) handleName(typ reflect.Type) (string, bool) {
	if r == nil {
		return "", false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	name, ok := r.handles[typ]
	return name, ok
}

// Handles lists the defined handle types, sorted, for the catalog.
func (r *Registry) Handles() []Type {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Type, 0, len(r.byHandle))
	for name := range r.byHandle {
		out = append(out, HandleOf(name))
	}
	slices.SortFunc(out, func(a, b Type) int { return strings.Compare(a.name, b.name) })
	return out
}

// Form is a lazy special form. The parser always recognises the syntax, but a
// program may only use the forms its registry enables, so the host decides how
// much language each console gets. Every form iterates a finite input, so a
// program is guaranteed to terminate whatever is enabled.
type Form string

const (
	SwitchForm Form = "switch"
	ForForm    Form = "for"
	ReduceForm Form = "reduce"
)

// knownForms is also the display order of the catalog.
var knownForms = []Form{SwitchForm, ForForm, ReduceForm}

func (r *Registry) EnableForm(forms ...Form) error {
	for _, form := range forms {
		if !slices.Contains(knownForms, form) {
			return fmt.Errorf("unknown form %q", string(form))
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, form := range forms {
		r.forms[form] = true
	}
	return nil
}

func (r *Registry) FormEnabled(form Form) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.forms[form]
}

func (r *Registry) EnabledForms() []Form {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.enabledFormsLocked()
}

// The shape of a name and the list of names the language keeps for itself are
// one authority, used both here (a function may not claim a reserved name) and
// by the parser and the JSON importer (a name in a document must be one a
// function could have). Two copies would drift.
//
// A function name may be dotted, to carry a namespace and a version:
// route.score_v1. A variable name may not: "." is kept for field access. The
// shapes are checked byte by byte rather than by a regular expression: the
// run's boundary asks of every currency unit whether it is a variable, and a
// regexp's matcher comes from a pool.
func nameShape(name string, dotted bool) bool {
	if name == "" || !wordStart(name[0]) {
		return false
	}
	for i := 1; i < len(name); i++ {
		if c := name[i]; !wordStart(c) && (c < '0' || c > '9') && (!dotted || c != '.') {
			return false
		}
	}
	return true
}

func wordStart(c byte) bool { return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

// reservedNames are parsed as literals or special forms, so no function may
// claim them.
var reservedNames = map[string]bool{
	"true": true, "false": true,
	"switch": true, "for": true, "reduce": true,
	"in": true, "else": true, "case": true, "let": true, "using": true, "with": true,
}

// IsValidFunctionName reports whether name has the shape of a function name.
func IsValidFunctionName(name string) bool {
	return nameShape(name, true)
}

// IsValidVariableName reports whether name has the shape of a variable name.
// A name shaped like a currency code is that currency, so no variable has it.
// Nor is one called if: if is a function's name, and in a comprehension or a
// reduce it starts the filter clause, which a variable called if would make
// a matter of context.
func IsValidVariableName(name string) bool {
	return nameShape(name, false) && !money.IsCurrencyCode(name) && name != "if"
}

// IsReservedName reports whether the language keeps name for itself.
func IsReservedName(name string) bool {
	return reservedNames[name]
}

// IsValidFieldName is the one rule for a record's field names, used by the
// type parser, the source parser and the ExprJSON importer alike: a plain
// name, and not one the syntax has taken.
func IsValidFieldName(name string) bool {
	return nameShape(name, false) && !IsReservedName(name)
}

func (r *Registry) Register(spec FunctionSpec) error {
	if !IsValidFunctionName(spec.Name) {
		return fmt.Errorf("invalid function name %q", spec.Name)
	}
	if IsReservedName(spec.Name) {
		return fmt.Errorf("function name %q is reserved", spec.Name)
	}
	if spec.Doc.Cost == 0 {
		spec.Doc.Cost = 1
	}
	params := make([]Type, len(spec.Params))
	for i := range spec.Params {
		params[i] = CloneType(spec.Params[i])
	}
	spec.Params = params
	spec.Result = CloneType(spec.Result)
	spec.Doc.Params = append([]string(nil), spec.Doc.Params...)
	if spec.Eval == nil && spec.special == specialNone {
		return fmt.Errorf("function %s has no evaluator", spec.Name)
	}
	if err := validateSignature(spec); err != nil {
		return err
	}
	if err := r.admitsMoney(spec); err != nil {
		return err
	}
	if err := normalizeDoc(&spec); err != nil {
		return err
	}
	key := spec.Signature()
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byKey[key]; exists {
		return fmt.Errorf("function signature already registered: %s", key)
	}
	registered := &RegisteredFunction{FunctionSpec: spec, key: key}
	r.byKey[key] = registered
	r.byName[spec.Name] = append(r.byName[spec.Name], registered)
	return nil
}

// admitsMoney refuses a signature that uses a money type before money is
// declared, and one that names a currency the registry did not declare.
func (r *Registry) admitsMoney(spec FunctionSpec) error {
	for _, typ := range append(slices.Clone(spec.Params), spec.Result) {
		if err := WalkTypes(typ, func(inner Type) error { return r.admitsMoneyType(spec.Name, inner) }); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) admitsMoneyType(name string, typ Type) error {
	if !IsMoneyKind(typ.kind) {
		return nil
	}
	table := r.currencies()
	if table == nil {
		return fmt.Errorf("function %s uses %s, but this registry declares no money (Registry.DeclareMoney)", name, typ)
	}
	return nil
}

func validateSignature(spec FunctionSpec) error {
	paramsVars := map[string]bool{}
	for _, param := range spec.Params {
		if err := validateTypePattern(param, paramsVars); err != nil {
			return fmt.Errorf("function %s parameter: %w", spec.Name, err)
		}
	}
	resultVars := map[string]bool{}
	if err := validateTypePattern(spec.Result, resultVars); err != nil {
		return fmt.Errorf("function %s result: %w", spec.Name, err)
	}
	for name := range resultVars {
		if !paramsVars[name] {
			return fmt.Errorf("function %s result type variable %s is not present in its parameters", spec.Name, name)
		}
	}
	return nil
}

func validateTypePattern(t Type, vars map[string]bool) error {
	switch t.kind {
	case BoolKind, IntKind, FloatKind, StringKind:
		return nil
	case EnumKind:
		if !IsAnyEnum(t) && !t.IsConcrete() {
			return fmt.Errorf("invalid enum type %s", t)
		}
		return nil
	case HandleKind:
		if !IsValidFunctionName(t.name) {
			return fmt.Errorf("invalid handle name %q", t.name)
		}
		return nil
	case VarKind:
		if t.name == "" {
			return fmt.Errorf("unnamed type variable")
		}
		vars[t.name] = true
		return nil
	case RatioKind:
		return nil
	case MoneyKind, CurrencyKind, FxRateKind:
		if t.name != "" || len(t.values) != 0 {
			return fmt.Errorf("invalid type %s", t)
		}
		return nil
	case ArrayKind, DictKind:
		if t.elem == nil {
			return fmt.Errorf("%s is missing its element type", t.kind)
		}
		return validateTypePattern(*t.elem, vars)
	case RecordKind:
		// A record in a signature is fixed: its fields are its identity, so
		// there is nothing to leave open the way an element type can be.
		if !t.IsConcrete() {
			return fmt.Errorf("invalid record type %s", t)
		}
		return nil
	default:
		return fmt.Errorf("invalid type %s", t)
	}
}

// Overloads returns every signature registered under a name. Inference walks
// them to find the ones an argument list can satisfy.
func (r *Registry) Overloads(name string) []*RegisteredFunction {
	r.mu.RLock()
	defer r.mu.RUnlock()
	functions := r.byName[name]
	out := make([]*RegisteredFunction, len(functions))
	copy(out, functions)
	return out
}

// Resolve looks up the exact signature a call is bound to.
func (r *Registry) Resolve(key string) (*RegisteredFunction, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	function, ok := r.byKey[key]
	return function, ok
}
