#!/usr/bin/env bash
# Execute only inside an initialized, owned disposable macOS VM/approved CI runner.
set -euo pipefail
[ "${OBEY_SDK_ISOLATED_MACOS:-}" = 1 ] || { echo 'Set OBEY_SDK_ISOLATED_MACOS=1 only inside the approved isolated Mac runner' >&2; exit 2; }
[ "$(uname -s)" = Darwin ] && [ "$(uname -m)" = arm64 ] || { echo 'Native Darwin arm64 required' >&2; exit 2; }
cd "$(dirname "$0")"
root=$PWD
mkdir -p "$root/scratch" "$root/home" "$root/results"
export TMPDIR="$root/scratch" HOME="$root/home" OBEY_SDK_TEST_CONTAINER=1
{
 uname -a
 sw_vers
 shasum -a 256 internal/execclient/process.test pkg/obey/process.test
} > results/platform.txt
(cd internal/execclient && ./process.test -test.v -test.count=1 -test.timeout=10m -test.run='^TestProcess') 2>&1 | tee results/execclient.log
(cd pkg/obey && ./process.test -test.v -test.count=1 -test.timeout=10m -test.run='^TestProcess') 2>&1 | tee results/public.log
printf 'Native isolated process suites passed; no real-daemon/provider certification implied\n' | tee results/RESULT.txt
