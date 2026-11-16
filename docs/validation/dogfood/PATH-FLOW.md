# Mutex handoffs follow executed paths

The published asynchronous-release repair was too permissive about when its
release effect reached later statements. A deferred body, an untaken branch,
a loop that executes zero times, or a callback that may not run could clear the
single held-state flag. That hid real same-goroutine lock reacquisitions. The
same traversal also inspected an invoked literal's body before its arguments,
which reversed Go's required evaluation order.

The repair keeps a separate held state for each alternative and joins only
continuing paths. A join remains held when any predecessor remains held. Return
states belong to their function literal and rejoin only that literal's caller.
Unmodeled branch transfers cannot justify a later release. Typed constant
conditions can select one path; other conditions retain all possibilities.

## Preconditions and effects

The analyzer consumes the checked AST and its object/type/constant information.
Its state tracks the rule's existing variable/field-path mutex identity. The
release-only call-graph proof remains bounded and rejects unsupported effects.
This is a syntactic deadlock diagnostic, not a general liveness proof.

| Construct | Required behavior |
| --- | --- |
| Ordinary call | Evaluate function operand and arguments before the body. |
| `defer` | Evaluate operands now; inspect the deferred invocation in isolated state. |
| Branch | Start alternatives from the same incoming state and retain every continuing held path. |
| Short circuit | Do not export a release from a possibly skipped operand. |
| Loop | Include the zero-iteration path unless checked constants establish entry. |
| `select` | Evaluate every send/receive operand before choosing an arm; evaluate receive destinations only in their arm. |
| Callback summary | Possible invocation does not prove execution order or occurrence; inspect each callback in isolated state. |
| Constant builtin | Skip operands only when the type checker establishes a constant result; preserve variable-size generic and shadowed-call evaluation. |

The first-iteration proof accepts only direct `=` and `:=` initialization. A
compound assignment's right operand is not the counter's starting value. The
additional frozen regression begins with `i = 2` and then uses `i += 0` with
`i < 1`: its body never starts, so its `go mu.Unlock()` cannot prevent the next
`mu.Lock()` from deadlocking. A paired direct-assignment case still enters its
body and remains free of this finding.

## Source and test history

The recovery branch starts from published PR #9 revision
`54295117fbfec093b6362ab752df3c415a44feec`. The corresponding prior local tree
`68db7f0` was byte-identical. Only its later commits were replayed, preserving the
original test-first order and allowing a fast-forward publication of PR #9.
The recovered tree at `edbfbd8` equals prior local revision `058707e`.

The original path fixtures were committed before their production repair, in
local commits `6d2ce05`, `3f1190b`, `4e8d6c9`, and `ab25321`. Their replayed
commits are `787dc8a`, `f1a0612`, `1cfd5c0`, and `7a6f581`. The production repair
is `edbfbd8`. The new compound-initialization test is `1268a84`, followed by the
production restriction in `136119a2a20bcf7237aee6e28b2a213631700eda`.

The inherited raw RED logs retain their original fixture line numbers.
Intermediate candidate logs are diagnostic evidence; they are not presented as
separate retained source snapshots. Existing test expectations were preserved.

| Failure class | Frozen evidence |
| --- | --- |
| Deferred, branch, and operand order | `path-flow/mutex-flow-red.log` |
| Expanded path and zero-iteration coverage | `path-flow/mutex-flow-expanded-red.log` |
| Return and fallthrough controls | `path-flow/mutex-flow-preservation-red.log` |
| Conditional and reordered callbacks | `path-flow/mutex-flow-callback-red.log` |
| Constant unevaluated operands | `path-flow/mutex-flow-constant-red.log` |
| Compound loop initialization | `path-flow/compound-loop-red.log` |

## Final validation

Final validation commands use stock Go 1.27.4 on Linux/amd64 with
`GOTOOLCHAIN=local`, `GOMAXPROCS=2`, `GOMEMLIMIT=256MiB`, `GOGC=25`, `-p 1`,
and `-parallel 1`. Test-result caching is disabled with `-count=1`. No rule,
assertion, timeout, or concurrency-test workload is weakened.

The commands below are the relevant acceptance checks. The adjacent logs record
actual outcomes; resource-terminated runs are not passes.

