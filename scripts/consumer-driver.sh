#!/usr/bin/env bash
set -euo pipefail
[ "${OBEY_MACHINE_TEST_CONTAINER:-}" = 1 ] || { echo 'consumer daemon driver requires isolated container' >&2; exit 1; }
mkdir -p "$HOME" "$TMPDIR" "$GOCACHE" "$GOMODCACHE" /tmp/current-consumer
cp "/sdk/tests/consumer/${SDK_CONSUMER_SUITE:-oneshot}/go.mod" /sdk/tests/consumer/"${SDK_CONSUMER_SUITE:-oneshot}"/*.go /tmp/current-consumer/
(
 cd /tmp/current-consumer
 export GOPROXY=file:///evidence/proxy GOSUMDB=off GOTOOLCHAIN=local GOCACHE=/tmp/current-cache GOMODCACHE=/tmp/current-modules
 mkdir -p "$GOCACHE" "$GOMODCACHE"
 go mod download all
 go list -m all
 if go list -m -json all | grep -q '"Replace":'; then echo 'consumer replacement directive is forbidden' >&2; exit 1; fi
 go test -mod=readonly ./...
 CGO_ENABLED=0 go build -mod=readonly -o /tmp/consumer-current .
 go build -mod=readonly github.com/Obedience-Corp/obey-go-sdk/examples/...
 go version
)
if [ "${SDK_COMPATIBILITY_ARTIFACTS:-false}" = true ]; then
 mkdir -p /tmp/persistent-consumer
 cp /sdk/tests/consumer/persistent/{go.mod,main.go} /tmp/persistent-consumer/
 (
  cd /tmp/persistent-consumer
  export GOPROXY=file:///evidence/proxy GOSUMDB=off GOTOOLCHAIN=local GOCACHE=/tmp/current-cache GOMODCACHE=/tmp/current-modules
  go mod download all
  CGO_ENABLED=0 go build -mod=readonly -o /tmp/consumer-persistent-current .
 )
fi
pattern='^TestMachineSDKConsumer'
tags=integration,sdk_consumer_tests
if [ "${SDK_COMPATIBILITY_ARTIFACTS:-false}" = true ]; then tags="$tags,sdk_compatibility_tests"; fi
if [ "${SDK_CONSUMER_SUITE:-oneshot}" = persistent ]; then pattern='^TestMachineSDKHandles'; tags="$tags,machine_contract_tests"; fi
if [ "${SDK_CONSUMER_SUITE:-oneshot}" = service ]; then pattern='^TestMachineSDKService'; tags="$tags,machine_contract_tests"; fi
if [ -n "${OBEY_SDK_CONSUMER_PATTERN:-}" ]; then pattern=$OBEY_SDK_CONSUMER_PATTERN; fi
printf 'Consumer selector: %s\n' "$pattern"
go test -v -race -mod=readonly -count=1 -timeout=20m -tags="$tags" -run "$pattern" ./tests/integration/daemon
