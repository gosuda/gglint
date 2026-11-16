# Driver failure-preservation evidence

The driver now keeps a failed tool's stderr when source diagnostics appear in
the same run. The earlier implementation returned findings as soon as any Go
source position appeared, discarding the rest of stderr. A killed child, panic,
internal compiler error, or unfamiliar failure could therefore become an exit-1
report without the tool failure that made the analysis incomplete.

## Source identity and scope

The driver implementation at baseline
`8cb0b8d95bc44514deeea5c8ba0e97a1cd218ad8` was unchanged when the first frozen
regressions were committed in `bcd7100`. The production repair is local commit
`7b7ea69fcb41571e6fc0b4c2261f3a59600ddf38`, tree
`c4faa01b46622a29e32d197444b884b7f0972341`. These are workspace Git identities;
publication through another transport may change commit metadata.
[DRIVER.sha256](DRIVER.sha256) identifies the complete driver source and tests.
[DRIVER-EVIDENCE.sha256](DRIVER-EVIDENCE.sha256) identifies this record and its
raw validation artifacts.

This is a narrow change to failed-command handling in `driver.Vet` and
`driver.Fix`. It does not change analyzer selection, fact export, cache keys,
facts-only units, recovery behavior, or successful-command handling. Existing
JSON decoder errors retain priority, including the `internal: package:
analyzer: error` contract for structured analyzer errors.

The new classification requires at least one positioned source diagnostic and
no unexplained stderr. Recognized additional lines are package headers, valid
download notices, blank lines, and attached `have`/`want` type details. Positioned
panic, fatal-runtime, killed-child, and internal-compiler-error messages also
make the command fail. Unknown content yields the existing `load:` error and
no partial findings. Interior whitespace, CRLF, blank lines, and stack context
remain in that error; the existing outer whitespace trim is preserved.

Continuation validation parses the complete type signature with Go's parser.
It admits the formatter's exact pseudo-types, quoted package qualifiers,
package annotations, and final expanded argument notation. It rejects arbitrary
comments, function bodies, trailing error text, and unattached continuations.
This recognizes the tool's diagnostic grammar; it does not authenticate output
from a malicious replacement tool.

## Test-first records

The first RED run preceded production edits. Later RED runs caught regressions
in intermediate classifier attempts. Their line numbers refer to those fixture
revisions. All final test expectations remain frozen or strengthened.

| Test stage | Observed failure | Evidence |
|---|---|---|
| Original mixed-output defect | 72 failed method cells across Vet/Fix and both output orders | [baseline RED](driver/driver-stderr-red.log) |
| Preserve JSON errors and whitespace-only lines | 2 preservation cases failed in the first classifier attempt | [preservation RED](driver/driver-stderr-preservation-red.log) |
| Require a complete continuation | 10 method cells exposed appended fatal text, comments, results, or function bodies | [continuation RED](driver/driver-stderr-continuation-red.log) |
| Reject arbitrary inline comments | 4 method cells exposed hidden text inside signatures | [comment RED](driver/driver-stderr-comment-red.log) |
| Preserve expanded arguments | The classifier initially rejected a real `have ([]bool...)` continuation | [variadic RED](driver/driver-stderr-variadic-red.log) |

The test-only commits are `bcd7100`, `be4713f`, `705b6c0`, `bf3dd19`, and
`2cc316f`, in that order. The initial attack set includes mixed positioned
errors and killed children, panic stacks, runtime failures, positioned and
unpositioned compiler errors, unfamiliar failures, and header/download
lookalikes. Final coverage contains 25 attack shapes in both orders, each
checked through Vet and Fix.

Preservation controls retain multi-package source findings plus analyzer JSON,
ordinary multiline type errors, Unicode method names, methods named `panic`,
nested signatures and method results, quoted string constants, struct tags,
secondary declaration positions, known progress notices, successful commands,
and structured analyzer failures.

The formatter exceptions were checked against the installed Go toolchain's
`go/types/assignments.go`, `format.go`, and `typestring.go`. The
[five-line source](driver/variadic.go.txt) independently produced
[this compiler output](driver/driver-stderr-variadic-compiler.log), with the
expected compile-error exit status 2:

