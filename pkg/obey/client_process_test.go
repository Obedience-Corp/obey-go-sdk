//go:build process_tests

package obey

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Obedience-Corp/obey-go-sdk/internal/execclient"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func init() {
	if os.Getenv("OBEY_SDK_PUBLIC_FAKE") != "1" {
		return
	}
	if os.Getenv("OBEY_SDK_TEST_CONTAINER") != "1" {
		os.Exit(99)
	}
	args := os.Args[1:]
	if len(args) > 1 && args[0] == "--socket" {
		args = args[2:]
	}
	if path := os.Getenv("SDK_CALL_LEDGER"); path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			os.Exit(98)
		}
		fmt.Fprintln(f, args[0])
		_ = f.Close()
	}
	if len(args) > 0 && args[0] == "doctor" {
		fmt.Println(`{"schema":"obey.doctor","version":3,"status":"fail","capabilities":{"session_run":{"protocol":"obey.session.run","versions":[1]}}}`)
		os.Exit(1)
	}
	emit := func(kind string, fields map[string]any) {
		fields["protocol"] = "obey.session.run"
		fields["version"] = 1
		fields["type"] = kind
		_ = json.NewEncoder(os.Stdout).Encode(fields)
	}
	mode := os.Getenv("SDK_FAKE_MODE")
	if mode == "no-daemon" {
		emit("error", map[string]any{"code": "create_session", "message": "daemon unavailable"})
		emit("terminal", map[string]any{"status": "failed"})
		os.Exit(7)
	}
	var signals chan os.Signal
	if mode == "cancel" || mode == "block" {
		signals = make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGTERM)
	}
	emit("session_created", map[string]any{"session_id": "s1", "campaign_id": "c1"})
	if mode == "malformed" {
		fmt.Fprintln(os.Stdout, "{broken")
		os.Exit(0)
	}
	if mode == "version" {
		fmt.Fprintln(os.Stdout, `{"protocol":"obey.session.run","version":99,"type":"future"}`)
		os.Exit(0)
	}
	if mode == "oversize" {
		fmt.Fprintln(os.Stdout, strings.Repeat("x", 1024))
		os.Exit(0)
	}
	if signals != nil {
		<-signals
		time.Sleep(30 * time.Millisecond)
		emit("error", map[string]any{"session_id": "s1", "code": "canceled", "message": "cancelled"})
		emit("terminal", map[string]any{"session_id": "s1", "status": "canceled"})
		os.Exit(7)
	}
	if mode == "provider-error" {
		emit("error", map[string]any{"session_id": "s1", "code": "activity_error", "message": "provider refused", "daemon_code": 5011})
		emit("terminal", map[string]any{"session_id": "s1", "status": "failed"})
		os.Exit(8)
	}
	input, _ := io.ReadAll(os.Stdin)
	content := string(input)
	if mode == "capture" {
		cwd, _ := os.Getwd()
		data, _ := json.Marshal(map[string]any{"args": args, "stdin": string(input), "env": os.Getenv("SDK_VALUE"), "cwd": cwd})
		content = string(data)
	}
	if mode == "empty" {
		content = ""
	}
	emit("assistant_content", map[string]any{"session_id": "s1", "content": content, "final": true})
	emit("usage", map[string]any{"session_id": "s1", "input_tokens": 3, "output_tokens": 2, "cached_tokens": int64(9007199254740993), "latency_ms": 12})
	if mode == "warning" {
		emit("error", map[string]any{"session_id": "s1", "code": "stop_session", "message": "stop failed"})
	}
	emit("terminal", map[string]any{"session_id": "s1", "status": "completed"})
	os.Exit(0)
}
func publicGuard(t *testing.T) {
	t.Helper()
	if os.Getenv("OBEY_SDK_TEST_CONTAINER") != "1" {
		t.Fatal("public subprocess tests require just test process")
	}
}
func fakeClient(t *testing.T, mode string, options ...ClientOption) *Client {
	t.Helper()
	publicGuard(t)
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	options = append([]ClientOption{WithSessionDefaults(SessionOptions{Provider: "chosen-provider", Model: "chosen-model"})}, options...)
	options = append(options, WithEnvironment(map[string]string{"OBEY_SDK_PUBLIC_FAKE": "1", "SDK_FAKE_MODE": mode, "SDK_VALUE": "override"}))
	c, err := NewClient(bin, options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}
func TestProcessPublicRunAndSnapshots(t *testing.T) {
	for _, mode := range []string{"normal", "empty", "warning"} {
		t.Run(mode, func(t *testing.T) {
			c := fakeClient(t, mode)
			s, err := c.StreamPrompt(t.Context(), "exact\nbytes", nil)
			if err != nil {
				t.Fatal(err)
			}
			if s.Fidelity() != FidelityFinalOnly {
				t.Fatal(s.Fidelity())
			}
			var events int
			for s.Next() {
				events++
				e := s.Event()
				if len(e.RawRecord) > 0 {
					e.RawRecord[0] = 'x'
				}
				if !json.Valid(s.Event().RawRecord) {
					t.Fatal("event aliases snapshot")
				}
			}
			if s.Err() != nil {
				t.Fatal(s.Err())
			}
			r, err := s.Result()
			if err != nil {
				t.Fatal(err)
			}
			if r.Session.SessionID != "s1" || r.Usage.CachedTokens != 9007199254740993 || events < 4 {
				t.Fatalf("%+v events%d", r, events)
			}
			if mode == "empty" && r.Text != "" {
				t.Fatal(r.Text)
			}
			if mode == "warning" && r.Cleanup != CleanupStopFailed {
				t.Fatal(r.Cleanup)
			}
			r.Text = "changed"
			if len(r.Diagnostics) > 0 {
				r.Diagnostics[0].Message = "changed"
			}
			again, err := s.Result()
			if err != nil || again.Text == "changed" {
				t.Fatal(again, err)
			}
		})
	}
}
func TestProcessPublicCancelDrainsCleanup(t *testing.T) {
	c := fakeClient(t, "cancel", WithLimits(Limits{EventQueueSize: 1, ShutdownGrace: 2 * time.Second}))
	ctx, cancel := context.WithCancel(t.Context())
	s, err := c.StreamPrompt(ctx, "prompt", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Next() || s.Event().Kind != EventSessionCreated {
		t.Fatal(s.Err())
	}
	cancel()
	for s.Next() {
	}
	r, err := s.Result()
	var e *Error
	if !errors.As(err, &e) || e.Code != "canceled" || !errors.Is(err, context.Canceled) || r.Session.SessionID != "s1" || r.Cleanup != CleanupStopped {
		t.Fatalf("%+v %v", r, err)
	}
	if e.ExitCode == nil || *e.ExitCode != 7 {
		t.Fatalf("%+v", e)
	}
}
func TestProcessPublicCopiesArgvAndDefaults(t *testing.T) {
	publicGuard(t)
	cwd := t.TempDir()
	config := json.RawMessage(`{"nested":{"v":1}}`)
	defaults := SessionOptions{Provider: "p", Model: "m", Config: config}
	c := fakeClient(t, "capture", WithSessionDefaults(defaults), WithWorkingDirectory(cwd), WithCamp("camp-default"), WithSocket("/chosen/socket"))
	config[0] = 'x'
	timeout := time.Duration(0)
	options := &RunOptions{SessionOptions: SessionOptions{Camp: "explicit", WorkingDirectory: "agent-work"}, Timeout: &timeout, Files: []string{"file;one"}}
	s, err := c.StreamPrompt(t.Context(), "$(echo no shell)\n'", options)
	if err != nil {
		t.Fatal(err)
	}
	options.Files[0] = "mutated"
	options.Camp = "mutated"
	timeout = time.Second
	r, err := s.Result()
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Args            []string
		Stdin, Env, Cwd string
	}
	if err := json.Unmarshal([]byte(r.Text), &got); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got.Args, "|")
	for _, text := range []string{"--campaign|explicit", "--workdir|agent-work", "--timeout|0s", "--file|file;one", "--config|{\"nested\":{\"v\":1}}"} {
		if !strings.Contains(joined, text) {
			t.Fatalf("missing%s in%s", text, joined)
		}
	}
	if strings.Contains(joined, "--protocol-version") || strings.Contains(joined, "echo no shell") || got.Stdin != "$(echo no shell)\n'" || got.Env != "override" || got.Cwd != cwd {
		t.Fatalf("%+v", got)
	}
}
func TestProcessPublicValidationAndConstructor(t *testing.T) {
	publicGuard(t)
	bin, _ := os.Executable()
	ledger := filepath.Join(t.TempDir(), "calls")
	c, err := NewClient(bin, WithEnvironment(map[string]string{"OBEY_SDK_PUBLIC_FAKE": "1", "SDK_CALL_LEDGER": ledger}))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := os.Stat(ledger); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("constructor spawned CLI")
	}
	if _, err := c.RunPrompt(t.Context(), "prompt", nil); err == nil {
		t.Fatal("missing model/provider accepted")
	}
	if _, err := os.Stat(ledger); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("validation spawned CLI")
	}
	for _, config := range []json.RawMessage{[]byte(`[]`), []byte(`null`), []byte(`"text"`)} {
		if _, err := NewClient(bin, WithSessionDefaults(SessionOptions{Config: config})); err == nil {
			t.Fatal("non-object config accepted")
		}
	}
	c2 := fakeClient(t, "normal")
	if _, err := c2.StreamPrompt(t.Context(), "prompt", &RunOptions{RequireLiveEvents: true}); err == nil {
		t.Fatal("v1 falsely live")
	}
	if err := c2.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := c2.RunPrompt(t.Context(), "prompt", nil); err == nil {
		t.Fatal("closed client accepted")
	}
}
func TestProcessPublicErrorsAndMisuse(t *testing.T) {
	for _, mode := range []string{"provider-error", "no-daemon"} {
		t.Run(mode, func(t *testing.T) {
			c := fakeClient(t, mode)
			r, err := c.RunPrompt(t.Context(), "prompt", nil)
			var e *Error
			if !errors.As(err, &e) || e.ExitCode == nil {
				t.Fatal(r, err)
			}
			if mode == "provider-error" && (r.Session.SessionID != "s1" || e.DaemonCode == nil || *e.DaemonCode != 5011) {
				t.Fatalf("%+v %v", r, err)
			}
		})
	}
	c := fakeClient(t, "block", WithLimits(Limits{ShutdownGrace: 100 * time.Millisecond}))
	s, err := c.StreamPrompt(t.Context(), "prompt", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Next() {
		t.Fatal(s.Err())
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); s.Next() }()
	deadline := time.Now().Add(time.Second)
	for !s.active.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if _, err := s.Result(); err == nil {
		t.Fatal("concurrent consumer accepted")
	}
	_ = s.Close()
	wg.Wait()
	_, _ = s.Result()
}
func TestProcessEnvironmentOptionCopies(t *testing.T) {
	publicGuard(t)
	env := map[string]string{"OBEY_SDK_PUBLIC_FAKE": "1", "SDK_VALUE": "original", "SDK_FAKE_MODE": "capture"}
	option := WithEnvironment(env)
	env["SDK_VALUE"] = "mutated"
	bin, _ := os.Executable()
	c, err := NewClient(bin, option, WithSessionDefaults(SessionOptions{Provider: "p", Model: "m"}))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r, err := c.RunPrompt(t.Context(), "prompt", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains([]byte(r.Text), []byte(`"env":"original"`)) {
		t.Fatal(r.Text)
	}
}

func TestProcessPublicErrWithoutResult(t *testing.T) {
	for _, tc := range []struct{ mode, code string }{{"malformed", "protocol_error"}, {"version", "unsupported_protocol"}, {"oversize", "output_limit"}, {"provider-error", "activity_error"}} {
		t.Run(tc.mode, func(t *testing.T) {
			c := fakeClient(t, tc.mode, WithLimits(Limits{MaxRecordBytes: 512, ShutdownGrace: 100 * time.Millisecond}))
			s, err := c.StreamPrompt(t.Context(), "prompt", nil)
			if err != nil {
				t.Fatal(err)
			}
			for s.Next() {
			}
			var before *Error
			if !errors.As(s.Err(), &before) || before.Code != tc.code || before.Session.SessionID != "s1" {
				t.Fatalf("before %+v", s.Err())
			}
			_, err = s.Result()
			var after *Error
			if !errors.As(err, &after) || after.Code != before.Code {
				t.Fatalf("before%+v after%v", before, err)
			}
			if tc.mode == "oversize" && (!errors.Is(before, execclient.ErrOutputLimit) || !errors.Is(after, execclient.ErrOutputLimit)) {
				t.Fatal("lost output-limit cause")
			}
		})
	}
}

func TestProcessPublicFinalContentLimitIsSeparate(t *testing.T) {
	c := fakeClient(t, "normal", WithLimits(Limits{MaxResultBytes: 3, ShutdownGrace: 100 * time.Millisecond}))
	r, err := c.RunPrompt(t.Context(), "ok", nil)
	if err != nil || r.Text != "ok" {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = c.RunPrompt(t.Context(), "four", nil)
	var e *Error
	if !errors.As(err, &e) || e.Code != "output_limit" || r.Session.SessionID != "s1" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestProcessPublicLiveRejectionDoesNotRun(t *testing.T) {
	publicGuard(t)
	bin, _ := os.Executable()
	ledger := filepath.Join(t.TempDir(), "calls")
	c, err := NewClient(bin, WithSessionDefaults(SessionOptions{Provider: "p", Model: "m"}), WithEnvironment(map[string]string{"OBEY_SDK_PUBLIC_FAKE": "1", "SDK_CALL_LEDGER": ledger}))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, err = c.StreamPrompt(t.Context(), "prompt", &RunOptions{RequireLiveEvents: true})
	var e *Error
	if !errors.As(err, &e) || e.Code != "unsupported_capability" {
		t.Fatal(err)
	}
	data, err := os.ReadFile(ledger)
	if err != nil || strings.TrimSpace(string(data)) != "doctor" {
		t.Fatalf("calls=%q err=%v", data, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = c.RunPrompt(ctx, "prompt", nil)
	if !errors.As(err, &e) || e.Code != "canceled" || !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(ledger)
	if strings.TrimSpace(string(data)) != "doctor" {
		t.Fatalf("pre-canceled operation spawned: %q", data)
	}
}

func TestProcessPublicFinishedBufferedCancellation(t *testing.T) {
	c := fakeClient(t, "normal")
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithCancel(t.Context())
		s, err := c.StreamPrompt(ctx, "buffered-answer", nil)
		if err != nil {
			t.Fatal(err)
		}
		if out := s.process.Wait(); out.Err != nil {
			t.Fatal(out)
		}
		cancel()
		for s.Next() {
		}
		r, err := s.Result()
		if err != nil || r.Text != "buffered-answer" || r.Cleanup != CleanupStopped {
			t.Fatalf("iteration%d result%+v err%v", i, r, err)
		}
	}
}
