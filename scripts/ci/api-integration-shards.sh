#!/usr/bin/env bash
set -euo pipefail

fail() {
  echo "::error::$*" >&2
  exit 1
}

# Read the actual task default and workflow assignments so either can drift only
# if this guard fails. Keep API_TEST_PACKAGES as a single-line YAML scalar.
default_list=$(sed -n "s/.*env.API_TEST_PACKAGES | default(value='\([^']*\)').*/\1/p" .mise/tasks/api.toml)
read -r -a default_packages <<< "$default_list"
mapfile -t shard_packages < <(awk '
  /^  [[:alnum:]_-]+:/ { shard = ($1 ~ /^api-integration-[abc]:$/) }
  shard && $1 == "API_TEST_PACKAGES:" {
    for (i = 2; i <= NF; i++) print $i
  }
' .github/workflows/_checks.yml)
(( ${#default_packages[@]} > 0 )) || fail "API integration default list is empty or unreadable"
(( ${#shard_packages[@]} > 0 )) || fail "API integration shard lists are empty or unreadable"

declare -A defaults=() shards=()
for package in "${default_packages[@]}"; do
  [[ -z ${defaults[$package]:-} ]] || fail "Duplicate default package: $package"
  defaults[$package]=1
done
for package in "${shard_packages[@]}"; do
  [[ $package =~ ^\./internal/([[:alnum:]_]+/)+\.\.\.$ ]] || fail "Invalid shard package: $package"
  [[ -n ${defaults[$package]:-} ]] || fail "Shard package absent from default list: $package"
  [[ -z ${shards[$package]:-} ]] || fail "Package occurs in multiple shards: $package"
  shards[$package]=1
done
for package in "${default_packages[@]}"; do
  [[ -n ${shards[$package]:-} ]] || fail "Default package absent from shards: $package"
done

# Match the integration build tag even in compound expressions, and account for
# recursive package patterns when a nested directory gains integration tests.
mapfile -t integration_files < <(grep -rlE \
  '^//go:build[[:space:]]+(.*[^[:alnum:]_])?integration([^[:alnum:]_]|$)' \
  --include='*_test.go' services/api/internal)
(( ${#integration_files[@]} > 0 )) || fail "No API integration tests found"
declare -A directories=()
for file in "${integration_files[@]}"; do
  directory=${file%/*}
  directories[./${directory#services/api/}]=1
done
for directory in "${!directories[@]}"; do
  coverage=0
  for package in "${shard_packages[@]}"; do
    if [[ "$directory/" == "${package%...}"* ]]; then
      (( coverage += 1 ))
    fi
  done
  [[ $coverage == 1 ]] || fail "Integration test directory $directory is covered by $coverage shards (expected 1)"
done

echo "API integration shards cover all ${#default_packages[@]} default packages exactly once and all ${#directories[@]} integration test directories."