```text
have ([]bool...)
want (int, string, ...bool)
```

## Focused validation

Runs used Go 1.27.4 on linux/amd64, `GOTOOLCHAIN=local`, `GOMAXPROCS=2`, and the
task-specific build cache. Test-result caching was disabled with `-count=1`.
The Go executable SHA-256 is
`de0ae3e082ce7599eae4fb69fa2ef1e43b8f31ffd3fce62cdf81930aa9005050`.

| Gate | Result | Evidence |
|---|---|---|
| Complete driver package tests | Passed | [normal](driver/driver-stderr-green.log) |
| Complete driver package with the race detector | Passed | [race](driver/driver-stderr-race.log) |
| `go vet ./internal/driver` | Passed with empty output | [vet](driver/driver-stderr-vet.log) |
| `gofmt -l` and `git diff --check` | Passed with empty output | Source manifest |
| Deliberately bypass residual classification | 100 required assertions failed, with no compile error | [mutation](driver/driver-stderr-mutation.log) |
| Restore unchanged production source | Passed | [restored](driver/driver-stderr-restored.log) |

Replay from the repository root:

```sh
GOTOOLCHAIN=local GOMAXPROCS=2 go test -count=1 ./internal/driver
GOTOOLCHAIN=local GOMAXPROCS=2 go test -race -count=1 ./internal/driver
GOTOOLCHAIN=local GOMAXPROCS=2 go vet ./internal/driver
sha256sum -c docs/validation/dogfood/DRIVER.sha256
```

The mutation used a Go source overlay. It changed only
`return findings, known && len(findings) != 0` to
`return findings, (known || true) && len(findings) != 0`. This recreates the
old any-position-is-enough behavior. All 100 mixed-output assertions failed;
removing the overlay restored the passing result. Production files were never
rewritten for the mutation.

## Executable acceptance

The original matrix executable was preserved at SHA-256
`2266274ba58e7be7dbb21ef07e3a6ffc729266a6c8f57e08143e54c6a36a0150`.
The separate updated executable has SHA-256
`04e1bd33fa7f725e521f6cd22242cf3d0eb440fcc1426053dbd977a8bc246034`.
Both build records show Go 1.27.4, `CGO_ENABLED=1`, and clean source trees. The
updated record identifies source commit `7b7ea69`. The
[build command](driver/identity/build-command.txt),
[original metadata](driver/identity/original-buildinfo.txt),
[updated metadata](driver/identity/updated-buildinfo.txt), and
[tool hashes](driver/identity/tools.sha256) are retained.

A small replacement `go` command emitted a real-looking positioned type error,
an unrelated analyzer finding, and a simulated child termination with extra
context. It performed no upstream compilation or runtime execution.

| Input and CLI path | Original executable | Updated executable |
|---|---|---|
| Mixed failure, `run` | Exit 1, two findings, error context lost | Exit 3, empty stdout, full stderr |
| Mixed failure, `run -fix` | Exit 1, two findings, error context lost | Exit 3, empty stdout, full stderr |
| Source-only failure, `run` and `run -fix` | Existing source-report contract | Exit 1, identical two-finding report |

The [verification script](driver/cli/verify.sh) checks exact exit codes, empty
stdout on the new failure paths, byte-for-byte stderr preservation, and unchanged
source-only reports. Its [result](driver/cli/verification.log), replacement
[Go command](driver/cli/bin/go), [inputs](driver/cli/mixed.stderr),
[JSON stream](driver/cli/stdout.json), [source-only input](driver/cli/source.stderr),
and all raw outputs and argv records under `driver/cli/outputs` are preserved.
The [replay instructions](driver/cli/REPLAY.md) identify the complete invocation.

## Limits of this evidence

The fake-Go tests deliberately emit known stderr and JSON streams. They prove
the driver's failure contract, not the cause of a real upstream process
termination. The original broad corpus cells with missing vetx dependencies
remain incomplete. A separate raw-stderr replay must establish any underlying
compiler or analyzer failure before attributing those outcomes to this bug.

The full-repository test gate remains separately incomplete because the
pre-existing nine-engine vettool test was killed on both the pristine baseline
and the analyzer candidate in this environment. No vettool test was changed or
weakened for this repair.
