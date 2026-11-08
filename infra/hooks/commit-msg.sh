#!/usr/bin/env bash
set -euo pipefail
IFS= read -r subject < "$1" || [[ -n ${subject:-} ]]
pattern='^(feat|fix|chore|docs|test|refactor|ci|build|perf|style|revert|merge)(\([a-zA-Z0-9_./-]+\))?(!)?: .+'
# Git-generated subjects (merge, revert, autosquash) are allowed as-is.
generated='^(Merge |Revert "|(fixup|squash|amend)! )'
if [[ $subject =~ $generated ]]; then
  exit 0
fi
if [[ ! $subject =~ $pattern ]]; then
  printf 'Commit subject must use Conventional Commits, e.g. chore(infra): add local stack\n' >&2
  exit 1
fi
