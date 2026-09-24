package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nethinwei/funroute"
	"github.com/nethinwei/funroute/extensions/std"
	"github.com/nethinwei/funroute/lsp"
)

// commands are the subcommands, by the name that picks one.
var commands = map[string]func(args []string) error{
	"inspect": inspect,
	"export":  exportExpr,
	"compile": compile,
	"run":     run,
	"fmt":     formatSource,
	"lsp":     serveLanguage,
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	command, known := commands[os.Args[1]]
	if !known {
		usage()
		command = func([]string) error { return fmt.Errorf("unknown command %q", os.Args[1]) }
	}
	if err := command(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// locate prefixes a compile error with line:column, the way every compiler
// reports one. The position comes off the error; the text is right here.
func locate(err error, source string) error {
	line, column, ok := funroute.LineColumn(err, source)
	if !ok {
		return err
	}
	return fmt.Errorf("%d:%d: %w", line, column, err)
}

func usage() {
	fmt.Fprintln(os.Stderr, `funroute - strongly typed pure expression language

Commands:
  inspect -expr 'if(a,b,add(1,1))'
  export  -expr 'if(a,b,add(1,1))'
  compile -expr 'if(a,b,add(1,1))'
  run     -expr 'if(a,b,add(1,1))' -args '{"a":true,"b":7}'
  fmt     -expr 'let(a=1,a+2)'        (or the program on standard input)
  lsp     [-manifest registry.json]   the language server, on stdin and stdout

The contract is the host's: -types 'a=bool,b=int' declares the arguments and
their order. Without it both are inferred. -alias names a type so a record
does not have to be written out for every argument that has its shape:
  -alias 'Order=record{amount: int, currency: string}' -types 'a=Order,b=Order'

Money: -currencies iso (the default, ISO 4217) or none. A rule writes every
rounding it does — round(amount * 2.9%, @half_up) — and -> converts only
inside a using, at the rates it names, written in the rule or given as
arguments like any other:
  run -expr 'using(rates, round(amount -> JPY, @half_even))' -types 'amount=money,rates=array<fxrate>' \
    -args '{"amount":"USD 1.00","rates":[{"base":"USD","quote":"JPY","rate":"150"}]}'`)
}

type commonFlags struct {
	set                              *flag.FlagSet
	expr, types, aliases, currencies string
}

func flags(name string) *commonFlags {
	common := &commonFlags{set: flag.NewFlagSet(name, flag.ContinueOnError)}
	common.set.StringVar(&common.expr, "expr", "", "expression source")
	common.set.StringVar(&common.types, "types", "", "comma-separated argument type hints")
	common.set.StringVar(&common.aliases, "alias", "", "comma-separated type declarations, Name=type")
	common.set.StringVar(&common.currencies, "currencies", "iso", "the money declared: iso (ISO 4217) or none")
	return common
}

// parseFlags reads a command's arguments into the flags every command shares.
func parseFlags(name string, args []string) (*commonFlags, error) {
	common := flags(name)
	return common, common.set.Parse(args)
}

// isoMoney is the money -currencies iso declares: ISO 4217.
func isoMoney() *funroute.MoneySpec {
	return &funroute.MoneySpec{Currencies: std.ISO4217()}
}

// registry is the registry with the money -currencies declares: iso or none.
func (common *commonFlags) registry() (*funroute.Registry, error) {
	switch common.currencies {
	case "none":
		return newRegistry(nil)
	case "iso":
		return newRegistry(isoMoney())
	default:
		return nil, fmt.Errorf("-currencies is iso or none, not %q", common.currencies)
	}
}

func inspect(args []string) error {
	common, err := parseFlags("inspect", args)
	if err != nil {
		return err
	}
	artifact, _, err := compileSource(common)
	if err != nil {
		return err
	}
	params := make([]string, 0, len(artifact.Args()))
	for _, param := range artifact.Args() {
		params = append(params, param.String())
	}
	fmt.Printf("(%s) -> %s\n", strings.Join(params, ", "), artifact.Result())
	fmt.Println("digest:", artifact.Digest())
	fmt.Println("instructions:", artifact.InstructionCount())
	return nil
}

func exportExpr(args []string) error {
	common, err := parseFlags("export", args)
	if err != nil {
		return err
	}
	if common.expr == "" {
		return errors.New("-expr is required")
	}
	encoded, err := funroute.ParseToJSON(common.expr)
	if err != nil {
		return locate(err, common.expr)
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, encoded, "", "  "); err != nil {
		return err
	}
	fmt.Println(pretty.String())
	return nil
}

// serveLanguage runs the language server on stdin and stdout. The registry is
// the kernel and the standard library; a manifest adds a host's functions by
// their signatures, which is all the server needs to check and explain them.
func serveLanguage(args []string) error {
	set := flag.NewFlagSet("lsp", flag.ContinueOnError)
	manifestPath := set.String("manifest", "", "a registry manifest (Registry.Manifest as JSON) to serve")
	if err := set.Parse(args); err != nil {
		return err
	}
	var manifest *funroute.Manifest
	if *manifestPath != "" {
		read, err := readManifest(*manifestPath)
		if err != nil {
			return err
		}
		manifest = read
	}
	// The manifest's money is declared before the standard pack registers,
	// so the pack's aggregates over money are the real ones.
	money := isoMoney()
	if manifest != nil {
		money = nil
		if spec, declared := manifest.Money(); declared {
			money = &spec
		}
	}
	registry, err := newRegistry(money)
	if err != nil {
		return err
	}
	if manifest != nil {
		if err := manifest.Apply(registry); err != nil {
			return err
		}
	}
	return lsp.Serve(os.Stdin, os.Stdout, registry)
}

func readManifest(path string) (*funroute.Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var manifest funroute.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("manifest %s: %w", path, err)
	}
	return &manifest, nil
}

// formatSource prints a program laid out the way the language prints it.
func formatSource(args []string) error {
	common, err := parseFlags("fmt", args)
	if err != nil {
		return err
	}
	source := common.expr
	if source == "" {
		input, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		source = string(input)
	}
	formatted, err := funroute.Format(source)
	if err != nil {
		return locate(err, source)
	}
	fmt.Println(formatted)
	return nil
}

func compile(args []string) error {
	common, err := parseFlags("compile", args)
	if err != nil {
		return err
	}
	artifact, _, err := compileSource(common)
	if err != nil {
		return err
	}
	encoded, err := artifact.MarshalIndent()
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

func run(args []string) error {
	common := flags("run")
	argsSource := common.set.String("args", "{}", "JSON object containing argument values")
	fuel := common.set.Uint64("fuel", funroute.DefaultFuel, "execution fuel")
	if err := common.set.Parse(args); err != nil {
		return err
	}
	artifact, registry, err := compileSource(common)
	if err != nil {
		return err
	}
	runtime, err := funroute.Instantiate(artifact, registry)
	if err != nil {
		return err
	}
	rawArgs, err := funroute.DecodeArgs([]byte(*argsSource))
	if err != nil {
		return err
	}
	result, err := runtime.Run(context.Background(), rawArgs, funroute.RunOptions{Fuel: *fuel})
	if err != nil {
		return err
	}
	value, err := registry.EncodeJSON(result)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(map[string]any{
		"digest": artifact.Digest(),
		"type":   result.Type().String(),
		"value":  json.RawMessage(value),
	}, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

// compileSource compiles -expr against the registry -currencies names, and
// returns that registry too, for running what it compiled.
func compileSource(common *commonFlags) (*funroute.Artifact, *funroute.Registry, error) {
	if common.expr == "" {
		return nil, nil, errors.New("-expr is required")
	}
	contract, err := textContract(common.aliases, common.types)
	if err != nil {
		return nil, nil, err
	}
	options, err := contract.Options()
	if err != nil {
		return nil, nil, err
	}
	registry, err := common.registry()
	if err != nil {
		return nil, nil, err
	}
	artifact, err := funroute.CompileExpr(common.expr, registry, options)
	if err != nil {
		return nil, nil, locate(err, common.expr)
	}
	return artifact, registry, nil
}

// newRegistry is the kernel with every lazy form enabled, the money given
// (none when nil) and the standard pack. Domain functions are the host's
// business, so the CLI registers none of those — but a tool for trying
// expressions out is useless without sum, len and the rest.
func newRegistry(money *funroute.MoneySpec) (*funroute.Registry, error) {
	registry := funroute.CoreRegistry()
	if err := registry.EnableForm(funroute.SwitchForm, funroute.ForForm, funroute.ReduceForm); err != nil {
		return nil, err
	}
	if money != nil {
		if err := registry.DeclareMoney(*money); err != nil {
			return nil, err
		}
	}
	if err := std.Register(registry); err != nil {
		return nil, err
	}
	return registry, nil
}

// textContract reads -alias 'Order=record{...}' and -types 'a=bool,b=int'
// into the contract they spell, which funroute.TextContract then reads the one way
// every host does. An alias is spelling only: it is expanded where it is named,
// so nothing about it reaches the artifact. The -types text is ordered, and
// that order is the artifact's ABI.
func textContract(aliases, types string) (*funroute.TextContract, error) {
	contract := &funroute.TextContract{}
	err := declarations(aliases, func(name, typ string) {
		if contract.Types == nil {
			contract.Types = map[string]string{}
		}
		contract.Types[name] = typ
	})
	if err != nil {
		return nil, err
	}
	err = declarations(types, func(name, typ string) {
		contract.Args = append(contract.Args, funroute.TextArg{Name: name, Type: typ})
	})
	if err != nil {
		return nil, err
	}
	return contract, nil
}

// declarations cuts "a=t1,b=t2" into its name=type pairs and gives each to
// declare, in order. A type's own commas are inside <> or {}, so only the
// top-level ones separate.
func declarations(source string, declare func(name, typ string)) error {
	if strings.TrimSpace(source) == "" {
		return nil
	}
	for _, part := range splitTopLevel(source) {
		name, typ, ok := strings.Cut(part, "=")
		if name = strings.TrimSpace(name); !ok || name == "" {
			return fmt.Errorf("invalid declaration %q, expected name=type", part)
		}
		declare(name, strings.TrimSpace(typ))
	}
	return nil
}

func splitTopLevel(source string) []string {
	var parts []string
	depth, start := 0, 0
	for i, r := range source {
		switch r {
		case '<', '{':
			depth++
		case '>', '}':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, source[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, source[start:])
	return parts
}
