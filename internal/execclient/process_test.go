//go:build process_tests

package execclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func guard(t *testing.T) {
	t.Helper()
	if os.Getenv("OBEY_SDK_TEST_CONTAINER") != "1" {
		t.Fatal("process fixtures require just test process in container")
	}
}

// The test executable is itself the fake CLI: it exercises real fork/exec and
// pipes without generating shell scripts or trusting shell quoting.
func TestProcessHelper(t *testing.T) {
	mode := os.Getenv("OBEY_SDK_HELPER")
	if mode == "" {
		return
	}
	guard(t)
	switch mode {
	case "echo":
		data, _ := io.ReadAll(os.Stdin)
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"args": os.Args[1:], "stdin": string(data), "env": os.Getenv("SDK_VALUE")})
	case "fragment":
		fmt.Fprint(os.Stdout, "{\"x\":")
		time.Sleep(10 * time.Millisecond)
		fmt.Fprint(os.Stdout, "1}")
	case "stderr":
		_, _ = os.Stderr.Write(bytes.Repeat([]byte("z"), 200000))
		fmt.Fprint(os.Stderr, "distinctive-final-suffix")
		fmt.Fprint(os.Stdout, "{}\n")
	case "huge":
		chunk := bytes.Repeat([]byte("x"), 32<<10)
		for written := 0; written < 64<<20; written += len(chunk) {
			if _, err := os.Stdout.Write(chunk); err != nil {
				os.Exit(1)
			}
		}
		_, _ = os.Stdout.Write([]byte("x"))
	case "flood":
		for {
			if _, err := fmt.Fprintln(os.Stdout, strings.Repeat("x", 4096)); err != nil {
				break
			}
		}
	case "ignore":
		signal.Ignore(syscall.SIGTERM)
		fmt.Fprintln(os.Stdout, "ready")
		time.Sleep(time.Minute)
	case "grace":
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGTERM)
		fmt.Fprintln(os.Stdout, "ready")
		<-ch
		fmt.Fprintln(os.Stdout, "terminal")
	}
	os.Exit(0)
}
func startHelper(t *testing.T, mode string, limits Limits, stdin string, extra ...string) (*Runner, *Process) {
	t.Helper()
	guard(t)
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(bin, limits)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	args := append([]string{"-test.run=^TestProcessHelper$", "--"}, extra...)
	p, err := r.Start(t.Context(), Request{Args: args, Env: []string{"OBEY_SDK_HELPER=" + mode, "SDK_VALUE=replaced"}, Stdin: strings.NewReader(stdin)})
	if err != nil {
		t.Fatal(err)
	}
	return r, p
}
func TestProcessBytesAndEnvironment(t *testing.T) {
	input := "$(touch should-never-exist)\nquotes '\"; & |"
	_, p := startHelper(t, "echo", Limits{}, input, "$(echo bad)", ";*")
	record, err := p.Next(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Args       []string
		Stdin, Env string
	}
	if err := json.Unmarshal(record, &got); err != nil {
		t.Fatal(err)
	}
	if got.Stdin != input || got.Env != "replaced" || got.Args[len(got.Args)-2] != "$(echo bad)" || got.Args[len(got.Args)-1] != ";*" {
		t.Fatalf("%+v", got)
	}
	if outcome := p.Wait(); outcome.Err != nil || outcome.ExitCode != 0 {
		t.Fatal(outcome)
	}
}
func TestProcessFragmentAndStderr(t *testing.T) {
	for _, mode := range []string{"fragment", "stderr"} {
		t.Run(mode, func(t *testing.T) {
			_, p := startHelper(t, mode, Limits{StderrTailBytes: 1024}, "")
			if _, err := p.Next(t.Context()); err != nil {
				t.Fatal(err)
			}
			out := p.Wait()
			if out.Err != nil {
				t.Fatal(out)
			}
			if mode == "stderr" && (len(out.StderrTail) != 1024 || !strings.HasSuffix(out.StderrTail, "distinctive-final-suffix")) {
				t.Fatal(len(out.StderrTail))
			}
		})
	}
}
func TestProcessLimitAndBlockedConsumer(t *testing.T) {
	for _, limits := range []Limits{{MaxRecordBytes: 64, ShutdownGrace: 100 * time.Millisecond}, {EventQueueSize: 1, ShutdownGrace: 100 * time.Millisecond}} {
		r, p := startHelper(t, "flood", limits, "")
		time.Sleep(30 * time.Millisecond)
		_ = r.Close()
		out := p.Wait()
		if out.Err == nil {
			t.Fatal("expected cancellation or limit")
		}
		if err := p.cmd.Process.Signal(syscall.Signal(0)); err == nil {
			t.Fatal("child alive after close")
		}
	}
}
func TestProcessCancellationAndConcurrentClose(t *testing.T) {
	r, p := startHelper(t, "ignore", Limits{ShutdownGrace: 100 * time.Millisecond}, "")
	if _, err := p.Next(t.Context()); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = r.Close(); _ = p.Close() }()
	}
	wg.Wait()
	if time.Since(start) > 3*time.Second {
		t.Fatal("close unbounded")
	}
	out := p.Wait()
	if !errors.Is(out.Err, context.Canceled) {
		t.Fatal(out)
	}
	if _, err := r.Start(t.Context(), Request{}); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}
func TestProcessLimit(t *testing.T) {
	_, p := startHelper(t, "flood", Limits{MaxRecordBytes: 64, ShutdownGrace: 100 * time.Millisecond}, "")
	out := p.Wait()
	if !errors.Is(out.Err, ErrOutputLimit) {
		t.Fatal(out)
	}
}

func TestProcessGracefulDrain(t *testing.T) {
	_, p := startHelper(t, "grace", Limits{ShutdownGrace: 2 * time.Second}, "")
	if _, err := p.Next(t.Context()); err != nil {
		t.Fatal(err)
	}
	_ = p.Close()
	record, err := p.Next(context.Background())
	if err != nil || string(record) != "terminal" {
		t.Fatalf("cleanup record=%q err=%v", record, err)
	}
	out := p.Wait()
	if out.ExitCode != 0 || !errors.Is(out.Err, context.Canceled) {
		t.Fatal(out)
	}
}
func TestProcessStartupAndPrecancel(t *testing.T) {
	guard(t)
	r, _ := New("/does-not-exist", Limits{})
	defer r.Close()
	if _, err := r.Start(t.Context(), Request{}); err == nil {
		t.Fatal("missing binary started")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := r.Start(ctx, Request{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestProcessBlockedNextCancellation(t *testing.T) {
	_, p := startHelper(t, "ignore", Limits{ShutdownGrace: 100 * time.Millisecond}, "")
	if _, err := p.Next(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := p.Next(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if out := p.Wait(); !errors.Is(out.Err, context.DeadlineExceeded) {
		t.Fatal(out)
	}
}

func TestProcessCleanExitEOF(t *testing.T) {
	_, p := startHelper(t, "fragment", Limits{}, "")
	if _, err := p.Next(t.Context()); err != nil {
		t.Fatal(err)
	}
	if out := p.Wait(); out.Err != nil {
		t.Fatal(out)
	}
	for i := 0; i < 1000; i++ {
		if _, err := p.Next(context.Background()); err != io.EOF {
			t.Fatalf("iteration%d: %v", i, err)
		}
	}
}

func TestProcessDefaultRecordLimit(t *testing.T) {
	_, p := startHelper(t, "huge", Limits{ShutdownGrace: 100 * time.Millisecond}, "")
	if out := p.Wait(); !errors.Is(out.Err, ErrOutputLimit) {
		t.Fatal(out)
	}
}
