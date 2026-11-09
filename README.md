# gglint

gglint is a Go linter that ships as one binary with one rule catalog. It runs every check through `go vet -vettool`, so the go command owns the package graph, parallelism, analysis facts, and the cache. The go command caches each package's results keyed by the gglint version, the vet flags, and the package inputs, so a second run over unchanged code reuses the cached results. The resolved configuration is part of the vet flags, so a change to `gglint.toml` takes effect on the next run. gglint itself selects the rules, filters the findings, and prints the report. You choose how much it checks with one of three nested modes: `basic`, `standard` (the default), or `strict`.

## Install

```sh
go install github.com/gosuda/gglint/cmd/gglint@latest
```

gglint needs Go 1.27 or later, as `go.mod` declares.

## Commands

| Command | What it does |
|---|---|
| `gglint run [flags] [packages]` | Lints the packages. The default pattern is `./...`. |
| `gglint rules [-mode M] [-selected] [-config PATH]` | Lists rules. |
| `gglint version` | Prints `gglint <module version> <Go version>`. A build without a module version prints `(devel)`. |

`gglint run -h` and `gglint rules -h` print the command's flags to stderr and exit 2.

### `gglint run`

| Flag | Meaning |
|---|---|
| `-config PATH` | Use this `gglint.toml` instead of searching for one. A relative path is resolved against the working directory. |
| `-mode basic\|standard\|strict` | Override the `mode` in the config file. |
| `-format text\|json` | Output format. The default is `text`. |
| `-fix` | Apply suggested fixes, then report what is left. gglint first runs `go vet -fix` through its vettool, which applies the first fix of every kept finding, and then runs the normal report. |
| `-new-from-rev REV` | Report only findings on lines changed since `REV`. gglint compares each finding's printed path and line with `git diff REV` of the working tree. `typecheck` findings are always kept. |

### `gglint rules`

Without `-selected`, `gglint rules` prints the whole catalog and reads no config file. Add `-mode M` to print only the rules whose mode is `M` or lower.

With `-selected`, it finds and loads the config exactly as `gglint run` does, honors `-mode` and `-config`, and prints only the rules a run would select. `-config` is only accepted together with `-selected`.

Each line has four tab-separated fields, sorted by mode and then by name:

```text
name<TAB>mode<TAB>origin<TAB>description
```

### Exit codes

| Code | Meaning |
|---|---|
| `0` | No findings after filtering. A run whose findings `-fix` removed also exits 0. |
| `1` | At least one finding, `typecheck` findings included. |
| `2` | Usage or config error, including a bad flag. |
| `3` | Package load failure, analyzer error, internal error, or interrupt. |

### Output

Stdout carries only the report in the selected format, and only on exit 0 or 1. On exit 2 or 3, stdout is empty.

Stderr carries only lines that start with `gglint: `:

| Line | When |
|---|---|
| `gglint: config <path>` | A config file was loaded. |
| `gglint: N findings (mode M)` | Text format with at least one finding. |
| `gglint: <message>` | Usage or config error. |
| `gglint: load: <error>` | `go vet` failed and printed no Go source position. |
| `gglint: diff: <error>` | `-new-from-rev` could not run `git diff`. |
| `gglint: internal: <error>` | An analyzer or gglint itself failed. |
| `gglint: interrupted` | The run received SIGINT or SIGTERM. |

Paths are slash-separated and relative to the working directory, so they can start with `../`. Columns start at 1; a missing column prints as 1.

Each finding carries a token: the rule name, or `rule:check` when the rule runs more than one analyzer, for example `staticcheck:SA1015`, `modernize:forvar`, or `iface:identical`. The `check` part is the upstream analyzer name.

Text format prints one line per finding, then one indented line per related position and per suggested fix:

```text
path:line:col: message [token]
	related: path:line:col: message
	fix: <message>
```

When a fix has edits but no message, gglint prints `fix: available` once.

JSON format prints one object:

```json
{"mode":"standard","config":"gglint.toml","findings":[{"rule":"staticcheck","check":"SA1015","path":"a/b.go","line":3,"column":2,"end_line":3,"end_column":9,"message":"...","related":[{"path":"a/b.go","line":1,"column":1,"message":"..."}],"fixes":["..."],"fixable":true}]}
```

gglint omits `config`, `check`, `end_line`, `end_column`, `related`, and `fixes` when they are empty, and `fixable` when it is false. `findings` is `[]` on a clean run.

