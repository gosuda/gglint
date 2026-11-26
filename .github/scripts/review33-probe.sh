#!/usr/bin/env bash
set -euo pipefail
export GOENV=off GOCACHEPROG= GOTOOLCHAIN=local GOMAXPROCS=4
base=acd1afa65384242aecd1456873b9ad3a32119084
candidate=$CANDIDATE_SHA
evidence="$RUNNER_TEMP/gglint-review33"
mkdir -p "$evidence"
printf '%s\n' "$candidate" > "$evidence/candidate.txt"
mapfile -t candidate_paths < <(git diff --name-only "$base" "$candidate")
restore_candidate() {
  rm -f internal/driver/review33_boundary_test.go
  git restore --source "$candidate" --worktree -- "${candidate_paths[@]}"
}
trap restore_candidate EXIT
go version | tee "$evidence/go-version.txt"
go mod download
git restore --source "$base" --worktree -- internal/driver internal/cli/run.go internal/vettool/engine.go internal/vettool/vettool.go
cat > internal/driver/review33_boundary_test.go <<'GO'
package driver_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gosuda/gglint/internal/driver"
)

func TestReview33RollbackBoundary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	before := []byte("package p\nvar Value = 1\n")
	if err := os.WriteFile(path, before, 0o600); err != nil { t.Fatal(err) }
	pre, err := driver.SnapshotGoFiles(dir)
	if err != nil { t.Fatal(err) }
	outside := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outside, []byte("outside unchanged\n"), 0o600); err != nil { t.Fatal(err) }
	if err := os.Remove(path); err != nil { t.Fatal(err) }
	if err := os.Symlink(outside, path); err != nil { t.Fatal(err) }
	violations := driver.VerifyFixed(dir, pre, map[string]bool{}, true)
	if len(violations) != 1 { t.Fatalf("want one refusal, got %v", violations) }
	if err := violations[0].Undo(dir); err != nil { t.Fatal(err) }
	got, err := os.ReadFile(outside)
	if err != nil { t.Fatal(err) }
	if string(got) != "outside unchanged\n" { t.Fatalf("outside target overwritten: %q", got) }
}
GO
cli_cases=(
  TestFixRefusesGeneratedFileWithMessage
  TestFixKeepsAtomicGroupWhenTestsAreExcluded
  TestFixRefusesInvalidSiblingRewriteAndStillReports
  TestFixConflictLeavesOtherPackageUnchanged
  TestTargetedFixIgnoresUnreadableUnselectedDirectory
  TestFixCLIAccountsForUnrecordedWrite
)
pattern=$(IFS='|'; printf '%s' "${cli_cases[*]}")
if go test -json -count=1 -p 1 -parallel 2 -timeout 20m ./internal/cli -run "^($pattern)$" >"$evidence/baseline-cli.jsonl" 2>&1; then
  echo "Baseline unexpectedly passed the CLI counterexamples" >&2
  exit 1
fi
for name in "${cli_cases[@]}"; do
  jq -se --arg name "$name" 'any(.[]; .Action == "fail" and .Test == $name)' "$evidence/baseline-cli.jsonl" >/dev/null
done
if go test -json -count=1 -p 1 ./internal/vettool -run '^TestFixRefusesEveryEditInAMultiFileGroup$' >"$evidence/baseline-targets.jsonl" 2>&1; then
  echo "Baseline unexpectedly admitted no forbidden fix targets" >&2
  exit 1
fi
for name in TestFixRefusesEveryEditInAMultiFileGroup/tests_off TestFixRefusesEveryEditInAMultiFileGroup/generated; do
  jq -se --arg name "$name" 'any(.[]; .Action == "fail" and .Test == $name)' "$evidence/baseline-targets.jsonl" >/dev/null
done
if go test -json -count=1 -p 1 ./internal/driver -run '^TestReview33RollbackBoundary$' >"$evidence/baseline-boundary.jsonl" 2>&1; then
  echo "Baseline unexpectedly preserved the outside symlink target" >&2
  exit 1
fi
jq -se 'any(.[]; .Action == "fail" and .Test == "TestReview33RollbackBoundary")' "$evidence/baseline-boundary.jsonl" >/dev/null
jq -se 'any(.[]; (.Output // "") | contains("outside target overwritten"))' "$evidence/baseline-boundary.jsonl" >/dev/null
restore_candidate
git diff --exit-code "$candidate" -- "${candidate_paths[@]}"
sha256sum "${candidate_paths[@]}" > "$evidence/source.sha256"
go test -race -count=1 -p 1 -parallel 2 -timeout 20m ./internal/driver ./internal/cli | tee "$evidence/fixed-driver-cli.log"
go test -race -count=1 -p 1 -parallel 1 -timeout 20m ./internal/vettool | tee "$evidence/fixed-vettool.log"
git diff --exit-code "$candidate" -- "${candidate_paths[@]}"
sha256sum -c "$evidence/source.sha256"
