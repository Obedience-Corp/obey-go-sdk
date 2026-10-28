# Persistent service integration

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
latency is unmeasured. Native macOS lifecycle and public distribution remain separate
release prerequisites.
