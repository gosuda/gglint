# Go 1.27.4 output classification regression

This evidence preserves the failed hosted checker before its schema repair.
The analyzer, all Go fixtures, the exact negative diagnostics, and every chi
expected array stay unchanged.

## Observed run

[Run 43960061537](https://github.com/gosuda/gglint/actions/runs/43960061537)
tested candidate `f8da9ae49e08ba4507cea58318badde2b3c974c1` through merge commit
`b82aed6994e134fb58f19517a84fa40f153ba8bf`. Both commits have tree
`4fe1a7411ae41c6a7c4ef28d3d07bbc54f02157b`.

The existing lint, full race, and isolated vettool jobs passed. The new
default-select regression job failed because its strict JSON field allowlist
did not include `OutputType`. Its original report remains `accepted: false`.

| Captured command | Exit | Required named outcomes | Original checker result |
| --- | ---: | --- | --- |
| Archived analyzer overlay | 1 | 2 runs and 2 failures; exactly 8 unexpected diagnostics | Unknown field |
| Complete analyzer package | 0 | 7 runs and 7 passes | Unknown field |
| Analyzer package under race detector | 0 | 7 runs and 7 passes | Unknown field |
| LoGOs parity case | 0 | 2 runs and 2 passes | Unknown field |

All six chi comparisons passed in the original report: basic, standard, and
strict on both source copies. They retain exactly 3, 52, and 101 findings,
respectively, together with the expected exit 1 and configuration stderr.

## Protocol and correction boundary

The pinned stock Go 1.27.4 documentation in `cmd/test2json/main.go` permits
`OutputType` only on `Action: "output"`. Its string values are empty for regular
output, `frame`, `error`, and `error-continue`. The converter emits these values
in `cmd/internal/test2json/test2json.go`. Both complete source files are archived.
The actual hosted records contain `frame` and `error`.

The repair must validate that finite string set and its action context. Other
unknown fields, absent output text, malformed types, changed diagnostics,
incorrect lifetimes, crashes, skips, build failures, and incorrect exits remain
rejected. Classification does not bypass the existing output-content checks.

The native LoGOs tagged-method checker reads the event action, package, name,
and output without rejecting this additional field. It does not share the
specific unknown-field rejection; this slice changes only the gglint checker.

## Preserved evidence

`failed-hosted-evidence.tar.gz` contains 335 exact files: every file from the
regression artifact except its generated CLI executable, all four raw job logs,
initial and final GitHub metadata, the original checker, and the stock protocol
source. It includes every command receipt, all raw stdout/stderr, setup logs,
both chi source copies, and before/after source/tool identities. Tar metadata
and gzip time are normalized; file bytes are unchanged.

`frozen.json` records every archived file's size and SHA256, the original artifact
identity, and the sole excluded binary. The complete original ZIP is artifact
`14274102066`, 18,770,930 bytes, SHA256
`7ea202ad476c4f8bf68ff113eda397dcc7fdf9319b0e2c2ae7a869f9e2480a80`.
Its generated CLI is 34,647,510 bytes, SHA256
`4ea13c96695c6aa1724a1379e21c43e8286b476c80db58f684c125888e845c20`.
The executable remains in the original hosted artifact; its build command,
build metadata, identity receipt, and every output are included here.

The Python-only reproduction, recorded in `red.stdout` and `red.stderr`, ran
the unchanged checker against the new frozen controls. It produced **8 failures
and 100 passes**. All four captured test streams and all four documented values
failed specifically at `unknown test event field`. The six captured chi replay
controls passed. No local Go command was executed.

The schema repair needs a fresh hosted run on its new tree. Retrospective
acceptance of these raw command outputs cannot change the original job's
failure into a successful hosted gate.
