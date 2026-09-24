package compile

import (
	"github.com/nethinwei/funroute/internal/kit"
	"github.com/nethinwei/funroute/internal/machine"
)

// TextContract is a contract as a host writes it down as data — in a rule
// record, a request, a language server's settings: every type as text, and
// the record types it uses named once. It is the one reading of that text, so
// a console, a server and an editor cannot disagree about what it declares.
type TextContract struct {
	// Types names a record once so every argument with that shape can refer to
	// it. Aliases are expanded where they are named and never reach the
	// artifact, so two contracts that differ only in spelling still agree.
	Types  map[string]string `json:"types,omitempty"`
	Args   []TextArg         `json:"args,omitempty"`
	Result *TextResult       `json:"result,omitempty"`
}

type TextArg struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Doc  string `json:"doc,omitempty"`
}

type TextResult struct {
	Type string `json:"type"`
	Doc  string `json:"doc,omitempty"`
}

// Options turns the contract into what the compiler takes. A nil contract
// declares nothing, and everything is inferred.
func (c *TextContract) Options() (CompileOptions, error) {
	var options CompileOptions
	if c == nil {
		return options, nil
	}
	aliases, err := c.Aliases()
	if err != nil {
		return options, err
	}
	for _, arg := range c.Args {
		typ, err := machine.ParseTypeWith(arg.Type, aliases)
		if err != nil {
			return options, kit.Errorf(machine.ErrContract, "argument %q: %v", arg.Name, err)
		}
		options.Args = append(options.Args, ArgSpec{Name: arg.Name, Type: typ, Doc: arg.Doc})
	}
	if c.Result != nil {
		typ, err := machine.ParseTypeWith(c.Result.Type, aliases)
		if err != nil {
			return options, kit.Errorf(machine.ErrContract, "result: %v", err)
		}
		options.Result = &typ
		options.ResultDoc = c.Result.Doc
	}
	return options, ValidateContract(options)
}

// Aliases resolves the declared types. They do not nest: one alias may not be
// written in terms of another, so there is no order to resolve them in.
func (c *TextContract) Aliases() (map[string]machine.Type, error) {
	if c == nil {
		return map[string]machine.Type{}, nil
	}
	aliases := make(map[string]machine.Type, len(c.Types))
	for name, text := range c.Types {
		typ, err := machine.ParseType(text)
		if err != nil {
			return nil, kit.Errorf(machine.ErrContract, "type %q: %v", name, err)
		}
		aliases[name] = typ
	}
	return aliases, nil
}
