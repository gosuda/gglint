# gglint

[![CI](https://github.com/gosuda/gglint/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/gosuda/gglint/actions/workflows/ci.yml) [![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

[![Built with Depot](https://depot.dev/badges/built-with-depot.svg)](https://depot.dev?utm_source=gosuda)

One `go vet` linter binary with 104 rules, organized into nested `basic`, `standard`, and `strict` modes.

gglint runs every check through `go vet -vettool`, so the `go` command owns package loading, parallelism, analysis facts, and the cache. gglint selects the rules, filters the findings, and prints the report. The catalog combines `go/analysis` passes, Staticcheck, selected third-party linters, gVisor ports, and analyzers that mirror the LoGOs lint rules.

```text
$ gglint run -format text
main.go:6:13: Printf format %d has arg #1 of wrong type string [staticcheck:SA5009]
main.go:6:14: fmt.Printf format %d has arg "not a number" of wrong type string [printf]
gglint: 2 findings (mode standard)
```

Real output from `main.go` containing `fmt.Printf("%d\n", "not a number")`. The run exits 1.

## Installation

gglint needs Go 1.27 or later, as `go.mod` declares. It supports every platform `go vet` supports; CI runs on Linux and Windows.

`go install github.com/gosuda/gglint/cmd/gglint@latest` is the only supported installation method. The binary lands in `$(go env GOPATH)/bin` (or `$GOBIN` when set), so make sure that directory is on your `PATH`.

## Quick start

```sh
go install github.com/gosuda/gglint/cmd/gglint@latest
cd your-go-project
gglint run
```

A clean run prints `gglint: 0 findings in 0 files (mode standard)` and exits 0. Use `gglint run -h` for flags.

## Why gglint

- One binary, one catalog: no plugin loading, no linter config schema to learn beyond `gglint.toml`.
- Nested modes: `basic` reports defects with positive evidence, `standard` (the default) adds rewrites and the gglint concurrency and lifetime rules, `strict` adds rules that report missing proof of safety.
- Agent-readable output: the default `agent` format puts each finding on one line and ends with per-rule counts; `-format json` emits one object.
- Suggested fixes: `gglint run -fix` applies admitted fixes, refuses the rest with a message, and reports what is left (see "Automatic fixes").
- Kept evidence: every rule name, mode, origin, and description in this file is tested against the registered catalog.

## Usage

| Command | What it does |
|---|---|
| `gglint run [flags] [packages]` | Lints packages. The default pattern is `./...`. |
| `gglint rules [-mode M] [-selected] [-config PATH]` | Lists rules. |
| `gglint version` | Prints `gglint <module version> <Go version>`. |

`gglint run -h` and `gglint rules -h` print the command's flags to stderr and exit 2.

### `gglint run`

| Flag | Meaning |
|---|---|
| `-config PATH` | Use this `gglint.toml` instead of searching for one. A relative path is resolved against the working directory. |
| `-mode basic\|standard\|strict` | Override the `mode` in the config file. |
| `-format agent\|text\|json` | Output format. The default is `agent`. |
| `-fix` | Apply admitted fixes, refuse the rest with a message, then report what is left. |
| `-new-from-rev REV` | Report only findings on lines changed since `REV`, by comparing each finding with `git diff REV`. `typecheck` findings are always kept. |

### `gglint rules`

Without `-selected`, `gglint rules` prints the whole catalog and reads no config file. Add `-mode M` to print only the rules whose mode is `M` or lower.

With `-selected`, it finds and loads the config exactly as `gglint run` does, honors `-mode` and `-config`, and prints only the rules a run would select. `-config` is only accepted together with `-selected`.

Each line has four tab-separated fields, sorted by mode and then by name:

```text
name<TAB>mode<TAB>origin<TAB>description
```

### Output

Stdout carries only the report in the selected format, and only on exit 0 or 1. On exit 2 or 3, stdout is empty. Stderr carries status lines and errors prefixed with `gglint: `.

The default `agent` format prints one line per finding and then one summary line, sorted by path, line, column, token, and message:

```text
path:line:col: message [token]<TAB>related: path:line:col: message<TAB>fix: <message>

gglint: N findings in F files (mode M) rule=count rule=count
```

`text` format indents related positions and fixes on their own lines. `json` format prints one object with `mode`, `config` when set, and a `findings` array. In `agent` and `text`, each finding carries a token: the rule name, or `rule:check` when the rule runs more than one analyzer, for example `staticcheck:SA1015` or `chanstate:doubleclose`. In `json`, the same value is split into separate `rule` and `check` fields, so the first example reads `"rule": "staticcheck", "check": "SA1015"`.

### Exit codes

| Code | Meaning |
|---|---|
| `0` | No findings after filtering. A run whose findings `-fix` removed also exits 0. |
| `1` | At least one finding, `typecheck` findings included. |
| `2` | Usage or config error, including a bad flag. |
| `3` | Package load failure, analyzer error, internal error, or interrupt. |

## Documentation

This file is the complete reference: [Configuration](#configuration), [Suppressing findings](#suppressing-findings), [Modes](#modes), [Rules](#rules), and [Coming from golangci-lint](#coming-from-golangci-lint). Repository invariants for analyzer changes live in [AGENTS.md](AGENTS.md).

## Configuration

gglint reads one TOML file, `gglint.toml`. Keys use snake_case, and unknown keys are an error. Every key is optional.

```toml
mode = "standard"           # basic | standard | strict; -mode overrides it
enable = []                 # rule names added beyond the mode
disable = []                # rule names removed from the mode

[run]
tests = true                # report findings in _test.go files
build_tags = []             # passed to go vet as -tags

[rules.complexity]          # override single limits of the mode; 0 turns a limit off
max_cognitive = 19
max_nesting = 5

[rules.checkimports]
deny = [{ path = "archive/zip", allow_from = ["example.com/m/internal/zipx"] }]

[[exclude]]                 # drops a finding when every set field matches
paths = ["^internal/legacy/"]                  # Go regexps against the path relative to the config file
rules = ["complexity", "staticcheck:SA1019"]   # finding tokens, same syntax as //nolint
text = "^function .* is too complex"           # Go regexp against the message
tests = false               # true: only _test.go files; false: only other files; omit for both
```

| Key | Default | Meaning |
|---|---|---|
| `mode` | `"standard"` | `basic`, `standard`, or `strict`. |
| `enable` | `[]` | Rules to add beyond the mode. |
| `disable` | `[]` | Rules to remove from the mode. `typecheck` cannot be disabled. |
| `run.tests` | `true` | When `false`, gglint drops every finding in a `_test.go` file, `typecheck` included. |
| `run.build_tags` | `[]` | Build tags for `go vet`. |
| `rules.complexity.<limit>` | the mode's value | One of the eight limits in [Complexity limits](#complexity-limits). Values must be 0 or more. |
| `rules.checkimports.deny` | `[]` | Imports that `checkimports` reports. `path` is an exact import path or a prefix ending in `/...`; `allow_from` lists the importing packages that may use it. |
| `[[exclude]]` | none | Each entry needs at least one of `paths`, `rules`, `tests`, `text`. `rules` cannot name `typecheck`. |

Without `-config`, gglint looks for `gglint.toml` in the working directory and each parent. It stops after the first directory that contains `.git` and checks that directory too. On Unix-like systems it stops before a world-writable directory, because any local user could write a config there. If no file is found it uses the defaults.

gglint selects every rule whose mode is at or below the configured mode, adds `enable`, and removes `disable`. `typecheck` always runs.

## Suppressing findings

```go
//nolint:token[,token...] // reason
```

A token is a rule name, which suppresses every check of that rule, or `rule:check`, which suppresses one check.

| Where the comment is | What it covers |
|---|---|
| At the end of a line of code | That line. |
| Alone on its line | The next declaration or statement, through its last line. |
| Before the `package` clause | The whole file. |

The `nolint` rule checks the directives themselves: it reports malformed or unknown rules, directives that name no rule (`standard` and above) or lack a reason (`strict`), and suppressions that no selected rule needed; a directive that names any unselected rule is left alone. `typecheck` findings cannot be suppressed.

## Modes

| Mode | What it adds |
|---|---|
| `basic` | Defects with positive evidence, checks of annotations and configuration, and the complexity limits. |
| `standard` | Everything in `basic`, plus rewrites that carry a fix, the gglint rules for errors, locks, wait groups, channels, races, contexts, resource lifetime, aliasing, and spin loops, and the defect checks listed under Added in standard. |
| `strict` | Everything in `standard`, plus rules that report missing proof of safety, duplicated code, and comment markers. |

The modes are nested: every rule in `basic` is in `standard`, and every rule in `standard` is in `strict`. Some rules also check more in a higher mode; their descriptions in [Rules](#rules) say how.

The rewrite rules `modernize`, `gofumpt`, `gosimple`, `misspell`, and `usetesting` report only findings that carry a fix, so `gglint run -fix` resolves every finding of such a rule.

### Automatic fixes

`gglint run -fix` recomputes diagnostics from the current file content in the
same run: fixes are never stored, so there is no cross-run staleness. What the
fixer writes and what it refuses is fixed by this contract:

- Admitted: a fix is applied only to a `.go` file in the analyzed packages
  that is not generated, with test files admitted only when tests are on.
  After the fix, every changed file must still parse when it parsed before.
- Refused with a message: a suggested fix the gate will not apply leaves its
  file byte-identical and prints `gglint: refused <path>: <reason>` on
  stderr. The reasons are `outside the analysis scope`, `generated file`,
  `test file excluded when tests are off`, `does not parse after fix`,
  `changed without an applied fix`, and `created during fix`. A refused file
  is restored to its pre-fix bytes, so a refusal never leaves a partial
  write behind.
- Suggested only: every finding without an applied fix stays in the report.
  Rules without fixes, such as `checkimports` denials, and files the fixer
  cannot parse never change on disk.

Applying the fix twice yields a byte-identical file, and repeated runs on a
clean tree change nothing. Do not modify files concurrently with `-fix`: the
gate restores files that fail its post-fix checks, but a concurrent write
that still parses is indistinguishable from an applied fix.

### Complexity limits

The `complexity` rule reports a value only when it is greater than the limit. A value equal to the limit is clean, and a limit of 0 turns that check off. Each limit is at least as tight in `strict` as in `standard`, and in `standard` as in `basic`.

In `_test.go` files, `complexity` skips the function line limit (`max_lines`) and the file line limit (`max_file_lines`).

| Key | Measures | basic | standard | strict |
|---|---|---|---|---|
| `max_cognitive` | Cognitive score of a function | 24 | 19 | 14 |
| `max_cyclomatic` | Cyclomatic complexity of a function | 30 | 15 | 9 |
| `max_nesting` | Deepest nesting of `if`, `for`, `switch`, and `select` | 6 | 5 | 3 |
| `max_lines` | Lines of a function, blank and comment lines included | 150 | 80 | 60 |
| `max_params` | Parameters of a function | 8 | 6 | 4 |
| `max_bool_params` | `bool` parameters of an exported function | 0 | 2 | 1 |
| `max_interface_methods` | Methods of a named interface | 0 | 10 | 5 |
| `max_file_lines` | Lines of a file | 1499 | 999 | 799 |

Large files and deeply nested functions exceed what a reader or an agent can hold and edit reliably. Each nested branch multiplies the paths a reader must keep in mind.

| Hazard | Rules |
|---|---|
| Unjoined, unstructured, or unbounded goroutines | `goroutine` (`strict`: join, errgroup or `WaitGroup.Go` only, exit paths, and bounded fan-out), `testinggoroutine`, `staticcheck:SA2002` |
| Goroutines blocked forever on channels | `chanstate`, `chanblock` (`strict`), and the exit-path and empty-select checks of `goroutine` |
| Lost or misused contexts | `lostcancel`, `ctxflow`, `fatcontext`, `noctx`, and in `strict` `contextcheck` |
| Data races | `gorace`, `checklocks` (`strict`: guards, `+checkatomic`, inferred guards), `copylocks`, `atomic`, `atomicalign`, `staticcheck:SA2000` to `SA2003` |
| `sync.WaitGroup` and `errgroup` misuse | `wgstate`, `waitgroup`, `modernize:waitgroupgo`, `staticcheck:SA2000` |
| Deadlock by re-entry | `lockstate`, `reentrant_withlock_deadlock`, `checklocks` (`strict`: double-acquire check) |
| Loops that spin without waiting or without advancing, five shapes only (spin:poll, spin:closedrecv, spin:busywait, spin:stalecas, spin:skipadvance) | `spin` |
| Timer churn and leaks | `timeafter`, `staticcheck:SA1015` |

## Rules

The catalog has 104 rules. `basic` runs 51 of them, `standard` runs 92, and `strict` runs all 104. Run `gglint rules` for the list that matches your build.

| Origin | Source |
|---|---|
| `go/analysis` | Analysis passes from `golang.org/x/tools` |
| `staticcheck` | Staticcheck, `honnef.co/go/tools` |
| `third-party` | Published linter modules, built from their public APIs |
| `gvisor` | Analyzers ported from gVisor |
| `logos` | gglint analyzers that port loGos lint rules to Go |
| `gglint` | Analyzers native to gglint |

### basic

| Rule | Origin | Description |
|---|---|---|
| `appends` | go/analysis | check for missing values after append |
| `asmdecl` | go/analysis | report mismatches between assembly files and Go declarations |
| `assign` | go/analysis | check for useless assignments |
| `atomic` | go/analysis | check for common mistakes using the sync/atomic package |
| `bidichk` | third-party | Reports dangerous Unicode bidirectional control characters in source. |
| `bools` | go/analysis | check for common mistakes involving boolean operators |
| `buildtag` | go/analysis | check //go:build and // +build directives |
| `cgocall` | go/analysis | detect some violations of the cgo pointer passing rules |
| `checkconst` | gvisor | Checks +checkconst, +checkalign, +checkoffset, and +checksize against assembly constants. |
| `checkescape` | gvisor | Checks +checkescape and +mustescape annotations; unannotated callees read as unknown, failing only :all and :unknown. |
| `checkimports` | gvisor | Enforces the configured import deny list. |
| `checklinkname` | gvisor | Validates //go:linkname directives against the signatures of their targets. |
| `complexity` | logos | Caps cognitive score, nesting, lines, parameters, and file size; large units exceed what agents edit reliably. |
| `composites` | go/analysis | check for unkeyed composite literals |
| `copylocks` | go/analysis | check for locks erroneously passed by value |
| `defers` | go/analysis | report common mistakes in defer statements |
| `directive` | go/analysis | check Go toolchain directives such as //go:debug |
| `errorsas` | go/analysis | report passing non-pointer or non-error values to errors.As |
| `framepointer` | go/analysis | report assembly that clobbers the frame pointer before saving it |
| `gocheckcompilerdirectives` | third-party | Reports malformed or unknown //go: compiler directives. |
| `gofixdirective` | go/analysis | validate uses of gofix comment directives |
| `hostport` | go/analysis | check format of addresses passed to net.Dial |
| `httpresponse` | go/analysis | check for mistakes using HTTP responses |
| `ifaceassert` | go/analysis | detect impossible interface-to-interface type assertions |
| `ineffassign` | third-party | Reports assignments to variables whose value is never used. |
| `infinite_recursion` | logos | Reports functions that call themselves with their own arguments on every path before any return. |
| `loopclosure` | go/analysis | check references to loop variables from within nested functions |
| `lostcancel` | go/analysis | check cancel func returned by context.WithCancel is called |
| `nilfunc` | go/analysis | check for useless comparisons between functions and nil |
| `nolint` | gglint | Checks //nolint directives for unknown rules, missing names or reasons, and unused suppressions. |
| `printf` | go/analysis | check consistency of Printf format strings and arguments |
| `reentrant_withlock_deadlock` | logos | Reports locking a sync mutex the goroutine already holds, directly or through same-package calls. |
| `shift` | go/analysis | check for shifts that equal or exceed the width of the integer |
| `sigchanyzer` | go/analysis | check for unbuffered channel of os.Signal |
| `slog` | go/analysis | check for invalid structured logging calls |
| `stackaddr` | logos | Reports unsafe.Pointer built from a non-pointer-derived uintptr, and copied or addressed reflect header values. |
| `staticcheck` | staticcheck | Staticcheck SA bug checks; standard adds SA1019 (deprecated API use). |
| `stdmethods` | go/analysis | check signature of methods of well-known interfaces |
| `stdversion` | go/analysis | report uses of too-new standard library symbols |
| `stringintconv` | go/analysis | check for string(int) conversions |
| `structtag` | go/analysis | check that struct field tags conform to reflect.StructTag.Get |
| `testinggoroutine` | go/analysis | report calls to (*testing.T).Fatal from goroutines started by a test |
| `tests` | go/analysis | check for common mistaken usages of tests and examples |
| `timeformat` | go/analysis | check for calls of (time.Time).Format or time.Parse with 2006-02-01 |
| `typecheck` | gglint | Reports package load and type errors; it cannot be disabled or suppressed. |
| `unmarshal` | go/analysis | report passing non-pointer or non-interface values to unmarshal |
| `unreachable` | go/analysis | check for unreachable code |
| `unsafeptr` | go/analysis | check for invalid conversions of uintptr to unsafe.Pointer |
| `unused` | staticcheck | Reports unused constants, variables, functions, types, and fields (staticcheck U1000). |
| `unusedresult` | go/analysis | check for unused results of calls to some functions |
| `waitgroup` | go/analysis | check for misuses of sync.WaitGroup |

### Added in standard

| Rule | Origin | Description |
|---|---|---|
| `alias` | gglint | Reports four shapes of append and copy whose writes land in memory still read or are lost. |
| `asasalint` | third-party | Reports a []any passed as one element to a variadic ...any parameter. |
| `atomicalign` | go/analysis | check for non-64-bits-aligned arguments to sync/atomic functions |
| `bodyclose` | third-party | Reports HTTP response bodies that are never closed. |
| `chanstate` | gglint | Reports channel operations that panic, deadlock, or strand a sender, on channels the function declares. |
| `ctxflow` | gglint | Reports contexts that drop cancellation, deferred cancels of returned contexts, and loops that poll Done with a default. |
| `deepequalerrors` | go/analysis | check for calls of reflect.DeepEqual on error values |
| `durationcheck` | third-party | Reports multiplication of two time.Duration values. |
| `err_chain_reset` | logos | Reports fmt.Errorf calls that format an error with a verb other than %w or pass err.Error(). |
| `errdrop` | gglint | Reports a dropped error that later code depends on, a written file's Close error, or an error dropped before return nil. |
| `fatcontext` | third-party | Reports contexts nested in loops and closures, which grow the context chain each iteration. |
| `gochecksumtype` | third-party | Requires type switches on sealed sum-type interfaces to cover every variant. |
| `gocritic` | third-party | Runs go-critic diagnostic checkers plus deferInLoop. |
| `gofumpt` | third-party | Reports files that are not gofumpt-formatted, with a fix that formats them. |
| `gorace` | gglint | Reports unordered conflicting accesses by goroutines or parallel subtests; double-checked reads outside the lock. |
| `gosec` | third-party | Reports security problems found by gosec; standard skips G101, G103, G104, G115, G204, G301, G302, G304, G306, and G404. |
| `gosimple` | staticcheck | Staticcheck S simplifications that carry a fix. |
| `httpmux` | go/analysis | report using Go 1.22 enhanced ServeMux patterns in older Go versions |
| `inline` | go/analysis | apply fixes based on 'go:fix inline' comment directives |
| `lockstate` | gglint | Reports lock misuse that panics, deadlocks, or guards nothing, within one function's own lock calls. |
| `makezero` | third-party | Reports append to a slice created with a non-zero length. |
| `misspell` | third-party | Reports commonly misspelled English words, with a fix that corrects them. |
| `modernize` | go/analysis | Suggests modern Go replacements for outdated idioms and library calls. |
| `musttag` | third-party | Requires field tags on structs passed to JSON, XML, YAML, and TOML marshaling functions. |
| `nilerr` | third-party | Reports returning nil after checking a non-nil error, or an error after checking it is nil. |
| `nilness` | go/analysis | check for redundant or impossible nil comparisons |
| `nilnesserr` | third-party | Reports returning a nil error after checking that a different error is non-nil. |
| `placeholder` | logos | Reports placeholder panics; strict also reports comments that start with TODO or FIXME. |
| `reassign` | third-party | Reports assignments to variables of other packages, such as io.EOF. |
| `reflectvaluecompare` | go/analysis | check for comparing reflect.Value values with == or reflect.DeepEqual |
| `resstate` | gglint | Reports os, net, and http calls after Close, uses after Put or free, and buffer views kept past an overwrite. |
| `scannererr` | go/analysis | scannererr: report failure to check bufio.Scanner.Err |
| `sortslice` | go/analysis | check the argument type of sort.Slice |
| `spin` | gglint | Reports five shapes of loop that spin without waiting or without advancing; test files are skipped. |
| `sqlrowserr` | go/analysis | sqlrowserr: report failure to check sql.Rows.Err |
| `timeafter` | logos | Reports time.After inside a loop, which allocates a timer per iteration. |
| `tparallel` | third-party | Reports t.Parallel used in subtests but not the top-level test, or the reverse. |
| `unparam` | third-party | Reports unexported function parameters and results that are unused or always the same value. |
| `unusedwrite` | go/analysis | checks for unused writes |
| `usetesting` | third-party | Reports os.CreateTemp("", ...) in tests that name their test parameter, with a fix that passes its TempDir(). |
| `wgstate` | gglint | Reports WaitGroup misuse that hangs Wait or lets it return early, and errgroup Wait calls that hang. |

`chanstate:firstresult` reports sends exceeding receives plus capacity; it counts every receive site, so a send that leaks through only one branch is not reported, and a function with a backward goto is not checked. `alias:appendclobber` reports an append through `b := a[:n]` that overwrites local `a` read later; it needs a constant `n` and capacity proof, an append in a loop that neither ranges over `a` nor tests its length counts once, and a slice touched by a closure, changed through a pointer, or whose address is taken is not tracked. `gorace` is silent when the accesses are joined, when a send and receive around the access order them on a channel of capacity one or one the function does not build (a parameter, a field, a package variable, or a call result), when a lock, a read lock included, is held at both accesses, and when parallel subtests share one mutex.

`wgstate` reads a goroutine only when it is a function literal: a goroutine that runs a named function is not checked.

### Added in strict

| Rule | Origin | Description |
|---|---|---|
| `backingpin` | logos | Reports subslices and substrings that keep a backing store over 64 KiB alive past the function. |
| `chanblock` | logos | Reports channel sends and receives that can block forever without a ctx.Done() case or buffer proof. |
| `checklocks` | gvisor | Checks +checklocks and +checkatomic annotations and reports double acquisition of a held lock. |
| `closer` | logos | Reports local io closers that are neither closed nor handed on. |
| `contextcheck` | third-party | Reports functions that use a non-inherited context. |
| `dupl` | third-party | Reports duplicated code fragments of 150 or more tokens. |
| `errorlint` | third-party | Requires errors.Is and errors.As for error comparisons and assertions. |
| `exhaustive` | third-party | Requires switch statements on enum types without a default case to cover every enum member. |
| `forcetypeassert` | third-party | Reports type assertions whose ok result is not checked. |
| `goroutine` | logos | Requires joined goroutines with exit paths; strict requires errgroup or WaitGroup.Go and bounded fan-out. |
| `noctx` | third-party | Reports network and SQL calls made without a context.Context. |
| `typednil` | logos | Reports a typed nil pointer returned as an interface, which is never nil. |

### Coming from golangci-lint

gglint keeps these golangci-lint linters in a new form:

| golangci-lint | gglint |
|---|---|
| `govet` | One rule per `go/analysis` pass |
| `modernize` | The `modernize` rule |
| `nolintlint` | The `nolint` rule |
| `typecheck` | The `typecheck` rule |
| `gofumpt` | The `gofumpt` rule |

These linters are dropped because a kept rule already covers the concern:

| Kept rule that owns the concern | Dropped linters |
|---|---|
| `checkimports` | `depguard`, `gomodguard_v2` |
| `closer` | `sqlclosecheck` |
| `complexity` | `cyclop`, `funlen`, `gocognit`, `gocyclo`, `interfacebloat`, `maintidx`, `nestif` |
| `gofumpt` | `whitespace` |
| `hostport` | `nosprintfhostport` |
| `ineffassign`, `unusedwrite` | `wastedassign` |
| `modernize:forvar` | `copyloopvar` |
| `modernize:rangeint` | `intrange` |
| `placeholder` | `godox` |
| `sqlrowserr` | `rowserrcheck` |
| `staticcheck:SA1008` | `canonicalheader` |
| `staticcheck`, `gocritic` | `revive` |

These linters are dropped for other reasons:

| Reason | Dropped linters |
|---|---|
| Targets a third-party library or `golang.org/x/exp` | `arangolint`, `clickhouselint`, `exptostd`, `ginkgolinter`, `loggercheck`, `promlinter`, `protogetter`, `spancheck`, `testifylint`, `zerologlint` |
| Layout or ordering opinion | `decorder`, `dogsled`, `embeddedstructfieldcheck`, `funcorder`, `godot`, `goprintffuncname`, `grouper`, `inamedparam`, `iotamixing`, `lll`, `nlreturn`, `noinlineerr`, `tagalign`, `wsl_v5` |
| Contested convention with no defect | `asciicheck`, `containedctx`, `dupword`, `err113`, `errcheck`, `errname`, `gochecknoglobals`, `gochecknoinits`, `goconst`, `iface`, `ireturn`, `mirror`, `nakedret`, `nilnil`, `paralleltest`, `perfsprint`, `predeclared`, `recvcheck`, `sloglint`, `testableexamples`, `thelper`, `unconvert`, `wrapcheck`, `errchkjson`, `exhaustruct_v5`, `godoclint`, `gosmopolitan`, `mnd`, `nonamedreturns`, `prealloc`, `testpackage`, `unqueryvet`, `usestdlibvars`, `varnamelen` |
| Does nothing useful without project configuration | `forbidigo`, `goheader`, `gomoddirectives`, `importas`, `tagliatelle` |
| Deprecated upstream | `exhaustruct`, `gomodguard`, `wsl` |

The formatters `gci`, `gofmt`, and `goimports` are covered by `gofumpt`. `golines` is dropped as a layout opinion, and `swaggo` because it targets one library.

gglint does not rebuild Go or module plugins, `custom`, `migrate`, `config path|verify`, `cache`, the `formatters` and `fmt` commands, the JSON schema, the docs site, presets, severities, issue caps, SARIF and the other printers, `-new-from-patch`, `whole-files`, colors, or source excerpts.

### Upgrading from the first catalog

| Removed | Use instead |
|---|---|
| `errcheck`, `discarded_error` | `errdrop` |
| `capturerace` | `gorace` |
| `staticcheck:S…` tokens | `gosimple:S…` |
| `staticcheck:SA4006` | `ineffassign` |
| `staticcheck:ST…`, `staticcheck:QF…`, `structtag_malformed`, `checkaligned`, `noinit`, `unsizedint`, and the third-party rules listed under `Coming from golangci-lint` | no replacement |

A `gglint.toml` that names a deleted rule in `enable` or `disable` fails with `enable: unknown rule "X"` or `disable: unknown rule "X"`, and in `[[exclude]] rules` with `exclude[N].rules: unknown rule "X"` (exit 2). A `//nolint:X` comment that names a deleted rule gets `unknown nolint rule "X"`. A `staticcheck:S1005` token stays valid, because `staticcheck` stays known, and matches nothing: the finding now carries `gosimple:S1005`, so the directive is reported as `unused nolint directive for staticcheck:S1005`.

## Contributing

There is no `CONTRIBUTING.md`. Before opening a change, read [AGENTS.md](AGENTS.md), which fixes the verbatim gVisor port policy, the loGos parity rules, analysis-fact encoding, and the dependency constraints.

gglint lints itself at `standard` with the repository's `gglint.toml`. Run these gates before you commit:

```sh
go vet ./...
go test -race ./...
go run ./cmd/gglint run ./...
golangci-lint run ./...
go test -count=1 -run TestCorpusBudget ./internal/precision/
```

GitHub Actions runs these gates on pull requests, pushes to `main`, merge groups, and manual dispatches. Three Linux jobs separate the race suite, the memory-heavy vettool race tests, and linting; a Windows job runs the test suite. README changes run the checks because the rule catalog tests read this file.

## License

gglint is licensed under Apache-2.0; see [LICENSE](LICENSE). Code copied from gVisor keeps its file headers and is covered by [third_party/gvisor/LICENSE](third_party/gvisor/LICENSE). Test fixtures copied from loGos are covered by [third_party/logos/LICENSE](third_party/logos/LICENSE).

## CI ownership

Native Depot CI runs the Linux race, vettool, and lint checks for internal pull requests, merge groups, main pushes, and Depot dispatches. Fork pull requests use the same Linux checks through GitHub Actions on Depot runners. Windows checks run weekly or through GitHub workflow dispatch on `depot-windows-2022-4`. All runner allocations use Depot. Runtime and billing savings require completed runs on these runners; job-count reductions do not establish either.
