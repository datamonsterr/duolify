#!/usr/bin/env bash
set -eu

# Tauri 2 prerequisites: https://tauri.app/start/prerequisites/
packages=(libwebkit2gtk-4.1-dev libjavascriptcoregtk-4.1-dev libsoup-3.0-dev
  build-essential curl wget file libxdo-dev libssl-dev
  libayatana-appindicator3-dev librsvg2-dev)
archives="$HOME/.cache/tauri-apt-archives"
lists="$HOME/.cache/tauri-apt-lists"
apt_config=/etc/apt/apt.conf.d/99duolify-tauri-timeouts
mirror=/etc/apt/apt-mirrors.txt
mirror_backup=$(mktemp)
mkdir -p "$archives/partial"
if [ -f "$mirror" ]; then sudo cp "$mirror" "$mirror_backup"; fi
# shellcheck disable=SC2329 # Invoked by the EXIT trap.
cleanup() {
  if [ -s "$mirror_backup" ]; then sudo cp "$mirror_backup" "$mirror"; fi
  rm -f "$mirror_backup"
  sudo rm -f "$apt_config"
  sudo chmod -R a+rX "$archives" "$lists" 2>/dev/null || true
}
trap 'cleanup' EXIT
printf '%s\n' \
  'Acquire::Retries "3";' \
  'Acquire::http::Timeout "30";' \
  'Acquire::https::Timeout "30";' \
  'APT::Sandbox::User "root";' \
  "Dir::Cache::archives \"$archives\";" | sudo tee "$apt_config" >/dev/null

if [ -d "$lists" ]; then
  sudo cp -a "$lists/." /var/lib/apt/lists/
  echo 'Trying cached Tauri apt packages offline.'
  if sudo timeout --kill-after=10s 90s apt-get install -y --no-install-recommends --no-download "${packages[@]}"; then
    echo 'Tauri apt dependencies installed offline from cache.'
    exit 0
  fi
  echo 'Cached Tauri apt packages were incomplete; falling back to the network.'
fi

for attempt in 1 2 3; do
  # Root owns the timeout process group, so timed-out apt children are killed too.
  if sudo timeout --kill-after=10s 120s bash -c 'apt-get update -qq --error-on=any && apt-get install -y --no-install-recommends "$@"' _ "${packages[@]}"; then
    mkdir -p "$lists"
    sudo cp -a /var/lib/apt/lists/. "$lists/"
    exit 0
  else
    status=$?
  fi
  if [ "$attempt" -lt 3 ]; then
    echo "Tauri dependency installation failed (attempt $attempt/3, exit $status); retrying in 10 seconds" >&2
    if [ -s "$mirror_backup" ]; then
      printf '%s\n' 'http://archive.ubuntu.com/ubuntu/' | sudo tee "$mirror" >/dev/null
    fi
    sleep 10
  fi
done
echo 'Tauri dependency installation failed after 3 attempts' >&2
exit "$status"
