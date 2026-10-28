//go:build process_tests

package obey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/obediencecorp/obey-go-sdk/internal/execclient"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func init() {
	if os.Getenv("OBEY_WAIT_FAKE") != "1" {
		return
	}
	if os.Getenv("OBEY_SDK_TEST_CONTAINER") != "1" {
		os.Exit(99)
	}
	args := os.Args[1:]
	mode := os.Getenv("OBEY_WAIT_CASE")
	root := os.Getenv("OBEY_WAIT_ROOT")
	if len(args) > 1 && args[0] == "--socket" {
		args = args[2:]
	}
	log, _ := os.OpenFile(filepath.Join(root, "calls"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	fmt.Fprintln(log, strings.Join(args, " "))
	_ = log.Close()
	emit := func(value any) { _ = json.NewEncoder(os.Stdout).Encode(value) }
	get := func(flag string) string {
		for i, arg := range args {
			if arg == flag && i+1 < len(args) {
				return args[i+1]
			}
		}
		return ""
	}
	if args[0] == "doctor" {
		emit(map[string]any{"schema": "obey.doctor", "version": 1, "status": "pass", "capabilities": map[string]any{"machine_read_errors": map[string]any{"protocol": "obey.machine.error", "versions": []int{1}}, "session_attach": map[string]any{"protocol": "obey.session.attach", "versions": []int{1}}}})
		os.Exit(0)
	}
	if args[0] == "turn" {
		calls, _ := os.ReadFile(filepath.Join(root, "calls"))
		reads := strings.Count(string(calls), "turn show")
		status := "completed"
		attempt := "a2"
		astate := "completed"
		if mode == "active" || mode == "redirect" || mode == "cancel" || mode == "drop" || mode == "backpressure" || mode == "sibling" || mode == "attach-limit" {
			if _, err := os.Stat(filepath.Join(root, "completed")); err != nil {
				status = "running"
				astate = "running"
			}
		}
		if (mode == "drop" || mode == "redirect" || mode == "sibling") && reads <= 2 {
			status = "running"
			astate = "running"
		}
		switch mode {
		case "recovery":
			status = "recovery_required"
		case "unknown":
			status = "TURN_FUTURE"
		case "failed":
			status = "failed"
		case "canceled":
			status = "cancelled"
		case "historical":
			status = "historical_unknown"
		}
		if mode == "changed-attempt" {
			if _, err := os.Stat(filepath.Join(root, "paged")); err == nil {
				attempt = "a3"
			}
		}
		if mode == "attach-limit" && status == "completed" {
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, err := os.Stat(filepath.Join(root, "reaped")); err == nil {
					break
				}
				if time.Now().After(deadline) {
					fmt.Fprintln(os.Stderr, "reader-triggered SIGTERM was not acknowledged before State")
					os.Exit(94)
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
		emit(map[string]any{"schema_version": "obey.turn.v1", "turn": map[string]any{"id": "target", "session_id": "session", "campaign_id": handleCamp, "status": status, "latest_attempt": map[string]any{"id": attempt, "turn_id": "target", "state": astate, "attempt_number": 2}, "usage": map[string]any{"tokens_output": int64(9007199254740993)}}})
		os.Exit(0)
	}
	if args[1] == "transcript" {
		row := func(seq, kind, turn, attempt, text string) map[string]any {
			return map[string]any{"id": "e" + seq, "session_id": "session", "campaign_id": handleCamp, "sequence_number": seq, "kind": kind, "activity_type": "AGENT_COMPLETION", "turn_id": turn, "attempt_id": attempt, "content": text}
		}
		if get("--cursor") == "foreign-cursor" {
			row := row("12", "completion", "target", "a2", "foreign")
			row["session_id"] = "successor"
			emit(map[string]any{"entries": []any{row}})
			os.Exit(0)
		}
		if strings.Contains(strings.Join(args, " "), "--newest-first") {
			if mode == "no-history" {
				emit(map[string]any{"entries": []any{}})
				os.Exit(0)
			}
			emit(map[string]any{"entries": []any{row("12", "completion", "target", "a2", "")}})
			os.Exit(0)
		}
		_ = os.WriteFile(filepath.Join(root, "paged"), []byte("yes"), 0600)
		text := "retained answer"
		if mode == "empty" {
			text = ""
		}
		entries := []any{row("9", "completion", "target", "old", "wrong old text"), row("10", "completion", "sibling", "a2", "wrong sibling"), row("11", "completion", "target", "a2", text), row("12", "tool_result", "target", "a2", "not answer")}
		if mode == "paged" {
			if get("--cursor") == "" {
				emit(map[string]any{"entries": entries[:2], "page": map[string]any{"has_more": true, "next_cursor": "opaque-page2"}})
			} else {
				emit(map[string]any{"entries": entries[2:]})
			}
			os.Exit(0)
		}
		if mode == "missing" {
			entries = entries[:2]
			entries = append(entries, row("12", "tool_result", "target", "a2", "not answer"))
		}
		if mode == "boundary" {
			entries = entries[:3]
		}
		if mode == "foreign-cursor" {
			emit(map[string]any{"entries": entries[:2], "page": map[string]any{"has_more": true, "next_cursor": "foreign-cursor"}})
			os.Exit(0)
		}
		if mode == "stalled" {
			emit(map[string]any{"page": map[string]any{"has_more": true, "next_cursor": "same"}})
			os.Exit(0)
		}
		emit(map[string]any{"entries": entries})
		os.Exit(0)
	}
	if args[1] == "attach" {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGTERM)
		defer signal.Stop(signals)
		observed := "session"
		if mode == "redirect" {
			observed = "successor"
		}
		emitAttach := func(payload any, turn string) {
			emit(map[string]any{"protocol": "obey.session.attach", "version": 1, "type": "event", "payload_type": "local.v1.AttachSessionResponse", "payload": payload, "campaign_id": handleCamp, "session_id": observed, "requested_session_id": "session", "observed_session_id": observed, "turn_id": turn})
		}
		_ = os.WriteFile(filepath.Join(root, "attached"), []byte("yes"), 0600)
		if mode == "sibling" {
			emitAttach(map[string]any{"live": map[string]any{"session_id": observed, "turn_id": "sibling", "type": "AGENT_COMPLETION", "completion": map[string]any{"content": "wrong sibling"}}}, "sibling")
		}
		emitAttach(map[string]any{"marker": map[string]any{"type": "ATTACH_MARKER_TYPE_BACKFILL_COMPLETE", "high_water_sequence": "12"}}, "")
		if mode != "cancel" {
			_ = os.WriteFile(filepath.Join(root, "completed"), []byte("yes"), 0600)
		}
		if mode == "attach-limit" {
			emitAttach(map[string]any{"future": strings.Repeat("x", 4096)}, "")
		}
		if mode == "backpressure" {
			for i := 0; i < 200; i++ {
				emitAttach(map[string]any{"live": map[string]any{"session_id": observed, "turn_id": "sibling", "type": "AGENT_MESSAGE_DELTA"}}, "sibling")
			}
		}
		// No target completion notification: periodic state reconciles a lost event.
		<-signals
		_ = os.WriteFile(filepath.Join(root, "reaped"), []byte("yes"), 0600)
		os.Exit(0)
	}
	os.Exit(98)
}
func waitClient(t *testing.T, mode string, options ...ClientOption) (*Client, string) {
	t.Helper()
	publicGuard(t)
	root := t.TempDir()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	options = append([]ClientOption{WithLimits(Limits{EventQueueSize: 1, ShutdownGrace: 2 * time.Second})}, options...)
	options = append(options, WithEnvironment(map[string]string{"OBEY_WAIT_FAKE": "1", "OBEY_WAIT_CASE": mode, "OBEY_WAIT_ROOT": root}))
	client, err := NewClient(binary, options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, root
}
func TestProcessWaitRetainedAndFailureStates(t *testing.T) {
	for _, tc := range []struct{ mode, code string }{{"completed", ""}, {"empty", ""}, {"paged", ""}, {"no-history", "result_unavailable"}, {"missing", "result_unavailable"}, {"boundary", "result_unavailable"}, {"changed-attempt", "result_unavailable"}, {"foreign-cursor", "scope_mismatch"}, {"stalled", "protocol_error"}, {"recovery", "recovery_required"}, {"unknown", "protocol_error"}, {"failed", "daemon_error"}, {"canceled", "canceled"}, {"historical", "result_unavailable"}} {
		t.Run(tc.mode, func(t *testing.T) {
			client, root := waitClient(t, tc.mode)
			turn := &Turn{client: client, ref: TurnRef{SessionRef: SessionRef{handleCamp, "session"}, TurnID: "target"}}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			result, err := turn.Wait(ctx)
			var typed *Error
			if tc.code == "" {
				if err != nil || result == nil || result.HighWaterSequence == nil || *result.HighWaterSequence != 12 || result.Usage.OutputTokens != 9007199254740993 || result.Cleanup != CleanupNotOwned {
					t.Fatalf("result=%+v err=%v", result, err)
				}
				text := "retained answer"
				if tc.mode == "empty" {
					text = ""
				}
				if result.Text != text {
					t.Fatalf("text=%q", result.Text)
				}
			} else if !errors.As(err, &typed) || typed.Code != tc.code {
				t.Fatalf("result=%+v err=%v want%s", result, err, tc.code)
			}
			calls, _ := os.ReadFile(filepath.Join(root, "calls"))
			if strings.Contains(string(calls), "send") || strings.Contains(string(calls), "stop") || strings.Contains(string(calls), "cancel") || strings.Contains(string(calls), "attach") {
				t.Fatalf("terminal wait spawned mutation/attach: %s", calls)
			}
		})
	}
}
func TestProcessWaitMarkerDropRedirectAndReceiverCleanup(t *testing.T) {
	for _, mode := range []string{"active", "drop", "redirect", "sibling", "backpressure", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			client, root := waitClient(t, mode)
			turn := &Turn{client: client, ref: TurnRef{SessionRef: SessionRef{handleCamp, "session"}, TurnID: "target"}}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			if mode == "cancel" {
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
				go func() {
					for ctx.Err() == nil {
						if _, err := os.Stat(filepath.Join(root, "attached")); err == nil {
							cancel()
							return
						}
						time.Sleep(10 * time.Millisecond)
					}
				}()
			}
			result, err := turn.Wait(ctx)
			if mode == "cancel" {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancel err=%v", err)
				}
			} else if err != nil || result.Text != "retained answer" || result.TurnID != "target" || result.Session.SessionID != "session" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if mode == "redirect" && result.ObservedSessionID != "successor" {
				t.Fatalf("redirect=%+v", result)
			}
			if _, err := os.Stat(filepath.Join(root, "reaped")); err != nil {
				t.Fatalf("receiver child not gracefully reaped=%v", err)
			}
			calls, _ := os.ReadFile(filepath.Join(root, "calls"))
			if strings.Contains(string(calls), "transcript id:successor") {
				t.Fatalf("redirect reused successor cursor scope: %s", calls)
			}
			if (mode == "drop" || mode == "redirect" || mode == "sibling") && strings.Count(string(calls), "turn show") < 4 {
				t.Fatalf("periodic state/recheck absent: %s", calls)
			}
			if strings.Contains(string(calls), " send ") || strings.Contains(string(calls), " stop ") || strings.Contains(string(calls), " cancel ") {
				t.Fatalf("wait mutated: %s", calls)
			}
		})
	}
}

