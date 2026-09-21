#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then echo "usage: $0 <gotth-extension-godaddy-dns-repository>" >&2; exit 2; fi
source_repo=$1
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
scratch=$(mktemp -d "${TMPDIR:-/tmp}/gotth-provider-conformance.XXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
git clone -q --no-hardlinks "$source_repo" "$scratch/provider"
git -C "$scratch/provider" checkout -q c1f3525753da982a3ba84a5f443ae8e35dadebc8
test -z "$(git -C "$scratch/provider" status --porcelain=v1 --untracked-files=all)"
GOTTH_EXTENSION_REQUIRE_CLEAN=1 "$scratch/provider/scripts/build-artifact.sh" 1.0.0-alpha.2 "$scratch/artifact" >/dev/null
artifact="$scratch/artifact/gotth-extension-godaddy-dns-1.0.0-alpha.2-linux-amd64.tar.gz"
test "$(sha256sum "$artifact" | cut -d' ' -f1)" = 88ce5fb3c2bd2dc1f66707a8657886055632a31e0a4e7e1cec8e459e0742435f
cd "$root"
GOTTH_GODADDY_ARTIFACT="$artifact" go test -count=1 -run '^TestCompiledProviderConformance$' ./internal/extensions/godaddydns
