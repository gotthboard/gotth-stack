#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then echo "usage: $0 <gotth-extension-godaddy-dns-repository>" >&2; exit 2; fi
source_repo=$1
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
scratch=$(mktemp -d "${TMPDIR:-/tmp}/gotth-provider-conformance.XXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
git clone -q --no-hardlinks "$source_repo" "$scratch/provider"
git -C "$scratch/provider" checkout -q b313b413dc9ea7d495fa45b21b26c6e3dc94b064
test -z "$(git -C "$scratch/provider" status --porcelain=v1 --untracked-files=all)"
GOTTH_EXTENSION_REQUIRE_CLEAN=1 "$scratch/provider/scripts/build-artifact.sh" 1.0.0-alpha.1 "$scratch/artifact" >/dev/null
artifact="$scratch/artifact/gotth-extension-godaddy-dns-1.0.0-alpha.1-linux-amd64.tar.gz"
test "$(sha256sum "$artifact" | cut -d' ' -f1)" = 03b532d557a9b2894e71ccb224f2d5cd18fcf07f15b94575e1ac48212c9129bd
cd "$root"
GOTTH_GODADDY_ARTIFACT="$artifact" go test -count=1 -run '^TestCompiledProviderConformance$' ./internal/extensions/godaddydns
