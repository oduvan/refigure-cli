#!/usr/bin/env bash
#
# Points every published package at this repository's release workflow, so npm
# accepts publishes from it over OIDC and no token is needed again.
#
# Run once, after scripts/npm-first-publish.sh has created the packages. npm
# asks for a two-factor code; anything already configured is left alone, so
# this can be run again if a code expires part way through.

set -euo pipefail

repo="oduvan/refigure-cli"
workflow="release.yml"

packages=(
  refigure-cli
  refigure-cli-darwin-arm64
  refigure-cli-darwin-x64
  refigure-cli-linux-arm64
  refigure-cli-linux-x64
  refigure-cli-win32-arm64
  refigure-cli-win32-x64
)

if ! npm whoami >/dev/null 2>&1; then
  echo "Run 'npm login' first." >&2
  exit 1
fi

for package in "${packages[@]}"; do
  if npm trust list "$package" 2>/dev/null | grep -q "$repo"; then
    echo "  $package already trusts $repo — skipping"
    continue
  fi
  echo "  trusting $repo/$workflow for $package"
  npm trust github "$package" --file "$workflow" --repo "$repo" -y
done

cat <<'NEXT'

Done. Every release from now on publishes over OIDC:

  git tag v0.1.7 && git push origin v0.1.7

No token, no secret, and provenance on every package.
NEXT
