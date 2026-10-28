//go:build process_tests

package obey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func init() {
	if os.Getenv("OBEY_CONTROLS_FAKE") != "1" {
		return
	}
	if os.Getenv("OBEY_SDK_TEST_CONTAINER") != "1" {
		os.Exit(99)
	}
	args := os.Args[1:]
	if len(args) > 1 && args[0] == "--socket" {
		args = args[2:]
	}
	path := os.Getenv("OBEY_CONTROLS_LOG")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(98)
	}
	_ = json.NewEncoder(file).Encode(handleCall{Args: args})
	_ = file.Close()
	mode := os.Getenv("OBEY_CONTROL_CASE")
	emit := func(v any) { _ = json.NewEncoder(os.Stdout).Encode(v) }
	if args[0] == "doctor" {
		caps := map[string]any{}
		for _, pair := range [][2]string{{"daemon_status", "obey.daemon.status"}, {"machine_read_errors", "obey.machine.error"}, {"session_approve", "obey.session.approve"}, {"session_stop", "obey.session.stop"}, {"turn_cancel", "obey.turn.cancel"}} {
			caps[pair[0]] = map[string]any{"protocol": pair[1], "versions": []int{1}}
		}
		emit(map[string]any{"schema": "obey.doctor", "version": 1, "status": "pass", "capabilities": caps})
		os.Exit(0)
	}
	record := func(name, kind string, extra map[string]any) {
		v := map[string]any{"protocol": name, "version": 1, "type": kind}
		if args[0] != "status" {
			v["campaign_id"] = handleCamp
			v["session_id"] = "session"
			if args[0] == "turn" {
				v["turn_id"] = "turn"
			}
		}
		if name == "obey.session.approve" {
			v["call_id"] = "shared-call"
			if mode == "foreign-call" || mode == "foreign-refusal" || (mode == "foreign-terminal" && kind == "terminal") {
				v["call_id"] = "foreign"
			}
			if mode == "missing-call" || mode == "missing-refusal" || (mode == "missing-terminal" && kind == "terminal") {
				delete(v, "call_id")
			}
		}
		for k, value := range extra {
			v[k] = value
		}
		if name == "obey.session.approve" {
			if mode == "foreign-call" || mode == "foreign-refusal" || (mode == "foreign-terminal" && kind == "terminal") {
				v["call_id"] = "foreign"
			}
			if mode == "missing-call" || mode == "missing-refusal" || (mode == "missing-terminal" && kind == "terminal") {
				delete(v, "call_id")
			}
		}
		emit(v)
	}
	terminal := func(name string) {
		record(name, "terminal", map[string]any{"status": "completed", "outcome": "rpc_completed", "cleanup": "not_owned"})
	}
	if args[0] == "status" {
		record("obey.daemon.status", "event", map[string]any{"payload_type": "local.v1.PingResponse", "payload": map[string]any{"capabilities": map[string]bool{"chat_surfaces": mode != "gate-off", "cancel_turn": mode != "no-cancel"}}})
		terminal("obey.daemon.status")
		os.Exit(0)
	}
	if args[0] == "session" && args[1] == "approvals" {
		scope := "session"
		if mode == "foreign-pending" {
			scope = "foreign"
		}
		approval := map[string]any{"session_id": scope, "campaign_id": handleCamp, "request": map[string]any{"call_id": "shared-call", "tool_name": "fixture", "arguments": "{}"}, "requested_at": "2026-10-28T12:00:00.000000001Z", "expires_at": "2026-10-28T12:01:00Z"}
		if mode == "plan-notice" {
			approval["request"] = map[string]any{"reason": "notice"}
		}
		emit(map[string]any{"approvals": []any{approval, approval}})
		os.Exit(0)
	}
	name, kind := "obey.session.approve", "local.v1.RespondApprovalResponse"
	if args[0] == "turn" {
		name, kind = "obey.turn.cancel", "local.v1.CancelTurnResponse"
	} else if args[1] == "stop" {
		name, kind = "obey.session.stop", "local.v1.StopSessionResponse"
	}
	if mode == "decision-cancel" || mode == "stop-cancel" {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM)
		_ = os.WriteFile(path+".ready", []byte("RPC may have happened"), 0600)
		<-sig
		record(name, "error", map[string]any{"code": "canceled", "message": "caller canceled after possible RPC", "severity": "error", "call_id": "shared-call", "decision_indeterminate": true})
		record(name, "terminal", map[string]any{"status": "canceled", "outcome": "canceled", "cleanup": "not_owned", "decision_indeterminate": true})
		os.Exit(9)
	}
	if strings.HasPrefix(mode, "error:") || mode == "foreign-refusal" || mode == "missing-refusal" {
		code := strings.TrimPrefix(mode, "error:")
		if mode == "foreign-refusal" || mode == "missing-refusal" {
			code = "approval_resolved"
		}
		record(name, "error", map[string]any{"code": code, "message": "original refusal", "severity": "error", "daemon_code": 5004, "grpc_code": "InvalidArgument", "call_id": "shared-call", "details": map[string]any{"approval_state": "fixture", "exact": "retained"}})
		record(name, "terminal", map[string]any{"status": "failed", "outcome": "failed", "cleanup": "not_owned"})
		os.Exit(7)
	}
	payload := map[string]any{}
	if kind == "local.v1.CancelTurnResponse" {
		status := "TURN_CANCELLED"
		if mode == "already-completed" {
			status = "TURN_COMPLETED"
		}
		payload["status"] = status
		if mode == "unknown-status" {
			payload["status"] = 999
		}
		scope := "session"
		if mode == "foreign-cancel" {
			scope = "foreign"
		}
		payload["turn"] = map[string]any{"id": "turn", "session_id": scope, "campaign_id": handleCamp, "status": status, "version": "9007199254740993"}
	}
	if kind == "local.v1.StopSessionResponse" {
		scope := "session"
		if mode == "foreign-stop" {
			scope = "foreign"
		}
		payload["session"] = map[string]any{"id": scope, "campaign_id": handleCamp, "status": "SESSION_STATUS_STOPPED"}
	}
	extra := map[string]any{"payload_type": kind, "payload": payload}
	if name == "obey.session.approve" {
		extra["call_id"] = "shared-call"
		if mode == "foreign-call" {
			extra["call_id"] = "foreign"
		}
	}
	record(name, "event", extra)
	terminal(name)
	os.Exit(0)
}
func controlsClient(t *testing.T, mode string) (*Client, string) {
	t.Helper()
	publicGuard(t)
	root := t.TempDir()
	ledger := filepath.Join(root, "calls")
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewClient(bin, WithEnvironment(map[string]string{"OBEY_CONTROLS_FAKE": "1", "OBEY_CONTROL_CASE": mode, "OBEY_CONTROLS_LOG": ledger}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, ledger
}
func TestProcessControlsScopeAckAndErrors(t *testing.T) {
	for _, tc := range []struct{ mode, operation, code string }{{"ok", "pending", ""}, {"foreign-pending", "pending", "scope_mismatch"}, {"plan-notice", "pending", "protocol_error"}, {"ok", "approve", ""}, {"ok", "deny", ""}, {"foreign-call", "approve", "scope_mismatch"}, {"missing-call", "approve", "scope_mismatch"}, {"foreign-refusal", "approve", "scope_mismatch"}, {"missing-refusal", "approve", "scope_mismatch"}, {"foreign-terminal", "approve", "scope_mismatch"}, {"missing-terminal", "approve", "scope_mismatch"}, {"error:approval_expired", "approve", "approval_expired"}, {"error:approval_resolved", "approve", "approval_resolved"}, {"error:stale_call", "approve", "stale_call"}, {"error:scope_mismatch", "approve", "scope_mismatch"}, {"error:unsupported_capability", "approve", "unsupported_capability"}, {"gate-off", "approve", "unsupported_capability"}, {"ok", "cancel", ""}, {"already-completed", "cancel", ""}, {"unknown-status", "cancel", "protocol_error"}, {"foreign-cancel", "cancel", "scope_mismatch"}, {"no-cancel", "cancel", "unsupported_capability"}, {"ok", "stop", ""}, {"foreign-stop", "stop", "scope_mismatch"}, {"error:timeout", "stop", "timeout"}} {
		t.Run(tc.mode+"/"+tc.operation, func(t *testing.T) {
			c, ledger := controlsClient(t, tc.mode)
			s := &Session{client: c, ref: SessionRef{CampID: handleCamp, SessionID: "session"}}
			turn := &Turn{client: c, ref: TurnRef{SessionRef: s.ref, TurnID: "turn"}}
			var err error
			switch tc.operation {
			case "pending":
				var pending []Approval
				pending, err = s.PendingApprovals(t.Context())
				if err == nil && (len(pending) != 1 || pending[0].RequestedAt == nil || pending[0].ExpiresAt == nil || pending[0].RequestedAt.Nanosecond() != 1 || pending[0].CallID != "shared-call") {
					t.Fatalf("pending=%+v", pending)
				}
			case "approve", "deny":
				err = s.RespondApproval(t.Context(), ApprovalDecision{CallID: "shared-call", Approve: tc.operation == "approve", Reason: "exact reason"})
			case "cancel":
				var result *CancelResult
				result, err = turn.Cancel(t.Context())
				if err == nil && (result.State == nil || result.State.Version != 9007199254740993 || result.Status != result.State.Status) {
					t.Fatalf("cancel=%+v", result)
				}
				if tc.mode == "already-completed" && err == nil && result.Status != TurnCompleted {
					t.Fatal(result)
				}
			case "stop":
				err = s.Stop(t.Context())
			}
			var typed *Error
			if tc.code == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.As(err, &typed) || typed.Code != tc.code {
				t.Fatalf("code=%s err=%v", tc.code, err)
			}
			if strings.HasPrefix(tc.mode, "foreign-") || strings.HasPrefix(tc.mode, "missing-") {
				if tc.operation == "approve" && (!typed.DecisionIndeterminate || typed.CallID != "shared-call" || len(typed.RawRecord) == 0) {
					t.Fatalf("correlation uncertainty=%+v", typed)
				}
			}
			if tc.mode == "error:timeout" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("remote timeout cause lost")
			}
			if strings.HasPrefix(tc.mode, "error:") && (typed.DaemonCode == nil || *typed.DaemonCode != 5004 || typed.GRPCCode != "InvalidArgument" || !strings.Contains(string(typed.Details), "retained")) {
				t.Fatalf("raw refusal=%+v", typed)
			}
			calls := readHandleCalls(t, ledger)
			mutations := 0
			for _, call := range calls {
				args := strings.Join(call.Args, " ")
				if strings.Contains(args, "session approve") || strings.Contains(args, "turn cancel") || strings.Contains(args, "session stop") {
					mutations++
					if !strings.Contains(args, "--campaign "+handleCamp) || !strings.Contains(args, "session") {
						t.Fatal(args)
					}
					if tc.operation == "deny" && !strings.Contains(args, "--deny") {
						t.Fatal(args)
					}
				}
			}
			want := 1
			if tc.operation == "pending" || tc.mode == "gate-off" || tc.mode == "no-cancel" {
				want = 0
			}
			if mutations != want {
				t.Fatalf("mutations=%d want=%d calls=%v", mutations, want, calls)
			}
		})
	}
}
func TestProcessControlsCancellationUncertainNoRetry(t *testing.T) {
	for _, mode := range []string{"decision-cancel", "stop-cancel"} {
		t.Run(mode, func(t *testing.T) {
			c, ledger := controlsClient(t, mode)
			s := &Session{client: c, ref: SessionRef{CampID: handleCamp, SessionID: "session"}}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				if mode == "decision-cancel" {
					done <- s.RespondApproval(ctx, ApprovalDecision{CallID: "shared-call", Approve: true})
				} else {
					done <- s.Stop(ctx)
				}
			}()
			deadline := time.After(10 * time.Second)
			for {
				if _, err := os.Stat(ledger + ".ready"); err == nil {
					break
				}
				select {
				case err := <-done:
					t.Fatalf("before RPC barrier: %v", err)
				case <-deadline:
					t.Fatal("missing RPC barrier")
				case <-time.After(10 * time.Millisecond):
				}
			}
			cancel()
			err := <-done
			var typed *Error
			if !errors.As(err, &typed) || typed.Code != "canceled" || !errors.Is(err, context.Canceled) || (mode == "decision-cancel" && (!typed.DecisionIndeterminate || typed.CallID != "shared-call")) {
				t.Fatalf("uncertain=%+v %v", typed, err)
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, call := range readHandleCalls(t, ledger) {
				if len(call.Args) > 1 && (call.Args[1] == "approve" || call.Args[1] == "stop") {
					count++
				}
			}
			if count != 1 {
				t.Fatal(fmt.Sprintf("control retry count%d", count))
			}
		})
	}
}