func TestProcessWaitFinalContentLimitIndependentFromRecords(t *testing.T) {
	client, _ := waitClient(t, "completed", WithLimits(Limits{MaxResultBytes: 3}))
	turn := &Turn{client: client, ref: TurnRef{SessionRef: SessionRef{handleCamp, "session"}, TurnID: "target", IdempotencyKey: "original"}}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	result, err := turn.Wait(ctx)
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != "output_limit" || typed.TurnID != "target" || typed.IdempotencyKey != "original" || len(typed.RawRecord) == 0 || result.Cleanup != CleanupNotOwned {
		t.Fatalf("result=%+v err=%+v", result, err)
	}
}

func TestProcessWaitRetainedSuccessCannotMaskAttachOutputLimit(t *testing.T) {
	client, _ := waitClient(t, "attach-limit", WithLimits(Limits{MaxRecordBytes: 2048, ShutdownGrace: 2 * time.Second}))
	turn := &Turn{client: client, ref: TurnRef{SessionRef: SessionRef{handleCamp, "session"}, TurnID: "target"}}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	result, err := turn.Wait(ctx)
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != "output_limit" || !errors.Is(err, execclient.ErrOutputLimit) || typed.TurnID != "target" || result.Cleanup != CleanupNotOwned {
		t.Fatalf("retained result=%+v error=%+v", result, err)
	}
}
