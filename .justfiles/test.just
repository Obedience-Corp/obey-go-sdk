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

# Real CLI/daemon durable acceptance; installed by durable integration task.
[no-cd]
durable:
    bash scripts/test-container.sh durable
