# Clean candidate inventory and isolated offline import; fails on pending prerequisites.
[no-cd]
check:
    bash scripts/release-check.sh

# Build native race-test executables only; execution requires an isolated Mac runner.
[no-cd]
native-bundle destination:
    bash scripts/native-macos-bundle.sh '{{destination}}'
