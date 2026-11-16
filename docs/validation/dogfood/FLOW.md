# Mutex execution-path evidence

This follow-up repairs two issues found during review of the asynchronous mutex
handoff change: a deferred body could release the caller's analysis state before
the defer ran, and a release in one conditional arm could suppress a diagnostic
in another arm. The same held-state handling is used for direct checks and
held-callback summaries.

## Source and review identity

The earlier analyzer implementation was
`badfdb4d096529d72b8d1bb578153b962de0aec8`. Its production analyzer sources were
unchanged when the expanded regression suite first failed. The review seeds are
[deferred invocation](https://github.com/gosuda/gglint/pull/9#discussion_r4586820446)
and [mutually exclusive arms](https://github.com/gosuda/gglint/pull/9#discussion_r4586827578).
Their claims were checked against local source and frozen tests before repair.

The repair is local commit `058707e12ac1894cd468a3c24d361aa2b9a2392b`, tree
`a5ceb8566ce3ad86949b1ecaad657d5e39fdf5bc`. These are workspace Git identities;
publication through a different transport can change commit metadata.
[FLOW.sha256](FLOW.sha256) identifies all source and fixtures in the analyzer
package. The older [analyzer evidence](ANALYZERS.md) and [driver evidence](DRIVER.md)
keep their original source and executable identities.

The accepted final executable has SHA-256
`b2559e7d57dbd9d1df1d201cf0a3b8885596177376f62c1c958f2fbdfc464358`.
Its [build metadata](flow/identity/buildinfo.txt) records the exact source above,
`vcs.modified=false`, Go 1.27.4, and `CGO_ENABLED=1`. Its CLI reports
`gglint v0.0.0-20261116153419-058707e12ac1 go1.27.4`.
Only trailing whitespace is removed from that metadata text.
The [executable hash record](flow/identity/tools.sha256) also confirms that the
baseline, first analyzer candidate, and driver-only candidate were preserved.
Corpus results for this source must name this new executable identity.

## Invariants and repair

The walker carries a small state for the current execution path. It records
whether the mutex is held, whether that path has returned, whether a return
path held the mutex, and whether an unmodeled branch transfer blocks further
release inference. It uses the existing AST and type information; it does not
build a general control-flow graph.

| Boundary | Behavior |
|---|---|
| Function operand and ordinary arguments | Evaluate in source order before the invocation. |
| Deferred invocation | Inspect using a saved state after immediate operand evaluation; its release effects do not alter subsequent synchronous statements. |
| Directly invoked literal | Keep actual effects; scope its return accumulator to that invocation. |
| Callback argument | Inspect in isolated state. A may-invoke fact does not establish that the callback ran or its order relative to another callback. |
| If, switch, type-switch, select arms | Start from their proper incoming state and keep mutually exclusive arm effects separate. |
| Join | Retain held state if any continuing predecessor holds the mutex. Returning predecessors do not contaminate the continuation. |
| Typed constants | Select the known arm. Constant-switch fallthrough still reaches the following body. |
| Short-circuit expression | Apply the always-evaluated operand; keep an optional operand from erasing the path where it did not execute. |
| Select operands | Evaluate every send channel/value and receive channel before any arm body. Receive-assignment left sides execute only in their chosen arm. |
| Loops | Retain the entry state when the body may execute zero times. A typed constant initializer and comparison can prove first entry into the modernc counter loop. |
| Unmodeled branch transfer | Keep conservative state; a later apparent release cannot justify suppressing a diagnostic on that path. |
| Constant call expression | Skip runtime operand effects, including constant unsafe.Sizeof/Alignof. Shadowed functions and generic variable-size calls remain evaluated. |

The previous acquisition, carrier-stability, copied-receiver, unsafe-pointer,
opaque-call, and release-summary barriers remain. Their original fixtures still
run in the complete reentrant package gate. The real-code-derived `wakeOnce`
fixture remains a required positive control.

This remains a bounded possible-release model for an actually reached spawn.
It does not prove whole-program deadlock freedom, channel liveness, the truth
of every condition inside a releasing goroutine, or arbitrary alias behavior.
Unknown loop trip counts, range bodies, callback invocation paths, and branch
transfers can retain diagnostics conservatively. General boolean path
correlation, complete defer-stack execution, and a full loop fixed point are
outside this repair.

## Frozen tests and failures

The primary test-only commit is `6d2ce05`. Against unchanged prior production
sources it found 21 missing diagnostics and three unexpected diagnostics.
Those include the exact review examples, direct and callback contexts,
defer-argument ordering, conditional joins, return scope, short-circuit effects,
loop entry, select evaluation order, and independent case expressions.

Subsequent test-only commits are `3f1190b`, `4e8d6c9`, and `ab25321`. They
strengthen preservation controls for returned paths, constant fallthrough,
conditionally invoked and reordered callbacks, and unevaluated constant
operands. No existing expected diagnostic was removed. Imported native LoGOs
fixtures were not changed.

| Stage | Observed RED | Raw record |
|---|---|---|
| Initial review examples and evaluation controls | 13 missing, one unexpected diagnostic | [initial](flow/mutex-flow-red.log) |
| Expanded branch, join, and operand controls | 21 missing, three unexpected diagnostics | [expanded](flow/mutex-flow-expanded-red.log) |
| Return accumulator and constant fallthrough | Two missing diagnostics in the intermediate implementation | [preservation](flow/mutex-flow-preservation-red.log) |
| May-invoke and reversed callback order | Two missing diagnostics | [callback](flow/mutex-flow-callback-red.log) |
| Unevaluated constant operands | Two missing and one unexpected diagnostic | [constant](flow/mutex-flow-constant-red.log) |

Intermediate RED logs refer to their particular fixture revision and local
implementation attempt. They are not a claim that every intermediate production
tree was separately committed. Final source hashes, unchanged expectations, and
the final normal and mutation gates establish the delivered behavior.

## Validation

Runs used the stock Go 1.27.4 toolchain with `GOTOOLCHAIN=local`, one worker,
`GOMEMLIMIT=384MiB`, a dedicated build cache, and shared-memory temporary files.
Test-result caching was disabled with `-count=1`. The toolchain executable hash
is `de0ae3e082ce7599eae4fb69fa2ef1e43b8f31ffd3fce62cdf81930aa9005050`.

| Gate | Result | Evidence |
|---|---|---|
| Complete reentrant analyzer package | Passed | [restored normal](flow/mutex-flow-restored.log) |
| Reentrant package with race detector | Passed | [race](flow/mutex-flow-race.log) |
| Native reentrant parity fixture | Passed | [focused parity](flow/mutex-flow-parity.log) |
| Complete `internal/rules` standalone retry | Passed with GOGC=30 and one worker | [full rules](flow/mutex-flow-rules-solo.log) |
| `go vet` on the changed analyzer | Passed with empty output | [vet](flow/mutex-flow-vet.log) |
| Standard source self-lint | Zero findings | [report](flow/mutex-flow-selflint-after.json) |
| Formatting and diff whitespace checks | Passed with empty output | Final source manifest |

Self-lint first caught a cognitive-complexity score of 28 in the AST dispatcher
and a backward loop that could use `slices.Backward`. The
[two original findings](flow/mutex-flow-selflint-before.json) are retained.
Dispatch was split by invocation phase and control flow, and the standard
iterator replaced the index loop. No threshold, rule selection, or exclusion
changed.

Replay focused gates from the repository root with the recorded toolchain:

```sh
GOTOOLCHAIN=local GOMAXPROCS=1 GOMEMLIMIT=384MiB go test -p=1 -count=1 ./internal/analyzers/reentrantlock
GOTOOLCHAIN=local GOMAXPROCS=1 GOMEMLIMIT=384MiB go test -race -p=1 -count=1 ./internal/analyzers/reentrantlock
GOTOOLCHAIN=local GOMAXPROCS=1 GOMEMLIMIT=384MiB go test -p=1 -parallel=1 -count=1 -run '^TestLogosPorts/concurrency_reentrant_withlock_deadlock$' ./internal/rules
GOTOOLCHAIN=local GOMAXPROCS=1 GOMEMLIMIT=384MiB GOGC=30 go test -p=1 -parallel=1 -count=1 ./internal/rules
GOTOOLCHAIN=local GOMAXPROCS=1 GOMEMLIMIT=384MiB go vet -p=1 ./internal/analyzers/reentrantlock
sha256sum -c docs/validation/dogfood/FLOW.sha256
```

## Mutation checks

Both probes used Go source overlays, leaving production source unchanged.
A compile error would not satisfy these controls.

| Deliberate defect | Required failure observed | Evidence |
|---|---|---|
| Keep the fork's outgoing held flag instead of restoring the incoming flag | 22 required diagnostics disappear | [leak mutation](flow/mutex-flow-leak-mutation.log) |
| Replace OR with AND when joining continuing held paths | Five required diagnostics disappear | [join mutation](flow/mutex-flow-join-mutation.log) |
| Remove the overlay | Complete package passes | [restored](flow/mutex-flow-restored.log) |

The first mutation changes the restoration in `flow.fork` to
`f.state = before; f.state.held = after.held`. The second changes only the `held`
expression in `pathState.join` from the OR of the two continuing held states
to their AND. The retained patch files identify the exact changes.

## Broader gate record

The combined final normal command passed the reentrant and driver packages,
but its full `internal/rules` process was killed. The
[complete raw output](flow/mutex-flow-final-normal-killed.log) is retained.
This alone does not establish the cause of termination.
A subsequent standalone full-rules retry passed with one worker and GOGC=30;
its separate log is retained above. The failed observation is not replaced.

The earlier full-repository nine-engine vettool gate is a separate incomplete
gate, already reproduced on both the pristine baseline and earlier candidate.
No vettool test was changed or weakened. Corpus replay results must be read
against their own executable hashes; the earlier matrix is not silently
relabelled as validation of this new source.

The first [final-binary build](flow/identity/build-command.txt) reached the linker,
which was [killed](flow/identity/build.log), and exited 1. A separately recorded
[retry](flow/identity/build-retry-command.txt) passed with the same clean source
and CGO profile, `GOGC=10`, `GOMEMLIMIT=256MiB`, a disk-backed temporary directory,
and no concurrent campaign build. Its [output](flow/identity/build-retry.log) was
empty and its exit status was 0. These observations do not establish why the
first linker was terminated or a performance comparison between the runs.
