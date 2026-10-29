# Native macOS lifecycle evidence

[SDK integration run 40827320383](https://github.com/Obedience-Corp/obey-go-sdk/actions/runs/40827320383)
passed on the isolated GitHub-hosted `macos-15` arm64 runner on 2026-10-29.
The native job completed in 5m53s using Go 1.24.13 on macOS 15.7.9.

The clean PR merge tested was `8b20a73faf7fa8db5ccf41965b9e517c9c3eb576`.
Its Git tree, `43d99c677fee828b26c7fdbc03751541763c4b78`, exactly matches SDK head
`2afb91e28ac858308b31ca715e1309862442967e`.

The job built Darwin arm64 race-test executables, set isolated HOME/TMPDIR, then
executed both real helper-child process suites through `native-macos-run.sh`:

| Suite | Top-level tests passed | Executable SHA-256 |
| --- | ---: | --- |
| `internal/execclient` | 12 | `2e3bc85079f0e5f8e4a19ef05acd904246b9a5624ff1ce664e4c3f1fdd7a9bec` |
| `pkg/obey` | 33 | `0cae3ceef80af934da6761123aa86220de81ee676acb830838c73d16d3b9095c` |

Both logs end in `PASS`, with no skipped top-level tests, race reports or failures.
The `native-macos-arm64` artifact retains BUILD.txt, platform and executable hashes,
complete suite logs, and the successful RESULT.txt. The retained LICENSE hash is
`cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30`.

This verifies native SDK child-process lifecycle, cancellation, reaping, stream
handling and persistent detach. It does not certify an installed macOS daemon or
provider. The separate Linux amd64/arm64 jobs in the same run passed integration
against actual Festival-installed stable Obey 0.2.5. Windows remains unsupported.