```sh
go test -p 1 -parallel 1 -count=1 -timeout 10m \
  ./internal/analyzers/reentrantlock ./internal/analyzers/closer \
  ./internal/analyzers/infiniterecursion ./internal/driver ./internal/rules
go test -race -p 1 -parallel 1 -count=1 -timeout 10m \
  ./internal/analyzers/reentrantlock ./internal/analyzers/closer \
  ./internal/analyzers/infiniterecursion ./internal/driver
go vet -p 1 ./internal/analyzers/reentrantlock ./internal/analyzers/closer \
  ./internal/analyzers/infiniterecursion ./internal/driver ./internal/rules
sha256sum -c docs/validation/dogfood/CURRENT.sha256
```

`ANALYZERS.md` and `ANALYZERS.sha256` retain the earlier release-effect checkpoint.
They do not describe the later path-flow source hashes. `CURRENT.sha256` is the
source manifest for this follow-up. The final driver checkpoint below records
the later signature-validator extraction; `DRIVER.sha256` remains historical.

## Broader limits

The initial full-suite attempt began before the additional compound-loop fix.
Its log includes that regression's RED result and several externally killed
test processes. It is retained only as diagnostic evidence, not as a final
candidate gate. A final complete run is required before whole-suite acceptance
is claimed.

The prior actual-source modernc comparison remains tied to its recorded binary.
A passing selected-rule replay does not close the separate full-matrix package
loading failures. The pinned modernc-libc preparation attempted to download
`modernc.org/ccgo/v4@v4.38.1`; the Go module proxy TLS handshake timed out.
Neither dependency versions nor upstream source were changed to hide that
setup failure.

## Focused acceptance results

All five focused normal packages passed, including the complete LoGOs parity
and catalog suite. All four changed analyzer/driver packages passed with the
race detector. The corresponding five-package vet command completed with empty
output. Source formatting and `git diff --check` passed. These are results for
the production source at `136119a`, separate from the earlier partial full run.

Three Go source overlays deliberately reversed named invariants without editing
production files:

| Mutation | Observed required test failures |
| --- | --- |
| Remove the `=`/`:=` initialization restriction. | The compound-initialization regression loses its diagnostic. |
| Require both predecessor paths to remain held at a join. | Six required diagnostics disappear. |
| Let a deferred invocation change following synchronous state. | Four required diagnostics disappear. |

All three mutations exited 1 with the expected missing-diagnostic assertions,
without a compilation error. Their raw logs are retained under `path-flow`.
Removing the overlays restored a passing analyzer test; all seven source and
fixture hashes checked against the saved pre-mutation manifest still matched.

The final executable was built from the clean `136119a` checkout. Its SHA-256 is
`b3b3f4542d4cf4cdbffda392abba5d53dd4f92775ccf53361597bacb52913886`.
`path-flow/final-buildinfo.txt` records `vcs.modified=false` and the complete
Go/module/build metadata. Subsequent documentation commits do not change these
production sources.

The initial full attempt completed with exit 1. The `errdrop`, `gorace`,
`lockstate`, and `vettool` test processes were killed, and the newly added
compound-loop fixture failed against the pre-fix analyzer compiled by that run.
The complete raw log is `path-flow/initial-full-nonfinal.log`. The cause of the
process terminations was not established by these logs. The existing nine-engine
vettool stress workload and its assertions remain unchanged.

## Self-lint and the final driver checkpoint

Standard-mode self-lint of the four changed analyzer/driver packages found one
issue in the recovered driver: `typecheckContinuation` had cyclomatic complexity
18 against the unchanged limit of 15. The original finding is retained in
`path-flow/selflint-before-refactor.json`.

Commit `38be7742c18ca634db02b84527701190d03367db` separates recognition of the
indented `have`/`want` header from validation of one complete function signature.
The helper names the grammar invariant. The classification logic, diagnostic
syntax, tests, and thresholds are unchanged. No further analyzer change follows
`136119a`.

The complete driver normal and race suites passed again, as did driver vet.
Repeating the existing residual-classification mutation against the extracted
version failed all 100 required mixed-output assertions; clean restoration
passed. Those raw results are the `v2-driver-*` files under `path-flow`.

The separate final executable has SHA-256
`625120495f2ab131024ccfff69db08a9c4df43597dc8f48fd646a7b97e308d9a`.
Its `v2-buildinfo.txt` records clean source revision `38be774`. The earlier
executable remains untouched so earlier corpus observations retain their exact
binary identity. `CURRENT.sha256` identifies the final combined sources;
`DRIVER.sha256` remains the historical pre-extraction source checkpoint.

The final executable's standard-mode self-lint then completed with exit 0 and
`findings: []` across all four changed analyzer/driver packages. The complete
JSON and stderr records are `path-flow/v2-selflint.json` and `.stderr`.
