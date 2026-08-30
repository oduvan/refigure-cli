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
  refigure-cli-windows-x64
)

if ! npm whoami >/dev/null 2>&1; then
  echo "Run 'npm login' first." >&2
  exit 1
fi

# `npm trust` exists before 11.15.0 but sends a request the registry rejects
# with a bare "400 Bad Request", which says nothing about the cause.
required="11.15.0"
installed="$(npm --version)"
if [ "$(printf '%s\n%s\n' "$required" "$installed" | sort -V | head -1)" != "$required" ]; then
  echo "npm $installed is too old for 'npm trust' — it needs $required or newer." >&2
  echo "Run: npm install -g npm@latest" >&2
  exit 1
fi

for package in "${packages[@]}"; do
  if npm trust list "$package" 2>/dev/null | grep -q "$repo"; then
    echo "  $package already trusts $repo — skipping"
    continue
  fi
  echo "  trusting $repo/$workflow for $package"
  # --allow-publish is required: without a permission flag npm refuses, and
  # `npm publish` is the only thing the release workflow does.
  npm trust github "$package" --file "$workflow" --repo "$repo" --allow-publish -y
done

cat <<'NEXT'

Done. Every release from now on publishes over OIDC:

  git tag v0.1.7 && git push origin v0.1.7

No token, no secret, and provenance on every package.
NEXT
