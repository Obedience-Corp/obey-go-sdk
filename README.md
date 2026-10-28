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
The durable lane remains pending its owning implementation task and fails explicitly. No CLI/daemon is contacted by the module scaffold.

This repository is local and unpublished. Remote ownership, license selection,
version tags and public module availability remain release prerequisites. No
license or permission to publish is implied by this scaffold.

The current baseline provides `RunPrompt`, final-only `StreamPrompt`, capability
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
and the basic, streaming and service examples. Persistent session/turn methods
and public release availability remain pending their own acceptance tasks.