Findings are sorted by path, line, column, token, and message. gglint prints identical findings once, because `go vet` analyzes a package and its test variant separately. Generated files never report.

`typecheck` findings come from `go vet` itself: every stderr line of the form `file.go:line[:col]: message` becomes a finding. If `go vet` fails and prints no such line, gglint exits 3 with `gglint: load: <stderr>`.

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

[[exclude]]                 # drops a finding when every field set in the entry matches
paths = ["^internal/legacy/"]                  # Go regexps against the path relative to the config file
rules = ["complexity", "staticcheck:SA1019"]   # finding tokens, same syntax as //nolint
text = "^function .* is too complex"           # Go regexp against the message

[[exclude]]                 # turns off two rules in test code only
tests = true                # true: only _test.go files; false: only other files; omit for both
rules = ["goconst", "staticcheck:SA1019"]
```

| Key | Default | Meaning |
|---|---|---|
| `mode` | `"standard"` | `basic`, `standard`, or `strict`. |
| `enable` | `[]` | Rules to add beyond the mode. |
| `disable` | `[]` | Rules to remove from the mode. `typecheck` cannot be disabled. |
| `run.tests` | `true` | When `false`, gglint drops every finding in a `_test.go` file, `typecheck` included. |
| `run.build_tags` | `[]` | Build tags for `go vet`. |
| `rules.complexity.<limit>` | the mode's value | One of the eight limits in [Complexity limits](#complexity-limits). A missing key keeps the mode's value. Values must be 0 or more. |
| `rules.checkimports.deny` | `[]` | Imports that `checkimports` reports. `path` is an exact import path or a prefix ending in `/...`, which matches the prefix and everything below it. `allow_from` lists the importing packages that may use it, in the same syntax. |
| `[[exclude]]` | none | Each entry needs at least one of `paths`, `rules`, `tests`, `text`. `rules` cannot name `typecheck`: typecheck findings cannot be excluded or suppressed. |

gglint selects every rule whose mode is at or below the configured mode, adds `enable`, and removes `disable`. `typecheck` always runs. Settings under `[rules.X]` for a rule that is not selected are accepted and ignored.

### How gglint finds the file

1. If you pass `-config PATH`, gglint uses that file.
2. Otherwise it looks for `gglint.toml` in the working directory, then in each parent directory. It stops after the first directory that contains `.git`, and checks that directory too. Without a `.git` directory it walks up to the filesystem root. Your home directory gets no special treatment. It stops before checking any world-writable directory, such as `/tmp`, because any local user can create files there, and uses no file from there or above. The world-writable check applies on Unix-like systems and is skipped on Windows, where permission bits do not reflect directory access.
3. If no file is found, gglint uses the defaults: mode `standard`, tests reported.

`[[exclude]]` paths match the slash-separated path relative to the directory of the config file, or to the working directory when there is no file.

`[[exclude]]` `tests` selects by file kind. With `tests = true` the entry matches only findings in files whose name ends in `_test.go`. With `tests = false` it matches only findings in other files. Without `tests` the entry matches files of both kinds. Like the other keys, `tests` only narrows the entry, so `tests = true` with `rules` turns those rules off in test code and leaves them on elsewhere.

### Config errors

A config error exits 2 and prints `gglint: <config path>: <message>`. A bad `-mode` value prints the message without a path. The messages are:

```text
unknown mode "x" (want basic, standard, or strict)
enable: unknown rule "x"
disable: unknown rule "x"
rule "x" is in both enable and disable
rule "typecheck" cannot be disabled
exclude[0]: set at least one of paths, rules, tests, text
exclude[0].paths[0]: <regexp error>
exclude[0].text: <regexp error>
exclude[0].rules: unknown rule "x"
exclude[0].rules: typecheck findings cannot be excluded
rules.complexity.max_lines: must be >= 0
rules.checkimports.deny[0]: path is empty
<row>:<column>: unknown key <key>
```

## Modes

| Mode | What it adds |
|---|---|
| `basic` | Defects with near-zero false positives, annotation-driven checks, and lenient complexity limits. |
| `standard` | Everything in `basic`, plus idiomatic Go, security, concurrency and leak hygiene, formatting, and complexity limits tuned for AI coding agents. This is the default. |
| `strict` | Everything in `standard`, plus structured concurrency, channel-blocking and closer discipline, opinionated style, and tight agent limits. |

The modes are nested: every rule in `basic` is in `standard`, and every rule in `standard` is in `strict`. Some rules also check more in a higher mode; their descriptions in [Rules](#rules) say how.

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

### Concurrency coverage

| Hazard | Rules |
|---|---|
| Unjoined, unstructured, or unbounded goroutines | `goroutine` (a join in `standard`; errgroup or `WaitGroup.Go` only, exit paths, and bounded fan-out in `strict`), `testinggoroutine`, `staticcheck:SA2002` |
| Goroutines blocked forever on channels | `chanblock` (`strict`), and the exit-path and empty-select checks of `goroutine` |
| Lost or misused contexts | `lostcancel`, `fatcontext`, `noctx`, and in `strict` `contextcheck` and `containedctx` |
| Data races | `capturerace`, `checklocks` (guards, `+checkatomic`, inferred guards in `strict`), `copylocks`, `atomic`, `atomicalign`, `checkaligned`, `staticcheck:SA2000` to `SA2003` |
| `sync.WaitGroup` misuse | `waitgroup`, `modernize:waitgroupgo`, `staticcheck:SA2000` |
| Deadlock by re-entry | `reentrant_withlock_deadlock`, `checklocks` double-acquire check |
| Timer churn and leaks | `timeafter`, `staticcheck:SA1015` |

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

The `nolint` rule checks the directives themselves and reports at the directive:

| Message | Modes |
|---|---|
| `malformed nolint directive` (for example `// nolint` with a space) | all |
| `unknown nolint rule "x"` | all |
| `typecheck findings cannot be suppressed` | all |
| `unused nolint directive for x` (only when every named rule is selected) | all |
| `nolint directive must name rules` | `standard`, `strict` |
| `nolint directive needs a reason after //` | `strict` |

