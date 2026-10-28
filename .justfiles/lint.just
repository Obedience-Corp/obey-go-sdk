# Verify Go source format and static correctness.
lint:
    #!/usr/bin/env bash
    set -euo pipefail
    files=$(gofmt -l .)
    if [ -n "$files" ]; then printf '%s\n' "$files"; exit 1; fi
    go vet ./...
