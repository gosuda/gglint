# Tiny CLI replay

This fixture substitutes only the `go` executable invoked by gglint. It prints
the selected JSON/stderr inputs, records its argv, and exits 1. It does not
compile or modify the module under `work`.

The retained runs used working directory
`/workspace/scratch/93e0563596d1/driver-stderr-cli/work` and these binary paths:

- Original: `/workspace/scratch/93e0563596d1/bin/gglint-candidate`
- Updated: `/workspace/scratch/93e0563596d1/bin/gglint-driver-candidate`

Each invocation used the same environment, with its label substituted in the
argv/output paths:

```sh
PATH=/workspace/scratch/93e0563596d1/driver-stderr-cli/bin:/usr/bin:/bin
XDG_CACHE_HOME=/workspace/scratch/93e0563596d1/driver-stderr-cli/cache
GGLINT_DRIVER_SMOKE_ARGS=/workspace/scratch/93e0563596d1/driver-stderr-cli/outputs/LABEL.args
GGLINT_DRIVER_SMOKE_STDOUT=/workspace/scratch/93e0563596d1/driver-stderr-cli/stdout.json
GGLINT_DRIVER_SMOKE_STDERR=/workspace/scratch/93e0563596d1/driver-stderr-cli/mixed.stderr
```

The argument vectors were `run -mode basic -format json ./...` and
`run -fix -mode basic -format json ./...`. Source-only controls used
`source.stderr` instead of `mixed.stderr`. Stdout, stderr, and the actual exit
status are recorded under `outputs/<label>.*`.

For a new replay, copy this fixture to a writable directory, point those
environment variables at the copy, select the two binaries by their recorded
hashes, and retain each subprocess's exit status even when it is nonzero.
Run `sh verify.sh <fixture-directory>` after generating all six output sets.
The verification script checks raw context and report preservation; a returned
report is not accepted for the mixed-failure inputs.
