# Release the SDK

SDK releases are immutable `vX.Y.Z` Git tags with GitHub release notes. Go users
install the tagged module directly; no platform-specific SDK binary is needed.
The Apache-2.0 LICENSE must remain in both source and the downloaded Go module.

## Prepare

1. Verify compatibility against the current public Obey release with
   `just test installed`. Record actual capabilities; do not claim persistent
   support merely because the SDK exposes those methods.
2. Update the README installation version and `docs/release-notes.md`. Keep
   `docs/compatibility.md` and platform claims consistent with runtime evidence.
3. Run `just build`, `just lint`, the relevant isolated test lanes and actionlint.
   Merge the reviewed changes and wait for all `SDK integration` jobs on main.

## Publish

From a clean checkout equal to the current `origin/main`, run:

```sh
just release stable v0.1.0
```

The recipe verifies the canonical repository, permissions, passing main CI,
license/dependency/native evidence and a clean isolated module archive. It then
creates and pushes a new annotated tag. An existing tag is always refused.

The `Release` workflow checks the stable tag and passing main source, repeats the
clean archive check, publishes the release notes, and verifies the exact published
version through a fresh public Go consumer. Wait for that workflow to pass before
declaring publication verified. GitHub provides the tagged source archives.

## Verify a published version

```sh
just release published v0.1.0
```

This uses a disposable Go 1.24 container with no SDK source, credentials, private
checkout or Obey binary mounted. It downloads the exact version through the normal
Go proxy/checksum database, checks the two-module graph and LICENSE hash, and builds
and runs an independent public-package import. It does not start a daemon.

Never move an existing release tag. If code needs correction after publication,
fix it on main and publish a new version.
