#!/usr/bin/env bash
# Build only. Never execute filesystem/process fixtures on the host.
set -euo pipefail
cd "$(dirname "$0")/.."
[ "$(go env GOHOSTOS)" = darwin ] || { echo 'Darwin race bundle requires a Darwin build host/SDK' >&2; exit 2; }
destination=${1:?Provide a new artifact directory outside the repository}
[ ! -e "$destination" ] || { echo 'Artifact destination already exists; choose a new directory' >&2; exit 2; }
mkdir -p "$destination/pkg/obey" "$destination/internal/execclient" "$destination/internal/protocol"
GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 go test -c -race -tags=process_tests -o "$destination/internal/execclient/process.test" ./internal/execclient
GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 go test -c -race -tags=process_tests -o "$destination/pkg/obey/process.test" ./pkg/obey
cp -R internal/protocol/testdata "$destination/internal/protocol/"
cp scripts/native-macos-run.sh "$destination/run.sh"
{
 printf 'Build-only bundle; native execution NOT established\n'
 git rev-parse HEAD
 git status --porcelain
 go version
 shasum -a 256 "$destination/internal/execclient/process.test" "$destination/pkg/obey/process.test"
} > "$destination/BUILD.txt"
printf 'Prepared build-only artifact: %s\n' "$destination"
