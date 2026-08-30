#!/usr/bin/env bash
#
# Points every version in the README at a release.
#
# The README shows commands people copy, so they carry a real version rather
# than a placeholder — and a real version goes stale the moment the next release
# lands. This runs on release so it never does.
#
# Only README.md is touched. CLAUDE.md mentions old versions on purpose, as
# history, and rewriting those would turn a record into a lie.
#
# Usage: scripts/pin-docs.sh <version>

set -euo pipefail

version="${1:?usage: pin-docs.sh <version>}"
root="$(cd "$(dirname "$0")/.." && pwd)"

python3 - "$root/README.md" "$version" <<'PY'
import re
import sys

path, version = sys.argv[1], sys.argv[2]
with open(path) as handle:
    text = handle.read()

# The pinned version in the CI example.
text = re.sub(r"REFIGURE_VERSION: v\d+\.\d+\.\d+", f"REFIGURE_VERSION: {version}", text)

# Download URLs, which carry the version twice: in the path and in the asset.
text = re.sub(
    r"releases/download/v\d+\.\d+\.\d+/refigure_v\d+\.\d+\.\d+_",
    f"releases/download/{version}/refigure_{version}_",
    text,
)

with open(path, "w") as handle:
    handle.write(text)
PY

echo "README now points at $version"
