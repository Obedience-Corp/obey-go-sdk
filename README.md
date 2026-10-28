# obey-go-sdk

Go module `github.com/obediencecorp/obey-go-sdk`, public package `pkg/obey`.
Requires Go 1.24 or newer and an installed compatible Obey CLI. The operator
configures Obey's daemon and authentication; the SDK does not own credentials
or link private protobuf packages. Runtime dependencies use the standard library.

Development uses `just build`, `just lint`, `just test unit`, and `just test race`.
Subprocess tests use `just test process` in disposable Docker scratch. Unit tests
must remain in memory; tests creating files, sockets or child executables require
the container tag and guard before fixture creation. Consumer and durable lanes
will be supplied by their owning implementation tasks; until then they fail
explicitly. No CLI/daemon is contacted by the module scaffold.

This repository is local and unpublished. Remote ownership, license selection,
version tags and public module availability remain release prerequisites. No
license or permission to publish is implied by this scaffold.
