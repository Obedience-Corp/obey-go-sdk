#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
mode=${1:?expected process, consumer or durable}
case "$mode" in
 process) pattern='^TestProcess'; tags=process_tests; packages=(./internal/execclient/... ./pkg/obey/...) ;;
 consumer|durable) echo "$mode verification is not implemented yet; complete its owning acceptance task" >&2; exit 1 ;;
 *) echo 'expected process, consumer or durable' >&2; exit 2 ;;
esac
if ! rg -l '^func TestProcess' internal/execclient pkg/obey >/dev/null; then
 echo 'process acceptance tests are not implemented yet; complete the bounded runner task' >&2; exit 1
fi
command -v docker >/dev/null || { echo 'Docker is required; start Docker/Colima then retry just test process' >&2; exit 1; }
docker info >/dev/null 2>&1 || { echo 'Docker daemon unavailable; start Docker/Colima then retry' >&2; exit 1; }
docker run --init --rm --read-only --network=none \
 --tmpfs /tmp:rw,exec,nosuid,nodev,size=2g \
 -e HOME=/tmp/sdk-home -e TMPDIR=/tmp/scratch -e GOCACHE=/tmp/gocache -e GOMODCACHE=/tmp/gomodcache \
 -e OBEY_SDK_TEST_CONTAINER=1 -v "$PWD:/workspace:ro" -w /workspace \
 golang:1.24-bookworm bash -c 'set -e; mkdir -p "$HOME" "$TMPDIR" "$GOCACHE" "$GOMODCACHE"; exec go test -v -race -count=1 -timeout=5m -tags="$1" -run "$2" "${@:3}"' sdk "$tags" "$pattern" "${packages[@]}"
