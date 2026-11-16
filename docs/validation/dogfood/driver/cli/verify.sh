#!/bin/sh
set -eu
smoke_dir=${1:?fixture directory required}
for action in run fix; do
  read -r smoke_status < "$smoke_dir/outputs/original-$action.exit"
  test "$smoke_status" -eq 1
  test -s "$smoke_dir/outputs/original-$action.stdout"
  test ! -s "$smoke_dir/outputs/original-$action.stderr"
  read -r smoke_status < "$smoke_dir/outputs/updated-$action.exit"
  test "$smoke_status" -eq 3
  test ! -s "$smoke_dir/outputs/updated-$action.stdout"
  read -r smoke_status < "$smoke_dir/outputs/updated-source-$action.exit"
  test "$smoke_status" -eq 1
  test ! -s "$smoke_dir/outputs/updated-source-$action.stderr"
  cmp "$smoke_dir/outputs/original-run.stdout" "$smoke_dir/outputs/updated-source-$action.stdout"
done
printf 'gglint: load: ' > "$smoke_dir/expected-error.txt"
cat "$smoke_dir/mixed.stderr" >> "$smoke_dir/expected-error.txt"
cmp "$smoke_dir/expected-error.txt" "$smoke_dir/outputs/updated-run.stderr"
cmp "$smoke_dir/expected-error.txt" "$smoke_dir/outputs/updated-fix.stderr"
printf 'PASS: mixed run/fix exit 3 with empty stdout and exact stderr; source-only reports preserved.\n'
