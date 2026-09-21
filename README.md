# commentcheck

commentcheck reports invalid Go declaration comments and detached doc comments
that look like they belong to a declaration in the same file:

- An exported function, method, or type must carry a doc comment starting with
  its name. Constants, variables, and methods on unexported receivers are
  exempt, as are methods implementing well-known standard library interfaces
  (`String`, `Error`, `Read`, `Write`, `Close`, `Unwrap`, …), like revive's
  exported rule. Test files only need doc comments on detached comments.
- A comment group that names a declaration further below in the same file and
  is separated from it by blank lines only is a doc comment that lost its
  attachment. It is reported whether it names the next declaration (detached
  by a blank line) or another one (misplaced before a different declaration).
- Comments starting with a directive (`//go:…`, `//+build`) are ignored, as
  are generated files.

Every detached-comment violation comes with a mechanical fix, so the comments
can be re-attached or relocated automatically: moved comments keep their exact
lines, blank line padding is preserved, and edited files are rewritten gofmt
clean or not at all. Missing or misnamed doc comments are report-only.

## Command line

```bash
# Check the module in the current directory.
go install github.com/maruel/commentcheck/cmd/commentcheck@latest
commentcheck ./...

# Re-attach and move the comments in place to resolve the violations.
commentcheck -fix ./...
```

## golangci-lint

The package ships a [golangci-lint module
plugin](https://golangci-lint.run/docs/plugins/module-plugins/). Build the
custom linter binary and run it instead of `golangci-lint`:

```bash
# .custom-gcl.yml, next to .golangci.yml
version: "2"
plugins:
  - module: github.com/maruel/commentcheck
    import: github.com/maruel/commentcheck
    path: ../commentcheck # or a published version: version: v0.1.0
```

```yaml
# .golangci.yml
version: "2"
linters:
  enable:
    - commentcheck
  settings:
    custom:
      commentcheck:
        type: module
        description: Exported declarations need doc comments starting with their name; doc comments must attach to their declaration.
        settings: {}
```

```bash
golangci-lint custom --version v2.13.2
./custom-gcl run ./...
```

The plugin carries suggested fixes for detached comments, so
`golangci-lint run --fix` reorders the comments automatically.

## License

Apache 2.0; see the LICENSE file.
