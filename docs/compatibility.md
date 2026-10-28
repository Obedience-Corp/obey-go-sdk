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
