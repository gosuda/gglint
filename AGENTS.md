# AGENTS.md

## Verbatim gVisor ports

- Do not edit these files: `internal/analyzers/checklocks/{analysis,annotations,checklocks,facts,importfacts,state}.go`, `internal/analyzers/checkescape/checkescape.go`, `internal/analyzers/checkescape/checkescape_test.go`, `internal/analyzers/checkconst/checkconst.go`, `internal/analyzers/checklinkname/check_linkname.go`. They must stay diffable against upstream gVisor.
- When a port needs a gglint change, put it in a separate file in the same package, as `checklocks/deterministic.go` and `checklinkname/goroot.go` do. Limit the edit in the port file to a one-line call into that file.
- When the set of port files changes, update all three exclusion lists together: `linters.exclusions.paths` and `formatters.exclusions.paths` in `.golangci.yml`, and the first `[[exclude]]` entry in `gglint.toml`.

## loGos parity

- The `// want` lines under `internal/rules/testdata/src/` come from the `// ERROR` lines of the loGos fixtures. Do not edit them to match an analyzer. When `TestLogosPorts` fails, change the analyzer to match the loGos rule source. Never report more than loGos does.
- A loGos rule that leaves the catalog loses its fixture under internal/rules/testdata/src/ in the same change.
- Copied loGos fixtures keep their copyright header. `third_party/logos/LICENSE` covers them.

## Analysis facts

- A fact type that holds a map must implement `GobEncode` and `GobDecode` with sorted keys. unitchecker panics on fact encodings that are not deterministic. See `checklocks/deterministic.go`.
- Test analyzers that export facts with `analyzertest.RunIgnoringFacts`. It drops only "unexpected fact" reports. Do not add other filters to it.

## Vettool units

- Keep `internal/vettool` from adding the nolint analyzer or panic recovery to facts-only (`VetxOnly`) units. A facts-only unit must run only the analyzers that produce facts. A recovered panic there makes go vet cache incomplete facts.

## Dependencies

- Do not add `github.com/golangci/golangci-lint`, `github.com/spf13/cobra`, or `github.com/spf13/viper` as direct requirements. golangci-lint is an external dev tool, not a module dependency. (Human-supplied: approved project plan.)