## Rules

The catalog has 122 rules. `basic` runs 54 of them, `standard` runs 101, and `strict` runs all 122. Run `gglint rules` for the list that matches your build.

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
| `checklinkname` | gvisor | Validates //go:linkname directives against the signatures of their targets. |
| `checklocks` | gvisor | Checks +checklocks and +checkatomic annotations and reports double acquisition of a held lock. |
| `complexity` | logos | Caps cognitive score, nesting, lines, parameters, and file size; large units exceed what agents edit reliably. |
| `composites` | go/analysis | check for unkeyed composite literals |
| `copylocks` | go/analysis | check for locks erroneously passed by value |
| `defers` | go/analysis | report common mistakes in defer statements |
| `directive` | go/analysis | check Go toolchain directives such as //go:debug |
| `errcheck` | third-party | Reports unchecked errors; standard adds blank assignments, strict adds unchecked type assertions. |
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
| `stackaddr` | logos | Reports a stack address kept as uintptr, or an unsafe.Pointer to stack storage that leaves its function. |
| `staticcheck` | staticcheck | Staticcheck: SA bug checks; standard adds S and ST checks, strict adds QF quick fixes. |
| `stdmethods` | go/analysis | check signature of methods of well-known interfaces |
| `stdversion` | go/analysis | report uses of too-new standard library symbols |
| `stringintconv` | go/analysis | check for string(int) conversions |
| `structtag` | go/analysis | check that struct field tags conform to reflect.StructTag.Get |
| `structtag_malformed` | logos | Reports struct tags reflect.StructTag.Get cannot parse and json or xml tags on unexported fields. |
| `testinggoroutine` | go/analysis | report calls to (\*testing.T).Fatal from goroutines started by a test |
| `tests` | go/analysis | check for common mistaken usages of tests and examples |
| `timeformat` | go/analysis | check for calls of (time.Time).Format or time.Parse with 2006-02-01 |
| `typecheck` | gglint | Reports package load and type errors; it cannot be disabled or suppressed. |
| `typednil` | logos | Reports a typed nil pointer returned as an interface, which is never nil. |
| `unmarshal` | go/analysis | report passing non-pointer or non-interface values to unmarshal |
| `unreachable` | go/analysis | check for unreachable code |
| `unsafeptr` | go/analysis | check for invalid conversions of uintptr to unsafe.Pointer |
| `unused` | staticcheck | Reports unused constants, variables, functions, types, and fields (staticcheck U1000). |
| `unusedresult` | go/analysis | check for unused results of calls to some functions |
| `waitgroup` | go/analysis | check for misuses of sync.WaitGroup |

