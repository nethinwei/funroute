package machine

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// EvalFunc receives immutable values in a slice that is only valid for the
// duration of the call; an implementation that needs to keep an argument must
// copy it out.
type EvalFunc func(args []Value) (Value, error)

type specialForm uint8

const (
	specialNone specialForm = iota
	specialIf
)

// FunctionSpec defines a pure host function. The evaluator receives the values
// themselves — read-only, never copied — and no ambient runtime capabilities.
type FunctionSpec struct {
	Name    string
	Params  []Type
	Result  Type
	Cost    uint64
	Eval    EvalFunc
	Display FunctionDisplay

	special specialForm
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

// IsLazyIf reports whether this is the kernel's `if`, which the compiler emits
// as jumps instead of a call so the untaken branch is never evaluated.
func (f *RegisteredFunction) IsLazyIf() bool { return f.special == specialIf }

// Registry is immutable from the point of view of a running VM. Registration
// is synchronized so applications can build a registry during startup.
type Registry struct {
	mu     sync.RWMutex
	byName map[string][]*RegisteredFunction
	byKey  map[string]*RegisteredFunction
	forms  map[Form]bool
}

func NewRegistry() *Registry {
	return &Registry{
		byName: map[string][]*RegisteredFunction{},
		byKey:  map[string]*RegisteredFunction{},
		forms:  map[Form]bool{},
	}
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
	out := make([]Form, 0, len(r.forms))
	for _, form := range knownForms {
		if r.forms[form] {
			out = append(out, form)
		}
	}
	return out
}

// The shape of a name and the list of names the language keeps for itself are
// one authority, used both here (a function may not claim a reserved name) and
// by the parser and the JSON importer (a name in a document must be one a
// function could have). Two copies would drift.
var functionNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)

// reservedNames are parsed as literals or special forms, so no function may
// claim them.
var reservedNames = map[string]bool{
	"expr": true, "true": true, "false": true,
	"switch": true, "for": true, "reduce": true, "recur": true,
	"in": true, "from": true, "else": true, "case": true, "let": true,
}

// IsValidFunctionName reports whether name has the shape of a function name.
func IsValidFunctionName(name string) bool {
	return functionNamePattern.MatchString(name)
}

// IsReservedName reports whether the language keeps name for itself.
func IsReservedName(name string) bool {
	return reservedNames[name]
}

func (r *Registry) Register(spec FunctionSpec) error {
	if !functionNamePattern.MatchString(spec.Name) {
		return fmt.Errorf("invalid function name %q", spec.Name)
	}
	if reservedNames[spec.Name] {
		return fmt.Errorf("function name %q is reserved", spec.Name)
	}
	if spec.Cost == 0 {
		spec.Cost = 1
	}
	params := make([]Type, len(spec.Params))
	for i := range spec.Params {
		params[i] = CloneType(spec.Params[i])
	}
	spec.Params = params
	spec.Result = CloneType(spec.Result)
	spec.Display = cloneFunctionDisplay(spec.Display)
	if spec.Eval == nil && spec.special == specialNone {
		return fmt.Errorf("function %s has no evaluator", spec.Name)
	}
	if err := validateSignature(spec); err != nil {
		return err
	}
	if err := normalizeFunctionDisplay(&spec); err != nil {
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
	switch t.Kind {
	case BoolKind, IntKind, FloatKind, StringKind:
		return nil
	case VarKind:
		if t.Name == "" {
			return fmt.Errorf("unnamed type variable")
		}
		vars[t.Name] = true
		return nil
	case ArrayKind, DictKind:
		if t.Elem == nil {
			return fmt.Errorf("%s is missing its element type", t.Kind)
		}
		return validateTypePattern(*t.Elem, vars)
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
