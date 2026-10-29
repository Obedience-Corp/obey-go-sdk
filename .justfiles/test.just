# Pure in-memory package tests; mutation suites require explicit build tags.
[no-cd]
unit:
    go test -count=1 ./...

# Fake executable process tests in disposable container scratch.
[no-cd]
process:
    bash scripts/test-container.sh process

# Race verification for pure and containerized process tests.
[no-cd]
race:
    go test -race -count=1 ./...
    bash scripts/test-container.sh process

# Clean independent module/cache consumer; installed by adoption task.
[no-cd]
consumer:
    bash scripts/test-container.sh consumer

# Independent persistent consumer through the existing real CLI/daemon ledger harness.
[no-cd]
durable:
    bash scripts/test-container.sh durable

# Pinned archived SDK/CLI client pairs through the same real daemon/ledger driver.
[no-cd]
compatibility:
    bash scripts/test-container.sh compatibility

# Standalone SDK-only HTTP service with app-owned journal and explicit controls.
[no-cd]
service:
    bash scripts/test-container.sh service

# Candidate SDK against public binaries installed by released Festival, no private checkout.
[no-cd]
installed:
    bash scripts/test-installed.sh
