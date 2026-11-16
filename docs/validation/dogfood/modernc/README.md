# Actual-source SQLite mutex replay

The pristine gglint baseline reports one false reentrant-lock diagnostic in
modernc SQLite's `TestMutexWakeOnce`. The candidate reports none when it analyzes
the same complete pinned source tree with the same selected rule and mandatory
type checking. This is a focused analyzer comparison, not a pass for the whole
modernc lint matrix.

## Inputs

- Upstream: <https://gitlab.com/cznic/sqlite.git>
- Source revision: `92d72a45bcb65ab33bbe79c8667f2b8b529ae902`.
- Test: [sharedcache_test.go](https://gitlab.com/cznic/sqlite/-/blob/92d72a45bcb65ab33bbe79c8667f2b8b529ae902/sharedcache_test.go),
  `TestMutexWakeOnce`, second `m.Lock()` at line 591.
- Release helper: [mutex.go](https://gitlab.com/cznic/sqlite/-/blob/92d72a45bcb65ab33bbe79c8667f2b8b529ae902/mutex.go),
  `(*mutex).wake`.
- All 2,022 tracked files were copied with `git archive` from the verified clean
  source pin. After both runs, every copied file matched the original tracked
  file's SHA-256. [source-files.sha256](source-files.sha256) records those hashes;
  its SHA-256 is
  `91747a32ba69b3d3cf315963ff82748829482d69f60d8a9d2c44934662299341`.
- [gglint.toml](gglint.toml) selects only `reentrant_withlock_deadlock` and the
  mandatory `typecheck` rule. It explicitly sets `tests = true`, has no build
  tags, and has SHA-256
  `f865c1cb1da037341793f66cb0ec1d6a1d8ffd49b1b1f3912d56120b4305ef87`.
- The analysis used stock Go 1.27.4, Linux/amd64, `CGO_ENABLED=0`, and one Go
  worker. [go.env.json](go.env.json) records the replay environment query and
  [toolchain.sha256](toolchain.sha256) identifies the stock Go driver. No LoGOs
  capability policy was involved.

The two selected-rule listings are byte-identical:
[baseline.rules](baseline.rules), [candidate.rules](candidate.rules). Disabling
other rules narrows this comparison; it does not suppress type errors or change
any source. The original full-matrix runs remain separate evidence.

## Result

| Binary | Exit status | Complete focused result |
|---|---:|---|
| Baseline `8cb0b8d95bc44514deeea5c8ba0e97a1cd218ad8` | 1 | One `reentrant_withlock_deadlock` finding at `sharedcache_test.go:591:3`; no type errors. |
| Candidate `badfdb4d096529d72b8d1bb578153b962de0aec8` | 0 | Empty findings; no type errors. |

These are the local source revisions embedded in each binary. Both build-info
records say `vcs.modified=false`. The binary hashes are:

| Binary | SHA-256 |
|---|---|
| Baseline | `77bca221a7c1cacf38c5458b5f458b25e50ba13b96e90174259190316bc2d2c9` |
| Candidate | `2266274ba58e7be7dbb21ef07e3a6ffc729266a6c8f57e08143e54c6a36a0150` |

The candidate hash also matches the binary used by the accepted corpus matrix.
Full build metadata is retained in [baseline.buildinfo.txt](baseline.buildinfo.txt)
and [candidate.buildinfo.txt](candidate.buildinfo.txt).

Raw CLI results are [baseline.stdout](baseline.stdout) and
[candidate.stdout](candidate.stdout), with their corresponding `.stderr` and
`.exit` files. The transparent scratch `go` wrapper preserved the real stock-Go
arguments and exit status while teeing subprocess stderr. Both underlying
`go vet` stderr files are empty; this pair has no hidden dependency failure.
The `.go.stderr.commands` files record the actual Go invocations.

The test starts two goroutines that call `wake`. Its compare-and-swap permits one
of them to unlock the mutex, so the second lock is a handoff in this program.
The repair recognizes that release effect on the same mutex. The separate
adversarial suite checks that acquisitions, copied receivers, changed carriers,
opaque calls, and unsupported control flow retain the deadlock diagnostic.
Neither this comparison nor that release model proves general program liveness.

## Reproduction

From a private checkout or archive of the pinned SQLite tree, run the following
with each checked binary. `STOCK_GO_BIN` names the stock Go 1.27.4 `bin`
directory, `GGLINT_BINARY` names one of the binaries above, and
`EVIDENCE_CONFIG` names the delivered `gglint.toml`.

```sh
PATH="$STOCK_GO_BIN:$PATH" \
GOTOOLCHAIN=local GOENV=off GOWORK=off GOTELEMETRY=off \
CGO_ENABLED=0 GOMAXPROCS=1 GOPROXY=off GOSUMDB=off \
GOFLAGS='-mod=readonly -buildvcs=false -p=1' \
"$GGLINT_BINARY" run -config "$EVIDENCE_CONFIG" -mode basic -format json .
```

The original runs used `/home/agent/go/pkg/mod` as the already populated module
cache and `/workspace/scratch/93e0563596d1/corpus-cache/gglint` as the shared Go
build cache. Each command had a 240-second timeout. Elapsed and CPU times are
retained in the `.resource.txt` files. The runs had different cache states and
must not be treated as a performance comparison.

To verify a private copy before replay, run
`sha256sum --check source-files.sha256` from its root with the hash file's full
path. The relevant individual file hashes are also listed in
[inputs.sha256](inputs.sha256).

## Broader gate remains incomplete

The same final binary's full `basic`, `standard`, and `strict` modernc matrix
cells returned typecheck findings about missing dependency `vetx` artifacts.
Those cells remain `load-failed`; this focused pass does not replace them or
establish whole-package coverage for the other analyzers. Their underlying
failure must be diagnosed separately.

[EVIDENCE.sha256](EVIDENCE.sha256) hashes the delivered evidence files, including
the complete source-file hash manifest. It can be checked from this directory.
