# Obey Go SDK v0.1.0

The first versioned Go SDK for applications using the proprietary Obey daemon.
Requires Go 1.24+, an installed Obey CLI and an operator-managed daemon.

```sh
go get github.com/Obedience-Corp/obey-go-sdk@v0.1.0
```

- Submit prompts with `RunPrompt` and `StreamPrompt`, using contexts, stdin input,
  typed results/errors and bounded subprocess output and cleanup.
- Discover machine capabilities before selecting a protocol. Preserve legacy v1
  compatibility and refuse unavailable features before mutation.
- Keep standard-library-only runtime dependencies and the Apache-2.0 license.
  Neither private Obey code, protobuf modules nor provider SDKs are dependencies.

## Released compatibility

Public Festival 0.3.26 installs Obey 0.3.0 from the verified official marketplace.
Installed-daemon integration verifies exact prompt output, one persisted turn,
stopped owned-session cleanup, safe capability refusal and offline failure without
daemon startup. Obey 0.2.5 was also verified through the v1 path.

**Obey 0.3.0 advertises only `obey.session.run` v1.** Prompt streaming therefore has
final-result fidelity. Persistent session/turn, observation, approval and control
SDK methods require additional advertised machine protocols and currently return
`unsupported_protocol` before mutation. The SDK does not start the daemon or retry
mutations automatically. Applications own their persistence, retries and approvals.

Linux amd64/arm64 use actual installed CLI/daemon integration. Native macOS arm64
SDK process lifecycle is verified in an isolated runner; it does not certify the
native daemon or every provider. Windows SDK runtime remains unsupported.

See [compatibility](compatibility.md), [lifecycle](lifecycle.md) and
[integration evidence](integration-evidence.md) for exact scope. Provider fixtures
prove orchestration and persisted state, not model quality or external tool effects.

## Distribution

The SDK is open source under [Apache-2.0](../LICENSE). The daemon is distributed
separately under its own terms. The canonical module is
`github.com/Obedience-Corp/obey-go-sdk`; import its `pkg/obey` package.
Releases use immutable Git tags. The publication workflow verifies an independent
consumer through the public Go proxy and checksum database, including license
retention and the absence of source replacements or private dependencies.
