# Analyzer hardening evidence

This is the earlier release-effect checkpoint. The subsequent executed-path
repair and its current source manifest are recorded in [PATH-FLOW.md](PATH-FLOW.md).
The historical `ANALYZERS.sha256` applies to the source revision below; use
`CURRENT.sha256` to verify the current follow-up sources.

This record covers the mutex handoff, explicit closer, and recursion-exit
repairs. The focused normal, race, parity, formatting, vet, and self-lint gates
passed. It does not establish that the whole repository or every upstream
package is correct. The wider corpus campaign records its own executed cases.

## Source identity

The baseline is `8cb0b8d95bc44514deeea5c8ba0e97a1cd218ad8`. The initial analyzer
repair was validated at local commit
`badfdb4d096529d72b8d1bb578153b962de0aec8`, tree
`04591bdf52f4d1ca6055647afbe54c45dea048eb`. These are workspace Git identities;
publishing commits through another transport may change their metadata.
[ANALYZERS.sha256](ANALYZERS.sha256) independently identifies the 18 changed
source and test files. Documentation-only changes do not alter those inputs.

This record and its source manifest retain that historical identity. The later
[execution-path correction](FLOW.md) has its own frozen regressions, normal and
race gates, complete rules retry, mutation controls, and final executable hash.
Its `FLOW.sha256` applies to the recorded `058707e` checkpoint; checking
`ANALYZERS.sha256` requires the earlier source snapshot named above. The further
[compound-initialization and driver follow-up](PATH-FLOW.md) supplies the final
combined source manifest, `CURRENT.sha256`.

The toolchain reports `go1.27.4 linux/amd64`. Its `go` executable SHA-256 is
`de0ae3e082ce7599eae4fb69fa2ef1e43b8f31ffd3fce62cdf81930aa9005050`.
The baseline gglint executable used for source self-lint has SHA-256
`77bca221a7c1cacf38c5458b5f458b25e50ba13b96e90174259190316bc2d2c9`.
Runs used `GOTOOLCHAIN=local`, `GOMAXPROCS=2`, and a task-specific build cache.
Test result caching was disabled with `-count=1`.

## Findings and changes

### Cross-goroutine mutex wake

