// Command lint enforces the repository style budget — file length, function
// length and nesting depth — that only the root package and lsp import
// internal/ (see checkInternalImports), and, in _test.go files, the testing
// package's current idioms (see checkTestFile). It uses only the standard library so the
// project keeps zero third-party dependencies.
package main

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
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
	slices.SortFunc(violations, func(a, b violation) int {
		return cmp.Or(strings.Compare(a.file, b.file), cmp.Compare(a.line, b.line))
	})
	for _, item := range violations {
		fmt.Printf("%s:%d: %s\n", item.file, item.line, item.message)
	}
	fmt.Fprintf(os.Stderr, "\nlint: %d violation(s)\n", len(violations))
	os.Exit(1)
}

func run(root string) ([]violation, error) {
	module, err := modulePath(root)
	if err != nil {
		return nil, err
	}
	var violations []violation
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skipDir(entry.Name()) && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		found, err := checkFile(root, module, path)
		if err != nil {
			return err
		}
		violations = append(violations, found...)
		return nil
	})
	return violations, err
}

func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "testdata" || name == "dist" || name == "node_modules"
}

func checkFile(root, module, path string) ([]violation, error) {
	extension := filepath.Ext(path)
	if extension != ".go" && extension != ".js" && extension != ".ts" {
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
	functions, err := checkGoFunctions(root, module, path, source)
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
	return []violation{{path, 1, fmt.Sprintf("file has %d lines, limit is %d", lines, maxFileLines)}}
}

func checkGoFunctions(root, module, path string, source []byte) ([]violation, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, source, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	test := strings.HasSuffix(path, "_test.go")
	var violations []violation
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		violations = append(violations, checkFunction(path, fileSet, function)...)
		if test {
			violations = append(violations, checkTestFunction(path, fileSet, function)...)
		}
	}
	line := func(node ast.Node) int { return fileSet.Position(node.Pos()).Line }
	violations = append(violations, checkInternalImports(root, module, path, file, line)...)
	if test {
		violations = append(violations, checkTestPairing(path)...)
	}
	return violations, nil
}

func checkFunction(path string, fileSet *token.FileSet, function *ast.FuncDecl) []violation {
	var violations []violation
	start := fileSet.Position(function.Pos()).Line
	name := function.Name.Name
	if lines := fileSet.Position(function.End()).Line - start + 1; lines > maxFuncLines {
		violations = append(violations, violation{path, start, fmt.Sprintf("func %s has %d lines, limit is %d", name, lines, maxFuncLines)})
	}
	if depth := blockDepth(function.Body); depth > maxNestingDepth {
		violations = append(violations, violation{path, start, fmt.Sprintf("func %s nests %d levels deep, limit is %d", name, depth, maxNestingDepth)})
	}
	return violations
}

// blockDepth reports how deeply blocks nest inside node, not counting node
// itself: each block — an if, else, for, switch or select body, a function
// literal, a bare block — is one level. `else if` continues the level of its
// if, since the else holds the next if, not a block.
func blockDepth(node ast.Node) int {
	deepest := 0
	ast.Inspect(node, func(child ast.Node) bool {
		if block, ok := child.(*ast.BlockStmt); ok && child != node {
			deepest = max(deepest, 1+blockDepth(block))
			return false
		}
		return true
	})
	return deepest
}
