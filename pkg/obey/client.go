package obey

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/obediencecorp/obey-go-sdk/internal/execclient"
)

type Client struct {
	binary          string
	config          clientConfig
	env             []string
	runner          *execclient.Runner
	cache           capabilityCache
	mu              sync.Mutex
	closed          bool
	identityVersion uint64
	fileInfo        os.FileInfo
}

func NewClient(binaryPath string, options ...ClientOption) (*Client, error) {
	path, err := exec.LookPath(binaryPath)
	if err != nil {
		return nil, &Error{Code: "binary_not_found", Operation: "construct", Cause: err}
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, &Error{Code: "invalid_options", Operation: "construct", Cause: err}
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || (runtime.GOOS != "windows" && info.Mode()&0111 == 0) {
		if err == nil {
			err = errors.New("binary is not executable")
		}
		return nil, &Error{Code: "binary_not_found", Operation: "construct", Cause: err}
	}
	cfg := clientConfig{}
	for _, option := range options {
		if option != nil {
			if err := option(&cfg); err != nil {
				return nil, &Error{Code: "invalid_options", Operation: "construct", Cause: err}
			}
		}
	}
	if err := validateConfig(cfg.defaults.Config); err != nil {
		return nil, &Error{Code: "invalid_options", Operation: "construct", Cause: err}
	}
	runner, err := execclient.New(path, execclient.Limits{MaxShutdownBytes: cfg.limits.MaxShutdownBytes, MaxRecordBytes: cfg.limits.MaxRecordBytes, MaxResultBytes: cfg.limits.MaxResultBytes, StderrTailBytes: cfg.limits.StderrTailBytes, EventQueueSize: cfg.limits.EventQueueSize, ShutdownGrace: cfg.limits.ShutdownGrace})
	if err != nil {
		return nil, &Error{Code: "invalid_options", Operation: "construct", Cause: err}
	}
	keys := make([]string, 0, len(cfg.env))
	for k := range cfg.env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, k := range keys {
		env = append(env, k+"="+cfg.env[k])
	}
	return &Client{binary: path, config: cfg, env: env, runner: runner}, nil
}
func NewClientFromPath(options ...ClientOption) (*Client, error) {
	return NewClient("obey", options...)
}
func (c *Client) checkOpen() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return &Error{Code: "closed_client", Operation: "client"}
	}
	return nil
}
func (c *Client) Close() error { c.mu.Lock(); c.closed = true; c.mu.Unlock(); return c.runner.Close() }
func (c *Client) identity() (string, error) {
	info, err := os.Stat(c.binary)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fileInfo == nil || !os.SameFile(info, c.fileInfo) || info.ModTime() != c.fileInfo.ModTime() || info.Size() != c.fileInfo.Size() {
		c.identityVersion++
		c.fileInfo = info
	}
	return fmt.Sprintf("%s:%d", c.binary, c.identityVersion), nil
}
func (c *Client) start(ctx context.Context, args []string, stdin io.Reader) (*execclient.Process, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	if c.config.socket != "" {
		args = append([]string{"--socket", c.config.socket}, args...)
	}
	p, err := c.runner.Start(ctx, execclient.Request{Args: args, Dir: c.config.cwd, Env: c.env, Stdin: stdin})
	if err != nil {
		code := "command_error"
		if errors.Is(err, context.DeadlineExceeded) {
			code = "timeout"
		} else if errors.Is(err, context.Canceled) {
			code = "canceled"
		} else if errors.Is(err, os.ErrNotExist) {
			code = "binary_not_found"
		}
		if errors.Is(err, execclient.ErrClosed) {
			code = "closed_client"
		}
		return nil, &Error{Code: code, Operation: "start", Cause: err}
	}
	return p, nil
}
func (c *Client) Capabilities(ctx context.Context) (*Capabilities, error) {
	return c.capabilities(ctx, false)
}
func (c *Client) RefreshCapabilities(ctx context.Context) (*Capabilities, error) {
	return c.capabilities(ctx, true)
}
func (c *Client) capabilities(ctx context.Context, refresh bool) (*Capabilities, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	identity, err := c.identity()
	if err != nil {
		return nil, &Error{Code: "binary_not_found", Operation: "capabilities", Cause: err}
	}
	if !refresh {
		if value := c.cache.load(identity); value != nil {
			return value, nil
		}
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	p, err := c.start(bounded, []string{"doctor", "--json"}, nil)
	if err != nil {
		return nil, err
	}
	var document []byte
	limit := c.config.limits.MaxRecordBytes
	if limit == 0 {
		limit = execclient.DefaultBytes
	}
	for {
		record, readErr := p.DrainNext()
		if readErr != nil {
			break
		}
		if int64(len(document))+int64(len(record))+1 > limit {
			p.Cancel(execclient.ErrOutputLimit)
			_ = p.Close()
			return nil, &Error{Code: "output_limit", Operation: "capabilities", Cause: execclient.ErrOutputLimit}
		}
		document = append(document, record...)
		document = append(document, '\n')
	}
	out := p.Wait()
	if errors.Is(out.Err, context.Canceled) || errors.Is(out.Err, context.DeadlineExceeded) || errors.Is(out.Err, execclient.ErrOutputLimit) || out.ExitCode < 0 || (out.Err != nil && !exitOnly(out.Err)) {
		code := "command_error"
		if errors.Is(out.Err, context.DeadlineExceeded) {
			code = "timeout"
		} else if errors.Is(out.Err, context.Canceled) {
			code = "canceled"
		} else if errors.Is(out.Err, execclient.ErrOutputLimit) {
			code = "output_limit"
		}
		return nil, &Error{Code: code, Operation: "capabilities", ExitCode: &out.ExitCode, StderrTail: out.StderrTail, Cause: out.Err}
	}
	if err := c.cache.store(identity, document); err != nil {
		return nil, &Error{Code: "unsupported_protocol", Operation: "capabilities", ExitCode: &out.ExitCode, StderrTail: out.StderrTail, Cause: err}
	}
	return c.cache.load(identity), nil
}
func supports(c *Capabilities, key, name string, version int) bool {
	s, ok := c.Protocols[key]
	if !ok || s.Protocol != name {
		return false
	}
	for _, v := range s.Versions {
		if v == version {
			return true
		}
	}
	return false
}

func exitOnly(err error) bool {
	if _, ok := err.(*exec.ExitError); ok {
		return true
	}
	if many, ok := err.(interface{ Unwrap() []error }); ok {
		children := many.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !exitOnly(child) {
				return false
			}
		}
		return true
	}
	if one, ok := err.(interface{ Unwrap() error }); ok {
		return exitOnly(one.Unwrap())
	}
	return false
}
