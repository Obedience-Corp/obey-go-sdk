# Clean candidate inventory and isolated offline import; fails on pending prerequisites.
[no-cd]
check:
    bash scripts/release-check.sh

# Build native race-test executables only; execution requires an isolated Mac runner.
[no-cd]
native-bundle destination:
    bash scripts/native-macos-bundle.sh '{{destination}}'

# Verify a published version through the public Go proxy and checksum database.
[no-cd]
published version:
    bash scripts/test-published.sh {{quote(version)}}

# Publish an immutable stable tag from clean, passing origin/main.
[no-cd]
stable version:
    bash scripts/release-stable.sh {{quote(version)}}
