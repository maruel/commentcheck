// Copyright 2026 Marc-Antoine Ruel. All Rights Reserved. Use of this
// source code is governed by the Apache v2 license that can be found in the
// LICENSE file.

package commentcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fileOf reads a file of the package written by writeModule.
func fileOf(t *testing.T, pkg Package, name string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(pkg.Dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(src)
}

func TestFixPackage(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		src := "package p\n\n// Foo does X.\nfunc Foo() {}\n"
		pkg := writeModule(t, map[string]string{"a.go": src})
		if _, err := FixPackage(pkg); err != nil {
			t.Fatal(err)
		}
		if got := fileOf(t, pkg, "a.go"); got != src {
			t.Fatalf("valid file was modified:\n%s", got)
		}
	})
	t.Run("detach", func(t *testing.T) {
		pkg := writeModule(t, map[string]string{"a.go": "package p\n\n// Foo does X.\n\nfunc Foo() {}\n"})
		if _, err := FixPackage(pkg); err != nil {
			t.Fatal(err)
		}
		want := "package p\n\n// Foo does X.\nfunc Foo() {}\n"
		if got := fileOf(t, pkg, "a.go"); got != want {
			t.Fatalf("got:\n%s\nwant:\n%s", got, want)
		}
	})
	t.Run("two detached comments in one file", func(t *testing.T) {
		pkg := writeModule(t, map[string]string{"a.go": "package p\n\n// Foo does X.\n\nfunc Foo() {}\n\n// Bar does Y.\n\nfunc Bar() {}\n"})
		if _, err := FixPackage(pkg); err != nil {
			t.Fatal(err)
		}
		want := "package p\n\n// Foo does X.\nfunc Foo() {}\n\n// Bar does Y.\nfunc Bar() {}\n"
		if got := fileOf(t, pkg, "a.go"); got != want {
			t.Fatalf("got:\n%s\nwant:\n%s", got, want)
		}
	})
	t.Run("move", func(t *testing.T) {
		pkg := writeModule(t, map[string]string{"a.go": "package p\n\n// Bar does Y.\n\nfunc foo() {}\n\nfunc Bar() {}\n"})
		if _, err := FixPackage(pkg); err != nil {
			t.Fatal(err)
		}
		want := "package p\n\nfunc foo() {}\n\n// Bar does Y.\nfunc Bar() {}\n"
		if got := fileOf(t, pkg, "a.go"); got != want {
			t.Fatalf("got:\n%s\nwant:\n%s", got, want)
		}
	})
	t.Run("move pads the comment above its target", func(t *testing.T) {
		pkg := writeModule(t, map[string]string{"a.go": "package p\n\n// Bar does Y.\n\nfunc foo() {}\n\nvar x = 1\nfunc Bar() {}\n"})
		if _, err := FixPackage(pkg); err != nil {
			t.Fatal(err)
		}
		want := "package p\n\nfunc foo() {}\n\nvar x = 1\n\n// Bar does Y.\nfunc Bar() {}\n"
		if got := fileOf(t, pkg, "a.go"); got != want {
			t.Fatalf("got:\n%s\nwant:\n%s", got, want)
		}
	})
	t.Run("fixes across files", func(t *testing.T) {
		pkg := writeModule(t, map[string]string{
			"a.go": "package p\n\n// Foo does X.\n\nfunc Foo() {}\n",
			"b.go": "package p\n\n// Bar does Y.\n\nfunc Bar() {}\n",
		})
		if _, err := FixPackage(pkg); err != nil {
			t.Fatal(err)
		}
		if got, want := fileOf(t, pkg, "a.go"), "package p\n\n// Foo does X.\nfunc Foo() {}\n"; got != want {
			t.Fatalf("a.go:\n%s\nwant:\n%s", got, want)
		}
		if got, want := fileOf(t, pkg, "b.go"), "package p\n\n// Bar does Y.\nfunc Bar() {}\n"; got != want {
			t.Fatalf("b.go:\n%s\nwant:\n%s", got, want)
		}
	})
	t.Run("unfixable violations are reported", func(t *testing.T) {
		pkg := writeModule(t, map[string]string{"a.go": "package p\n\nfunc Foo() {}\n"})
		vs, err := FixPackage(pkg)
		if err == nil || !strings.Contains(err.Error(), "no fix can be applied") {
			t.Fatalf("got %v", err)
		}
		if len(vs) != 1 || vs[0].Message != "exported declaration Foo has no doc comment" {
			t.Fatalf("got %v", vs)
		}
		if got := fileOf(t, pkg, "a.go"); got != "package p\n\nfunc Foo() {}\n" {
			t.Fatalf("unfixable file was modified:\n%s", got)
		}
	})
	t.Run("fixable violations are fixed before reporting the rest", func(t *testing.T) {
		pkg := writeModule(t, map[string]string{"a.go": "package p\n\n// Foo does X.\n\nfunc Foo() {}\n\nfunc Bar() {}\n"})
		vs, err := FixPackage(pkg)
		if err == nil || !strings.Contains(err.Error(), "no fix can be applied") {
			t.Fatalf("got %v", err)
		}
		if len(vs) != 1 || vs[0].Message != "exported declaration Bar has no doc comment" {
			t.Fatalf("got %v", vs)
		}
		want := "package p\n\n// Foo does X.\nfunc Foo() {}\n\nfunc Bar() {}\n"
		if got := fileOf(t, pkg, "a.go"); got != want {
			t.Fatalf("got:\n%s\nwant:\n%s", got, want)
		}
	})
	t.Run("detached comment in a test file", func(t *testing.T) {
		pkg := writeModule(t, map[string]string{
			"a.go":        "package p\n\n// Foo does X.\nfunc Foo() {}\n",
			"a_test.go":   "package p\n\nfunc TestFoo(t *T) {}\n",
			"a_x_test.go": "package p_test\n\n// Bar does Y.\n\nfunc Bar() {}\n",
		})
		if _, err := FixPackage(pkg); err != nil {
			t.Fatal(err)
		}
		if got, want := fileOf(t, pkg, "a_x_test.go"), "package p_test\n\n// Bar does Y.\nfunc Bar() {}\n"; got != want {
			t.Fatalf("got:\n%s\nwant:\n%s", got, want)
		}
	})
}

