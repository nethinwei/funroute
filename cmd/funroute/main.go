package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
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

func usage() {
	fmt.Fprintln(os.Stderr, `funroute - strongly typed pure expression language

Commands:
  inspect -expr 'if(a,b,add(1,1))'
  export  -expr 'if(a,b,add(1,1))'
  compile -expr 'if(a,b,add(1,1))'
  run     -expr 'if(a,b,add(1,1))' -args '{"a":true,"b":7}'

The contract is the host's: -types 'a=bool,b=int' declares the arguments and
their order. Without it both are inferred.`)
}

type commonFlags struct {
	set   *flag.FlagSet
	expr  *string
	types *string
}

func flags(name string) commonFlags {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	return commonFlags{
		set:   set,
		expr:  set.String("expr", "", "expression source"),
		types: set.String("types", "", "comma-separated argument type hints"),
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
	result, err := runtime.Run(rawArgs, lang.RunOptions{Fuel: *fuel})
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(map[string]any{
		"digest": artifact.Digest,
		"type":   result.Type().String(),
		"value":  result.Any(),
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
	return rawArgs, nil
}

func compileSource(common commonFlags) (*lang.Artifact, error) {
	if *common.expr == "" {
		return nil, fmt.Errorf("-expr is required")
	}
	contract, err := parseContract(*common.types)
	if err != nil {
		return nil, err
	}
	registry, err := newRegistry()
	if err != nil {
		return nil, err
	}
	return lang.CompileExpr(*common.expr, registry, lang.CompileOptions{Args: contract})
}

// newRegistry is the kernel with every lazy form enabled. Domain functions are
// the host's business, so the CLI registers none.
func newRegistry() (*lang.Registry, error) {
	registry := lang.CoreRegistry()
	if err := registry.EnableForm(lang.SwitchForm, lang.ForForm, lang.ReduceForm); err != nil {
		return nil, err
	}
	return registry, nil
}

// parseContract reads -types 'a=bool,b=int'. The text is ordered, so the
// contract it produces is ordered too, and that order is the artifact's ABI.
func parseContract(source string) ([]lang.ArgSpec, error) {
	if strings.TrimSpace(source) == "" {
		return nil, nil
	}
	var contract []lang.ArgSpec
	for _, part := range splitTopLevel(source) {
		pair := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(pair) != 2 || strings.TrimSpace(pair[0]) == "" {
			return nil, fmt.Errorf("invalid type hint %q", part)
		}
		name := strings.TrimSpace(pair[0])
		typ, err := lang.ParseType(strings.TrimSpace(pair[1]))
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
		case '<':
			depth++
		case '>':
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
