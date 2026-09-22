package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"funroute/extensions/std"
	"io"
	"os"
	"strings"

	"funroute/lang"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "inspect":
		err = inspect(os.Args[2:])
	case "export":
		err = exportExpr(os.Args[2:])
	case "compile":
		err = compile(os.Args[2:])
	case "run":
		err = run(os.Args[2:])
	default:
		usage()
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// locate prefixes a compile error with line:column, the way every compiler
// reports one. The position comes off the error; the text is right here.
func locate(err error, source string) error {
	line, column, ok := lang.LineColumn(err, source)
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

The contract is the host's: -types 'a=bool,b=int' declares the arguments and
their order. Without it both are inferred. -alias names a type so a record
does not have to be written out for every argument that has its shape:
  -alias 'Order=record{amount: int, currency: string}' -types 'a=Order,b=Order'`)
}

type commonFlags struct {
	set     *flag.FlagSet
	expr    *string
	types   *string
	aliases *string
}

func flags(name string) commonFlags {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	return commonFlags{
		set:     set,
		expr:    set.String("expr", "", "expression source"),
		types:   set.String("types", "", "comma-separated argument type hints"),
		aliases: set.String("alias", "", "comma-separated type declarations, Name=type"),
	}
}

func inspect(args []string) error {
	common := flags("inspect")
	if err := common.set.Parse(args); err != nil {
		return err
	}
	artifact, err := compileSource(common)
	if err != nil {
		return err
	}
	params := make([]string, len(artifact.Args))
	for i, param := range artifact.Args {
		params[i] = param.Name + ": " + param.Type.String()
	}
	fmt.Printf("(%s) -> %s\n", strings.Join(params, ", "), artifact.Result)
	fmt.Println("digest:", artifact.Digest)
	fmt.Println("instructions:", len(artifact.Instructions))
	return nil
}

func exportExpr(args []string) error {
	common := flags("export")
	if err := common.set.Parse(args); err != nil {
		return err
	}
	if *common.expr == "" {
		return fmt.Errorf("-expr is required")
	}
	encoded, err := lang.ParseToJSON(*common.expr)
	if err != nil {
		return locate(err, *common.expr)
	}
	if err != nil {
		return err
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, encoded, "", "  "); err != nil {
		return err
	}
	fmt.Println(pretty.String())
	return nil
}

func compile(args []string) error {
	common := flags("compile")
	if err := common.set.Parse(args); err != nil {
		return err
	}
	artifact, err := compileSource(common)
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
	fuel := common.set.Uint64("fuel", 10_000, "execution fuel")
	if err := common.set.Parse(args); err != nil {
		return err
	}
	artifact, err := compileSource(common)
	if err != nil {
		return err
	}
	registry, err := newRegistry()
	if err != nil {
		return err
	}
	runtime, err := lang.Instantiate(artifact, registry)
	if err != nil {
		return err
	}
	rawArgs, err := decodeArgs(*argsSource)
	if err != nil {
		return err
	}
	result, err := runtime.Run(context.Background(), rawArgs, lang.RunOptions{Fuel: *fuel})
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(map[string]any{
		"digest": artifact.Digest,
		"type":   result.Type().String(),
		"value":  result,
	}, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

func decodeArgs(source string) (map[string]any, error) {
	decoder := json.NewDecoder(strings.NewReader(source))
	decoder.UseNumber()
	var rawArgs map[string]any
	if err := decoder.Decode(&rawArgs); err != nil {
		return nil, fmt.Errorf("decode -args: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("decode -args: trailing JSON value")
		}
		return nil, fmt.Errorf("decode -args: %w", err)
	}
	return rawArgs, nil
}

func compileSource(common commonFlags) (*lang.Artifact, error) {
	if *common.expr == "" {
		return nil, fmt.Errorf("-expr is required")
	}
	aliases, err := parseAliases(*common.aliases)
	if err != nil {
		return nil, err
	}
	contract, err := parseContract(*common.types, aliases)
	if err != nil {
		return nil, err
	}
	registry, err := newRegistry()
	if err != nil {
		return nil, err
	}
	artifact, err := lang.CompileExpr(*common.expr, registry, lang.CompileOptions{Args: contract})
	if err != nil {
		return nil, locate(err, *common.expr)
	}
	return artifact, nil
}

// newRegistry is the kernel with every lazy form enabled and the standard
// pack. Domain functions are the host's business, so the CLI registers none of
// those — but a tool for trying expressions out is useless without sum, len
// and the rest.
func newRegistry() (*lang.Registry, error) {
	registry := lang.CoreRegistry()
	if err := registry.EnableForm(lang.SwitchForm, lang.ForForm, lang.ReduceForm); err != nil {
		return nil, err
	}
	if err := std.Register(registry); err != nil {
		return nil, err
	}
	return registry, nil
}

// parseAliases reads -alias 'Order=record{...}'. An alias is spelling only: it
// is expanded where it is named, so nothing about it reaches the artifact.
func parseAliases(source string) (map[string]lang.Type, error) {
	if strings.TrimSpace(source) == "" {
		return nil, nil
	}
	aliases := map[string]lang.Type{}
	for _, part := range splitTopLevel(source) {
		name, text, err := splitDeclaration(part)
		if err != nil {
			return nil, err
		}
		typ, err := lang.ParseType(text)
		if err != nil {
			return nil, fmt.Errorf("type %s: %w", name, err)
		}
		aliases[name] = typ
	}
	return aliases, nil
}

// splitDeclaration cuts "name=type" at the first =, which no type text uses.
func splitDeclaration(part string) (string, string, error) {
	pair := strings.SplitN(strings.TrimSpace(part), "=", 2)
	if len(pair) != 2 || strings.TrimSpace(pair[0]) == "" {
		return "", "", fmt.Errorf("invalid declaration %q, expected name=type", part)
	}
	return strings.TrimSpace(pair[0]), strings.TrimSpace(pair[1]), nil
}

// parseContract reads -types 'a=bool,b=int'. The text is ordered, so the
// contract it produces is ordered too, and that order is the artifact's ABI.
func parseContract(source string, aliases map[string]lang.Type) ([]lang.ArgSpec, error) {
	if strings.TrimSpace(source) == "" {
		return nil, nil
	}
	var contract []lang.ArgSpec
	for _, part := range splitTopLevel(source) {
		name, text, err := splitDeclaration(part)
		if err != nil {
			return nil, err
		}
		typ, err := lang.ParseTypeWith(text, aliases)
		if err != nil {
			return nil, fmt.Errorf("type hint %s: %w", name, err)
		}
		contract = append(contract, lang.ArgSpec{Name: name, Type: typ})
	}
	return contract, nil
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
