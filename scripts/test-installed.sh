#!/usr/bin/env bash
# Public release integration. Every installer/daemon fixture lives in Docker.
set -euo pipefail
cd "$(dirname "$0")/.."
sdk_root=$PWD
mode=${1:-verify}
case "$mode" in verify|prepare) ;; *) echo 'usage: test-installed.sh [verify|prepare <container-name>]' >&2; exit 2 ;; esac
expected_version=${OBEY_EXPECT_VERSION:-}
if [ -n "$expected_version" ] && ! [[ "$expected_version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
 echo 'OBEY_EXPECT_VERSION must be a stable version such as 0.4.0 (without v)' >&2
 exit 2
fi
command -v docker >/dev/null
docker info >/dev/null
sdk_uid=$(id -u)
sdk_gid=$(id -g)
[ "$sdk_uid" != 0 ] || { echo 'Run the integration driver as a normal user; the fixture is deliberately non-root.' >&2; exit 2; }
out=${OBEY_SDK_INSTALLED_OUTPUT:-$sdk_root/.test-output/installed}
mkdir -p "$out"
out=$(cd "$out" && pwd)
image="obey-sdk-installed:${sdk_uid}-${sdk_gid}"
docker build --tag "$image" --build-arg "SDK_UID=$sdk_uid" --build-arg "SDK_GID=$sdk_gid" \
 --build-arg "GO_IMAGE=${SDK_TEST_GO_IMAGE:-golang:1.24-bookworm}" tests/integration/installed
name_args=()
if [ "$mode" = prepare ]; then
 name=${2:?prepare requires a new container name}
 [[ "$name" =~ ^[a-zA-Z0-9][a-zA-Z0-9_.-]*$ ]] || { echo 'Invalid container name' >&2; exit 2; }
 name_args=(--name "$name")
fi
revision=$(git rev-parse HEAD)
dirty=false
if [ -n "$(git status --porcelain)" ]; then dirty=true; fi
container=$(docker create --init "${name_args[@]}" \
 --label obey.scenario=sdk-installed-release \
 -e OBEY_SDK_TEST_CONTAINER=1 -e "FESTIVAL_VERSION=${FESTIVAL_VERSION:-latest}" \
 -e "OBEY_EXPECT_VERSION=$expected_version" \
 -e "SDK_SOURCE_REVISION=$revision" -e "SDK_SOURCE_DIRTY=$dirty" \
 --mount "type=bind,src=$sdk_root,dst=/sdk,readonly" \
 --mount "type=bind,src=$out,dst=/evidence" \
 --entrypoint sleep "$image" infinity)
keep=false
cleanup() { if [ "$keep" = false ]; then docker rm -f "$container" >/dev/null; fi; }
trap cleanup EXIT
docker start "$container" >/dev/null
docker exec "$container" timeout --signal=TERM --kill-after=10s 300 bash /sdk/scripts/installed/prepare.sh
if [ "$mode" = prepare ]; then
 keep=true
 printf 'Prepared isolated recording container: %s\n' "$name"
 exit 0
fi
docker exec "$container" timeout --signal=TERM --kill-after=10s 180 bash /sdk/scripts/installed/install-obey.sh
args=()
if [ "${OBEY_REQUIRE_PERSISTENT:-0}" = 1 ]; then args+=(-require-persistent); fi
docker exec "$container" timeout --signal=TERM --kill-after=10s 150 /demo/.local/bin/sdk-check \
 -binary /demo/.obey/installer/bin/obey -camp sdk-installed-check \
 -workdir /demo/camp -evidence-dir /evidence "${args[@]}" | tee "$out/verify.log"
