// This independent consumer exercises only an installed Obey binary and SDK APIs.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	obey "github.com/Obedience-Corp/obey-go-sdk/pkg/obey"
)

const answer = "deterministic fixture response"
const fixtureKey = "fixture-not-a-secret"
const providerName = "fixture-openai"
const modelName = "fixture-model"

type options struct {
	binary, camp, workdir, evidence string
	timeout                         time.Duration
	requirePersistent               bool
}
type sessionInfo struct {
	ID        string `json:"id"`
	CampID    string `json:"campaign_id"`
	Status    string `json:"status"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	TurnCount uint64 `json:"turn_count"`
}
type report struct {
	Success               bool                            `json:"success"`
	Error                 string                          `json:"error,omitempty"`
	ObeyVersion           string                          `json:"obey_version,omitempty"`
	Protocols             map[string]obey.ProtocolSupport `json:"protocols,omitempty"`
	RunProtocol           int                             `json:"run_protocol,omitempty"`
	RunSession            obey.SessionRef                 `json:"run_session"`
	RunCleanup            obey.CleanupDisposition         `json:"run_cleanup,omitempty"`
	LiveEvents            string                          `json:"live_events"`
	LiveEventsErrorCode   string                          `json:"live_events_error_code,omitempty"`
	Persistent            string                          `json:"persistent"`
	PersistentErrorCode   string                          `json:"persistent_error_code,omitempty"`
	PersistentRef         *obey.TurnRef                   `json:"persistent_ref,omitempty"`
	SessionIDsBefore      []string                        `json:"session_ids_before,omitempty"`
	SessionIDsAfter       []string                        `json:"session_ids_after,omitempty"`
	ProviderRequests      int                             `json:"provider_requests"`
	DaemonUnavailableCode string                          `json:"daemon_unavailable_code,omitempty"`
	NoAutoStart           bool                            `json:"no_auto_start"`
}
type probe struct {
	opts                   options
	binary, config, socket string
	fixture                *completionFixture
	report                 *report
}

func main() {
	var o options
	flag.StringVar(&o.binary, "binary", "obey", "installed Obey executable")
	flag.StringVar(&o.camp, "camp", "sdk-installed-check", "fixture camp name")
	flag.StringVar(&o.workdir, "workdir", "/demo/camp", "fixture camp directory")
	flag.StringVar(&o.evidence, "evidence-dir", "/evidence", "structured evidence directory")
	flag.DurationVar(&o.timeout, "timeout", 120*time.Second, "overall operation deadline; cleanup remains bounded")
	flag.BoolVar(&o.requirePersistent, "require-persistent", false, "fail if persistent APIs are not advertised")
	flag.Parse()
	if err := execute(o); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: installed SDK integration: %v (see sdk-report.json when evidence was initialized)\n", err)
		os.Exit(1)
	}
}

func execute(o options) (resultErr error) {
	// Both checks precede every fixture write, listener and child process.
	if os.Getenv("OBEY_SDK_TEST_CONTAINER") != "1" {
		return errors.New("container fixture guard is required")
	}
	if _, err := os.Stat("/.dockerenv"); err != nil {
		return errors.New("Docker isolation marker is required")
	}
	if o.timeout <= 0 || strings.TrimSpace(o.camp) == "" {
		return errors.New("positive timeout and explicit camp are required")
	}
	if err := os.MkdirAll(o.evidence, 0700); err != nil {
		return err
	}
	r := &report{Persistent: "not_tested"}
	defer func() {
		r.Success = resultErr == nil
		if resultErr != nil {
			r.Error = resultErr.Error()
		}
		if err := writeJSON(filepath.Join(o.evidence, "sdk-report.json"), r); err != nil {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	binary, err := exec.LookPath(o.binary)
	if err != nil {
		return fmt.Errorf("resolve installed binary: %w", err)
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		return err
	}
	// Runtime files need Linux-native filesystem semantics: evidence may be a
	// macOS bind mount that cannot chmod a Unix socket.
	runtimeDir := "/demo/runtime"
	if err := os.MkdirAll(runtimeDir, 0700); err != nil {
		return fmt.Errorf("initialize container-native runtime: %w", err)
	}
	p := &probe{opts: o, binary: binary, config: filepath.Join(runtimeDir, "daemon.yaml"), socket: filepath.Join(runtimeDir, "daemon.sock"), report: r}
	signals, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	ctx, cancel := context.WithTimeout(signals, o.timeout)
	defer cancel()
	version, err := p.cli(ctx, "version")
	if err != nil {
		return err
	}
	r.ObeyVersion = string(version)
	if err := os.WriteFile(filepath.Join(o.evidence, "obey-version.txt"), version, 0600); err != nil {
		return err
	}
	if _, err := p.command(ctx, "camp", "init", o.workdir, "--name", o.camp, "--type", "product", "--description", "Released SDK integration fixture", "--mission", "Prove installed binary orchestration without real credentials", "--no-git", "--no-skills"); err != nil {
		return fmt.Errorf("initialize fixture camp: %w", err)
	}

	p.fixture = &completionFixture{}
	server := httptest.NewServer(p.fixture)
	defer server.Close()
	if err := p.writeConfig(server.URL + "/v1"); err != nil {
		return err
	}
	daemon, err := startDaemon(ctx, p)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, daemon.stop()) }()
	if err := p.ready(ctx, daemon); err != nil {
		return err
	}
	fmt.Println("Installed daemon: ready with isolated fixture configuration")
	client, err := p.client()
	if err != nil {
		return err
	}
	defer client.Close()
	caps, err := client.Capabilities(ctx)
	if err != nil {
		return fmt.Errorf("capability discovery: %w", err)
	}
	r.Protocols = caps.Protocols
	if !json.Valid(caps.Raw) {
		return errors.New("doctor returned invalid evidence JSON")
	}
	if err := os.WriteFile(filepath.Join(o.evidence, "doctor.json"), caps.Raw, 0600); err != nil {
		return err
	}
	initial, err := p.sessions(ctx, o.camp, "sessions-initial.json")
	if err != nil {
		return err
	}
	if len(initial) != 0 {
		return errors.New("fixture camp unexpectedly contains sessions")
	}
	r.LiveEvents = "advertised_not_verified"
	if !supports(caps, "session_run", "obey.session.run", 2) {
		stream, liveErr := client.StreamPrompt(ctx, "live events must refuse before mutation", &obey.RunOptions{
			SessionOptions:    obey.SessionOptions{Provider: providerName, Model: modelName, Mode: "discussion"},
			RequireLiveEvents: true,
		})
		if stream != nil {
			_ = stream.Close()
			return errors.New("unadvertised live run unexpectedly returned a stream")
		}
		var typed *obey.Error
		if !errors.As(liveErr, &typed) || typed.Code != "unsupported_capability" {
			return fmt.Errorf("unadvertised live run did not safely refuse: %w", liveErr)
		}
		after, err := p.sessions(ctx, o.camp, "sessions-after-live-refusal.json")
		if err != nil {
			return err
		}
		if len(after) != 0 || p.fixture.count() != 0 {
			return errors.New("unadvertised live run mutated sessions or invoked provider")
		}
		r.LiveEvents, r.LiveEventsErrorCode = "unsupported", typed.Code
		fmt.Println("Live events: not advertised; typed unsupported_capability; no session or provider mutation")
	}
	run, err := client.RunPrompt(ctx, "public installed SDK fixture prompt", &obey.RunOptions{SessionOptions: obey.SessionOptions{Provider: providerName, Model: modelName, Mode: "discussion"}})
	if err != nil {
		return fmt.Errorf("RunPrompt: %w", err)
	}
	if run == nil || run.Text != answer || run.Cleanup != obey.CleanupStopped || (run.Protocol.Version != 1 && run.Protocol.Version != 2) || run.Session.SessionID == "" || run.Session.CampID == "" {
		return errors.New("RunPrompt result/identity/protocol/cleanup assertion failed")
	}
	r.RunProtocol, r.RunSession, r.RunCleanup = run.Protocol.Version, run.Session, run.Cleanup
	if _, err := p.stopped(ctx, run.Session, 1, "run-session-info.json"); err != nil {
		return err
	}
	if p.fixture.count() != 1 {
		return errors.New("RunPrompt did not cause exactly one provider request")
	}
	fmt.Printf("RunPrompt: protocol v%d; exact fixture answer; owned session STOPPED; provider requests 1\n", run.Protocol.Version)

	before, err := p.sessions(ctx, run.Session.CampID, "sessions-before-persistent.json")
	if err != nil {
		return err
	}
	r.SessionIDsBefore = ids(before)
	if len(before) != 1 || before[0].ID != run.Session.SessionID {
		return errors.New("unexpected session set after owned run")
	}
	fresh, err := p.client()
	if err != nil {
		return err
	}
	defer fresh.Close()
	session, createErr := fresh.CreateSession(ctx, &obey.SessionOptions{Provider: providerName, Model: modelName, Mode: "discussion"})
	advertised := supports(caps, "session_create", "obey.session.create", 1)
	if !advertised {
		var typed *obey.Error
		if session != nil || !errors.As(createErr, &typed) || typed.Code != "unsupported_protocol" {
			return fmt.Errorf("unadvertised persistent create did not safely refuse: %w", createErr)
		}
		r.Persistent, r.PersistentErrorCode = "unsupported", typed.Code
		after, err := p.sessions(ctx, run.Session.CampID, "sessions-after-persistent.json")
		if err != nil {
			return err
		}
		r.SessionIDsAfter = ids(after)
		if !slices.Equal(r.SessionIDsBefore, r.SessionIDsAfter) || p.fixture.count() != 1 {
			return errors.New("persistent refusal changed sessions or provider request count")
		}
		fmt.Println("Persistent: not advertised; typed unsupported_protocol; no session or provider mutation")
	} else {
		if createErr != nil {
			if session != nil {
				cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				createErr = errors.Join(createErr, session.Stop(cleanup))
			}
			return fmt.Errorf("advertised persistent create: %w", createErr)
		}
		if session == nil {
			return errors.New("advertised persistent create returned no handle")
		}
		if err := p.persistent(ctx, session, run.Session.CampID); err != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return errors.Join(err, session.Stop(cleanup))
		}
		r.Persistent = "executed"
		fmt.Println("Persistent: Create/Send/Wait/same-key dedup/Stop verified; one provider request")
	}
	r.ProviderRequests = p.fixture.count()
	if err := daemon.stop(); err != nil {
		return err
	}
	if err := p.unavailable(ctx); err != nil {
		return err
	}
	fmt.Println("Daemon unavailable: typed failure; no auto-start or provider request")
	if o.requirePersistent && !advertised {
		fmt.Println("Persistent execution required: installed release does not advertise persistent APIs")
		return errors.New("persistent execution required, but installed release does not advertise it")
	}
	fmt.Println("Installed SDK integration: verified (persistent support reported separately)")
	return nil
}

func (p *probe) client() (*obey.Client, error) {
	return obey.NewClient(p.binary, obey.WithCamp(p.opts.camp), obey.WithWorkingDirectory(p.opts.workdir), obey.WithSocket(p.socket), obey.WithEnvironment(map[string]string{"OBEY_CONFIG": p.config}))
}
func (p *probe) writeConfig(endpoint string) error {
	config := map[string]any{
		"data_dir": filepath.Join(filepath.Dir(p.config), "daemon-data"), "campaigns": []string{p.opts.workdir},
		"grpc": map[string]any{"enabled": true, "socket": p.socket}, "features": map[string]any{"enable_chat_surfaces": true},
		"network": map[string]any{"enabled": false}, "watcher": map[string]any{"enabled": false},
		"auto_commits": map[string]any{"enabled": false},
		"providers":    map[string]any{providerName: map[string]any{"type": "openai", "enabled": true, "api_key": fixtureKey, "base_url": endpoint, "default_model": modelName}},
	}
	// JSON is a strict subset of YAML, accepted by the public configuration loader.
	return writeJSON(p.config, config)
}
func (p *probe) command(ctx context.Context, binary string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = append(os.Environ(), "OBEY_CONFIG="+p.config)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("installed command failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
func (p *probe) cli(ctx context.Context, args ...string) ([]byte, error) {
	return p.command(ctx, p.binary, append([]string{"--socket", p.socket}, args...)...)
}
func (p *probe) ready(ctx context.Context, d *ownedDaemon) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-d.done:
			return errors.New("fixture daemon exited before readiness")
		default:
		}
		call, cancel := context.WithTimeout(ctx, time.Second)
		_, err := p.cli(call, "session", "list", "--campaign", p.opts.camp, "--all", "--json")
		cancel()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func (p *probe) sessions(ctx context.Context, camp, name string) ([]sessionInfo, error) {
	raw, err := p.cli(ctx, "session", "list", "--campaign", camp, "--all", "--json")
	if err != nil {
		return nil, err
	}
	var rows []sessionInfo
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("session list must be a top-level JSON array: %w", err)
	}
	for _, row := range rows {
		if row.ID == "" || row.CampID == "" {
			return nil, errors.New("session list lacks scope")
		}
	}
	if err := os.WriteFile(filepath.Join(p.opts.evidence, name), raw, 0600); err != nil {
		return nil, err
	}
	return rows, nil
}
func (p *probe) stopped(ctx context.Context, ref obey.SessionRef, turns uint64, name string) (sessionInfo, error) {
	raw, err := p.cli(ctx, "session", "info", ref.SessionID, "--campaign", ref.CampID, "--json")
	if err != nil {
		return sessionInfo{}, err
	}
	var document struct {
		Session sessionInfo `json:"session"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return sessionInfo{}, err
	}
	s := document.Session
	if s.ID != ref.SessionID || s.CampID != ref.CampID || s.Status != "SESSION_STATUS_STOPPED" || s.Provider != providerName || s.Model != modelName || s.TurnCount != turns {
		return s, errors.New("exact session STOPPED readback assertion failed")
	}
	if err := os.WriteFile(filepath.Join(p.opts.evidence, name), raw, 0600); err != nil {
		return s, err
	}
	return s, nil
}
func (p *probe) persistent(ctx context.Context, s *obey.Session, camp string) error {
	ref := s.Ref()
	if ref.SessionID == "" || ref.CampID != camp {
		return errors.New("persistent session scope mismatch")
	}
	turn, err := s.Send(ctx, "persistent installed fixture prompt", &obey.SendOptions{IdempotencyKey: "installed-fixture-original-key", Mode: "discussion"})
	if err != nil {
		return err
	}
	if turn == nil || turn.Acceptance() == nil || turn.Ref().SessionID != ref.SessionID || turn.Ref().CampID != camp || turn.Ref().TurnID == "" {
		return errors.New("missing exact durable acceptance")
	}
	result, err := turn.Wait(ctx)
	if err != nil {
		return err
	}
	if result == nil || result.Text != answer || result.TurnID != turn.Ref().TurnID {
		return errors.New("persistent Wait returned incorrect original result")
	}
	duplicate, err := s.Send(ctx, "persistent installed fixture prompt", &obey.SendOptions{IdempotencyKey: "installed-fixture-original-key", Mode: "discussion"})
	if err != nil {
		return err
	}
	if duplicate == nil || duplicate.Ref().TurnID != turn.Ref().TurnID || duplicate.Acceptance() == nil || !duplicate.Acceptance().Deduplicated {
		return errors.New("same-key retry did not return original deduplicated turn")
	}
	if p.fixture.count() != 2 {
		return errors.New("persistent turn/dedup did not cause exactly one additional provider request")
	}
	state, err := turn.State(ctx)
	if err != nil {
		return err
	}
	if state.Status != obey.TurnCompleted || state.Ref.TurnID != turn.Ref().TurnID {
		return errors.New("original persistent turn is not completed")
	}
	if err := writeJSON(filepath.Join(p.opts.evidence, "persistent-turn-state.json"), state); err != nil {
		return err
	}
	p.report.PersistentRef = new(obey.TurnRef)
	*p.report.PersistentRef = turn.Ref()
	if err := s.Stop(ctx); err != nil {
		return err
	}
	if _, err := p.stopped(ctx, ref, 1, "persistent-session-info.json"); err != nil {
		return err
	}
	rows, err := p.sessions(ctx, camp, "sessions-after-persistent.json")
	if err != nil {
		return err
	}
	p.report.SessionIDsAfter = ids(rows)
	expected := append(append([]string(nil), p.report.SessionIDsBefore...), ref.SessionID)
	slices.Sort(expected)
	if !slices.Equal(expected, p.report.SessionIDsAfter) {
		return errors.New("unexpected persistent session set")
	}
	return nil
}
func (p *probe) unavailable(ctx context.Context) error {
	count := p.fixture.count()
	if conn, err := net.DialTimeout("unix", p.socket, 100*time.Millisecond); err == nil {
		conn.Close()
		return errors.New("owned daemon still reachable after stop")
	}
	// Bind the known canonical camp so the offline probe tests daemon I/O,
	// rather than failing earlier while resolving a camp-name selector.
	if p.report.RunSession.CampID == "" {
		return errors.New("offline probe requires the successful run's canonical camp")
	}
	fresh, err := obey.NewClient(p.binary, obey.WithCamp(p.report.RunSession.CampID), obey.WithWorkingDirectory(p.opts.workdir), obey.WithSocket(p.socket), obey.WithEnvironment(map[string]string{"OBEY_CONFIG": p.config}))
	if err != nil {
		return err
	}
	defer fresh.Close()
	call, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result, err := fresh.RunPrompt(call, "unavailable daemon must not submit", &obey.RunOptions{SessionOptions: obey.SessionOptions{Provider: providerName, Model: modelName, Mode: "discussion"}})
	var typed *obey.Error
	if !errors.As(err, &typed) || (typed.Code != "create_session" && typed.Code != "daemon_unavailable") {
		return fmt.Errorf("expected typed daemon-unavailable failure: %w", err)
	}
	if result != nil && result.Session.SessionID != "" {
		return errors.New("unavailable daemon invented a session identity")
	}
	if conn, err := net.DialTimeout("unix", p.socket, 100*time.Millisecond); err == nil {
		conn.Close()
		return errors.New("SDK auto-started daemon")
	}
	if p.fixture.count() != count {
		return errors.New("unavailable daemon caused provider request")
	}
	p.report.DaemonUnavailableCode, p.report.NoAutoStart = typed.Code, true
	return nil
}
func ids(rows []sessionInfo) []string {
	result := make([]string, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.ID)
	}
	slices.Sort(result)
	return result
}
func supports(c *obey.Capabilities, key, name string, version int) bool {
	p, ok := c.Protocols[key]
	return ok && p.Protocol == name && slices.Contains(p.Versions, version)
}
func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}

