#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
[ -z "$(git status --porcelain)" ] || { echo 'Release check requires a clean owned SDK commit' >&2; exit 1; }
printf 'SDK candidate revision: '; git rev-parse HEAD
[ "$(go list -m)" = github.com/Obedience-Corp/obey-go-sdk ] || { echo 'Unexpected module identity; reconcile approved owner decision' >&2; exit 1; }
[ "$(go list -m all | wc -l | tr -d ' ')" = 1 ] || { echo 'Unexpected runtime module dependency' >&2; exit 1; }
if go list -m -json all | rg -q '"Replace":'; then echo 'Source replacement is forbidden' >&2; exit 1; fi
[ -z "$(gofmt -l pkg internal examples scripts/moduleproxy)" ] || { echo 'Go formatting drift' >&2; exit 1; }
go vet ./...
go build ./...
go list -m all
go list -deps ./pkg/obey
docker info >/dev/null
# Independent empty cache and offline local archive, not a published install.
docker run --init --rm --read-only --network=none \
 --tmpfs /tmp:rw,exec,nosuid,nodev,size=2g --tmpfs /evidence:rw,exec,nosuid,nodev,size=1g \
 -e OBEY_SDK_TEST_CONTAINER=1 -e SDK_CONSUMER_SUITE=service \
 -e HOME=/tmp/home -e GOCACHE=/tmp/archive-cache -e GOMODCACHE=/tmp/archive-modules \
 -v "$PWD:/sdk:ro" -w /sdk golang:1.24-bookworm bash scripts/consumer-build.sh
printf 'Clean source + isolated offline import/build checks passed; local archive only\n'
missing=0
if [ ! -s LICENSE ] || ! rg -q '^License: Apache-2.0$' docs/distribution-decision.md; then
 echo 'PENDING: owner-approved Apache-2.0 license' >&2; missing=1
fi
if ! rg -q '^Source distribution: open source$' docs/distribution-decision.md || ! rg -q '^Repository decision: approved$' docs/distribution-decision.md; then
 echo 'PENDING: canonical repository/module ownership decision' >&2; missing=1
fi
if [ ! -s docs/native-macos-evidence.md ]; then echo 'PENDING: actual isolated native macOS lifecycle evidence' >&2; missing=1; fi
[ "$missing" = 0 ] || { echo 'Versioned release readiness incomplete; do not tag a release or mark platform acceptance complete' >&2; exit 1; }
printf 'Review recorded decisions/evidence before any separately authorized publication\n'
