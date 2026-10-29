//go:build process_tests

package obey

import (
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
	"syscall"
	"testing"
	"time"
)

const handleCamp = "11111111-1111-1111-1111-111111111111"
const handleForeignCamp = "22222222-2222-2222-2222-222222222222"

type handleCall struct {
	Args []string `json:"args"`
	CWD  string   `json:"cwd"`
}

func init() {
	if os.Getenv("OBEY_HANDLES_FAKE") != "1" {
		return
	}
	if os.Getenv("OBEY_SDK_TEST_CONTAINER") != "1" {
		os.Exit(99)
	}
	args := os.Args[1:]
	if len(args) > 1 && args[0] == "--socket" {
		args = args[2:]
	}
	mode := os.Getenv("OBEY_HANDLE_CASE")
	if data, err := os.ReadFile(os.Getenv("OBEY_HANDLE_MODE_FILE")); err == nil {
		mode = strings.TrimSpace(string(data))
	}
	var terminateSignals chan os.Signal
	if mode == "terminal-then-limit" || mode == "send-cancel-after-receipt" || mode == "send-terminal-then-limit" {
		terminateSignals = make(chan os.Signal, 1)
		signal.Notify(terminateSignals, syscall.SIGTERM)
	}
	if path := os.Getenv("OBEY_HANDLES_LOG"); path != "" {
		cwd, _ := os.Getwd()
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			os.Exit(98)
		}
		_ = json.NewEncoder(f).Encode(handleCall{Args: args, CWD: cwd})
		_ = f.Close()
	}
	emit := func(value any) { _ = json.NewEncoder(os.Stdout).Encode(value) }
	if args[0] == "doctor" {
		caps := map[string]any{"daemon_status": map[string]any{"protocol": "obey.daemon.status", "versions": []int{1}}, "session_create": map[string]any{"protocol": "obey.session.create", "versions": []int{1}}}
		if mode != "send-old" {
			caps["session_send"] = map[string]any{"protocol": "obey.session.send", "versions": []int{1}}
		}
		if mode != "old" {
			caps["machine_read_errors"] = map[string]any{"protocol": "obey.machine.error", "versions": []int{1}}
		}
		emit(map[string]any{"schema": "obey.doctor", "version": 1, "status": "fail", "capabilities": caps})
		os.Exit(1)
	}
	get := func(flag string) string {
		for i, arg := range args {
			if arg == flag && i+1 < len(args) {
				return args[i+1]
			}
		}
		return ""
	}
	record := func(protocolName, kind, session, camp string, extra map[string]any) map[string]any {
		value := map[string]any{"protocol": protocolName, "version": 1, "type": kind}
		if session != "" {
			value["session_id"] = session
		}
		if camp != "" {
			value["campaign_id"] = camp
		}
		for k, v := range extra {
			value[k] = v
		}
		return value
	}
	terminal := func(name, session, camp, status, outcome string) {
		emit(record(name, "terminal", session, camp, map[string]any{"status": status, "outcome": outcome, "cleanup": "not_owned"}))
	}
	if args[0] == "status" {
		emit(record("obey.daemon.status", "event", "", "", map[string]any{"payload_type": "local.v1.PingResponse", "payload": map[string]any{"version": "fake", "hostname": "fixture", "capabilities": map[string]bool{"chat_surfaces": true, "durable_turns": true, "send_acceptance": mode != "send-no-cap", "per_turn_model": mode != "send-no-model"}}}))
		terminal("obey.daemon.status", "", "", "completed", "rpc_completed")
		os.Exit(0)
	}
	if args[0] == "turn" && args[1] == "show" {
		refCamp := get("--campaign")
		id := args[2]
		if mode == "not-found" || mode == "wrong-camp" || mode == "read-gate" {
			code, grpc, daemon := "not_found", "NotFound", 5004
			if mode == "wrong-camp" {
				code, grpc, daemon = "scope_mismatch", "PermissionDenied", 6001
			}
			if mode == "read-gate" {
				code, grpc, daemon = "unsupported_capability", "PermissionDenied", 6030
			}
			emit(map[string]any{"schema": "obey.machine.error", "version": 1, "operation": "turn.show", "code": code, "message": "retained original diagnostic", "campaign_id": refCamp, "turn_id": id, "daemon_code": daemon, "grpc_code": grpc, "details": map[string]any{"future": "retained"}})
			os.Exit(7)
		}
		session := "session"
		if mode == "wrong-session" {
			session = "foreign"
		}
		status := "completed"
		if strings.HasPrefix(mode, "status:") {
			status = strings.TrimPrefix(mode, "status:")
		}
		emit(map[string]any{"schema_version": "obey.turn.v1", "turn": map[string]any{"id": id, "session_id": session, "campaign_id": refCamp, "status": status, "version": int64(9007199254740993), "usage": map[string]any{"tokens_input": int64(9007199254740993)}}})
		os.Exit(0)
	}
	if args[0] == "session" && args[1] == "info" {
		session := strings.TrimPrefix(args[2], "id:")
		if mode == "wrong-session" {
			session = "foreign"
		}
		emit(map[string]any{"session": map[string]any{"id": session, "campaign_id": get("--campaign")}})
		os.Exit(0)
	}
	if args[0] == "session" && args[1] == "send" {
		input, _ := io.ReadAll(os.Stdin)
		capture, _ := json.Marshal(struct {
			Args  []string
			Input string
		}{args, string(input)})
		_ = os.WriteFile(os.Getenv("OBEY_HANDLES_LOG")+".payload", capture, 0600)
		camp, session := get("--campaign"), args[2]
		sendRecord := func(kind, id string, extra map[string]any) map[string]any {
			value := record("obey.session.send", kind, session, camp, extra)
			value["idempotency_key"] = get("--idempotency-key")
			if valueRef := get("--client-ref"); valueRef != "" {
				value["client_ref"] = valueRef
			}
			if id != "" {
				value["turn_id"] = id
			}
			return value
		}
		sendTerminal := func(state, outcome string) {
			emit(sendRecord("terminal", "turn", map[string]any{"status": state, "outcome": outcome, "cleanup": "not_owned"}))
		}
		failure := func(code, id string) {
			emit(sendRecord("error", id, map[string]any{"code": code, "message": "fixture refusal/uncertainty", "severity": "error", "daemon_code": 2001}))
		}
		if mode == "send-note" || mode == "send-model-refusal" {
			code := "undelivered_note"
			if mode == "send-model-refusal" {
				code = "indeterminate_acceptance"
			}
			failure(code, "")
			emit(sendRecord("terminal", "", map[string]any{"status": "failed", "outcome": "failed", "cleanup": "not_owned"}))
			os.Exit(7)
		}
		if mode == "send-unknown-error" || mode == "send-invalid-error-scope" || mode == "send-invalid-error" || mode == "send-explicit-uncertainty" {
			code := "future_policy_code"
			if mode != "send-unknown-error" {
				code = "invalid_input"
			}
			value := sendRecord("error", "", map[string]any{"code": code, "message": "original policy detail", "severity": "error"})
			if mode == "send-invalid-error-scope" {
				value["idempotency_key"] = "foreign-key"
			}
			if mode == "send-explicit-uncertainty" {
				value["acceptance_indeterminate"] = true
			}
			emit(value)
			emit(sendRecord("terminal", "", map[string]any{"status": "failed", "outcome": "failed", "cleanup": "not_owned"}))
			os.Exit(7)
		}
		if mode == "send-error-known-id" {
			failure("indeterminate_acceptance", "turn")
			sendTerminal("failed", "failed")
			os.Exit(7)
		}
		if mode == "send-missing" {
			sendTerminal("completed", "accepted")
			os.Exit(0)
		}
		acceptedStatus := any("TURN_QUEUED")
		dedup := false
		if mode == "send-duplicate" {
			acceptedStatus = "TURN_COMPLETED"
			dedup = true
		}
		if mode == "send-unknown" {
			acceptedStatus = "FUTURE_STATUS"
		}
		if mode == "send-unknown-number" {
			acceptedStatus = 999
		}
		accepted := sendRecord("event", "turn", map[string]any{"payload_type": "local.v1.SendMessageResponse", "payload": map[string]any{"accepted": map[string]any{"turn_id": "turn", "status": acceptedStatus, "deduplicated": dedup, "future": "retained"}}})
		if mode == "send-foreign-key" {
			accepted["idempotency_key"] = "foreign-key"
		}
		if mode == "send-foreign-ref" {
			accepted["client_ref"] = "foreign-ref"
		}
		if mode == "send-foreign-session" {
			accepted["session_id"] = "foreign-session"
		}
		if mode == "send-wire-version" {
			accepted["version"] = 99
		}
		emit(accepted)
		if mode == "send-valid-then-foreign" {
			accepted["idempotency_key"] = "foreign-key"
			accepted["turn_id"] = "foreign-turn"
			accepted["payload"] = map[string]any{"accepted": map[string]any{"turn_id": "foreign-turn", "status": "TURN_QUEUED"}}
			emit(accepted)
			os.Exit(7)
		}
		if mode == "send-partial" {
			os.Exit(7)
		}
		if mode == "send-cancel-after-receipt" {
			_ = os.WriteFile(os.Getenv("OBEY_HANDLE_MARKER"), []byte("receipt-written"), 0600)
			<-terminateSignals
			failure("canceled", "turn")
			sendTerminal("canceled", "canceled")
			os.Exit(7)
		}
		if mode == "send-unknown" || mode == "send-unknown-number" {
			failure("indeterminate_acceptance", "turn")
			sendTerminal("failed", "failed")
			os.Exit(7)
		}
		sendTerminal("completed", "accepted")
		if mode == "send-terminal-then-limit" {
			_ = os.WriteFile(os.Getenv("OBEY_HANDLE_MARKER"), []byte("terminal-written"), 0600)
			<-terminateSignals
			fmt.Fprintln(os.Stdout, strings.Repeat("x", 2048))
		}
		os.Exit(0)
	}
	if args[0] == "session" && args[1] == "create" {
		camp := get("--campaign")
		if camp != handleCamp {
			camp = handleCamp
		}
		if mode == "uuid-foreign" {
			camp = handleForeignCamp
		}
		create := func(outer, inner, camp string) {
			emit(record("obey.session.create", "session_created", outer, camp, map[string]any{"payload_type": "local.v1.CreateSessionResponse", "payload": map[string]any{"session": map[string]any{"id": inner, "campaign_id": camp}}}))
		}
		failure := func(session, camp string) {
			emit(record("obey.session.create", "error", session, camp, map[string]any{"code": "indeterminate_creation", "message": "postpersist refusal", "severity": "error", "daemon_code": 2001, "grpc_code": "InvalidArgument"}))
		}
		if mode == "malformed-first" {
			create("untrusted", "different", camp)
			os.Exit(7)
		}
		if mode == "known-error" {
			failure("created", camp)
			terminal("obey.session.create", "created", camp, "failed", "failed")
			os.Exit(7)
		}
		create("created", "created", camp)
		switch mode {
		case "foreign-id":
			failure("foreign", camp)
			os.Exit(7)
		case "foreign-camp":
			failure("created", handleForeignCamp)
			os.Exit(7)
		case "malformed-second":
			create("foreign", "different", camp)
			os.Exit(7)
		case "missing-terminal":
			os.Exit(7)
		}
		if mode == "many-records" {
			for i := 0; i < 10; i++ {
				emit(record("obey.session.create", "future", "created", camp, map[string]any{"padding": strings.Repeat("x", 120)}))
			}
		}
		terminal("obey.session.create", "created", camp, "completed", "session_created")
		if mode == "terminal-then-limit" {
			if err := os.WriteFile(os.Getenv("OBEY_HANDLE_MARKER"), []byte("terminal-written"), 0600); err != nil {
				os.Exit(96)
			}
			<-terminateSignals
			fmt.Fprintln(os.Stdout, strings.Repeat("x", 2048))
		}
		os.Exit(0)
	}
	os.Exit(97)
}
func handlesClient(t *testing.T, mode string, options ...ClientOption) (*Client, string) {
	t.Helper()
	publicGuard(t)
	root := t.TempDir()
	ledger := filepath.Join(root, "calls")
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if mode == "replace-binary" {
		data, err := os.ReadFile(bin)
		if err != nil {
			t.Fatal(err)
		}
		bin = filepath.Join(root, "obey-helper")
		if err := os.WriteFile(bin, data, 0755); err != nil {
			t.Fatal(err)
		}
	}
	options = append([]ClientOption{WithSessionDefaults(SessionOptions{Provider: "provider", Model: "model"})}, options...)
	options = append(options, WithEnvironment(map[string]string{"OBEY_HANDLES_FAKE": "1", "OBEY_HANDLE_CASE": mode, "OBEY_HANDLES_LOG": ledger, "OBEY_HANDLE_MARKER": ledger + ".ready", "OBEY_HANDLE_MODE_FILE": ledger + ".mode"}))
	client, err := NewClient(bin, options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, ledger
}
func readHandleCalls(t *testing.T, path string) []handleCall {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var calls []handleCall
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var call handleCall
		if json.Unmarshal([]byte(line), &call) != nil {
			t.Fatalf("bad ledger %q", line)
		}
		calls = append(calls, call)
	}
	return calls
}
func TestProcessHandleReopenReadOnlyScopesAndStatuses(t *testing.T) {
	publicGuard(t)
	for _, mode := range []string{"normal", "status:recovery_required", "status:historical_unknown", "status:future_status", "wrong-session", "wrong-camp", "not-found", "old"} {
		t.Run(mode, func(t *testing.T) {
			client, ledger := handlesClient(t, mode)
			ref := TurnRef{SessionRef: SessionRef{CampID: handleCamp, SessionID: "session"}, TurnID: "turn"}
			serialized, _ := json.Marshal(ref)
			_ = client.Close()
			client, ledger = handlesClient(t, mode)
			if json.Unmarshal(serialized, &ref) != nil {
				t.Fatal("ref roundtrip")
			}
			turn, err := client.OpenTurn(t.Context(), ref)
			if mode == "old" || mode == "wrong-session" || mode == "wrong-camp" || mode == "not-found" {
				var typed *Error
				if turn != nil || !errors.As(err, &typed) {
					t.Fatalf("turn=%v err=%v", turn, err)
				}
				expected := map[string]string{"old": "unsupported_protocol", "wrong-session": "scope_mismatch", "wrong-camp": "scope_mismatch", "not-found": "not_found"}[mode]
				if typed.Code != expected {
					t.Fatalf("code=%s want=%s", typed.Code, expected)
				}
				if mode == "wrong-camp" && (typed.DaemonCode == nil || *typed.DaemonCode != 6001 || len(typed.RawRecord) == 0) {
					t.Fatal("daemon scope metadata lost")
				}
			} else {
				if err != nil || turn.Ref() != ref || turn.Acceptance() != nil {
					t.Fatalf("turn=%v err=%v", turn, err)
				}
				state, err := turn.State(t.Context())
				if err != nil || state.Usage.InputTokens != 9007199254740993 || state.Ref.IdempotencyKey != "" {
					t.Fatalf("state=%+v err=%v", state, err)
				}
			}
			for _, call := range readHandleCalls(t, ledger) {
				if mode == "old" && call.Args[0] != "doctor" {
					t.Fatalf("old binary invoked read: %v", call.Args)
				}
				if call.Args[0] != "doctor" && !(len(call.Args) > 1 && call.Args[0] == "turn" && call.Args[1] == "show") {
					t.Fatalf("OpenTurn mutated or resolved: %v", call.Args)
				}
			}
		})
	}
}
func TestProcessCreateCanonicalCampAndValidatedPartialIdentity(t *testing.T) {
	publicGuard(t)
	for _, mode := range []string{"normal", "foreign-id", "foreign-camp", "malformed-second", "malformed-first", "known-error", "missing-terminal", "uuid-foreign"} {
		t.Run(mode, func(t *testing.T) {
			client, _ := handlesClient(t, mode, WithCamp("my-camp"))
			options := &SessionOptions{}
			if mode == "uuid-foreign" {
				options.Camp = handleCamp
			}
			session, err := client.CreateSession(t.Context(), options)
			if mode == "normal" {
				if err != nil || session == nil || session.Ref() != (SessionRef{CampID: handleCamp, SessionID: "created"}) {
					t.Fatalf("session=%v err=%v", session, err)
				}
				reopened, err := client.OpenSession(t.Context(), session.Ref())
				if err != nil || reopened.Ref() != session.Ref() {
					t.Fatalf("reopen=%v %v", reopened, err)
				}
				return
			}
			var typed *Error
			if !errors.As(err, &typed) {
				t.Fatalf("missing typed failure: %v", err)
			}
			if mode == "malformed-first" || mode == "uuid-foreign" {
				if session != nil || typed.Session.SessionID != "" {
					t.Fatalf("invalid first frame invented handle: %v %+v", session, typed)
				}
				return
			}
			if session == nil || session.Ref() != (SessionRef{CampID: handleCamp, SessionID: "created"}) || typed.Session != session.Ref() {
				t.Fatalf("known scope overwritten: session=%v error=%+v", session, typed)
			}
			if mode == "known-error" && (typed.Code != "indeterminate_creation" || typed.DaemonCode == nil || *typed.DaemonCode != 2001) {
				t.Fatalf("known postpersist refusal lost: %+v", typed)
			}
			if strings.HasPrefix(mode, "foreign") || mode == "malformed-second" {
				if len(typed.RawRecord) == 0 {
					t.Fatal("invalid raw not retained diagnostically")
				}
			}
		})
	}
}
func TestProcessCreateOptionsPrecedenceAndDocumentLimits(t *testing.T) {
	publicGuard(t)
	cwd := t.TempDir()
	client, ledger := handlesClient(t, "many-records", WithWorkingDirectory(cwd), WithCamp("client-camp"), WithSessionDefaults(SessionOptions{Provider: "provider", Model: "model", Camp: "defaults-camp"}), WithLimits(Limits{MaxRecordBytes: 512, MaxResultBytes: 3}))
	if session, err := client.CreateSession(context.Background(), nil); err != nil || session == nil {
		t.Fatalf("finite per-record stream incorrectly aggregate limited: %v %v", session, err)
	}
	calls := readHandleCalls(t, ledger)
	last := calls[len(calls)-1]
	if last.CWD != cwd {
		t.Fatalf("child cwd=%s", last.CWD)
	}
	argValue := func(args []string, flag string) string {
		for i, arg := range args {
			if arg == flag && i+1 < len(args) {
				return args[i+1]
			}
		}
		return ""
	}
	if argValue(last.Args, "--campaign") != "client-camp" || argValue(last.Args, "--workdir") != "" {
		t.Fatalf("defaults/cwd precedence wrong: %v", last.Args)
	}
	options := &SessionOptions{Camp: "per-call", WorkingDirectory: "agent-workspace", AgentProfile: "chosen-profile", Config: json.RawMessage(`{"future":true}`)}
	if _, err := client.CreateSession(t.Context(), options); err != nil {
		t.Fatal(err)
	}
	calls = readHandleCalls(t, ledger)
	last = calls[len(calls)-1]
	if argValue(last.Args, "--campaign") != "per-call" || argValue(last.Args, "--workdir") != "agent-workspace" || argValue(last.Args, "--agent-profile") != "chosen-profile" || argValue(last.Args, "--config") != string(options.Config) {
		t.Fatalf("argv options lost: %v", last.Args)
	}
}

