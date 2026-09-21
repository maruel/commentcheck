// Copyright 2026 Marc-Antoine Ruel. All Rights Reserved. Use of this
// source code is governed by the Apache v2 license that can be found in the
// LICENSE file.

// Package commentcheck reports invalid Go declaration comments and detached
// doc comments that look like they belong to a declaration in the same file.
//
// An exported function, method, or type must carry a doc comment starting
// with its name. A comment group that names a declaration further below in
// the same file, separated from it by blank lines only, is a doc comment
// that lost its attachment: it is reported with a [Fix] that re-attaches or
// relocates it, so the whole check is auto-fixable.
package commentcheck

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// Package describes one Go package to check or fix.
type Package struct {
	ImportPath   string
	Dir          string
	GoFiles      []string
	CgoFiles     []string
	TestGoFiles  []string
	XTestGoFiles []string
}

// ListPackages resolves the patterns like `go list` does and returns each
// package, including its test files.
func ListPackages(patterns []string) ([]Package, error) {
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	args := append([]string{"list", "-json"}, patterns...)
	cmd := exec.Command("go", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go %s: %w\n%s", strings.Join(args, " "), err, bytes.TrimSpace(out))
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	var pkgs []Package
	for {
		var raw struct {
			Package
			Error *struct{ Err string }
		}
		if err := dec.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode go list JSON: %w", err)
		}
		if raw.Error != nil {
			return nil, fmt.Errorf("%s: %s", raw.ImportPath, raw.Error.Err)
		}
		pkgs = append(pkgs, raw.Package)
	}
	return pkgs, nil
}

// Check parses the package files and returns one Violation per invalid or
// detached comment, sorted by file then line.
func Check(pkg Package) ([]Violation, error) {
	fset := token.NewFileSet()
	var violations []Violation
	for _, group := range []struct {
		names []string
		test  bool
	}{
		{pkg.GoFiles, false},
		{pkg.CgoFiles, false},
		{pkg.TestGoFiles, true},
		{pkg.XTestGoFiles, true},
	} {
		for _, name := range group.names {
			vs, err := checkPath(fset, filepath.Join(pkg.Dir, name), group.test)
			if err != nil {
				return nil, err
			}
			violations = append(violations, vs...)
		}
	}
	sortViolations(violations)
	return violations, nil
}

// CheckSyntax reports violations for already parsed files belonging to one
// package, skipping generated files. It is meant for integrations like
// golangci-lint module plugins that parse the code themselves. The file names
// in fset must be absolute. It does not sort the violations.
func CheckSyntax(fset *token.FileSet, files []*ast.File) []Violation {
	var violations []Violation
	for _, f := range files {
		if ast.IsGenerated(f) {
			continue
		}
		test := strings.HasSuffix(fset.Position(f.Pos()).Filename, "_test.go")
		violations = append(violations, checkFile(fset, f, test)...)
	}
	return violations
}

// checkPath parses one source file and reports its violations.
func checkPath(fset *token.FileSet, path string, test bool) ([]Violation, error) {
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if ast.IsGenerated(f) {
		return nil, nil
	}
	return checkFile(fset, f, test), nil
}

func sortViolations(vs []Violation) {
	slices.SortFunc(vs, func(a, b Violation) int {
		if c := strings.Compare(a.File, b.File); c != 0 {
			return c
		}
		return a.Line - b.Line
	})
}
