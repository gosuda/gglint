# Hosted verification

The `Default-select regression and pinned chi replay` CI job supplements the
existing full race, isolated vettool stress, vet, external lint, and self-lint
jobs. Their commands, concurrency settings, and nine-worker stress workload
are unchanged.

The new job uses the existing pinned Go 1.27.4 setup action and uv 0.13.8,
whose archive SHA256 is verified before use. Python test, type-check, and
formatting dependencies are pinned in `test-requirements.txt`.

Invoke the checker on a clean checkout, with evidence outside it:

```sh
uv run --no-project --no-config --python 3.12 --no-python-downloads python \
  docs/validation/chanblock-default/check_ci.py \
  --repo "$GITHUB_WORKSPACE" \
  --evidence "$RUNNER_TEMP/chanblock-default" \
  --expected-head "$GITHUB_SHA"
```

The checkout must match the workflow's expected revision. Before invoking Go,
the checker verifies the pinned manifest and all 32 frozen files. It records
the Git commit/tree, every tracked file's SHA256, and executable hashes for
Go, compile, link, asm, vet, and Python. It repeats source/tool verification
after the run and fails on drift.

## Required evidence

1. **Archived production negative.** The old `chanblock.go` is supplied only
   through a Go overlay. Exactly the parent and `communications` child must
   run and fail, with exit 1 and a failing package. The child must emit the
   eight exact historical unexpected diagnostics, including source positions.
   Missing, duplicate, additional, misplaced, or changed diagnostics fail.
   The gate rejects skips, build events, crashes, timeouts, unknown failure
   output, and nonempty stderr.
2. **Repaired analyzer.** The complete analyzer package runs normally and
   under `-race`. Both runs require all seven named run/pass identities and
   successful package/exit outcomes. The focused LoGOs parity gate requires
   its two exact named identities. Parallel pause/continue events are checked
   without imposing an arbitrary subtest execution order.
3. **Pinned chi replay.** The helper builds an immutable CLI, fetches the exact
   recorded chi revision, and verifies the complete source inventory and h1.
   It creates a second copy from those verified source bytes. Both copies run
   every mode with the frozen tests-enabled configuration and `CGO_ENABLED=0`.
   Basic and standard must match their historical findings exactly. Strict
   must match the frozen 101-finding array. JSON types, field sets, mode,
   relative configuration path, stderr, and exit status are checked too.

The new profile uses Linux/amd64, `GOAMD64=v1`, `GOMAXPROCS=2`, readonly module
resolution, no workspace, no Go experiment/debug overrides, and offline Go
dependency resolution after CI downloads the pinned dependencies. Normal and
race tests use `CGO_ENABLED=1`; chi uses `0`. Safe environment receipts keep
resource-setting presence and values, so absent and empty settings remain
distinct. The installed Go directory is first on PATH for child Go commands.

Each command retains its argv, cwd, exit status, elapsed time, timeout/error
state, raw stdout/stderr, and their hashes. The runner kills and reaps a command
group on timeout or excessive output. The final report's `accepted` field is
true only when every required check and final identity check succeeds. A
completed historical negative or a warning-producing CLI run alone is not a
successful gate.

The artifact contains the command receipts, JSON test events, exact CLI
outputs, source/configuration receipts, built binary identity, and source/tool
snapshots. Setup and Python-check logs are retained alongside it, including
on failure. New evidence never overwrites the frozen historical records.

## Local scope

Only Python controls, static source review, formatting, frozen-hash checks,
and workflow validation were run while preparing this job. No local Go
execution was used. The Python controls include a demonstrated and repaired
JSON comparison error: Python treats `1`, `1.0`, and `true` as equal in ordinary
container comparisons; the checker now compares canonical JSON so those
types cannot be substituted in diagnostic records. Hosted Go results remain
pending until this commit runs in CI.
