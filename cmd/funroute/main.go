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

Optional type hints use -types 'a=bool,b=int'.
Use -profile engineer to also enable recur (Turing-complete layer).`)
}

type commonFlags struct {
	set     *flag.FlagSet
	expr    *string
	types   *string
	profile *string
}

func flags(name string) commonFlags {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	return commonFlags{
		set:     set,
		expr:    set.String("expr", "", "expression source"),
		types:   set.String("types", "", "comma-separated argument type hints"),
		profile: set.String("profile", "operator", "authoring profile: operator or engineer"),
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
	expr, err := lang.Parse(*common.expr)
	if err != nil {
		return err
	}
	encoded, err := lang.ExportExprJSON(expr)
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
	maxRecursion := common.set.Int("max-recursion", 128, "maximum recursive calls")
	if err := common.set.Parse(args); err != nil {
		return err
	}
	artifact, err := compileSource(common)
	if err != nil {
		return err
	}
	registry, err := newRegistry(*common.profile)
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
	result, err := runtime.Run(rawArgs, lang.RunOptions{Fuel: *fuel, MaxRecursion: *maxRecursion})
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
	hints, err := parseTypes(*common.types)
	if err != nil {
		return nil, err
	}
	registry, err := newRegistry(*common.profile)
	if err != nil {
		return nil, err
	}
	return lang.CompileExpr(*common.expr, registry, lang.CompileOptions{ArgTypes: hints})
}

// newRegistry builds the console the flag asks for: which lazy forms are
// enabled is the whole difference between the two.
func newRegistry(profile string) (*lang.Registry, error) {
	forms := []lang.Form{lang.SwitchForm, lang.ForForm, lang.ReduceForm}
	switch profile {
	case "", "operator":
	case "engineer":
		forms = append(forms, lang.RecurForm)
	default:
		return nil, fmt.Errorf("unknown profile %q", profile)
	}
	registry := lang.CoreRegistry()
	if err := registry.EnableForm(forms...); err != nil {
		return nil, err
	}
	return registry, nil
}

func parseTypes(source string) (map[string]lang.Type, error) {
	hints := map[string]lang.Type{}
	if strings.TrimSpace(source) == "" {
		return hints, nil
	}
	parts := splitTopLevel(source)
	for _, part := range parts {
		pair := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(pair) != 2 || strings.TrimSpace(pair[0]) == "" {
			return nil, fmt.Errorf("invalid type hint %q", part)
		}
		name := strings.TrimSpace(pair[0])
		typ, err := lang.ParseType(strings.TrimSpace(pair[1]))
		if err != nil {
			return nil, fmt.Errorf("type hint %s: %w", name, err)
		}
		if _, exists := hints[name]; exists {
			return nil, fmt.Errorf("duplicate type hint %q", name)
		}
		hints[name] = typ
	}
	return hints, nil
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
