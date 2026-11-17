# Default-select bounds

This slice restores one part of LoGOs parity: a select with a `default` case
cannot wait for one of its communications to become ready. Its operands and
case bodies can still block. An inner select needs its own bound.

## Source and observed failure

The corpus used gglint commit
`a95099aae13ff7ffb4b3825bd63f8510bd184c69`, built with Go 1.27.4. The original
primary and fresh-copy strict replays both reported this finding in chi commit
`c64ed7d6635f33a8b2feaff7dcfa55677023ac7d`:

```text
middleware/throttle.go:95:17: channel receive can block forever: select on ctx.Done()
```

That receive belongs to the immediate-token select, which has a `default`
case. Its deferred token send at line 97 is independent work and must retain
its warning under the existing rule.

`evidence/` preserves the exact source file, its license, configuration, and
both raw output streams for all three gglint modes. The complete original
corpus report is stored as deterministic gzip; decompression restores the
original bytes. `provenance.json` records its hash, the hosted run, tools,
source pins, and exact chi stage records. The original overall hosted run
failed elsewhere; these files do not recast that run as a pass.

The original strict output contains 102 findings, including six `chanblock`
findings. The prospective repaired output must contain the same findings
except for the single receive at line 95: 101 total and five `chanblock`
findings. `expected-chi-strict-findings.json` freezes that exact expected
array. Basic's three and standard's 52 findings must remain unchanged.

## Authoritative parity and explicit fixture correction

LoGOs commit `bbc506ab936608c24294f1603028905fff6f88a6` already corrected this
behavior. Its commit message identifies default selects as a false positive,
and its unchanged current fixtures omit `ERROR` on `selectDefault` and on the
select communication in `selectCounts`. The first standalone send in
`selectCounts` still has `ERROR`; select sends still count toward the existing
buffer proof.

The `logos/` snapshots preserve both complete authoritative fixtures and the
rule source from LoGOs commit `b61a6cfeb33051791348597fbb69818f4e2ec6f7`, source
tree `df267333b1d5c5727e2477215a6807cae52f8e18`. Their license is
`third_party/logos/LICENSE`. The compiler's `selectCovered` explicitly accepts
`cc.Comm == nil`, while `selectStmt` checks operands and bodies separately.

The repair changes exactly four stale copied expectations, with their full
old bytes preserved under `old/`:

| Existing fixture | Function | Expectation corrected |
| --- | --- | --- |
| Analyzer fixture | `selectDefault` | Remove warning on direct select send |
| Analyzer fixture | `selectCounts` | Remove warning on direct select send |
| Copied LoGOs fixture | `selectDefault` | Remove warning on direct select send |
| Copied LoGOs fixture | `selectCounts` | Remove warning on direct select send |

This is an explicit synchronization with unchanged authoritative fixtures,
not an adjustment of expectations to an analyzer's output. All other existing
expectations remain intact. Timeout bounds and group-specific compiler rules
are outside this slice; this is not a claim of complete rule parity.

## Frozen controls

`FROZEN.sha256` covers the new tests, their inputs, the old implementation and
fixtures, authoritative source snapshots, exact historical evidence, and the
prospective chi findings. Run it from the repository root:

```sh
sha256sum -c docs/validation/chanblock-default/FROZEN.sha256
```

`TestDefaultSelectBounds` has five named children:

| Child | Invariant |
| --- | --- |
| `communications` | Default bounds send, expression receive, assigned and comma-ok receive, nil channels, field channels, and a first-position default |
| `operands` | Send values, send and receive channel expressions, and receive assignment targets remain checked |
| `bodies` | Both case-body kinds, nested unbounded selects, closures, and operations after the select remain checked; a nested default does not bound its outer select |
| `accounting` | Covered sends still contribute to capacity and loop accounting; existing bounded-buffer behavior remains intact |
| `cancellation` | Existing Done coverage, operand/body boundaries, and unbounded selects retain their behavior |

The eight direct communications in `communications` have no expected warning.
The old production overlay must fail that child because it emits eight
unexpected diagnostics. The surrounding-work fixtures retain explicit
expected warnings, so treating an entire default-select subtree as covered
must also fail the positive suite.

## Hosted regression commands

No Go test, analyzer, or compiler was executed locally for this slice. The
frozen test commit precedes the production repair. RED, GREEN, race, CLI
replay, and the full repository gates remain requirements for hosted CI.

Use the CI's Go 1.27.4 and capture JSON, stderr, status, source revision, and
tool hash for each command. Do not treat a build error, skipped test, timeout,
or compiler crash as the expected negative result.

```sh
go test -count=1 -timeout=10m -p=1 -parallel=1 -json ./internal/analyzers/chanblock
go test -count=1 -timeout=10m -p=1 -parallel=1 -json ./internal/rules -run '^TestLogosPorts$/^correctness_unbounded_channel_op$'
go test -race -count=1 -timeout=10m -p=1 -parallel=1 -json ./internal/analyzers/chanblock
```

For the historical negative, write a Go overlay JSON object with one
`Replace` mapping from the absolute path of
`internal/analyzers/chanblock/chanblock.go` to the absolute path of
`docs/validation/chanblock-default/old/chanblock.go.txt`, then run:

```sh
go test -overlay "$RUNNER_TEMP/chanblock-old.json" -count=1 -timeout=10m -p=1 -parallel=1 -json ./internal/analyzers/chanblock -run '^TestDefaultSelectBounds$/^communications$'
```

Require exit 1, a failing package, and exactly two named run/fail identities:
`TestDefaultSelectBounds` and `TestDefaultSelectBounds/communications`.
Require the eight unexpected send/receive diagnostics from the frozen
communications file and no skips, build failures, or crash output. A generic
nonzero exit is insufficient.

The positive analyzer gate must run and pass `TestAnalyzer`,
`TestDefaultSelectBounds`, and all five named children: seven named identities
in total. The focused parity gate must run and pass `TestLogosPorts` and its
`correctness_unbounded_channel_op` child. The unchanged full normal, race,
vet, external lint, and self-lint gates also remain required.

Finally, replay the unchanged pinned chi source in all three modes with the
new immutable CLI, including the fresh-copy repeat. Compare strict findings
with `expected-chi-strict-findings.json`; compare the other modes with their
preserved historical arrays. Keep the new evidence separate from this
historical record.
