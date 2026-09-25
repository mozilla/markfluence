#!/bin/sh
# Check a published release (docs/releasing.md, step 6): download the
# darwin/arm64 archive, verify it against checksums.txt, and check that the
# binary in it reports the tag's version.
#
# Usage: scripts/check-release.sh vX.Y.Z
set -eu

if [ $# -ne 1 ]; then
    echo "usage: $0 vX.Y.Z" >&2
    exit 2
fi
tag=$1
case $tag in
    v[0-9]*.[0-9]*.[0-9]*) ;;
    *) echo "$0: $tag is not a vX.Y.Z tag" >&2; exit 2 ;;
esac

dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
cd "$dir"

gh release download "$tag" -R mozilla/markfluence \
    -p 'markfluence_*_darwin_arm64.tar.gz' -p checksums.txt
shasum -a 256 --check --ignore-missing checksums.txt
tar -xzf markfluence_*_darwin_arm64.tar.gz markfluence

# ./markfluence, not markfluence: the one on PATH is usually a dev build.
out=$(./markfluence --version)
echo "$out"
case $out in
    "markfluence ${tag#v} "*) echo "OK: $tag" ;;
    *) echo "$0: expected markfluence ${tag#v}, got: $out" >&2; exit 1 ;;
esac
