#!/usr/bin/env bash
#
# Builds the npm packages for a release into npm/dist/.
#
# One package per platform, each holding a single binary, plus the `refigure-cli`
# package that picks between them. npm installs only the platform package whose
# `os` and `cpu` match, so a user downloads one binary rather than six, and
# nothing is fetched after install — no postinstall script to be blocked by
# --ignore-scripts or an offline cache.
#
# Usage: scripts/npm-packages.sh <version> <directory holding the built binaries>
#   The binaries are named as the release publishes them:
#   refigure_<version>_<goos>_<goarch>[.exe]

set -euo pipefail

version="${1:?usage: npm-packages.sh <version> <binaries dir>}"
binaries="${2:?usage: npm-packages.sh <version> <binaries dir>}"

# npm versions have no leading v.
npm_version="${version#v}"

root="$(cd "$(dirname "$0")/.." && pwd)"
out="$root/npm/dist"
rm -rf "$out"
mkdir -p "$out"

# node's platform-arch, then Go's GOOS and GOARCH, then the package name.
#
# The name usually follows the first column. It does not for win32-x64: npm's
# spam filter refuses `refigure-cli-win32-x64`, consistently, while accepting
# every other name here. See the map in npm/cli/bin/refigure.js.
targets=(
  "darwin-arm64 darwin  arm64 refigure-cli-darwin-arm64"
  "darwin-x64   darwin  amd64 refigure-cli-darwin-x64"
  "linux-arm64  linux   arm64 refigure-cli-linux-arm64"
  "linux-x64    linux   amd64 refigure-cli-linux-x64"
  "win32-arm64  windows arm64 refigure-cli-win32-arm64"
  "win32-x64    windows amd64 refigure-cli-windows-x64"
)

optional=""

for target in "${targets[@]}"; do
  read -r key goos goarch name <<<"$target"
  node_os="${key%%-*}"
  node_cpu="${key##*-}"

  executable="refigure"
  suffix=""
  if [ "$goos" = "windows" ]; then
    executable="refigure.exe"
    suffix=".exe"
  fi

  source="$binaries/refigure_${version}_${goos}_${goarch}${suffix}"
  [ -f "$source" ] || { echo "missing $source" >&2; exit 1; }

  mkdir -p "$out/$name"
  cp "$source" "$out/$name/$executable"
  chmod +x "$out/$name/$executable"

  cat > "$out/$name/package.json" <<JSON
{
  "name": "$name",
  "version": "$npm_version",
  "description": "The refigure binary for $key. Installed automatically by refigure-cli.",
  "homepage": "https://github.com/oduvan/refigure-cli",
  "repository": { "type": "git", "url": "git+https://github.com/oduvan/refigure-cli.git" },
  "license": "MIT",
  "os": ["$node_os"],
  "cpu": ["$node_cpu"],
  "files": ["$executable"],
  "preferUnplugged": true
}
JSON

  optional="$optional    \"$name\": \"$npm_version\",\n"
done

# The wrapper, with every platform package listed as optional.
mkdir -p "$out/refigure-cli"
cp -R "$root/npm/cli/bin" "$out/refigure-cli/bin"
cp "$root/npm/cli/README.md" "$out/refigure-cli/README.md"
cp "$root/LICENSE" "$out/refigure-cli/LICENSE"

python3 - "$root/npm/cli/package.json" "$out/refigure-cli/package.json" "$npm_version" <<'PY'
import json, sys

source, destination, version = sys.argv[1], sys.argv[2], sys.argv[3]
with open(source) as handle:
    package = json.load(handle)

package["version"] = version
package["optionalDependencies"] = {
    name: version
    for name in (
        "refigure-cli-darwin-arm64",
        "refigure-cli-darwin-x64",
        "refigure-cli-linux-arm64",
        "refigure-cli-linux-x64",
        "refigure-cli-win32-arm64",
        # Not -win32-x64: npm's spam filter refuses that name.
        "refigure-cli-windows-x64",
    )
}
package["files"] = ["bin/refigure.js", "README.md", "LICENSE"]

with open(destination, "w") as handle:
    json.dump(package, handle, indent=2)
    handle.write("\n")
PY

echo "built $npm_version into $out:"
ls "$out"
