package machine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/nethinwei/funroute/internal/money"
)

// A manifest is a registry without its implementations: every function's
// full signature and Doc, the handle types and the enabled forms. It is what
// a tool needs to type-check, explain and complete a program — a language
// server, an editor in the browser — where a host's functions cannot run: a
// model needs its engine, a remote lookup its network. Applied to a registry
// that already holds the kernel and the standard library, it adds each
// function that is missing as its signature alone.
//
// An artifact records each call's signature and cost, so one compiled against
// a manifest binds to the real registry only if the two agree. Folding still
// differs: a host function marked Constexpr is folded where it can run and not
// where it cannot, so the artifact to deploy is the one the host compiles.

// ManifestVersion identifies the manifest's current shape.
const ManifestVersion = 1

// Manifest is a registry's signatures as data, for a language service that
// has the signatures but not the functions. A host gets one from
// Registry.Manifest, sends it as JSON, and Applies what it reads back; it has
// no fields to set.
type Manifest struct{ parts manifestParts }

// Version is the manifest's shape, ManifestVersion when it was written here.
func (m Manifest) Version() int { return m.parts.Version }

// Money is the money feature the manifest declares, if it declares one.
func (m Manifest) Money() (money.MoneySpec, bool) {
	if m.parts.Money == nil {
		return money.MoneySpec{}, false
	}
	spec := *m.parts.Money
	spec.Currencies = slices.Clone(spec.Currencies)
	return spec, true
}

func (m Manifest) MarshalJSON() ([]byte, error) { return json.Marshal(m.parts) }

func (m *Manifest) UnmarshalJSON(data []byte) error { return json.Unmarshal(data, &m.parts) }

// manifestParts is what a manifest is made of, and its JSON.
type manifestParts struct {
	Version   int                `json:"version"`
	Forms     []Form             `json:"forms"`
	Handles   []string           `json:"handles,omitempty"`
	Functions []ManifestFunction `json:"functions"`
	// Money is the declared money feature. Apply declares it first, since
	// the money kernel's functions come with it rather than one by one.
	Money *money.MoneySpec `json:"money,omitempty"`
}

// ManifestFunction is one registered signature. BoundedArgs is written out
// because it changes what compiles, and Apply keeps it. Constexpr records how
// the host folds the function; a signature has nothing to fold with, so Apply
// leaves it off — which is why an artifact to deploy is compiled by the host.
type ManifestFunction struct {
	Name        string `json:"name"`
	Params      []Type `json:"params"`
	Result      Type   `json:"result"`
	Special     string `json:"special,omitempty"`
	Doc         Doc    `json:"doc"`
	Constexpr   bool   `json:"constexpr,omitempty"`
	BoundedArgs bool   `json:"bounded_args,omitempty"`
}

// ErrUnavailable is what a function known only by its signature returns when
// a program calls it. It is an ErrExtension too, so fallback moves past it as
// it would past any failing host function.
var ErrUnavailable = errors.New("function is not available in this runtime")

// Manifest describes the registry: every signature in key order.
func (r *Registry) Manifest() Manifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]string, 0, len(r.byKey))
	for key := range r.byKey {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	functions := make([]ManifestFunction, len(keys))
	for i, key := range keys {
		functions[i] = manifestFunction(r.byKey[key])
	}
	handles := make([]string, 0, len(r.byHandle))
	for name := range r.byHandle {
		handles = append(handles, name)
	}
	slices.Sort(handles)
	parts := manifestParts{Version: ManifestVersion, Forms: r.enabledFormsLocked(), Handles: handles, Functions: functions}
	if r.money != nil {
		spec := r.money.Spec()
		spec.Currencies = slices.Clone(spec.Currencies)
		parts.Money = &spec
	}
	return Manifest{parts: parts}
}

func manifestFunction(function *RegisteredFunction) ManifestFunction {
	return ManifestFunction{
		Name: function.Name, Params: function.Params, Result: function.Result, Special: function.special.String(),
		Doc: cloneDoc(function.Doc), Constexpr: function.IsConstexpr(), BoundedArgs: function.Doc.BoundedArgs,
	}
}

