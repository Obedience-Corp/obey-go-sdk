package execclient

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrClosed      = errors.New("process runner closed")
	ErrOutputLimit = errors.New("process output limit exceeded")
)

type Request struct {
	Args  []string
	Dir   string
	Env   []string
	Stdin io.Reader
}
type Outcome struct {
	ExitCode   int
	StderrTail string
	Err        error
}

type Runner struct {
	binary   string
	limits   Limits
	mu       sync.Mutex
	closed   bool
	children map[*Process]struct{}
}

func New(binary string, limits Limits) (*Runner, error) {
	if !filepath.IsAbs(binary) {
		return nil, errors.New("CLI binary must be absolute")
	}
	l, err := limits.normalized()
	if err != nil {
		return nil, err
	}
	return &Runner{binary: binary, limits: l, children: make(map[*Process]struct{})}, nil
}

type Process struct {
	cmd     *exec.Cmd
	ctx     context.Context
	cancel  context.CancelCauseFunc
	records chan []byte
	done    chan struct{}
	mu      sync.Mutex
	outcome Outcome
}

func mergedEnv(overrides []string) []string {
	values := append([]string(nil), os.Environ()...)
	for _, entry := range overrides {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		prefix := key + "="
		replaced := false
		for i, old := range values {
			if strings.HasPrefix(old, prefix) {
				values[i] = entry
				replaced = true
				break
			}
		}
		if !replaced {
			values = append(values, entry)
		}
	}
	return values
}

func (r *Runner) Start(ctx context.Context, req Request) (*Process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, ErrClosed
	}
	childCtx, cancel := context.WithCancelCause(ctx)
	cmd := exec.Command(r.binary, append([]string(nil), req.Args...)...)
	cmd.Dir = req.Dir
	cmd.Env = mergedEnv(append([]string(nil), req.Env...))
	cmd.Stdin = req.Stdin
	cmd.WaitDelay = r.limits.ShutdownGrace
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel(err)
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdout.Close()
		cancel(err)
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		cancel(err)
		return nil, fmt.Errorf("start CLI: %w", err)
	}
	p := &Process{cmd: cmd, ctx: childCtx, cancel: cancel, records: make(chan []byte, r.limits.EventQueueSize), done: make(chan struct{})}
	r.children[p] = struct{}{}
	go r.run(p, stdout, stderr)
	return p, nil
}

func (r *Runner) run(p *Process, stdout, stderr io.ReadCloser) {
	var tail tailBuffer
	tail.limit = r.limits.StderrTailBytes
	stderrDone := make(chan struct{})
	go func() { defer close(stderrDone); _, _ = io.Copy(&tail, stderr) }()
	stopWatcher := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-p.ctx.Done():
			_ = terminate(p.cmd.Process)
			timer := time.NewTimer(r.limits.ShutdownGrace)
			defer timer.Stop()
			select {
			case <-stopWatcher:
			case <-timer.C:
				_ = p.cmd.Process.Kill()
				_ = stdout.Close()
				_ = stderr.Close()
			}
		case <-stopWatcher:
		}
	}()
	readErr := readRecords(stdout, r.limits, func(record []byte) error {
		select {
		case p.records <- record:
			return nil
		case <-p.ctx.Done():
			// Keep draining during graceful shutdown even if the consumer has
			// detached. Never block cleanup behind a full application queue.
			select {
			case p.records <- record:
			default:
			}
			return nil
		}
	})
	if readErr != nil {
		p.cancel(readErr)
		_, _ = io.Copy(io.Discard, stdout)
	}
	// The sole Wait owner runs after the stdout reader. Cancellation interrupts
	// blocked consumers and the watcher bounds a child that ignores termination.
	<-stderrDone
	waitErr := p.cmd.Wait()
	close(stopWatcher)
	<-watcherDone
	_ = stdout.Close()
	_ = stderr.Close()
	cause := context.Cause(p.ctx)
	p.mu.Lock()
	p.outcome = Outcome{ExitCode: p.cmd.ProcessState.ExitCode(), StderrTail: tail.String(), Err: errors.Join(cause, readErr, waitErr)}
	p.mu.Unlock()
	close(p.records)
	r.mu.Lock()
	delete(r.children, p)
	r.mu.Unlock()
	close(p.done)
	p.cancel(nil)
}

func readRecords(in io.Reader, l Limits, emit func([]byte) error) error {
	reader := bufio.NewReaderSize(in, 32<<10)
	var record []byte
	for {
		fragment, err := reader.ReadSlice('\n')
		if int64(len(record))+int64(len(fragment)) > l.MaxRecordBytes {
			return ErrOutputLimit
		}
		record = append(record, fragment...)
		if err == bufio.ErrBufferFull {
			continue
		}
		if len(record) > 0 {
			if record[len(record)-1] == '\n' {
				record = record[:len(record)-1]
			}
			if e := emit(record); e != nil {
				return e
			}
			record = nil
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func (p *Process) Next(ctx context.Context) ([]byte, error) {
	select {
	case record, ok := <-p.records:
		if ok {
			return record, nil
		}
		return nil, p.finalError()
	case <-ctx.Done():
		p.cancel(ctx.Err())
		return nil, ctx.Err()
	case <-p.ctx.Done():
		// Drain already available records before exposing cancellation; a CLI may
		// still produce its confirming cleanup terminal during the grace period.
		select {
		case record, ok := <-p.records:
			if ok {
				return record, nil
			}
			return nil, p.finalError()
		default:
		}
		return nil, context.Cause(p.ctx)
	}
}
func (p *Process) Wait() Outcome { <-p.done; p.mu.Lock(); defer p.mu.Unlock(); return p.outcome }
func (p *Process) Close() error  { p.cancel(context.Canceled); <-p.done; return nil }
func (r *Runner) Close() error {
	r.mu.Lock()
	r.closed = true
	children := make([]*Process, 0, len(r.children))
	for p := range r.children {
		children = append(children, p)
		p.cancel(context.Canceled)
	}
	r.mu.Unlock()
	for _, p := range children {
		<-p.done
	}
	return nil
}

type tailBuffer struct {
	limit int
	data  []byte
}

func (b *tailBuffer) Write(data []byte) (int, error) {
	n := len(data)
	if n >= b.limit {
		b.data = append(b.data[:0], data[n-b.limit:]...)
	} else {
		if extra := len(b.data) + n - b.limit; extra > 0 {
			copy(b.data, b.data[extra:])
			b.data = b.data[:len(b.data)-extra]
		}
		b.data = append(b.data, data...)
	}
	return n, nil
}
func (b *tailBuffer) String() string { return string(b.data) }

func (p *Process) finalError() error {
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.outcome.Err != nil {
		return p.outcome.Err
	}
	return io.EOF
}
