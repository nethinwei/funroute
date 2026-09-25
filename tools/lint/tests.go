package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// A test file is named for the source file it tests: foo_test.go sits next
// to foo.go, so the tests of a piece of code are found where the code is.
// Go's own conventions are the exceptions: example_test.go holds a package's
// godoc examples, and export_test.go gives external tests the internals they
// need. So is a test suite, a package whose only source file is doc.go: the
// ones under tests/ test what no one file holds — the public surface by
// topic, the language against its examples, the limits the docs state.
var unpairedTestFiles = map[string]bool{"example_test.go": true, "export_test.go": true}

func checkTestPairing(path string) []violation {
	name := filepath.Base(path)
	if unpairedTestFiles[name] || isTestSuite(filepath.Dir(path)) {
		return nil
	}
	source := filepath.Join(filepath.Dir(path), strings.TrimSuffix(name, "_test.go")+".go")
	if _, err := os.Stat(source); err == nil {
		return nil
	}
	return []violation{{path, 1, "test file has no source file " + filepath.Base(source) + " to test"}}
}

// isTestSuite reports a directory whose only source file is doc.go.
func isTestSuite(dir string) bool {
	sources, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	sources = slices.DeleteFunc(sources, func(source string) bool { return strings.HasSuffix(source, "_test.go") })
	return len(sources) == 1 && filepath.Base(sources[0]) == "doc.go"
}

// checkTestFunction holds a function of a _test.go file to the testing package's current
// idioms, the ones that keep a test from outliving its t and a benchmark from
// timing its own setup:
//
//   - t.Context() (or b's, or f's) rather than context.Background() or
//     context.TODO() in a function that has a testing value: it is cancelled
//     when the test ends. An Example has none, and may use Background;
//   - testing/synctest rather than time.Sleep in a test, so a timer runs on a
//     fake clock instead of making the test slow and flaky. A benchmark is
//     exempt: it measures real time, and sleeping is how it stands in for a
//     call that blocks, such as an inference engine;
//   - for b.Loop() rather than a loop over b.N, which also keeps the setup
//     before it out of the timing;
//   - a helper that takes a testing value calls Helper() first, so a failure
//     is reported at the line of the test that called it.
func checkTestFunction(path string, fileSet *token.FileSet, function *ast.FuncDecl) []violation {
	var violations []violation
	params := testingParams(function.Type)
	if message, bad := missingHelper(function, params); bad {
		violations = append(violations, violation{path, fileSet.Position(function.Pos()).Line, message})
	}
	benchmark := strings.HasPrefix(function.Name.Name, "Benchmark")
	ast.Inspect(function.Body, func(node ast.Node) bool {
		if message, bad := testIdiom(node, params, benchmark); bad {
			violations = append(violations, violation{path, fileSet.Position(node.Pos()).Line, message})
		}
		return true
	})
	return violations
}

// testingParams maps each parameter of a function that is a testing value to
// its type: "T", "B", "F" or "TB".
func testingParams(signature *ast.FuncType) map[string]string {
	params := map[string]string{}
	for _, field := range signature.Params.List {
		kind := testingKind(field.Type)
		if kind == "" {
			continue
		}
		for _, name := range field.Names {
			params[name.Name] = kind
		}
	}
	return params
}

// testingKinds names the testing values a parameter can hold.
var testingKinds = map[string]string{"*testing.T": "T", "*testing.B": "B", "*testing.F": "F", "testing.TB": "TB"}

func testingKind(expr ast.Expr) string {
	return testingKinds[types.ExprString(expr)]
}

// missingHelper reports a helper — a function with a testing value that is
// not itself a test, benchmark, fuzz target or example — whose first
// statement does not mark it as one.
func missingHelper(function *ast.FuncDecl, params map[string]string) (string, bool) {
	if len(params) == 0 || isTestEntry(function.Name.Name) {
		return "", false
	}
	if len(function.Body.List) > 0 && callsHelper(function.Body.List[0], params) {
		return "", false
	}
	return "helper " + function.Name.Name + " takes a testing value but does not call Helper() first", true
}

func isTestEntry(name string) bool {
	return slices.ContainsFunc([]string{"Test", "Benchmark", "Fuzz", "Example"}, func(prefix string) bool {
		return strings.HasPrefix(name, prefix)
	})
}

func callsHelper(statement ast.Stmt, params map[string]string) bool {
	expression, ok := statement.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := expression.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	receiver, ok := strings.CutSuffix(types.ExprString(call.Fun), ".Helper")
	return ok && params[receiver] != ""
}

// testIdiom reports one node of a test that has a better spelling.
func testIdiom(node ast.Node, params map[string]string, benchmark bool) (string, bool) {
	switch node := node.(type) {
	case *ast.CallExpr:
		return idiomaticCall(node, params, benchmark)
	case *ast.SelectorExpr:
		receiver, ok := node.X.(*ast.Ident)
		if ok && node.Sel.Name == "N" && params[receiver.Name] == "B" {
			return "loop with for " + receiver.Name + ".Loop() rather than over " + receiver.Name + ".N", true
		}
	}
	return "", false
}

func idiomaticCall(call *ast.CallExpr, params map[string]string, benchmark bool) (string, bool) {
	switch name := types.ExprString(call.Fun); name {
	case "time.Sleep":
		if !benchmark {
			return "a test does not sleep: run timers on synctest's fake clock", true
		}
	case "context.Background", "context.TODO":
		if len(params) > 0 {
			return "use the testing value's Context() rather than " + name + "()", true
		}
	}
	return "", false
}
