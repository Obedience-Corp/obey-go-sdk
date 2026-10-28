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

No public release/tag/install is claimed. The current module path remains
`github.com/obediencecorp/obey-go-sdk`; remote owner and license policy require the
owner decision. Do not retag an existing version. Publication, push and CI dispatch
are separate actions requiring authorization. Local module archives prove clean
consumer isolation, not public availability.
