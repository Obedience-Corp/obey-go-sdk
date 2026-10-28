# obey-go-sdk

Go module `github.com/obediencecorp/obey-go-sdk`, public package `pkg/obey`.
Requires Go 1.24 or newer and an installed compatible Obey CLI. The operator
configures Obey's daemon and authentication; the SDK does not own credentials
or link private protobuf packages. Runtime dependencies use the standard library.

Development uses `just build`, `just lint`, `just test unit`, and `just test race`.
Subprocess tests use `just test process` in disposable Docker scratch. Unit tests
must remain in memory; tests creating files, sockets or child executables require
the container tag and guard before fixture creation. The consumer lane builds an independent module through an offline local proxy
on minimum/current Go and runs it through the isolated real CLI/daemon harness.
The durable lane uses a separate persistent consumer through that same offline archive and real daemon/provider ledger harness. Builds and pure tests do not contact the CLI or daemon.

This SDK is open source under the [Apache License 2.0](LICENSE), intended for
people building applications around the Obey daemon. The daemon is proprietary;
this license applies to the SDK repository.

This repository is currently local and unpublished. Canonical repository/module
ownership, version tags and public module availability remain release prerequisites.

The current candidate provides `RunPrompt`, `StreamPrompt` with advertised live/final-only fidelity, capability
discovery, copied options/results, typed failures and bounded child cleanup.
Callers choose their provider/model explicitly:

```go
client, err := obey.NewClientFromPath(obey.WithCamp(camp))
if err != nil { return err }
defer client.Close()
result, err := client.RunPrompt(ctx, prompt, &obey.RunOptions{
    SessionOptions: obey.SessionOptions{Provider: provider, Model: model},
})
```

Import `github.com/obediencecorp/obey-go-sdk/pkg/obey`. See `docs/lifecycle.md`
and the basic, streaming and service examples. Persistent `CreateSession`,
`OpenSession`, read-only `OpenTurn`, `Session.Send`, `Turn.State`, context-only
`Turn.Wait` and paginated `Session.Transcript` are available. Send returns durable
acceptance; callers explicitly wait or inspect state. Persist your own submission
key before Send for crash recovery, and retain partial handles and typed uncertainty
on errors. Client closure detaches persistent work. `Session.Attach` and `Client.Observe`
expose typed/raw event wrappers with explicit scope and pagination. Explicit
approval/cancel/stop are available; public release readiness remains pending in
this local candidate. See `examples/observe` for caller-controlled reconnect.

Persistent handles expose explicit `PendingApprovals`, `RespondApproval`,
`Turn.Cancel` and `Session.Stop`. See [lifecycle and controls](docs/lifecycle.md)
and [approval inspection/decisions](examples/approvals/main.go). Decision errors
can be indeterminate; no mutation is retried automatically. Cancellation acknowledgements
are separate from physical exit and do not reverse a recorded effect.

The independent SDK-only HTTP fixture and registered journey are described in
[service integration evidence](docs/integration-evidence.md). `just test service`
proves journal-before-send, restart recovery and explicit controls in containers.
