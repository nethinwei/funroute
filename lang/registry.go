package lang

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

// FunctionSpec defines a pure host function. The evaluator receives cloned
// values and no ambient runtime capabilities.
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

type registeredFunction struct {
	FunctionSpec
	key string
}

// Registry is immutable from the point of view of a running VM. Registration
// is synchronized so applications can build a registry during startup.
type Registry struct {
	mu     sync.RWMutex
	byName map[string][]*registeredFunction
	byKey  map[string]*registeredFunction
	forms  map[Form]bool
}

func NewRegistry() *Registry {
	return &Registry{
		byName: map[string][]*registeredFunction{},
		byKey:  map[string]*registeredFunction{},
		forms:  map[Form]bool{},
	}
}

// Form is a lazy special form. The parser always recognises the syntax, but a
// program may only use the forms its registry enables, so the host decides how
// much language each console gets: an operator console enables switch/for/
// reduce, an engineer console also enables recur and becomes Turing complete.
type Form string

const (
	SwitchForm Form = "switch"
	ForForm    Form = "for"
	ReduceForm Form = "reduce"
	RecurForm  Form = "recur"
)

// knownForms is also the display order of the catalog.
var knownForms = []Form{SwitchForm, ForForm, ReduceForm, RecurForm}

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

var functionNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*(?:@[0-9]+)?$`)

// reservedNames are parsed as literals or special forms, so no function may
// claim them.
var reservedNames = map[string]bool{
	"expr": true, "true": true, "false": true,
	"recur": true, "switch": true, "for": true, "reduce": true,
	"in": true, "where": true, "from": true, "else": true, "case": true,
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
		params[i] = cloneType(spec.Params[i])
	}
	spec.Params = params
	spec.Result = cloneType(spec.Result)
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
	registered := &registeredFunction{FunctionSpec: spec, key: key}
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

func (r *Registry) functions(name string) []*registeredFunction {
	r.mu.RLock()
	defer r.mu.RUnlock()
	functions := r.byName[name]
	out := make([]*registeredFunction, len(functions))
	copy(out, functions)
	return out
}

func (r *Registry) resolve(key string) (*registeredFunction, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	function, ok := r.byKey[key]
	return function, ok
}
