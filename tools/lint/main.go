// Command lint enforces the repository style budget: file length, function
// length and nesting depth. It uses only the standard library so the project
// keeps zero third-party dependencies.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	maxFileLines    = 800
	maxFuncLines    = 50
	maxNestingDepth = 3
)

type violation struct {
	file    string
	line    int
	message string
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	violations, err := run(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if len(violations) == 0 {
		return
	}
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].file != violations[j].file {
			return violations[i].file < violations[j].file
		}
		return violations[i].line < violations[j].line
	})
	for _, item := range violations {
		fmt.Printf("%s:%d: %s\n", item.file, item.line, item.message)
	}
	fmt.Fprintf(os.Stderr, "\nlint: %d violation(s)\n", len(violations))
	os.Exit(1)
}

func run(root string) ([]violation, error) {
	var violations []violation
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skipDir(entry.Name()) && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		found, err := checkFile(path)
		if err != nil {
			return err
		}
		violations = append(violations, found...)
		return nil
	})
	return violations, err
}

func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "testdata"
}

func checkFile(path string) ([]violation, error) {
	extension := filepath.Ext(path)
	if extension != ".go" && extension != ".js" {
		return nil, nil
	}
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	violations := checkFileLength(path, source)
	if extension != ".go" {
		return violations, nil
	}
	functions, err := checkGoFunctions(path, source)
	if err != nil {
		return nil, err
	}
	return append(violations, functions...), nil
}

func checkFileLength(path string, source []byte) []violation {
	lines := strings.Count(string(source), "\n")
	if len(source) > 0 && !strings.HasSuffix(string(source), "\n") {
		lines++
	}
	if lines <= maxFileLines {
		return nil
	}
	return []violation{{
		file:    path,
		line:    1,
		message: fmt.Sprintf("file has %d lines, limit is %d", lines, maxFileLines),
	}}
}

func checkGoFunctions(path string, source []byte) ([]violation, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, source, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var violations []violation
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		position := fileSet.Position(function.Pos())
		violations = append(violations, checkFunction(path, position, fileSet, function)...)
	}
	return violations, nil
}

func checkFunction(path string, position token.Position, fileSet *token.FileSet, function *ast.FuncDecl) []violation {
	var violations []violation
	lines := fileSet.Position(function.End()).Line - position.Line + 1
	if lines > maxFuncLines {
		violations = append(violations, violation{
			file:    path,
			line:    position.Line,
			message: fmt.Sprintf("func %s has %d lines, limit is %d", function.Name.Name, lines, maxFuncLines),
		})
	}
	if depth := blockDepth(function.Body, 0); depth > maxNestingDepth {
		violations = append(violations, violation{
			file:    path,
			line:    position.Line,
			message: fmt.Sprintf("func %s nests %d levels deep, limit is %d", function.Name.Name, depth, maxNestingDepth),
		})
	}
	return violations
}

// nestedChild is a child node that opens a new nesting level, together with
// the cost of entering it: `else if` continues the current level and costs 0.
type nestedChild struct {
	node ast.Node
	cost int
}

func nestingChildren(node ast.Node) ([]nestedChild, bool) {
	switch statement := node.(type) {
	case *ast.IfStmt:
		children := []nestedChild{{node: statement.Body, cost: 1}}
		if statement.Else != nil {
			cost := 1
			if _, isElseIf := statement.Else.(*ast.IfStmt); isElseIf {
				cost = 0
			}
			children = append(children, nestedChild{node: statement.Else, cost: cost})
		}
		return children, true
	case *ast.ForStmt:
		return []nestedChild{{node: statement.Body, cost: 1}}, true
	case *ast.RangeStmt:
		return []nestedChild{{node: statement.Body, cost: 1}}, true
	case *ast.SwitchStmt:
		return []nestedChild{{node: statement.Body, cost: 1}}, true
	case *ast.TypeSwitchStmt:
		return []nestedChild{{node: statement.Body, cost: 1}}, true
	case *ast.SelectStmt:
		return []nestedChild{{node: statement.Body, cost: 1}}, true
	case *ast.FuncLit:
		return []nestedChild{{node: statement.Body, cost: 1}}, true
	}
	return nil, false
}

// blockDepth reports the deepest nesting level reached inside node.
func blockDepth(node ast.Node, current int) int {
	deepest := current
	keep := func(found int) {
		if found > deepest {
			deepest = found
		}
	}
	if children, ok := nestingChildren(node); ok {
		for _, child := range children {
			keep(blockDepth(child.node, current+child.cost))
		}
		return deepest
	}
	ast.Inspect(node, func(child ast.Node) bool {
		if child == nil || child == node {
			return true
		}
		if _, ok := nestingChildren(child); !ok {
			return true
		}
		keep(blockDepth(child, current))
		return false
	})
	return deepest
}
