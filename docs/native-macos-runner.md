# Isolated native macOS process proof

`just release native-bundle /absolute/new/artifact-directory` compiles Darwin arm64
race-test executables and copies required fixtures. It executes no tests. BUILD.txt
records the source revision, dirty paths, toolchain and binary hashes. A dirty bundle
is preparation evidence only; final runtime certification must identify the tested
clean revision. Cross-compilation/build success is never native runtime proof.

Transfer the bundle into an initialized, owned disposable macOS VM or approved
isolated CI runner. Never run it against a host workspace or another festival's
pending account/setup installation. Within that runner, explicitly set
`OBEY_SDK_ISOLATED_MACOS=1` and run `bash run.sh`. The runner verifies Darwin arm64,
creates isolated HOME/TMPDIR, runs both real helper-child process suites with race
instrumentation and bounded package timeouts, and retains platform, binary hashes,
complete logs and exit result. Its existing container marker denotes isolated
fixture execution here; it grants no permission to execute fixtures on the host.

The suites exercise actual native child processes, signal cancellation, stdout and
stderr pipes, process reaping, stream/client Close, ordered bounded shutdown drain,
protocol errors, exact scope and persistent detach. Linux actual CLI/daemon/provider
certification remains separate; these helper tests do not prove native daemon or
real-provider support. Windows is unsupported.

Current runner prerequisite: the discovered FA0027 Mac VM is owned by another active
festival and remains at its user account-creation handoff. It has not been started,
cloned, configured or used for this work. No native runtime evidence is currently
available. Keep release acceptance pending until an approved initialized runner and
actual passing logs exist. No remote CI dispatch is implied by preparing this bundle.
