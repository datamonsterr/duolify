#!/usr/bin/env bash
# Free ~25 GB on GitHub-hosted Ubuntu runners (about 14 GB free by default) for
# Rust, Tauri and Docker jobs. No-op outside GitHub Actions.
set -euo pipefail
if [[ ${GITHUB_ACTIONS:-} != true ]]; then
  echo "Not running in GitHub Actions; nothing to free."
  exit 0
fi
before=$(df -BG --output=avail / | tail -n 1 | tr -d ' ')
sudo rm -rf /usr/share/dotnet &
sudo rm -rf /usr/local/lib/android &
sudo rm -rf /opt/ghc /usr/local/.ghcup &
sudo rm -rf /opt/hostedtoolcache/CodeQL &
wait
after=$(df -BG --output=avail / | tail -n 1 | tr -d ' ')
echo "Free disk on /: $before -> $after"
