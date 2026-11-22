#!/usr/bin/env bash
# Fails when non-test Go or .sql query sources under services/api compare a casted
# column to a parameter (`col::text = $1`, `col::text = ANY($1)`, `col::text IN (...)`).
# The cast on the column defeats the primary-key and foreign-key indexes, so the
# statement becomes a sequential scan as the table grows. Cast the parameter instead:
# `col = $1::uuid`, `col = ANY($1::uuid[])`, and validate the id at the handler.
# Genuinely text columns are listed in scripts/ci/sql-casts.allow.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

allow=scripts/ci/sql-casts.allow
pattern='::text[[:space:]]*(=|<>|!=)[[:space:]]*(\$[0-9]|sqlc\.n?arg\(|ANY[[:space:]]*\()|::text[[:space:]]+(NOT[[:space:]]+)?IN[[:space:]]*\(|\$[0-9]+[[:space:]]*(=|<>|!=)[[:space:]]*[A-Za-z_.0-9]+::text'

# Generated sqlc code repeats the SQL of services/api/queries, which is checked itself.
mapfile -t hits < <(grep -rnE "$pattern" \
  --include='*.go' --include='*.sql' \
  --exclude='*_test.go' --exclude-dir=gen --exclude-dir=migrations --exclude-dir=db \
  services/api || true)

failed=0
for hit in "${hits[@]}"; do
  [[ -n $hit ]] || continue
  file=${hit%%:*}
  text=${hit#*:*:}
  allowed=0
  if [[ -f $allow ]]; then
    while IFS=$'\t' read -r allow_file allow_text _; do
      [[ -z $allow_file || $allow_file == \#* ]] && continue
      if [[ $file == "$allow_file" && $text == *"$allow_text"* ]]; then
        allowed=1
        break
      fi
    done < "$allow"
  fi
  if ((allowed == 0)); then
    echo "::error file=$file,line=$(cut -d: -f2 <<< "$hit")::cast column compared to a parameter; use col = \$n::uuid (or add a reasoned line to $allow)" >&2
    echo "$hit" >&2
    failed=1
  fi
done
exit "$failed"
