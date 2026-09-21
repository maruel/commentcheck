// Copyright 2026 Marc-Antoine Ruel. All Rights Reserved. Use of this
// source code is governed by the Apache v2 license that can be found in the
// LICENSE file.

package commentcheck

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"go/format"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// maxFixPasses bounds the fix loop. Each pass applies every fix whose file no
// other applied fix touches, so the number of passes stays well below this
// bound.
const maxFixPasses = 128

// FixPackage reorders the package files on disk until [Check] reports no
// violation. It returns the violations that remain when it cannot make
// progress, e.g. for missing doc comments, which have no mechanical fix. Each
// moved comment keeps its exact lines, and the edited files are rewritten
// gofmt clean or not at all.
func FixPackage(pkg Package) ([]Violation, error) {
	var touched []string
	seen := map[string]struct{}{}
	for range maxFixPasses {
		vs, err := Check(pkg)
		if err != nil {
			return nil, err
		}
		if len(vs) == 0 {
			return nil, nil
		}
		state, err := hashPackage(pkg)
		if err != nil {
			return vs, err
		}
		if _, ok := seen[state]; ok {
			return vs, errors.New("fixes cycle without converging; resolve the remaining violations by hand")
		}
		seen[state] = struct{}{}
		// Apply every fix whose file no other applied fix touches this pass;
		// line numbers of untouched files stay valid.
		used := map[string]bool{}
		applied := 0
		for i := range vs {
			fix := vs[i].Fix
			if fix == nil || used[fix.File] {
				continue
			}
			if err := applyFix(fix); err != nil {
				return vs, err
			}
			used[fix.File] = true
			if !slices.Contains(touched, fix.File) {
				touched = append(touched, fix.File)
			}
			applied++
		}
		if applied == 0 {
			return vs, errors.New("no fix can be applied; resolve the remaining violations by hand")
		}
	}
	vs, err := Check(pkg)
	if err != nil {
		return nil, err
	}
	return vs, fmt.Errorf("did not converge after %d fix passes", maxFixPasses)
}

// hashPackage summarizes the package source files so repeated states can be
// detected.
func hashPackage(pkg Package) (string, error) {
	h := sha256.New()
	names := slices.Concat(pkg.GoFiles, pkg.CgoFiles, pkg.TestGoFiles, pkg.XTestGoFiles)
	for _, name := range names {
		src, err := os.ReadFile(filepath.Join(pkg.Dir, name))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", name, len(src)) //nolint:errcheck // writes to a hash
		h.Write(src)
	}
	return string(h.Sum(nil)), nil
}

// applyFix rewrites the file holding the fix gofmt clean or not at all.
func applyFix(fix *Fix) error {
	contents, err := fix.Contents()
	if err != nil {
		return err
	}
	for path, content := range contents {
		mode := os.FileMode(0o644)
		if info, err := os.Stat(path); err == nil {
			mode = info.Mode().Perm()
		}
		if err := os.WriteFile(path, content, mode); err != nil {
			return err
		}
	}
	return nil
}

// Fix is one mechanical reorder of a single file: the lines [Start, End]
// (1-based, inclusive) are removed, and the comment lines Text are reinserted
// directly above line DstLine (1-based, in the original file) so they become
// its doc comment. Text is empty when the fix only removes lines, e.g. the
// blank lines that detached a doc comment.
type Fix struct {
	File       string // absolute path of the file to edit
	Start, End int    // 1-based inclusive line range to remove
	Text       string // comment lines to reinsert above DstLine, newline separated; empty to only remove
	DstLine    int    // 1-based line of the declaration the comment re-attaches to; 0 when Text is empty
}

// LineEdit describes the fix in original file coordinates: the lines [start,
// end] are replaced by replacement, which relocates the comment to its
// destination, blank line padding included. ok is false when the fix is stale.
func (fix *Fix) LineEdit() (start, end int, replacement []string, ok bool) {
	lines, err := readLines(fix.File)
	if err != nil || fix.Start < 1 || fix.Start > fix.End || fix.End > len(lines) {
		return 0, 0, nil, false
	}
	if fix.Text == "" {
		return fix.Start, fix.End, nil, true
	}
	block := strings.Split(fix.Text, "\n")
	d0, d1 := fix.Start-1, fix.End // 0-based inclusive remove range
	orig := fix.DstLine - 1        // 0-based insertion index in original coordinates
	shift := min(max(orig, d0), d1) - d0
	idx := orig - shift
	rest := slices.Delete(slices.Clone(lines), d0, d1)
	padded := paddedComment(rest, idx, block)
	switch {
	case idx == d0:
		return 0, 0, nil, false // already in place
	case idx < d0: // the comment moves up
		start, end = idx+1, fix.End
		replacement = append(slices.Clone(padded), lines[idx:d0]...)
	default: // the comment moves down
		start, end = fix.Start, orig
		replacement = append(slices.Clone(lines[d1:orig]), padded...)
	}
	return start, end, replacement, true
}

// Contents returns the gofmt-clean replacement contents for the file the fix
// touches, keyed by absolute path. The file on disk is left untouched.
func (fix *Fix) Contents() (map[string][]byte, error) {
	start, end, replacement, ok := fix.LineEdit()
	if !ok {
		return nil, fmt.Errorf("%s: stale or invalid fix: lines %d-%d", fix.File, fix.Start, fix.End)
	}
	lines, err := readLines(fix.File)
	if err != nil {
		return nil, err
	}
	out := slices.Concat(lines[:start-1], replacement, lines[end:])
	content, err := formatLines(fix.File, out)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{fix.File: content}, nil
}

// paddedComment adds a blank line above block when its insertion point idx in
// lines would otherwise butt the comment against the preceding code. No blank
// line is added below: the comment must attach to the declaration that
// follows it.
func paddedComment(lines []string, idx int, block []string) []string {
	if idx > 0 && idx <= len(lines) && lines[idx-1] != "" {
		block = append([]string{""}, block...)
	}
	return block
}

// formatLines validates that the edited lines still parse and returns the
// file contents gofmt clean.
func formatLines(path string, lines []string) ([]byte, error) {
	formatted, err := format.Source([]byte(strings.Join(lines, "\n") + "\n"))
	if err != nil {
		return nil, fmt.Errorf("format %s after edit: %w", path, err)
	}
	return formatted, nil
}

// readLines returns the 1-indexable lines of the file at path, without the
// trailing newline.
func readLines(path string) ([]string, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	src = bytes.TrimSuffix(src, []byte("\n"))
	lines := strings.Split(string(src), "\n")
	return lines, nil
}

// pathOf returns the absolute, cleaned path of pos.
func pathOf(fset *token.FileSet, pos token.Pos) string {
	return cleanPath(fset.Position(pos).Filename)
}

// cleanPath cleans and makes path absolute, falling back to the cleaned
// relative path.
func cleanPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

// rel renders path relative to the working directory when it is a subpath,
// leaving it absolute otherwise.
func rel(path string) string {
	wd, err := filepath.Abs(".")
	if err != nil {
		return path
	}
	r, err := filepath.Rel(wd, path)
	if err != nil || strings.HasPrefix(r, ".."+string(filepath.Separator)) || r == ".." {
		return path
	}
	return r
}