func TestFixContents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	src := "package p\n\n// Foo does X.\n\nfunc Foo() {}\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	fix := &Fix{File: path, Start: 4, End: 4}
	contents, err := fix.Contents()
	if err != nil {
		t.Fatal(err)
	}
	if want := "package p\n\n// Foo does X.\nfunc Foo() {}\n"; string(contents[path]) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", contents[path], want)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != src {
		t.Fatalf("Contents modified the file on disk: %v, %s", err, got)
	}

	// Stale fixes are rejected.
	for _, fix := range []*Fix{
		{File: path, Start: 99, End: 100},
		{File: path, Start: 2, End: 1},
		{File: filepath.Join(dir, "nope.go"), Start: 1, End: 1},
		{File: path, Start: 1, End: 1, Text: "// Foo does X.", DstLine: 1}, // already in place
	} {
		if _, err := fix.Contents(); err == nil {
			t.Fatalf("expected an error for %+v", fix)
		}
	}
}

func TestFixKeepsPermissions(t *testing.T) {
	pkg := writeModule(t, map[string]string{"a.go": "package p\n\n// Foo does X.\n\nfunc Foo() {}\n"})
	path := filepath.Join(pkg.Dir, "a.go")
	if err := os.Chmod(path, 0o755); err != nil { //nolint:gosec // testing that non-default permissions survive the fix
		t.Fatal(err)
	}
	if _, err := FixPackage(pkg); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("got %v", got)
	}
}
