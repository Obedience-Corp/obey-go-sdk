# Compatibility

The SDK targets an installed Obey executable and discovers explicit machine
capabilities before using new contracts. Legacy run-v1 remains supported without
private protobuf dependencies. CLI and daemon compatibility are distinct checks.
The foundation provides DTOs, bounded subprocess transport and strict codecs.
Public client/session operations are introduced by their owning implementation
tasks; no construction or module build probes the daemon.

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
