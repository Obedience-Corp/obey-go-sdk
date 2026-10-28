#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
sdk_root=$PWD
consumer_suite=${1:-oneshot}
compatibility=false
if [ "$consumer_suite" = compatibility ]; then compatibility=true; consumer_suite=oneshot; export OBEY_SDK_CONSUMER_PATTERN='^TestMachineSDKConsumerLegacyArtifactPairing$'; fi
case "$consumer_suite" in oneshot|persistent) ;; *) echo 'expected oneshot or persistent consumer suite' >&2; exit 2 ;; esac
sdk_revision=$(git rev-parse HEAD)
sdk_dirty=false
if [ -n "$(git status --porcelain)" ]; then sdk_dirty=true; fi
printf 'SDK candidate revision=%s dirty=%s suite=%s (local archive, unpublished)\n' "$sdk_revision" "$sdk_dirty" "$consumer_suite"
obey_root=${OBEY_TEST_SOURCE:-}
if [ -z "$obey_root" ] && command -v camp >/dev/null; then
 projects_root=$(camp go p --print)
 candidate="$projects_root/worktrees/obey/om0002-machine-api"
 if [ -f "$candidate/tests/integration/daemon/daemon_helpers.go" ]; then obey_root=$candidate; fi
fi
[ -n "$obey_root" ] && [ -f "$obey_root/scripts/test-machine-contracts.sh" ] || { echo 'Set OBEY_TEST_SOURCE to the dedicated Obey worktree containing the daemon harness' >&2; exit 1; }
command -v docker >/dev/null || { echo 'Docker/Colima is required for independent consumer acceptance' >&2; exit 1; }
docker info >/dev/null 2>&1 || { echo 'Start Docker/Colima then retry just test consumer' >&2; exit 1; }
bash "$obey_root/scripts/test-machine-contracts.sh" build
(cd "$obey_root" && just test mod-proxy)
volume=$(docker volume create)
trap 'docker volume rm "$volume" >/dev/null' EXIT
if [ "$compatibility" = true ]; then
 legacy_sdk=f337ff730e731a533f30c0da00d1a5e056281e68
 legacy_cli=27049b40
 printf 'Compatibility pinned source: SDK=%s CLI=%s (local archives, unpublished)\n' "$legacy_sdk" "$(git -C "$obey_root" rev-parse "$legacy_cli")"
 git archive "$legacy_sdk" | docker run --init --rm -i --read-only --network=none \
  --tmpfs /tmp:rw,exec,nosuid,nodev,size=2g \
  -e OBEY_SDK_TEST_CONTAINER=1 -e HOME=/tmp/home -e GOCACHE=/tmp/cache -e GOMODCACHE=/tmp/modules \
  -v "$sdk_root/scripts/compatibility-build.sh:/build.sh:ro" -v "$volume:/evidence" \
  golang:1.24-bookworm bash /build.sh sdk
 git -C "$obey_root" archive "$legacy_cli" | docker run --init --rm -i --read-only --network=host \
  --tmpfs /tmp:rw,exec,nosuid,nodev,size=4g \
  -e OBEY_SDK_TEST_CONTAINER=1 -e HOME=/tmp/home -e GOCACHE=/tmp/cache -e GOMODCACHE=/tmp/modules \
  -e GOPROXY='file:///gomodproxy,https://proxy.golang.org,direct' -e GOSUMDB=off \
  -v "$obey_root/.gomodproxy/cache/download:/gomodproxy:ro" \
  -v "$sdk_root/scripts/compatibility-build.sh:/build.sh:ro" -v "$volume:/evidence" \
  golang:1.26-bookworm bash /build.sh cli
fi
# No Obey source, credentials, network, or prepopulated module/build caches.
docker run --init --rm --read-only --network=none \
 --tmpfs /tmp:rw,exec,nosuid,nodev,size=2g \
 -e SDK_CONSUMER_SUITE="$consumer_suite" -e OBEY_SDK_TEST_CONTAINER=1 -e HOME=/tmp/home -e GOCACHE=/tmp/archive-cache -e GOMODCACHE=/tmp/archive-modules \
 -v "$sdk_root:/sdk:ro" -v "$volume:/evidence" -w /sdk \
 golang:1.24-bookworm bash /sdk/scripts/consumer-build.sh
# Current-toolchain consumer also builds from the offline archive with empty
# caches. Only afterward does the separate Obey integration driver use its own
# module graph/private read-only proxy to control existing daemon fixtures.
docker run --init --rm --read-only --network host \
 --tmpfs /tmp:rw,exec,nosuid,nodev,size=4g \
 -e HOME=/tmp/home -e TMPDIR=/tmp/scratch -e GOCACHE=/tmp/obey-cache -e GOMODCACHE=/tmp/obey-modules \
 -e SDK_COMPATIBILITY_ARTIFACTS="$compatibility" -e SDK_CONSUMER_LEGACY=/evidence/consumer-legacy -e SDK_LEGACY_CLI=/evidence/obey-v1 -e SDK_PERSISTENT_CURRENT=/tmp/consumer-persistent-current \
 -e SDK_CONSUMER_SUITE="$consumer_suite" -e OBEY_SDK_CONSUMER_PATTERN="${OBEY_SDK_CONSUMER_PATTERN:-}" -e OBEY_MACHINE_TEST_CONTAINER=1 -e OBEY_SDK_TEST_CONTAINER=1 \
 -e SDK_CONSUMER_GO124=/evidence/consumer-go124 -e SDK_CONSUMER_CURRENT=/tmp/consumer-current \
 -e DOCKER_HOST=unix:///var/run/docker.sock -e TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock -e TESTCONTAINERS_HOST_OVERRIDE=127.0.0.1 \
 -e GONOSUMDB='github.com/Obedience-Corp/*,github.com/lancekrogers/*' \
 -e GOPROXY='file:///gomodproxy,https://proxy.golang.org,direct' \
 -v /var/run/docker.sock:/var/run/docker.sock \
 -v "$sdk_root:/sdk:ro" -v "$obey_root:/workspace:ro" \
 -v "$obey_root/.gomodproxy/cache/download:/gomodproxy:ro" -v "$volume:/evidence:ro" -w /workspace \
 golang:1.26-bookworm bash /sdk/scripts/consumer-driver.sh
