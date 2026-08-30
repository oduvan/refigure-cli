#!/usr/bin/env bash
#
# The one-time bootstrap that puts these packages on npm for the first time.
#
# Every release after this one publishes from CI with no token and no secret,
# using npm trusted publishing. But a trusted publisher is configured on a
# package that already exists, and npm has no pending-publisher equivalent — so
# the packages have to exist before that can be set up. This is that step, and
# it is meant to be run once, by a person, from a machine that has run
# `npm login`.
#
# Usage: scripts/npm-first-publish.sh <version>
#   e.g. scripts/npm-first-publish.sh v0.1.6

set -euo pipefail

version="${1:?usage: npm-first-publish.sh <version>}"
root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

if ! npm whoami >/dev/null 2>&1; then
  echo "Run 'npm login' first — this publishes as you, once." >&2
  exit 1
fi
echo "Publishing $version as $(npm whoami)."

echo "Fetching the released binaries..."
base="https://github.com/oduvan/refigure-cli/releases/download/$version"
for target in darwin_arm64 darwin_amd64 linux_arm64 linux_amd64; do
  curl -fsSLo "$work/refigure_${version}_${target}" "$base/refigure_${version}_${target}"
done
for target in windows_arm64 windows_amd64; do
  curl -fsSLo "$work/refigure_${version}_${target}.exe" "$base/refigure_${version}_${target}.exe"
done

"$root/scripts/npm-packages.sh" "$version" "$work"

# Platform packages first: the wrapper depends on them, and a wrapper whose
# binaries are not on the registry yet installs into a broken state.
for package in "$root"/npm/dist/refigure-cli-*; do
  echo "publishing $(basename "$package")"
  npm publish "$package" --access public
done
npm publish "$root/npm/dist/refigure-cli" --access public

cat <<'NEXT'

Published. One thing left, once per package, on npmjs.com:

  Packages → <package> → Settings → Trusted Publisher → GitHub Actions
    Organization or user:  oduvan
    Repository:            refigure-cli
    Workflow filename:     release.yml
    Allowed actions:       npm publish

Do that for refigure-cli and for each refigure-cli-<platform> package. After
that every release publishes itself, with no token stored anywhere.
NEXT