func TestProcessCreateDefaultsConfigIsOwned(t *testing.T) {
	publicGuard(t)
	input := json.RawMessage(`{"keep":true}`)
	client, ledger := handlesClient(t, "normal", WithCamp("my-camp"), WithSessionDefaults(SessionOptions{Provider: "provider", Model: "model", Config: input}))
	input[0] = 'x'
	if _, err := client.CreateSession(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	calls := readHandleCalls(t, ledger)
	args := calls[len(calls)-1].Args
	for i, arg := range args {
		if arg == "--config" {
			if args[i+1] != `{"keep":true}` {
				t.Fatalf("caller mutated defaults: %v", args)
			}
			return
		}
	}
	t.Fatal("config not passed")
}

func TestProcessCreateCancellationCannotMaskTrailingOutputLimit(t *testing.T) {
	publicGuard(t)
	client, ledger := handlesClient(t, "terminal-then-limit", WithCamp(handleCamp), WithLimits(Limits{MaxRecordBytes: 512}))
	deadline, stop := context.WithTimeout(t.Context(), 10*time.Second)
	defer stop()
	ctx, cancel := context.WithCancel(deadline)
	defer cancel()
	type result struct {
		session *Session
		err     error
	}
	finished := make(chan result, 1)
	go func() { session, err := client.CreateSession(ctx, nil); finished <- result{session, err} }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(ledger + ".ready"); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		select {
		case result := <-finished:
			t.Fatalf("child exited before cancellation barrier: %v", result.err)
		case <-deadline.Done():
			t.Fatal(deadline.Err())
		case <-ticker.C:
		}
	}
	cancel() // The fake emits the oversized record only after receiving this TERM.
	select {
	case result := <-finished:
		var typed *Error
		if result.session == nil || result.session.Ref().SessionID != "created" || !errors.As(result.err, &typed) || typed.Code != "output_limit" || typed.ExitCode == nil || *typed.ExitCode != 0 || !errors.Is(result.err, execclient.ErrOutputLimit) || !errors.Is(result.err, context.Canceled) {
			t.Fatalf("terminal/ctx masked independent limit: session=%v error=%+v", result.session, typed)
		}
	case <-deadline.Done():
		t.Fatal(deadline.Err())
	}
}

