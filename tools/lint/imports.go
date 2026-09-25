package main

import (
	"errors"
	"go/ast"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The implementation under internal/ is imported only by the code that
// publishes it: the root package, whose files are aliases and forwards, and
// the language server in lsp/. Go's internal rule stops a host outside the
// module, but not this module's own extensions/, examples/, tests/ and cmd/,
// which are meant to stand where a host stands — so this check keeps them
// there, and the host-side code under internal/ (hostSide) too.

// hostSide is the code under internal/ that stands where a host stands and
// may use only the public package: the demo console that shows a host how to
// assemble one. Being a host itself, anyone may import it.
var hostSide = map[string]bool{"internal/demo": true}

// modulePath reads the module path from root's go.mod.
func modulePath(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	for line := range strings.Lines(string(data)) {
		if path, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(path), nil
		}
	}
	return "", errors.New("go.mod has no module line")
}

// mayImportInternal reports the files that publish the implementation.
func mayImportInternal(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	dir := filepath.ToSlash(filepath.Dir(relative))
	switch {
	case dir == ".":
		return !strings.HasSuffix(path, "_test.go")
	case hostSide[dir]:
		return false
	case dir == "lsp", strings.HasPrefix(dir, "internal/"), strings.HasPrefix(dir, "lsp/"):
		return true
	}
	return false
}

func checkInternalImports(root, module, path string, file *ast.File, line func(ast.Node) int) []violation {
	if mayImportInternal(root, path) {
		return nil
	}
	var violations []violation
	for _, spec := range file.Imports {
		imported, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		if hostSide[strings.TrimPrefix(imported, module+"/")] {
			continue // host-side code uses only the public package, so anyone may
		}
		if imported == module+"/internal" || strings.HasPrefix(imported, module+"/internal/") {
			violations = append(violations, violation{path, line(spec), "imports " + imported + ": only the root package and lsp publish internal/; use the public package " + module})
		}
	}
	return violations
}