type ownedDaemon struct {
	cmd     *exec.Cmd
	done    chan struct{}
	once    sync.Once
	stopErr error
	waitErr error
}

func startDaemon(ctx context.Context, p *probe) (*ownedDaemon, error) {
	log, err := os.OpenFile(filepath.Join(p.opts.evidence, "daemon.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(p.binary, "serve", "--config", p.config, "--foreground", "--no-network", "--no-firewall-setup", "--no-watcher", "--socket", p.socket)
	cmd.Env = append(os.Environ(), "OBEY_CONFIG="+p.config)
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		log.Close()
		return nil, err
	}
	child := &ownedDaemon{cmd: cmd, done: make(chan struct{})}
	go func() { child.waitErr = cmd.Wait(); _ = log.Close(); close(child.done) }()
	go func() {
		select {
		case <-ctx.Done():
			_ = child.stop()
		case <-child.done:
		}
	}()
	return child, nil
}
func (d *ownedDaemon) stop() error {
	d.once.Do(func() {
		select {
		case <-d.done:
			d.stopErr = fmt.Errorf("daemon exited before intended shutdown: %w", errors.Join(errors.New("unexpected daemon exit"), d.waitErr))
			return
		default:
		}
		if err := d.cmd.Process.Signal(syscall.SIGTERM); err != nil {
			d.stopErr = fmt.Errorf("signal owned daemon for shutdown: %w", err)
		}
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-d.done:
			if d.waitErr != nil {
				var exit *exec.ExitError
				if !errors.As(d.waitErr, &exit) || exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGTERM {
					d.stopErr = errors.Join(d.stopErr, fmt.Errorf("daemon shutdown failed: %w", d.waitErr))
				}
			}
			return
		case <-timer.C:
		}
		d.stopErr = errors.Join(d.stopErr, errors.New("daemon graceful shutdown exceeded 5 seconds; force kill required"))
		if err := d.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			d.stopErr = errors.Join(d.stopErr, err)
		}
		select {
		case <-d.done:
		case <-time.After(5 * time.Second):
			d.stopErr = errors.Join(d.stopErr, errors.New("owned daemon was not reaped after force kill"))
		}
	})
	return d.stopErr
}

type completionFixture struct {
	mu       sync.Mutex
	requests int
}

func (f *completionFixture) count() int { f.mu.Lock(); defer f.mu.Unlock(); return f.requests }
func (f *completionFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []any{map[string]any{"id": modelName, "object": "model", "created": 1, "owned_by": "fixture"}}})
		return
	}
	if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+fixtureKey {
		http.Error(w, "fixture authorization required", http.StatusUnauthorized)
		return
	}
	defer r.Body.Close()
	var request struct {
		Model    string            `json:"model"`
		Stream   bool              `json:"stream"`
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&request); err != nil || request.Model != modelName || !request.Stream || len(request.Messages) == 0 {
		http.Error(w, "invalid fixture request", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.requests++
	f.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	chunks := []map[string]any{{"role": "assistant"}, {"content": answer}, {}}
	for i, delta := range chunks {
		var finish any
		if i == len(chunks)-1 {
			finish = "stop"
		}
		frame := map[string]any{"id": "fixture-completion", "object": "chat.completion.chunk", "created": 1, "model": modelName, "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}}
		encoded, _ := json.Marshal(frame)
		if _, err := fmt.Fprintf(w, "data: %s\n\n", encoded); err != nil {
			return
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}
