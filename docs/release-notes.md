# Local candidate release notes

This candidate adds a standard-library subprocess SDK for an installed Obey CLI.
It preserves run-v1/default CLI compatibility and selects advertised run-v2 only
before mutation. Persistent handles expose acceptance, exact state/history/wait,
scoped observation, explicit approvals and controls. The application owns key/ref
storage; the SDK never automatically retries mutations or starts the daemon.

Linux arm64 certification uses actual CLI/daemon processes and a deterministic
provider ledger, including restart/crash, partial output, cancellation, cleanup,
queue/dedup, wrong scope and independent SDK-only service adoption. See lifecycle
and integration evidence for limits. Provider counts are not semantic memory or
real-provider latency evidence. Windows is unsupported. Native Darwin arm64 process
runtime is pending until the prepared bundle runs in an approved isolated runner.

The SDK is open source under Apache-2.0, as approved by the owner, for developers
building around the proprietary Obey daemon. The SDK license does not license the
daemon. See [distribution decisions](distribution-decision.md).

No public release/tag/install is claimed. The current module path remains
`github.com/obediencecorp/obey-go-sdk`; canonical repository/module ownership is
pending. Do not retag an existing version. Publication, push and CI dispatch
are separate actions requiring authorization. Local module archives prove clean
consumer isolation, not public availability.
