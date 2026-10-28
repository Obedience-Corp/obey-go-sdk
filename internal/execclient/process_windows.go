package execclient

import (
	"errors"
	"os"
)

// Windows has no equivalent child-only graceful SIGTERM contract. Kill/reap is
// available, but graceful CLI cleanup is unsupported and remains unconfirmed.
func terminate(p *os.Process) error {
	return errors.New("graceful CLI termination is unsupported on Windows")
}
