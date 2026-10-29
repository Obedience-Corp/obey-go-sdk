#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
SDK_RELEASE_TAG=${1:?usage: test-published.sh <immutable Go module version>}
export SDK_RELEASE_TAG
SDK_LICENSE_SHA=$(shasum -a 256 LICENSE | awk '{print $1}')
export SDK_LICENSE_SHA
# No SDK source, credentials, CLI or private checkout enters this consumer.
docker run --init --rm --read-only -i \
 --tmpfs /tmp:rw,exec,nosuid,nodev,size=2g \
 -e SDK_RELEASE_TAG -e SDK_LICENSE_SHA -e HOME=/tmp/home \
 -e GOPATH=/tmp/go -e GOCACHE=/tmp/cache -e GOMODCACHE=/tmp/modules \
 -e GOPROXY=https://proxy.golang.org,direct -e GOSUMDB=sum.golang.org \
 -e GOTOOLCHAIN=local golang:1.24-bookworm bash -s <<'CONSUMER'
set -euo pipefail
mkdir -p "$HOME" /tmp/consumer
cd /tmp/consumer
go mod init example.com/sdk-release-consumer
go get "github.com/Obedience-Corp/obey-go-sdk@$SDK_RELEASE_TAG"
module=github.com/Obedience-Corp/obey-go-sdk
test "$(go list -m -f '{{.Version}}' "$module")" = "$SDK_RELEASE_TAG"
test -n "$(go list -m -f '{{.Sum}}' "$module")"
go list -m all | tee /tmp/modules.txt
test "$(wc -l < /tmp/modules.txt)" -eq 2
if go list -m -json all | grep -q '"Replace":'; then
 echo 'Published consumer must not use source replacements.' >&2; exit 1
fi
sdk=$(go list -m -f '{{.Dir}}' "$module")
test "$(sha256sum "$sdk/LICENSE" | awk '{print $1}')" = "$SDK_LICENSE_SHA"
cat > main.go <<'GO'
package main

import (
 "fmt"
 "github.com/Obedience-Corp/obey-go-sdk/pkg/obey"
)

func main() { fmt.Printf("Obey Go SDK public import: %T\n", obey.SessionOptions{}) }
GO
go build -mod=readonly -o /tmp/sdk-consumer .
/tmp/sdk-consumer
go version
printf 'Published %s: exact version, checksum verification, Apache license and independent import passed.\n' "$SDK_RELEASE_TAG"
CONSUMER
