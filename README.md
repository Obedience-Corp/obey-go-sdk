<h1 align="center">Obey Go SDK</h1>

<p align="center">Build Go applications around the Obey daemon.</p>

<p align="center">
  <a href="go.mod"><img src="https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&amp;logoColor=white" alt="Go 1.24+"></a>
  <a href="https://pkg.go.dev/github.com/Obedience-Corp/obey-go-sdk/pkg/obey"><img src="https://pkg.go.dev/badge/github.com/Obedience-Corp/obey-go-sdk/pkg/obey.svg" alt="Go Reference"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue.svg" alt="License: Apache 2.0"></a>
  <a href="https://github.com/Obedience-Corp/festival"><img src="https://img.shields.io/badge/built%20with-Festival-7c3aed.svg" alt="Built with Festival"></a>
</p>

<p align="center">
  <a href="#installation">Installation</a> ·
  <a href="#quick-start">Quick start</a> ·
  <a href="#examples">Examples</a> ·
  <a href="https://pkg.go.dev/github.com/Obedience-Corp/obey-go-sdk/pkg/obey">API reference</a>
</p>

Submit prompts, stream activity, and manage durable sessions from your Go application.
The SDK uses the installed Obey CLI as its transport and has standard-library-only
runtime dependencies.

The SDK is open source under Apache-2.0. The Obey daemon is proprietary and runs
separately. This SDK is currently a prerelease; see [platform support](#platform-support)
for the verified runtime scope.

## Installation

Requires **Go 1.24+**, a compatible Obey CLI on `PATH`, and a running Obey daemon
with your camp, provider, and model configured.

```sh
go get github.com/Obedience-Corp/obey-go-sdk@main
```

Import the public package:

```go
import "github.com/Obedience-Corp/obey-go-sdk/pkg/obey"
```

The prerelease install resolves `main` to a Go pseudo-version recorded in your
application's `go.mod`. See [compatibility](docs/compatibility.md) for capability
discovery and supported machine protocols.

Install Obey through the [Festival installer](docs/installation.md). Stable Obey
0.2.5 supports one-shot prompts through protocol v1. Persistent SDK APIs require
a daemon and CLI that advertise the corresponding machine protocols; unsupported
operations return a typed error before creating a session.

## Quick start

Replace the camp, provider, and model below with values configured in your daemon.

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/Obedience-Corp/obey-go-sdk/pkg/obey"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	client, err := obey.NewClientFromPath(obey.WithCamp("my-camp"))
	if err != nil {
		return err
	}
	defer client.Close()

	result, err := client.RunPrompt(ctx, "Summarize the current project", &obey.RunOptions{
		SessionOptions: obey.SessionOptions{
			Provider: "your-provider",
			Model:    "your-model",
		},
	})
	if err != nil {
		return err
	}

	fmt.Println(result.Text)
	return nil
}
```

Daemon requests accept a `context.Context`, so HTTP request contexts and application
cancellation can flow through to the CLI operation.

## API overview

| Workflow | API |
| --- | --- |
| Run a prompt | `Client.RunPrompt`, `Client.StreamPrompt` |
| Create or reopen a session | `Client.CreateSession`, `Client.OpenSession` |
| Submit durable work | `Session.Send` |
| Inspect or recover a turn | `Client.OpenTurn`, `Turn.State`, `Turn.Wait` |
| Read history and activity | `Session.Transcript`, `Session.Attach`, `Client.Observe` |
| Handle approvals | `Session.PendingApprovals`, `Session.RespondApproval` |
| Control work explicitly | `Turn.Cancel`, `Session.Stop` |

One-shot runs own their new session and attempt cleanup when they finish. Persistent
sessions have a different lifetime: closing the client detaches, and your application decides when
to cancel a turn or stop a session.

For durable submissions, persist your application's idempotency key **before** calling
`Session.Send`. Retain returned references, including partial references on errors,
for recovery. Send acknowledges acceptance; `Turn.Wait` retrieves the original turn's
result. Applications control mutation retries and approval decisions explicitly.

See [lifecycle and recovery](docs/lifecycle.md) for ownership, cancellation, acceptance
uncertainty, observation cursors, and retained-result behavior.

## Examples

| Example | Demonstrates |
| --- | --- |
| [Basic prompt](examples/basic/main.go) | A one-shot prompt with explicit provider and model |
| [Streaming](examples/streaming/main.go) | Incremental events and final results |
| [Observation](examples/observe/main.go) | Scoped activity streams and caller-controlled reconnect |
| [Approvals](examples/approvals/main.go) | Inspecting pending calls and submitting explicit decisions |
| [HTTP service](examples/service/main.go) | Using request contexts in a Go HTTP handler |

The [persistent HTTP integration fixture](tests/consumer/service/README.md) demonstrates
application-owned key/ref journaling, process restart, and explicit session controls.
Its [integration evidence](docs/integration-evidence.md) records the tested scenarios.

## Platform support

| Platform | Status |
| --- | --- |
| Linux arm64 | CLI/daemon lifecycle and independent consumer integration verified |
| macOS arm64 | Native test binaries compile; isolated lifecycle execution pending |
| Windows | Unsupported |

The operator manages daemon startup, authentication, and provider configuration.
See [release notes](docs/release-notes.md) for the current verification limits.

## Development

```sh
just build
just lint
just test unit
```

Subprocess tests run in disposable Docker containers:

```sh
just test process
```

Keep filesystem, socket, database, and child-process fixtures in the isolated test
lanes. See [integration evidence](docs/integration-evidence.md) for real-daemon
consumer checks.

Run the public-release integration with Docker and network access:

```sh
just test installed
```

This installs Obey through released Festival, exercises the SDK against the actual
daemon with a deterministic loopback provider, and verifies persisted session state.
It needs no private Obey checkout or provider credentials. CI runs it on pull
requests, pushes to `main`, and nightly to catch release compatibility regressions.

## License

[Apache License 2.0](LICENSE). The license covers this SDK; the Obey daemon is
distributed separately under its own terms.
