# Compatibility

The SDK targets an installed Obey executable and discovers explicit machine
capabilities before using new contracts. Legacy run-v1 remains supported without
private protobuf dependencies. CLI and daemon compatibility are distinct checks.
The foundation provides DTOs, bounded subprocess transport and strict codecs.
Public client/session operations are introduced by their owning implementation
tasks; no construction or module build probes the daemon.

## Released CLI compatibility

| Obey CLI/daemon | Advertised prompt protocol | Supported public workflow |
| --- | --- | --- |
| 0.5.0 | `obey.session.run` v1 and provider discovery/check v1 | One-shot prompts plus typed provider/model catalogs and route preflight |
| 0.4.0 | `obey.session.run` v1 | One-shot prompts, final-result streaming and owned-session cleanup; verified against the public release |
| 0.3.0 | `obey.session.run` v1 | One-shot prompts, final-result streaming and owned-session cleanup |
| 0.2.5 | `obey.session.run` v1 | Same v1 path, verified before the 0.3.0 release |

These releases do not advertise the persistent machine protocols used by
`CreateSession`, durable turns, observation and approval/control methods.
Those SDK methods remain capability-gated and return `unsupported_protocol`
before mutation. The ordinary CLI's human session/chat commands do not imply
support for an SDK machine protocol. `RequireLiveEvents` also refuses when the
CLI cannot provide live events; it never silently substitutes final-only output.

Provider discovery requires Obey 0.5.0+ and its read-only
`doctor --json --capabilities-only` handshake. Earlier CLIs refuse before any
provider operation; the SDK never falls back to broad doctor health checks or
local configuration. The CLI must target a daemon advertising provider metadata.
`Capabilities` remains the general doctor report and its health status is not a
selected-route readiness verdict. `Status` requires a separate machine protocol.
Keep Obey construction and probes conditional when it is an optional backend.

Preflight checks provider/model availability and explicit tool/live requirements.
It does not validate a camp or working directory; requesting those scope checks
returns `unsupported_capability`. Stable 0.5.0 still cannot enforce tools-off or
provide live prompt events. Catalog discovery does not enable persistent APIs.

The public installed-release lane records actual versions, doctor capabilities,
provider request counts and independent persisted-state readbacks. It follows
the latest stable Obey release; it does not infer compatibility from version numbers.

Go 1.24 is the minimum module version. Runtime imports must use the standard
library. Fake-executable and real-daemon tests run in disposable containers with
source mounted read-only, and home/cache/fixture scratch inside the container.
An absent container marker must fail before mutation. The operator owns CLI
installation, authentication and daemon configuration.

The one-shot consumer lane packages a local verification candidate (not a public
version), builds with empty caches and no network using only the public SDK,
and checks minimum/current Go. Its separate daemon driver reuses Obey's existing
deterministic provider ledger. An owned-session stop can coexist with a durable
turn recovery barrier after a provider effect committed without its receipt;
that outcome does not make the original turn safely replayable. The SDK retains
the caller deadline cause and partial session identity and never reruns it.
