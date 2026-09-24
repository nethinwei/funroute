package main

import (
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// A test file is named for the source file it tests: foo_test.go sits next
// to foo.go, so the tests of a piece of code are found where the code is.
// Go's own conventions are the exceptions: example_test.go holds a package's
// godoc examples, and export_test.go gives external tests the internals they
// need. So is a test suite, a package whose only source file is doc.go — the
// public surface's, internal/hosttest, tests the one file funroute.go by
// topic.
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
	count := 0
	for _, source := range sources {
		if !strings.HasSuffix(source, "_test.go") {
			count++
			if filepath.Base(source) != "doc.go" {
				return false
			}
		}
	}
	return count == 1
}

// checkTestFile holds a _test.go file to the testing package's current
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
func checkTestFile(path string, fileSet *token.FileSet, file *ast.File) []violation {
	var violations []violation
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
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
	}
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

func testingKind(expr ast.Expr) string {
	if star, ok := expr.(*ast.StarExpr); ok {
		if kind := testingSelector(star.X); kind == "T" || kind == "B" || kind == "F" {
			return kind
		}
		return ""
	}
	if kind := testingSelector(expr); kind == "TB" {
		return kind
	}
	return ""
}

// testingSelector is the name in testing.<name>, or "".
func testingSelector(expr ast.Expr) string {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	if pkg, ok := selector.X.(*ast.Ident); ok && pkg.Name == "testing" {
		return selector.Sel.Name
	}
	return ""
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
	for _, prefix := range []string{"Test", "Benchmark", "Fuzz", "Example"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
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
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Helper" {
		return false
	}
	receiver, ok := selector.X.(*ast.Ident)
	return ok && params[receiver.Name] != ""
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
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	switch pkg.Name + "." + selector.Sel.Name {
	case "time.Sleep":
		if !benchmark {
			return "a test does not sleep: run timers on synctest's fake clock", true
		}
	case "context.Background", "context.TODO":
		if len(params) > 0 {
			return "use the testing value's Context() rather than context." + selector.Sel.Name + "()", true
		}
	}
	return "", false
}
