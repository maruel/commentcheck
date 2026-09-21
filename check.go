// Copyright 2026 Marc-Antoine Ruel. All Rights Reserved. Use of this
// source code is governed by the Apache v2 license that can be found in the
// LICENSE file.

package commentcheck

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"
	"unicode"
)

// Violation is one comment violation. Fix is nil when no automatic reorder is
// known, e.g. for a missing doc comment.
type Violation struct {
	File    string // absolute path of the offending file
	Line    int    // 1-based line of the offending comment or declaration
	Pos     token.Pos
	Message string
	Fix     *Fix
}

// String renders the violation as a diagnostic line with a path relative to
// the working directory when possible.
func (v *Violation) String() string {
	return fmt.Sprintf("%s:%d: %s", rel(v.File), v.Line, v.Message)
}

// checkFile reports the violations of one parsed file.
func checkFile(fset *token.FileSet, f *ast.File, test bool) []Violation {
	decls := collectDeclNames(f)
	docComments := map[*ast.CommentGroup]struct{}{}
	declNames := map[string]struct{}{}
	for _, d := range decls {
		declNames[d.name] = struct{}{}
		if d.doc != nil {
			docComments[d.doc] = struct{}{}
		}
	}
	var violations []Violation
	if !test {
		violations = append(violations, checkExportedDocs(fset, decls)...)
	}
	violations = append(violations, checkDetachedComments(fset, f, docComments, declNames, decls)...)
	return violations
}

// checkExportedDocs reports each exported function, method, and type whose
// doc comment is missing or does not start with the symbol name. Constants
// and variables, methods on unexported receivers, and methods implementing
// well-known standard library interfaces are exempt.
func checkExportedDocs(fset *token.FileSet, decls []declName) []Violation {
	var vs []Violation
	for _, d := range decls {
		if !ast.IsExported(d.name) || d.kind == "value" || d.method && commonMethods[d.name] {
			continue
		}
		if d.doc == nil {
			vs = append(vs, Violation{
				File:    pathOf(fset, d.pos),
				Line:    fset.Position(d.pos).Line,
				Pos:     d.pos,
				Message: fmt.Sprintf("exported declaration %s has no doc comment", d.name),
			})
			continue
		}
		if firstCommentWord(d.doc) != d.name {
			pos := fset.Position(d.doc.Pos())
			vs = append(vs, Violation{
				File:    pathOf(fset, d.doc.Pos()),
				Line:    pos.Line,
				Pos:     d.doc.Pos(),
				Message: fmt.Sprintf("doc comment for %s should start with %q", d.name, d.name),
			})
		}
	}
	return vs
}

// checkDetachedComments reports each comment group that names a declaration
// further below in the same file and is separated from it by blank lines
// only. When the named declaration is the next one, the blank lines simply
// detached the doc comment and the fix removes them. Otherwise the comment
// names another declaration, and the fix moves the comment above the first
// same-named declaration that has no doc comment, if any.
func checkDetachedComments(fset *token.FileSet, f *ast.File, docComments map[*ast.CommentGroup]struct{}, declNames map[string]struct{}, decls []declName) []Violation {
	lines, err := readLines(pathOf(fset, f.Pos()))
	if err != nil {
		lines = nil
	}
	var vs []Violation
	for _, group := range f.Comments {
		if _, ok := docComments[group]; ok {
			continue
		}
		name := firstCommentWord(group)
		if name == "" {
			continue
		}
		if _, ok := declNames[name]; !ok {
			continue
		}
		next, ok := nextDeclAfter(fset, decls, group.End())
		if !ok || !onlyBlankLines(lines, fset.Position(group.End()).Line, fset.Position(next.pos).Line) {
			continue
		}
		v := Violation{
			File: pathOf(fset, group.Pos()),
			Line: fset.Position(group.Pos()).Line,
			Pos:  group.Pos(),
		}
		if next.name == name {
			v.Message = fmt.Sprintf("doc comment for %s is detached by a blank line", name)
			v.Fix = &Fix{
				File: v.File,
				// Remove the blank lines between the comment and its
				// declaration so they attach again.
				Start: fset.Position(group.End()).Line + 1,
				End:   fset.Position(next.pos).Line - 1,
			}
		} else {
			v.Message = fmt.Sprintf("detached doc-like comment for %s before %s; move it next to the declaration or reword it", name, next.name)
			if target, ok := fixTarget(decls, name, next.pos); ok {
				v.Fix = &Fix{
					File:    v.File,
					Start:   fset.Position(group.Pos()).Line,
					End:     fset.Position(group.End()).Line,
					Text:    groupText(lines, fset, group),
					DstLine: fset.Position(target.pos).Line,
				}
			}
		}
		vs = append(vs, v)
	}
	return vs
}

