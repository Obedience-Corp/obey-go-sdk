#!/usr/bin/env bash
set -euo pipefail
[ "${OBEY_MACHINE_TEST_CONTAINER:-}" = 1 ] || { echo 'consumer daemon driver requires isolated container' >&2; exit 1; }
mkdir -p "$HOME" "$TMPDIR" "$GOCACHE" "$GOMODCACHE" /tmp/current-consumer
cp /sdk/tests/consumer/oneshot/go.mod /sdk/tests/consumer/oneshot/main.go /tmp/current-consumer/
(
 cd /tmp/current-consumer
 export GOPROXY=file:///evidence/proxy GOSUMDB=off GOTOOLCHAIN=local GOCACHE=/tmp/current-cache GOMODCACHE=/tmp/current-modules
 mkdir -p "$GOCACHE" "$GOMODCACHE"
 go mod download all
 go list -m all
 if go list -m -json all | grep -q '"Replace":'; then echo 'consumer replacement directive is forbidden' >&2; exit 1; fi
 CGO_ENABLED=0 go build -mod=readonly -o /tmp/consumer-current .
 go build -mod=readonly github.com/obediencecorp/obey-go-sdk/examples/...
 go version
)
go test -v -race -mod=readonly -count=1 -timeout=10m -tags=integration,sdk_consumer_tests -run '^TestMachineSDKConsumer' ./tests/integration/daemon
