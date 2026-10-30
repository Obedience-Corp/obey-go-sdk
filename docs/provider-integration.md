# Integrating Obey as an optional provider

Applications can offer Obey alongside direct provider integrations. Treat Obey
as a backend that routes to a provider profile and model configured in its daemon:

```text
Application backend: Obey
  Daemon provider profile: my-openai-profile
    Model: the configured model ID
```

The daemon profile ID can differ from a provider type or the application's native
provider names. The daemon owns its credentials; applications pass the route and
do not copy credentials or probe the underlying provider directly.

## Optional installation

Construct an Obey client only when that backend is selected. SDK imports do not
execute Obey; `NewClientFromPath` locates the executable and validates options
without contacting the daemon. A missing installation returns `*obey.Error` with
`Code == "binary_not_found"`. Keep this error local to the Obey selection, show
[installation guidance](installation.md), and preserve the user's selected
backend. Native providers need no Obey executable or running daemon.

Pass `WithSocket` when the application selects a daemon socket. Use the same
client options for discovery and execution; avoid checking one daemon and
submitting work to another. Apps own their client lifetime and call `Close` when
finished. Constructor success proves executable availability, not route readiness.

## Provider and model selection

SDK v0.2.0 provides typed route discovery with Obey 0.5.0+:

```go
profiles, err := client.Providers(ctx) // daemon metadata; no provider endpoint I/O
// Populate provider IDs from profiles.Providers; retain existing selections.
models, err := client.Models(ctx, selectedProvider)
// Populate raw model IDs; retain manual IDs if the catalog is incomplete.
models, err = client.RefreshModels(ctx, selectedProvider) // explicit network refresh
readiness, err := client.Preflight(ctx, obey.PreflightRequest{
    Provider: selectedProvider, Model: selectedModel,
    ToolPolicy: obey.ToolPolicyDaemonManaged,
})
```

Handle each error before using its result. These methods honor context cancellation,
selected socket, environment and working directory. They negotiate only through
read-only CLI capabilities and never initialize configuration or start the daemon.
Older CLIs return `unsupported_protocol`; a daemon without metadata support returns
`unsupported_capability`. Keep manual fields available and preserve saved IDs.

Provider profile IDs differ from provider types. Model catalogs report `Source`,
`State`, nullable `FetchedAt`/`Exhaustive` and nullable capability evidence. An absent
capability is unknown, not false. A static, stale, unavailable or nonexhaustive
catalog cannot prove an unlisted model invalid. `Models` follows daemon cache
policy and may populate an expired/missing catalog; `RefreshModels` explicitly
requests a new selected-profile snapshot. Returned snapshots are independent;
the SDK has no cross-call catalog cache. If the socket or provider changes while
a request is pending, cancel it and discard results from the old selection.

`Preflight` returns structured stage/code/state/remediation checks. `Ready()`
requires every check ready; `HasFailures()` detects explicit unavailable/unsupported
checks while preserving unknown evidence. Preflight is a snapshot, not an inference
probe or reservation. It creates no session and performs no inference. It does
not validate a camp/workdir; nonempty `PreflightRequest.Camp` or
`WorkingDirectory` returns `unsupported_capability` rather than skipping a requested
check. Client `WithCamp`/`WithWorkingDirectory` still set execution context.

Set `ToolPolicyOff` or `RequireLiveEvents` to check application requirements.
Obey 0.5.0 reports those requirements unsupported. Do not infer enforcement from a
successful catalog lookup or change permissions automatically. Zero tool policy
means daemon-managed; applications requiring tools-off must request it explicitly
and refuse the route when unavailable. A preflight tool policy is a check, not a
run option that enforces policy.

`Capabilities` describes CLI protocols and includes general doctor health checks;
it is not the selected-route verdict. Use `Preflight` for route requirements.
`Status` uses a separate protocol unavailable in stable 0.5.0.

## Streaming and conversation ownership

Obey 0.5.0 advertises only `obey.session.run` v1. `StreamPrompt` therefore returns
final-result fidelity: it does not deliver incremental assistant tokens. Inspect
`RunStream.Fidelity`, or set `RunOptions.RequireLiveEvents` to refuse before
execution when live events are mandatory. Do not report a completed answer as
live token streaming.

Each one-shot run creates a session and attempts owned-session cleanup. An app
that owns conversation history can include its bounded context in each prompt;
the SDK does not implicitly maintain app conversation memory. Persistent SDK
methods need additional advertised machine protocols and refuse on 0.5.0 before
mutation. Keep incomplete usage unknown, and never automatically replay a failed
request: provider work may have happened before a failure was observed.

## Tools, approvals and cancellation

Discussion mode does not guarantee tools are disabled. The current SDK offers no
universal tools-off enforcement across daemon providers. An application requiring
that guarantee must refuse an unsupported route; it must not silently treat a
mode or missing event as enforcement. Daemon-managed permissions require an
explicit application/operator policy. Never automatically approve an SDK approval
event to make an integration work.

Use caller-owned contexts for requests and cancellation. Drain bounded cleanup
through `Result`/`Close`, retain known partial session identity, and keep tool,
reasoning and diagnostic events out of assistant speech. A terminated local
process alone does not prove the provider stopped its remote work.

See [compatibility](compatibility.md) and [integration evidence](integration-evidence.md)
for verified release behavior. SDK transport tests do not establish the complete
setup, UI, audio or conversation experience of an integrating application.
