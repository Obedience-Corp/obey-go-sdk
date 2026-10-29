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

The released SDK requires explicit provider and model IDs in `SessionOptions`
or `WithSessionDefaults`. It currently has no typed provider/model catalog or
selected-route readiness API. Preserve manually configured IDs when a lookup is
unavailable, and do not substitute a native provider's catalog for Obey's catalog.
SDK and daemon versions are independent; an Obey update alone does not add SDK
methods or machine contracts.

`Capabilities` reports the CLI's explicit machine protocols. Its doctor health
status may include unrelated providers or host checks, so it is not a verdict on
the chosen route. `Status` requires a separate machine protocol not advertised by
stable Obey 0.4.0. Neither is a universal connectivity/readiness check on that
release. Preserve typed execution errors and provide route-specific guidance.

## Streaming and conversation ownership

Obey 0.4.0 advertises only `obey.session.run` v1. `StreamPrompt` therefore returns
final-result fidelity: it does not deliver incremental assistant tokens. Inspect
`RunStream.Fidelity`, or set `RunOptions.RequireLiveEvents` to refuse before
execution when live events are mandatory. Do not report a completed answer as
live token streaming.

Each one-shot run creates a session and attempts owned-session cleanup. An app
that owns conversation history can include its bounded context in each prompt;
the SDK does not implicitly maintain app conversation memory. Persistent SDK
methods need additional advertised machine protocols and refuse on0.4.0 before
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
