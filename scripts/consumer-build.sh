#!/usr/bin/env bash
set -euo pipefail
[ "${OBEY_SDK_TEST_CONTAINER:-}" = 1 ] || { echo 'consumer archive/build requires isolated container' >&2; exit 1; }
mkdir -p "$HOME" "$GOCACHE" "$GOMODCACHE" /tmp/consumer
# Only SDK source is mounted in this build stage; no Obey checkout is present.
go run /sdk/scripts/moduleproxy /sdk /evidence/proxy
cp "/sdk/tests/consumer/${SDK_CONSUMER_SUITE:-oneshot}/go.mod" /sdk/tests/consumer/"${SDK_CONSUMER_SUITE:-oneshot}"/*.go /tmp/consumer/
cd /tmp/consumer
export GOPROXY=file:///evidence/proxy GOSUMDB=off GOTOOLCHAIN=local
# These caches are empty and independent from the archive tool's compilation.
export GOCACHE=/tmp/consumer-cache GOMODCACHE=/tmp/consumer-modules
mkdir -p "$GOCACHE" "$GOMODCACHE"
go mod download all
go list -m all | tee /evidence/modules.txt
if go list -m -json all | grep -q '"Replace":'; then echo 'consumer replacement directive is forbidden' >&2; exit 1; fi
if [ "$(go list -m all | wc -l)" -ne 2 ]; then echo 'unexpected consumer dependency closure' >&2; exit 1; fi
while IFS= read -r module; do case "$module" in *protos*|*Obedience-Corp*|*obey/internal*) echo 'private dependency in consumer' >&2; exit 1 ;; esac; done < /evidence/modules.txt
go test -mod=readonly ./...
CGO_ENABLED=0 go build -mod=readonly -o /evidence/consumer-go124 .
go build -mod=readonly github.com/obediencecorp/obey-go-sdk/examples/...
go version | tee /evidence/minimum-toolchain.txt