// Apply adds what the manifest describes and r lacks: its forms, its handle
// types, and each function r has no implementation of, as its signature
// alone. A function r does have must cost what the manifest says, or an
// artifact compiled against one would not bind to the other.
func (m Manifest) Apply(r *Registry) error {
	if m.parts.Version != ManifestVersion {
		return fmt.Errorf("unsupported manifest version %d", m.parts.Version)
	}
	if err := r.EnableForm(m.parts.Forms...); err != nil {
		return err
	}
	if err := r.applyMoney(m.parts.Money); err != nil {
		return err
	}
	for _, name := range m.parts.Handles {
		if err := r.declareHandle(name); err != nil {
			return err
		}
	}
	for _, function := range m.parts.Functions {
		if err := r.applyFunction(function); err != nil {
			return err
		}
	}
	return nil
}

// applyMoney declares the manifest's money feature, or checks that the one
// already declared is the same: a literal's minor units depend on it.
func (r *Registry) applyMoney(spec *money.MoneySpec) error {
	if spec == nil {
		return nil
	}
	existing := r.currencies()
	if existing == nil {
		return r.DeclareMoney(*spec)
	}
	table, err := money.NewCurrencies(*spec)
	if err != nil {
		return err
	}
	if money.Identity(table) != money.Identity(existing) {
		return fmt.Errorf("the manifest declares other currencies than this registry")
	}
	return nil
}

func (r *Registry) applyFunction(function ManifestFunction) error {
	spec := FunctionSpec{Name: function.Name, Params: function.Params, Result: function.Result, Doc: function.Doc}
	spec.Doc.BoundedArgs = function.BoundedArgs
	if existing, ok := r.Resolve(spec.Signature()); ok {
		if existing.Cost() != function.Doc.Cost {
			return fmt.Errorf("%s costs %d here and %d in the manifest", spec.Signature(), existing.Cost(), function.Doc.Cost)
		}
		return nil
	}
	if function.Special != "" {
		return fmt.Errorf("the kernel's %s is missing from this registry", function.Special)
	}
	// There is nothing here to call, so folding must not try.
	spec.Doc.Constexpr = false
	spec.Eval = unavailable(function.Name)
	return r.Register(spec)
}

// declareHandle knows a handle type by its name alone: there is no Go value
// to carry, since nothing here produces one.
func (r *Registry) declareHandle(name string) error {
	if !IsValidFunctionName(name) {
		return fmt.Errorf("invalid handle name %q", name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byHandle[name]; !ok {
		r.byHandle[name] = nil
	}
	return nil
}

func unavailable(name string) EvalFunc {
	return func(ctx context.Context, _ []Value) (Value, error) {
		if calls, ok := ctx.Value(unavailableKey{}).(*unavailableCalls); ok {
			calls.add(name)
		}
		return Value{}, fmt.Errorf("%w: %w: %s", ErrExtension, ErrUnavailable, name)
	}
}

type unavailableKey struct{}

type unavailableCalls struct {
	mu    sync.Mutex
	names []string
}

func (c *unavailableCalls) add(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !slices.Contains(c.names, name) {
		c.names = append(c.names, name)
	}
}

// TrackUnavailable returns a context under which a run records the functions
// it called that exist here only as signatures, and a function that lists
// them in the order they were first called. A result that went through
// fallback past one of them is not the result the host would compute.
func TrackUnavailable(ctx context.Context) (context.Context, func() []string) {
	calls := &unavailableCalls{}
	return context.WithValue(ctx, unavailableKey{}, calls), func() []string {
		calls.mu.Lock()
		defer calls.mu.Unlock()
		return append([]string{}, calls.names...)
	}
}

func (r *Registry) enabledFormsLocked() []Form {
	out := make([]Form, 0, len(r.forms))
	for _, form := range knownForms {
		if r.forms[form] {
			out = append(out, form)
		}
	}
	return out
}
