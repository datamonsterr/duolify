#!/usr/bin/env bash
set -euo pipefail
IFS= read -r subject < "$1" || [[ -n ${subject:-} ]]
pattern='^(feat|fix|chore|docs|test|refactor|ci|build|perf|style|revert)(\([a-zA-Z0-9_./-]+\))?(!)?: .+'
if [[ ! $subject =~ $pattern ]]; then
  printf 'Commit subject must use Conventional Commits, e.g. chore(infra): add local stack\n' >&2
  exit 1
fi
