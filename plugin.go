// Copyright 2026 Marc-Antoine Ruel. All Rights Reserved. Use of this
// source code is governed by the Apache v2 license that can be found in the
// LICENSE file.

// This file provides commentcheck as a golangci-lint module plugin. Point
// .custom-gcl.yml at this module, build the linter with `golangci-lint custom`,
// and enable the linter named "commentcheck" in .golangci.yml:
//
//	# .custom-gcl.yml
//	version: "2"
//	plugins:
//	  - module: github.com/maruel/commentcheck
//	    import: github.com/maruel/commentcheck
//	    path: ../commentcheck
//
//	# .golangci.yml
//	version: "2"
//	linters:
//	  enable:
//	    - commentcheck
//	  settings:
//	    custom:
//	      commentcheck:
//	        type: module

package commentcheck

import (
	"go/token"
	"strings"

	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"
)

func init() {
	register.Plugin("commentcheck", newPlugin)
}

// settings is empty; the linter has no options yet.
type settings struct{}

func newPlugin(conf any) (register.LinterPlugin, error) {
	if _, err := register.DecodeSettings[settings](conf); err != nil {
		return nil, err
	}
	return plugin{}, nil
}

type plugin struct{}

// BuildAnalyzers implements register.LinterPlugin.
func (plugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{
		{
			Name: "commentcheck",
			Doc:  "reports invalid Go declaration comments and detached doc comments that look like they belong to a declaration in the same file",
			Run:  pluginRun,
		},
	}, nil
}

// GetLoadMode implements register.LinterPlugin.
func (plugin) GetLoadMode() string {
	return register.LoadModeSyntax
}

func pluginRun(pass *analysis.Pass) (interface{}, error) {
	for _, v := range CheckSyntax(pass.Fset, pass.Files) {
		diag := analysis.Diagnostic{Pos: v.Pos, Message: v.Message, Category: "commentcheck"}
		// All fixes reorder one file, so every violation carries suggested
		// edits for `golangci-lint run --fix`.
		if v.Fix != nil {
			if edits, ok := suggestedEdits(pass.Fset, v); ok {
				diag.SuggestedFixes = []analysis.SuggestedFix{
					{Message: "Re-attach or move the comment to satisfy commentcheck", TextEdits: edits},
				}
			}
		}
		pass.Report(diag)
	}
	return nil, nil //nolint:nilnil // analysis.Run results are unused by golangci-lint
}

// suggestedEdits returns the TextEdit applying the fix to the file holding
// the violation: the affected region is replaced such that the comment lands
// at its destination, blank line padding included.
func suggestedEdits(fset *token.FileSet, v Violation) ([]analysis.TextEdit, bool) {
	start, end, replacement, ok := v.Fix.LineEdit()
	if !ok {
		return nil, false
	}
	tf := fset.File(v.Pos)
	if tf == nil || end > tf.LineCount() {
		return nil, false
	}
	editEnd := tf.Pos(tf.Size())
	if end < tf.LineCount() {
		editEnd = tf.LineStart(end + 1)
	}
	text := ""
	if len(replacement) != 0 {
		text = strings.Join(replacement, "\n") + "\n"
	}
	return []analysis.TextEdit{{
		Pos:     tf.LineStart(start),
		End:     editEnd,
		NewText: []byte(text),
	}}, true
}
