# Install Obey

The SDK is an Apache-2.0 Go module. The Obey CLI and daemon are distributed
separately through [Festival](https://github.com/Obedience-Corp/festival).
You do not need the private Obey source repository.

Install Festival using its official bootstrap script:

```sh
curl -fsSL https://raw.githubusercontent.com/Obedience-Corp/festival/main/install.sh | bash
```

Follow the installer's shell setup instructions, then open Festival:

```sh
festival
```

In the TUI:

1. Choose **Browse catalog** and press Enter.
2. Select **obedience-corp/obey** from **official-obey** and press Enter to install.
3. Wait for **Install complete**, then return home and quit Festival.

Festival installs the `obey` CLI and `ob` companion from its official marketplace.
Confirm the installed binary:

```sh
festival resolve obey
obey version
```

Festival **0.3.25** has a catalog-routing bug that selects Festival when you choose
Obey. The fix is in [installer PR #73](https://github.com/Obedience-Corp/festival-installer/pull/73).
The TUI demo uses that explicitly identified installer candidate. Until the fix is
released, use the scripted installation below with the released installer.

## Scripted installation

For agents, automation, or users who prefer the CLI:

```sh
festival install obey --channel stable
```

Run `festival install obey --help` for service and shell setup options. Use
`--no-restart` to leave an existing daemon running, or `--no-shell-setup` to opt out
of automatic terminal integration.

## Configure the daemon

Configure your camp and provider using Obey, then run the daemon. For an existing
camp, the following commands let you inspect the available configuration:

```sh
obey serve --help
obey providers --help
obey campaign --help
obey doctor --json
```

Keep the daemon running when your Go application submits work. The SDK does not
start or restart it. Pass your configured camp, provider and model to the
[quick start](../README.md#quick-start); do not use the integration test's fixture
provider in a real application.

## Released compatibility

The installed-release test verified Festival **0.3.25** installing stable Obey
**0.2.5** through the CLI from the verified `official-obey` marketplace on Linux
amd64 and arm64.
The daemon returned the exact loopback-provider response, and an independent CLI
readback confirmed one persisted turn and a stopped session.

Obey 0.2.5 advertises `obey.session.run` **v1**. `RunPrompt` works with that
protocol; `StreamPrompt` provides its final result, without v2 live-event guarantees.
Persistent creation, durable submissions and the other machine APIs require
corresponding advertised capabilities. On this stable release, `CreateSession`
returns `*obey.Error` with `Code == "unsupported_protocol"` before mutation.
Installing the SDK alone does not enable unreleased daemon features.

See [compatibility](compatibility.md) for negotiation and
[integration evidence](integration-evidence.md) for the executable release checks.