The real-code seed is modernc SQLite's `TestMutexWakeOnce` at commit
`92d72a45bcb65ab33bbe79c8667f2b8b529ae902`:
[test source](https://gitlab.com/cznic/sqlite/-/blob/92d72a45bcb65ab33bbe79c8667f2b8b529ae902/sharedcache_test.go)
and [wake helper](https://gitlab.com/cznic/sqlite/-/blob/92d72a45bcb65ab33bbe79c8667f2b8b529ae902/mutex.go).
It locks a mutex, starts two notifiers, then locks again to wait for the winning
notifier. A scalar atomic comparison lets exactly one notifier unlock it.

The old rule ignored the asynchronous release and reported the second Lock as
a same-goroutine reentrant deadlock. Ten unexpected reports in the reduced
fixtures reproduced this class before production changed.

The repair applies an explicit release effect after a go statement's function
operand and arguments have been checked synchronously. A bounded fixed point
propagates effects through same-package functions. It reuses the existing
typed receiver/parameter keys and field-path bound.

The release inference is deliberately conservative:

| Requirement | Reason |
|---|---|
| The released receiver matches the held mutex after typed binding. | Releasing a different mutex does not end the held region. |
| Receiver and parameter identity must remain stable. | Reassignment, alias writes, and replacement helpers can unlock another object. |
| Value receivers do not establish a release alias. | Copying a receiver containing a mutex does not unlock the original. |
| The synchronous call graph has no acquisition or opaque effect. | External callbacks, local function values, and interface calls can acquire before the apparent unlock. |
| Mutations, loops, switches, selects, branches, and nested spawns block inference. | Their effects or control transfers are outside this bounded proof. |
| An unconditional return cannot precede another statement. | An unreachable Unlock is not a release effect. Simple blocks, labeled statements, and complete if/else returns are checked. |
| Unsafe operations, mutating builtins, panic, and recover block inference. | They can change a carrier or prevent the apparent release. |
| Only type-resolved scalar `sync/atomic` operations are admitted. | Modernc's scalar wake flag remains supported; Pointer/Value atomics and a package merely named `atomic` do not widen the boundary. |

This models a **possible release of the tracked mutex**. It is not a proof of
whole-program liveness, deadlock freedom, or arbitrary unsafe-memory behavior.
In particular, it does not solve channel protocols or prove which conditional
path executes. Unknown effects retain the earlier diagnostic. A helper with a
harmless local assignment can therefore remain outside this precision repair.

Preservation tests cover blocking readers, acquires hidden by aliases or call
results, opaque callbacks, `sync.Once.Do`, `sort.Slice`, external lock wrappers,
copied receivers, rebound parameters/receivers, pointer-slot aliases, replacement
helpers, unsafe atomic writes, joined nested spawns, implicit range-function
calls, goto cycles, unreachable returns, and recovered panics. The original
direct, recursive, method-expression, callback, and go-argument controls remain.

### Explicit Close calls

The old closer rule recognized deferred calls but warned after ordinary Close
calls used as assignment/declaration values, statements, or return operands.
Handling the returned error does not undo the Close call.

The repair recognizes a direct Close call in those contexts, including
parentheses and if/for/switch initializers. A call buried in a short-circuit
expression, a call on another or shadowed object, and a direct `go f.Close()` do
not satisfy that rule. The native LoGOs `io.NopCloser` exception is also restored.
A user function merely named `NopCloser` does not receive that exception.

The imported LoGOs fixture changed from the actual native rule/fixture contract:
the erroneous `explicitClose` expectation was removed, existing native
statement/return cases were imported, and the native `io.NopCloser` exception
was retained. These are contract corrections, not expectations changed to fit
an analyzer output. Twenty unexpected diagnostic instances across the analyzer
and parity fixtures preceded the repair.

### Builtin panic identity

The recursion checker treated the spelling `panic` as a terminating call.
A user function or variable with that name could therefore hide infinite
recursion. Conversely, a parenthesized builtin call was missed.

The repair uses the checked callee object and compares it with the universe
builtin. Tests first reproduced four missing reports for shadowed names and two
unexpected reports for parenthesized builtin calls. Existing process-exit
controls remain, including `os.Exit`, `runtime.Goexit`, and `log.Fatal`.

## Executed checks

| Check | Result | Evidence |
|---|---|---|
| Three changed analyzer packages and complete `internal/rules` tests | Passed | [normal.log](analyzers/normal.log) |
| Three changed analyzer packages with `-race` | Passed | [race.log](analyzers/race.log) |
| Complete LoGOs parity and README catalog checks | Passed as part of `internal/rules` | [normal.log](analyzers/normal.log) |
| `go vet` on those four packages | Passed with empty output | Commands below |
| `gofmt -l` on changed Go files and `git diff --check` | Passed with empty output | Final source manifest |
| Standard-mode source self-lint | Zero findings | [selflint-after.json](analyzers/selflint-after.json) |

Self-lint caught a cognitive-complexity score of 30 in the first handoff
implementation, plus a replaceable loop. The implementation was split around
its barrier and release fixed points and used `slices.ContainsFunc`.
[The original two findings](analyzers/selflint-before.json) remain recorded.
No complexity threshold or exclusion changed.

Replay these historical focused gates from the source snapshot above with Go
1.27.4. The later flow records supply their own commands and source hashes.

```sh
GOTOOLCHAIN=local GOMAXPROCS=2 go test -count=1 \
  ./internal/analyzers/reentrantlock ./internal/analyzers/closer \
  ./internal/analyzers/infiniterecursion ./internal/rules
GOTOOLCHAIN=local GOMAXPROCS=2 go test -race -count=1 \
  ./internal/analyzers/reentrantlock ./internal/analyzers/closer \
  ./internal/analyzers/infiniterecursion
GOTOOLCHAIN=local GOMAXPROCS=2 go vet \
  ./internal/analyzers/reentrantlock ./internal/analyzers/closer \
  ./internal/analyzers/infiniterecursion ./internal/rules
sha256sum -c docs/validation/dogfood/ANALYZERS.sha256
```

## Attack and mutation records

The RED logs retain the line numbers of their particular fixture revision.
Intermediate local candidate logs are diagnostic evidence, not a claim that
every uncommitted candidate had a separately retained source snapshot. The
final committed inputs, normal/race gates, and mutation restorations establish
the acceptance evidence. Test expectations were retained or strengthened;
one closer fixture received formatting without changing its assertions.

| Attack family | Observed RED | Log |
|---|---|---|
| Valid asynchronous handoffs | 10 unexpected diagnostics | [baseline](analyzers/mutex-baseline-red.log) |
| Acquisitions through aliases/call results | 2 missing diagnostics | [alias](analyzers/mutex-alias-red.log) |
| Blocking read acquisition | 1 missing diagnostic | [reader](analyzers/mutex-reader-red.log) |
| Unresolved callbacks | 2 missing diagnostics | [callback](analyzers/mutex-callback-red.log) |
| External callbacks, atomic-name spoof, copied receivers | 6 missing diagnostics | [boundary](analyzers/mutex-boundary-red.log) |
| Carrier rebinding | 6 missing diagnostics | [rebinding](analyzers/mutex-rebinding-red.log) |
| Unsafe-source conversion and implicit/unreachable flow | 4 missing diagnostics | [implicit](analyzers/mutex-implicit-red.log) |
| Goto, returns, recovered panic | 6 missing diagnostics | [termination](analyzers/mutex-termination-red.log) |
| Explicit Close/native parity | 20 unexpected diagnostic instances | [closer](analyzers/closer-red.log) |
| Panic identity | 4 missing and 2 unexpected diagnostics | [recursion](analyzers/recursion-red.log) |

Mutation probes use Go source overlays; production files are not rewritten.

| Deliberately wrong implementation | Expected test failure observed | Evidence |
|---|---|---|
| Return true for every mutex handoff. | 31 retained diagnostics disappear. | [mutation](analyzers/mutex-final-mutation.log), [clean restoration](analyzers/mutex-final-restored.log) |
| Accept every Close-call parent context. | The two short-circuit cases and asynchronous Close lose their required findings. | [mutation](analyzers/closer-mutation.log) |
| Restore the old spelling-based panic test. | All six panic-identity regressions return. | [mutation](analyzers/recursion-mutation.log) |

For example, the mutex mutation replaces only
`return releases && !blocked` in `handoff.go` with `return true`, maps that
temporary file over the original in a Go `-overlay` JSON file, and runs
`go test -count=1 ./internal/analyzers/reentrantlock`. Its exit status must be
1 with the missing-diagnostic failures above. Removing the overlay restores
the passing result. A compile failure would not satisfy this control.

## Remaining campaign gate

The [actual-source modernc replay](modernc/README.md) additionally compares the
baseline and candidate on the complete pinned SQLite source tree with tests
enabled and only the mutex rule plus mandatory type checking selected. The
baseline reports `TestMutexWakeOnce`'s second lock; the candidate reports none.
That focused result does not replace the full modernc matrix's separate
dependency-loading failures.

The full-repository test gate remains incomplete. The pre-existing test that
loads and runs nine vettool engines was killed on both the candidate and the
pristine `8cb0b8d` baseline in this environment. That is not a pass and does not
establish which resource or platform restriction caused termination. No
vettool test or assertion was weakened in the delivered changes. The campaign
report retains that separate limitation and its raw logs; a suitable independent
runner must complete the full gate.
