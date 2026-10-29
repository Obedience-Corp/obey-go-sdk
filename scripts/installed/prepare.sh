#!/usr/bin/env bash
set -euo pipefail
[ "${OBEY_SDK_TEST_CONTAINER:-}" = 1 ] && [ -f /.dockerenv ] || { echo 'Disposable Docker fixture required' >&2; exit 2; }
[ "$(id -u)" != 0 ] || { echo 'Non-root fixture user required' >&2; exit 2; }
mkdir -p "$HOME/.local/bin" "$GOPATH" "$GOCACHE" "$GOMODCACHE" /evidence
if command -v obey >/dev/null; then echo 'Fixture is not fresh: Obey exists before installation' >&2; exit 1; fi
SDK_CONSUMER_SUITE=installed bash /sdk/scripts/consumer-build.sh
cp /evidence/consumer-go124 "$HOME/.local/bin/sdk-check"
printf '%s\n' "revision=$SDK_SOURCE_REVISION" "dirty=$SDK_SOURCE_DIRTY" > /evidence/sdk-source.txt
curl --fail --silent --show-error --location --retry 3 --connect-timeout 15 --max-time 120 \
 https://raw.githubusercontent.com/Obedience-Corp/festival/main/install.sh -o /demo/festival-install.sh
VERSION="${FESTIVAL_VERSION:-latest}" INSTALL_DIR="$HOME/.local/bin" \
 bash /demo/festival-install.sh --no-shell > /evidence/festival-bootstrap.log 2>&1
festival version > /evidence/festival-version.txt
camp --version > /evidence/camp-version.txt
go version > /evidence/go-version.txt
sha256sum "$HOME/.local/bin/festival" > /evidence/festival-binary.sha256
printf 'Released Festival prepared; Obey is not installed yet.\n'
