//go:build installed_process_tests

package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

func processGuard(t *testing.T) {
	t.Helper()
	if os.Getenv("OBEY_SDK_TEST_CONTAINER") != "1" {
		t.Fatal("container fixture guard required")
	}
	if _, err := os.Stat("/.dockerenv"); err != nil {
		t.Fatal("Docker marker required")
	}
}

func TestInstalledLifecycleChild(t *testing.T) {
	mode := os.Getenv("INSTALLED_LIFECYCLE_CHILD")
	if mode == "" {
		return
	}
	if mode == "exit" {
		os.Exit(0)
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	// A pipe handshake proves the signal handler is installed before parent stop.
	_, _ = os.Stdout.Write([]byte("ready\n"))
	<-signals
	os.Exit(0)
}

func launchLifecycleChild(t *testing.T, mode string) *ownedDaemon {
	t.Helper()
	processGuard(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestInstalledLifecycleChild$")
	cmd.Env = append(os.Environ(), "INSTALLED_LIFECYCLE_CHILD="+mode)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	d := &ownedDaemon{cmd: cmd, done: make(chan struct{})}
	if mode == "signal" {
		var ready [6]byte
		if _, err := io.ReadFull(stdout, ready[:]); err != nil {
			cancel()
			t.Fatal(err)
		}
	}
	go func() { d.waitErr = cmd.Wait(); close(d.done) }()
	t.Cleanup(func() { _ = d.stop(); cancel() })
	return d
}

func TestInstalledLifecycleRejectsExitedChild(t *testing.T) {
	d := launchLifecycleChild(t, "exit")
	select {
	case <-d.done:
	case <-time.After(5 * time.Second):
		t.Fatal("child did not exit")
	}
	if err := d.stop(); err == nil {
		t.Fatal("unexpected exit0 was accepted as intentional shutdown")
	}
}
func TestInstalledLifecycleGracefulSignalAndReap(t *testing.T) {
	d := launchLifecycleChild(t, "signal")
	if err := d.stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-d.done:
	default:
		t.Fatal("shutdown returned without reaping")
	}
	if err := d.stop(); err != nil {
		t.Fatalf("repeat cleanup: %v", err)
	}
}