func TestProcessHandleCapabilitiesInvalidateOnReplacementAndGate(t *testing.T) {
	publicGuard(t)
	t.Run("replacement", func(t *testing.T) {
		client, ledger := handlesClient(t, "replace-binary")
		if _, err := client.Capabilities(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ledger+".mode", []byte("old"), 0600); err != nil {
			t.Fatal(err)
		}
		source, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		replacement := client.binary + ".replacement"
		if err := os.WriteFile(replacement, data, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(replacement, client.binary); err != nil {
			t.Fatal(err)
		}
		_, err = client.OpenTurn(t.Context(), TurnRef{SessionRef: SessionRef{CampID: handleCamp, SessionID: "session"}, TurnID: "turn"})
		var typed *Error
		if !errors.As(err, &typed) || typed.Code != "unsupported_protocol" {
			t.Fatalf("stale executable capabilities used: %v", err)
		}
		for _, call := range readHandleCalls(t, ledger) {
			if call.Args[0] != "doctor" {
				t.Fatalf("replacement invoked unsupported read: %v", call.Args)
			}
		}
	})
	t.Run("gate", func(t *testing.T) {
		client, ledger := handlesClient(t, "normal")
		if err := os.WriteFile(ledger+".mode", []byte("read-gate"), 0600); err != nil {
			t.Fatal(err)
		}
		ref := TurnRef{SessionRef: SessionRef{CampID: handleCamp, SessionID: "session"}, TurnID: "turn"}
		_, err := client.OpenTurn(t.Context(), ref)
		var typed *Error
		if !errors.As(err, &typed) || typed.Code != "unsupported_capability" {
			t.Fatalf("gate error=%v", err)
		}
		if got := len(readHandleCalls(t, ledger)); got != 2 {
			t.Fatalf("gate was silently retried: calls=%d", got)
		}
		if err := os.WriteFile(ledger+".mode", []byte("normal"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := client.OpenTurn(t.Context(), ref); err != nil {
			t.Fatal(err)
		}
		doctors := 0
		for _, call := range readHandleCalls(t, ledger) {
			if call.Args[0] == "doctor" {
				doctors++
			}
		}
		if doctors != 2 {
			t.Fatalf("gate did not invalidate discovery: doctors=%d", doctors)
		}
	})
}
