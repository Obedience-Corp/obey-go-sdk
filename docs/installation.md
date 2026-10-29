# Install Obey

The SDK is an Apache-2.0 Go module. The Obey CLI and daemon are distributed
separately through [Festival](https://github.com/Obedience-Corp/festival).
You do not need the private Obey source repository.

Install Festival using its official bootstrap script:

```sh
curl -fsSL https://raw.githubusercontent.com/Obedience-Corp/festival/main/install.sh | bash
```

Follow the installer's shell setup instructions, then install stable Obey:

```sh
festival install obey --channel stable
festival resolve obey
obey version
```

Festival installs the `obey` CLI and `ob` companion from its official marketplace.
Run `festival install obey --help` for service and shell setup options. For an
installation where you manage the daemon process yourself, use:

```sh
festival install obey --channel stable --no-restart --no-shell-setup
```

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
**0.2.5** from the verified `official-obey` marketplace on Linux arm64.
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
