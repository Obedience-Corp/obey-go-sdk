# Public installed-binary consumer

Run from the repository root:

```sh
just test installed
```

The driver builds this independent application through a candidate module archive,
then bootstraps released Festival and installs stable Obey from the verified official
marketplace. It runs the actual daemon with an isolated camp and deterministic
OpenAI-compatible loopback provider. All configuration, sockets, databases and
child processes belong to a disposable non-root Docker container. Only evidence
is exported; the driver removes its own container on success or failure.

The application requires both `OBEY_SDK_TEST_CONTAINER=1` and Docker's isolation
marker before any fixture mutation. Do not run it directly on the host.
The tagged lifecycle tests also require these guards.

The check verifies one-shot execution and cleanup through SDK and independent CLI
readbacks, persistent execution when advertised or safe refusal otherwise, and
failure without automatic daemon startup after its own daemon has stopped.
It never treats an absent capability as successful persistent execution.

`FESTIVAL_VERSION` selects the bootstrap version (`latest` by default).
`OBEY_REQUIRE_PERSISTENT=1` fails when stable Obey lacks persistent APIs.
`OBEY_SDK_INSTALLED_OUTPUT` selects the evidence directory; use a new directory
when retaining multiple runs. Installer metadata and binary hashes make the
tested release identifiable even when the stable channel advances.

See [integration evidence](../../../docs/integration-evidence.md) for scope and
[installation](../../../docs/installation.md) for end-user setup.