### Added in standard

| Rule | Origin | Description |
|---|---|---|
| `asasalint` | third-party | Reports a []any passed as one element to a variadic ...any parameter. |
| `atomicalign` | go/analysis | check for non-64-bits-aligned arguments to sync/atomic functions |
| `bodyclose` | third-party | Reports HTTP response bodies that are never closed. |
| `capturerace` | logos | Reports multi-word variables written across a goroutine boundary without synchronization. |
| `checkaligned` | gvisor | Bans sync/atomic 32-bit and 64-bit function calls; use the typed atomics. |
| `deepequalerrors` | go/analysis | check for calls of reflect.DeepEqual on error values |
| `discarded_error` | logos | Reports call statements and blank assignments that drop an error result. |
| `dupword` | third-party | Reports duplicated words in comments and strings, such as "the the". |
| `durationcheck` | third-party | Reports multiplication of two time.Duration values. |
| `err_chain_reset` | logos | Reports fmt.Errorf calls that format an error with a verb other than %w or pass err.Error(). |
| `errorlint` | third-party | Reports error comparisons and assertions that break wrapping, and fmt.Errorf without %w. |
| `exhaustive` | third-party | Requires switch statements on enum types to cover every enum member. |
| `fatcontext` | third-party | Reports contexts nested in loops and closures, which grow the context chain each iteration. |
| `gochecksumtype` | third-party | Requires type switches on sealed sum-type interfaces to cover every variant. |
| `gocritic` | third-party | Runs go-critic diagnostic and style checkers plus deferInLoop; strict adds opinionated and performance. |
| `gofumpt` | third-party | Reports files that are not gofumpt-formatted, with a fix that formats them. |
| `goroutine` | logos | Requires joined goroutines with exit paths; strict requires errgroup or WaitGroup.Go and bounded fan-out. |
| `gosec` | third-party | Reports security problems found by gosec; standard skips G104, G115, G304, strict skips G104. |
| `httpmux` | go/analysis | report using Go 1.22 enhanced ServeMux patterns in older Go versions |
| `inline` | go/analysis | apply fixes based on 'go:fix inline' comment directives |
| `makezero` | third-party | Reports append to a slice created with a non-zero length. |
| `misspell` | third-party | Reports commonly misspelled English words, with a fix that corrects them. |
| `modernize` | go/analysis | Suggests modern Go replacements for outdated idioms and library calls. |
| `musttag` | third-party | Requires field tags on structs passed to JSON, XML, YAML, and TOML marshaling functions. |
| `nakedret` | third-party | Reports naked returns in functions longer than 30 lines. |
| `nilerr` | third-party | Reports returning nil after checking a non-nil error, or an error after checking it is nil. |
| `nilness` | go/analysis | check for redundant or impossible nil comparisons |
| `nilnesserr` | third-party | Reports returning a nil error after checking that a different error is non-nil. |
| `noctx` | third-party | Reports network and SQL calls made without a context.Context. |
| `noinit` | gglint | Reports init functions; move setup into an explicit function called from main. |
| `placeholder` | logos | Reports placeholder panics; strict also reports comments that start with TODO or FIXME. |
| `predeclared` | third-party | Reports declarations that shadow predeclared identifiers. |
| `reassign` | third-party | Reports assignments to variables of other packages, such as io.EOF. |
| `recvcheck` | third-party | Reports types whose methods mix value and pointer receivers. |
| `reflectvaluecompare` | go/analysis | check for comparing reflect.Value values with == or reflect.DeepEqual |
| `scannererr` | go/analysis | scannererr: report failure to check bufio.Scanner.Err |
| `sloglint` | third-party | Reports log/slog handlers built over io.Discard; use slog.DiscardHandler. |
| `sortslice` | go/analysis | check the argument type of sort.Slice |
| `sqlrowserr` | go/analysis | sqlrowserr: report failure to check sql.Rows.Err |
| `testableexamples` | third-party | Reports example functions without an output comment, which compile but never run. |
| `thelper` | third-party | Requires test helpers to call Helper first and take their \*testing.T, B, F, or TB first, conventionally named. |
| `timeafter` | logos | Reports time.After inside a loop, which allocates a timer per iteration. |
| `tparallel` | third-party | Reports t.Parallel used in subtests but not the top-level test, or the reverse. |
| `unconvert` | third-party | Reports type conversions to the type the value already has. |
| `unparam` | third-party | Reports unexported function parameters and results that are unused or always the same value. |
| `unusedwrite` | go/analysis | checks for unused writes |
| `usetesting` | third-party | Reports os.MkdirTemp, os.CreateTemp, and os.Chdir in tests, which have testing package replacements. |

