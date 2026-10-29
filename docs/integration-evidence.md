# Integration evidence

## Public installed-release check

`just test installed` builds the candidate SDK into a clean independent Go 1.24
consumer using a local module archive. The module graph must contain exactly the
application and public SDK, with no `replace` directive, private Obey source,
protos or provider SDK. The downloaded module must retain the Apache-2.0 LICENSE.
The candidate archive tests the current source; it is not a published SDK version.

Released Festival installs stable Obey from its verified official marketplace in
a disposable, non-root Docker container. The test records installer receipts,
marketplace verification, versions, source provenance and executable hashes in
`.test-output/installed/`. Runtime sockets and databases stay inside the container.
The only provider is an in-process OpenAI-compatible loopback fixture with a dummy
key. No external model credentials or private checkout are required.

Assertions cover the exact prompt response, negotiated protocol, exact session and
camp identity, one provider request, and an independent CLI readback of one turn
with the session stopped. When persistent creation is advertised, the test also
checks Send/Wait, same-key deduplication, State and explicit Stop. Otherwise it
requires a typed `unsupported_protocol` refusal with identical before/after session
sets and no additional provider request. It then stops its own daemon and checks
typed failure without automatic daemon startup or another provider request.

Stable Obey 0.2.5 advertises only the v1 one-shot protocol. Persistent operations
are therefore a verified safe-refusal path, not a claim of released support.
Set `OBEY_REQUIRE_PERSISTENT=1` to make missing persistent support fail the check.
Pin the bootstrap release with `FESTIVAL_VERSION=0.3.25`; the default follows latest
Festival and stable Obey to detect future release regressions.

GitHub Actions runs pure race tests and the installed check on Linux amd64 and
arm64 for pull requests, `main`, and nightly. A separate isolated macOS arm64 job
executes the existing native helper-child lifecycle suites. Artifacts retain
provenance and failure logs. The macOS job proves SDK process handling, not a
native installed-daemon integration.

The first run passed all four lanes, including the native helper-process suites;
see [native evidence](native-macos-evidence.md) for clean source, test counts and
executable hashes. The deterministic install-and-SDK VHS recording is embedded in
[PR #2](https://github.com/Obedience-Corp/obey-go-sdk/pull/2), with a separate fresh
PTY/pyte replay and persisted readback checks. The video opens Festival's TUI,
navigates Browse catalog to Obey, installs it, then runs the SDK consumer.
It uses the identified candidate from [installer PR #73](https://github.com/Obedience-Corp/festival-installer/pull/73),
which fixes the released 0.3.25 catalog-routing bug. Automated CI retains the
released CLI installation path. Docker lacks systemd; the SDK check owns its
foreground daemon and does not claim automatic service setup passed.

## Persistent service integration

`tests/consumer/service` is an independent, SDK-only HTTP application. Run
`just test service` to build it through a local module archive under both supported
Go toolchains and exercise actual subprocess CLI/daemon interactions in disposable
containers. It imports no private Obey packages, protos, source replacements or
provider SDK. The archive is a local candidate, not a published installation.

The application owns its job journal. It fsyncs the original caller key before
Send, then saves the returned exact SessionRef, TurnRef and receipt, including
known partial identity on uncertain errors. Restarting the app reopens those refs
without submitting another turn. A saved exact TurnRef remains usable for read-only
State/Wait when optional key metadata is missing; explicit retry then refuses rather
than inventing a key. Retrying requires the original stored input/key and an explicit
request. Duplicate POST job IDs return stored state without submission.

HTTP request cancellation controls Wait and observation lifetime only. Separate
endpoints request approval decisions, turn cancellation and session stop. The app
requires an explicit approval boolean. Stop preserves daemon-managed child custody;
an independent session remains usable. Its loopback fixture server has no production
authentication claim. Never run its filesystem fixture paths directly on the host.

The registered lab scenario is `vhs/obey/go-sdk-persistent-service-om0002` in the
social-fitness-app camp. Its `just verify` selects one complete HTTP journey and
normalizes environment paths, identities and timestamps. Product tests remain here
and in the owning Obey harness. Set OBEY_SDK_SOURCE and OBEY_TEST_SOURCE to dedicated
worktrees; the wrapper records revisions and dirty provenance before execution.

Actual assertions cover a gated running turn and queued sibling, explicit approval,
request timeout and NDJSON detach without cancellation, app restart, missing-key
read-only recovery and retry refusal, wrong stored scope, selected cancellation,
selected stop and independent-session execution. Separate service cases establish
that the key journal precedes daemon acceptance and that old CLI/unavailable daemon
refusals survive app restart without hidden daemon startup or submission.

The fixture uses crash-provider/crash-model and a durable deterministic effect ledger.
Counts prove original identities, orchestration and no replay; they do not prove
semantic conversation memory, external tool effects or real-provider performance.
Fresh capability discovery and Ping timings are setup-only measurements; provider
latency is unmeasured. Public SDK source distribution has been verified separately
through the normal Go proxy and checksum database. A versioned release still needs
the festival's remaining certification gates.