// fixTarget returns the first declaration named name that has no doc comment,
// so the detached comment can become its doc comment. It reports false when
// every same-named declaration is already documented.
func fixTarget(decls []declName, name string, notPos token.Pos) (declName, bool) {
	for _, d := range decls {
		if d.name == name && d.doc == nil && d.pos != notPos {
			return d, true
		}
	}
	return declName{}, false
}

// groupText returns the exact source lines of the comment group, or "" when
// the source is not available.
func groupText(lines []string, fset *token.FileSet, group *ast.CommentGroup) string {
	if lines == nil {
		return ""
	}
	start := fset.Position(group.Pos()).Line
	end := fset.Position(group.End()).Line
	if start < 1 || end > len(lines) || start > end {
		return ""
	}
	return strings.Join(lines[start-1:end], "\n")
}

// declName is one declaration eligible for a doc comment.
type declName struct {
	name   string
	pos    token.Pos
	doc    *ast.CommentGroup
	kind   string // "func", "type", or "value"
	method bool   // declared with a receiver
}

// commonMethods lists the method names that implement well-known standard
// library interfaces. Like revive's exported rule, methods with these names
// are exempt from the doc comment requirement.
var commonMethods = map[string]bool{
	"Close":     true,
	"Error":     true,
	"Flush":     true,
	"Read":      true,
	"ReadByte":  true,
	"ReadFrom":  true,
	"ReadRune":  true,
	"Scan":      true,
	"Seek":      true,
	"ServeHTTP": true,
	"String":    true,
	"Unwrap":    true,
	"Value":     true,
	"Write":     true,
	"WriteTo":   true,
}

// collectDeclNames returns the declarations that carry or could carry a doc
// comment: functions and types, including methods on exported receivers.
// Methods on unexported receivers are skipped entirely, like their unexported
// receiver type is.
func collectDeclNames(f *ast.File) []declName {
	var out []declName
	for _, decl := range f.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			method := decl.Recv != nil && len(decl.Recv.List) != 0
			if method && !ast.IsExported(receiverTypeName(decl.Recv.List[0].Type)) {
				continue
			}
			out = append(out, declName{name: decl.Name.Name, pos: decl.Name.Pos(), doc: decl.Doc, kind: "func", method: method})
		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					out = append(out, declName{name: spec.Name.Name, pos: spec.Name.Pos(), doc: docForSpec(decl.Doc, spec.Doc), kind: "type"})
				case *ast.ValueSpec:
					for _, name := range spec.Names {
						out = append(out, declName{name: name.Name, pos: name.Pos(), doc: docForSpec(decl.Doc, spec.Doc), kind: "value"})
					}
				}
			}
		}
	}
	return out
}

// nextDeclAfter returns the declaration that starts on the first line after
// pos.
func nextDeclAfter(fset *token.FileSet, decls []declName, pos token.Pos) (declName, bool) {
	var best declName
	found := false
	for _, d := range decls {
		if d.pos <= pos {
			continue
		}
		if !found || fset.Position(d.pos).Line < fset.Position(best.pos).Line {
			best = d
			found = true
		}
	}
	return best, found
}

// onlyBlankLines reports whether the lines strictly between the comment end
// at fromLine and the declaration at toLine are all blank. lines is nil when
// the source is unavailable, in which case nothing is assumed blank.
func onlyBlankLines(lines []string, fromLine, toLine int) bool {
	if lines == nil {
		return false
	}
	for line := fromLine + 1; line < toLine; line++ {
		if line < 1 || line > len(lines) {
			continue
		}
		if strings.TrimSpace(lines[line-1]) != "" {
			return false
		}
	}
	return true
}

// docForSpec returns the doc comment of a spec, falling back to the doc
// comment of its enclosing declaration.
func docForSpec(groupDoc, specDoc *ast.CommentGroup) *ast.CommentGroup {
	if specDoc != nil {
		return specDoc
	}
	return groupDoc
}

// firstCommentWord returns the first word of the comment group text, skipping
// directives like //go:generate and //+build. It returns "" when the comment
// does not start with a word.
func firstCommentWord(group *ast.CommentGroup) string {
	if group == nil || len(group.List) == 0 {
		return ""
	}
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(group.List[0].Text, "//"), "/*"))
	text = strings.TrimLeftFunc(text, unicode.IsSpace)
	if text == "" || strings.HasPrefix(text, "go:") || strings.HasPrefix(text, "+") {
		return ""
	}
	for i, r := range text {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return text[:i]
		}
	}
	return text
}

// receiverTypeName returns the name of the receiver type expression, digging
// through pointers and type parameters.
func receiverTypeName(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name
	case *ast.StarExpr:
		return receiverTypeName(expr.X)
	case *ast.IndexExpr:
		return receiverTypeName(expr.X)
	case *ast.IndexListExpr:
		return receiverTypeName(expr.X)
	default:
		return ""
	}
}