### Added in strict

| Rule | Origin | Description |
|---|---|---|
| `asciicheck` | third-party | Reports identifiers that contain non-ASCII characters. |
| `backingpin` | logos | Reports subslices and substrings that keep a backing store over 64 KiB alive past the function. |
| `chanblock` | logos | Reports channel sends and receives that can block forever without a ctx.Done() case or buffer proof. |
| `checkimports` | gvisor | Restricts unsafe to \*_unsafe.go files and enforces the configured import deny list. |
| `closer` | logos | Reports local io closers that are neither closed with defer nor handed on. |
| `containedctx` | third-party | Reports struct types that hold a context.Context field. |
| `contextcheck` | third-party | Reports functions that use a non-inherited context. |
| `dupl` | third-party | Reports duplicated code fragments of 150 or more tokens. |
| `err113` | third-party | Reports dynamic errors.New and fmt.Errorf values and errors compared with ==. |
| `errname` | third-party | Requires error types named XxxError and sentinel errors named ErrXxx. |
| `forcetypeassert` | third-party | Reports type assertions whose ok result is not checked. |
| `gochecknoglobals` | third-party | Reports package-level variables. |
| `goconst` | third-party | Reports strings of 3 or more bytes repeated 3 or more times that should be constants. |
| `iface` | third-party | Reports identical, unused, and unexported interfaces and interface methods never called. |
| `ireturn` | third-party | Reports functions that return interfaces; accept interfaces, return concrete types. |
| `mirror` | third-party | Reports bytes and string conversions where a mirrored bytes or strings function exists. |
| `nilnil` | third-party | Reports returning a nil error together with an invalid nil value. |
| `paralleltest` | third-party | Requires tests and subtests to call t.Parallel. |
| `perfsprint` | third-party | Reports fmt.Sprintf calls that a faster strconv or string operation can replace. |
| `unsizedint` | logos | Reports int and uint fields in JSON structs; their size depends on the platform. |
| `wrapcheck` | third-party | Reports errors from other packages returned without wrapping. |

### Coming from golangci-lint

gglint keeps these golangci-lint linters in a new form:

| golangci-lint | gglint |
|---|---|
| `govet` | One rule per `go/analysis` pass |
| `modernize` | The `modernize` rule |
| `nolintlint` | The `nolint` rule |
| `gochecknoinits` | The `noinit` rule |
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
| Contested convention with no defect | `errchkjson`, `exhaustruct_v5`, `godoclint`, `gosmopolitan`, `mnd`, `nonamedreturns`, `prealloc`, `testpackage`, `unqueryvet`, `usestdlibvars`, `varnamelen` |
| Does nothing useful without project configuration | `forbidigo`, `goheader`, `gomoddirectives`, `importas`, `tagliatelle` |
| Deprecated upstream | `exhaustruct`, `gomodguard`, `wsl` |

The formatters `gci`, `gofmt`, and `goimports` are covered by `gofumpt`. `golines` is dropped as a layout opinion, and `swaggo` because it targets one library.

gglint does not rebuild Go or module plugins, `custom`, `migrate`, `config path|verify`, `cache`, the `formatters` and `fmt` commands, the JSON schema, the docs site, presets, severities, issue caps, SARIF and the other printers, `-new-from-patch`, `whole-files`, colors, or source excerpts.

## Development

gglint lints itself at `standard` with the repository's `gglint.toml`. That file excludes the verbatim gVisor port files, so they stay diffable against upstream, and turns off `gosec` in test files. golangci-lint with `.golangci.yml` stays as a second lint gate.

Run these gates before you commit:

```sh
go vet ./...
go test -race ./...
go run ./cmd/gglint run ./...
golangci-lint run ./...
```

## License

gglint is licensed under Apache-2.0; see [LICENSE](LICENSE). Code copied from gVisor keeps its file headers and is covered by [third_party/gvisor/LICENSE](third_party/gvisor/LICENSE). Test fixtures copied from loGos are covered by [third_party/logos/LICENSE](third_party/logos/LICENSE).
