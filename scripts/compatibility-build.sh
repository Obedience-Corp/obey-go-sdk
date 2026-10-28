#!/usr/bin/env bash
# Archive extraction/build happens only in disposable container scratch.
set -euo pipefail
[ "${OBEY_SDK_TEST_CONTAINER:-}" = 1 ] || { echo 'compatibility artifacts require isolated container' >&2; exit 1; }
kind=${1:?expected sdk or cli}
mkdir -p "$HOME" "$GOCACHE" "$GOMODCACHE" /tmp/source
cat > "/evidence/legacy-$kind-source.tar"
sha256sum "/evidence/legacy-$kind-source.tar"
tar xf "/evidence/legacy-$kind-source.tar" -C /tmp/source
case "$kind" in
 sdk)
  cd /tmp/source
  go run /tmp/source/scripts/moduleproxy /tmp/source /tmp/legacy-proxy
  mkdir -p /tmp/legacy-consumer
  cp /tmp/source/tests/consumer/oneshot/{go.mod,main.go} /tmp/legacy-consumer/
  cd /tmp/legacy-consumer
  export GOPROXY=file:///tmp/legacy-proxy GOSUMDB=off GOTOOLCHAIN=local GOCACHE=/tmp/legacy-consumer-cache GOMODCACHE=/tmp/legacy-consumer-modules
  mkdir -p "$GOCACHE" "$GOMODCACHE"
  go mod download all
  go list -m all
  if [ "$(go list -m all | wc -l)" -ne 2 ] || go list -m -json all | grep -q '"Replace":'; then echo 'legacy consumer graph must contain consumer+SDK only, no replace' >&2; exit 1; fi
  CGO_ENABLED=0 go build -mod=readonly -o /evidence/consumer-legacy .
  sha256sum /evidence/consumer-legacy
  ;;
 cli)
  cd /tmp/source
  # Native Linux static build: this archived executable is only a compatibility
  # client in Alpine, never a replacement daemon or a platform release artifact.
  CGO_ENABLED=1 go build -mod=readonly -tags dev -ldflags '-linkmode=external -extldflags=-static' -o /evidence/obey-v1 ./cmd/obey
  sha256sum /evidence/obey-v1
  ;;
 *) echo 'expected sdk or cli' >&2; exit 2 ;;
esac
go version
