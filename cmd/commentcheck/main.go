// Copyright 2026 Marc-Antoine Ruel. All Rights Reserved. Use of this
// source code is governed by the Apache v2 license that can be found in the
// LICENSE file.

// Command commentcheck reports invalid Go declaration comments and detached
// doc comments that look like they belong to a declaration in the same file.
//
// Usage: commentcheck [-fix] [patterns...]
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/maruel/commentcheck"
)

func main() {
	fix := flag.Bool("fix", false, "reorder the source files to resolve the violations")
	flag.Parse()
	if err := run(os.Stderr, flag.Args(), *fix); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run checks the packages matched by patterns, or fixes them in place, and
// writes one diagnostic line per remaining violation to stdout.
func run(out io.Writer, patterns []string, fix bool) error {
	pkgs, err := commentcheck.ListPackages(patterns)
	if err != nil {
		return err
	}
	var violations int
	for _, pkg := range pkgs {
		var vs []commentcheck.Violation
		if fix {
			if vs, err = commentcheck.FixPackage(pkg); err != nil {
				return err
			}
		} else {
			if vs, err = commentcheck.Check(pkg); err != nil {
				return err
			}
		}
		for i := range vs {
			if _, err := fmt.Fprintln(out, vs[i].String()); err != nil {
				return err
			}
		}
		violations += len(vs)
	}
	if violations != 0 && !fix {
		return fmt.Errorf("found %d comment violation(s); re-run with -fix to resolve them automatically", violations)
	}
	return nil
}
