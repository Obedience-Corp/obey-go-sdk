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
	if os.Getenv("OBEY_RICH_FAKE") != "1" {
		return
	}
	if os.Getenv("OBEY_SDK_TEST_CONTAINER") != "1" {
		os.Exit(99)
	}
	args := os.Args[1:]
	if len(args) > 1 && args[0] == "--socket" {
		args = args[2:]
	}
	mode := os.Getenv("OBEY_RICH_CASE")
	root := os.Getenv("OBEY_RICH_ROOT")
	f, _ := os.OpenFile(filepath.Join(root, "calls"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	fmt.Fprintln(f, strings.Join(args, " "))
	_ = f.Close()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	emit := func(value any) { _ = json.NewEncoder(os.Stdout).Encode(value) }
	get := func(flag string) string {
		for i, arg := range args {
			if arg == flag && i+1 < len(args) {
				return args[i+1]
			}
		}
		return ""
	}
	has := func(flag string) bool {
		for _, arg := range args {
			if arg == flag {
				return true
			}
		}
		return false
	}
	if args[0] == "doctor" {
		versions := []int{1, 2}
		if mode == "v1-only" || strings.HasPrefix(mode, "v1-remote-") {
			versions = []int{1}
		}
		emit(map[string]any{"schema": "obey.doctor", "version": 3, "status": "fail", "capabilities": map[string]any{"session_run": map[string]any{"protocol": "obey.session.run", "versions": versions}, "daemon_status": map[string]any{"protocol": "obey.daemon.status", "versions": []int{1}}, "session_attach": map[string]any{"protocol": "obey.session.attach", "versions": []int{1}}, "observe": map[string]any{"protocol": "obey.observe", "versions": []int{1}}}})
		os.Exit(1)
	}
	record := func(name string, version int, kind, session, turn string, fields map[string]any) map[string]any {
		if fields == nil {
			fields = map[string]any{}
		}
		fields["protocol"] = name
		fields["version"] = version
		fields["type"] = kind
		fields["campaign_id"] = handleCamp
		if session != "" {
			fields["session_id"] = session
		}
		if turn != "" {
			fields["turn_id"] = turn
		}
		return fields
	}
	event := func(name string, version int, wrapper string, payload any, session, turn string, scope map[string]any) {
		if scope == nil {
			scope = map[string]any{}
		}
		scope["payload_type"] = wrapper
		scope["payload"] = payload
		emit(record(name, version, "event", session, turn, scope))
	}
	if args[0] == "status" {
		features := map[string]bool{"chat_surfaces": true, "durable_turns": true, "turn_identity": true, "send_acceptance": true}
		if mode == "missing-identity" {
			features["turn_identity"] = false
		}
		if mode == "wrong-key" {
			delete(features, "turn_identity")
			features["turn_identity_events"] = true
		}
		event("obey.daemon.status", 1, "local.v1.PingResponse", map[string]any{"capabilities": features}, "", "", nil)
		emit(record("obey.daemon.status", 1, "terminal", "", "", map[string]any{"status": "completed", "outcome": "rpc_completed", "cleanup": "not_owned"}))
		if mode == "late-cancel" {
			_ = os.WriteFile(filepath.Join(root, "status-ready"), []byte("yes"), 0600)
			<-signals
		}
		os.Exit(0)
	}
	if args[0] == "run" {
		version := 1
		if get("--protocol-version") == "2" {
			version = 2
		}
		name := "obey.session.run"
		created := map[string]any{}
		if version == 2 {
			created["payload_type"] = "local.v1.CreateSessionResponse"
			created["payload"] = map[string]any{"session": map[string]any{"id": "session", "campaign_id": handleCamp}}
		}
		emit(record(name, version, "session_created", "session", "", created))
		if strings.Contains(mode, "remote-timeout") || strings.Contains(mode, "remote-canceled") {
			if get("--timeout") != "500ms" {
				os.Exit(98)
			}
			code := "timeout"
			if strings.Contains(mode, "remote-canceled") {
				code = "canceled"
			}
			emit(record(name, version, "error", "session", "", map[string]any{"code": code, "message": "CLI-owned request timeout/cancellation", "severity": "error"}))
			emit(record(name, version, "terminal", "session", "", map[string]any{"status": "failed", "outcome": "failed", "cleanup": "stopped"}))
			os.Exit(1)
		}

		if version == 1 {
			emit(record(name, 1, "assistant_content", "session", "", map[string]any{"content": "v1 answer", "final": true}))
			emit(record(name, 1, "usage", "session", "", map[string]any{"input_tokens": 1, "output_tokens": 2, "cached_tokens": 0, "latency_ms": 0}))
			emit(record(name, 1, "terminal", "session", "", map[string]any{"status": "completed"}))
			os.Exit(0)
		}
		send := func(body any) { event(name, 2, "local.v1.SendMessageResponse", body, "session", "turn", nil) }
		send(map[string]any{"accepted": map[string]any{"turn_id": "turn", "status": "TURN_QUEUED"}})
		activity := func(typ, arm string, payload any) {
			send(map[string]any{"event": map[string]any{"session_id": "session", "turn_id": "turn", "type": typ, arm: payload}})
		}
		activity("AGENT_TURN_STARTED", "turn_started", map[string]any{})
		if mode == "malformed-run" || mode == "foreign-cleanup" {
			send(map[string]any{"event": map[string]any{"session_id": "session", "turn_id": "turn", "type": "AGENT_TURN_FINISHED"}})
			<-signals
			terminal := record(name, 2, "terminal", "session", "turn", map[string]any{"status": "canceled", "outcome": "failed", "cleanup": "stopped"})
			if mode == "foreign-cleanup" {
				terminal["campaign_id"] = handleForeignCamp
			}
			emit(terminal)
			os.Exit(1)
		}
		activity("AGENT_MESSAGE_DELTA", "message_delta", map[string]any{"content": "chunk"})
		if mode == "unknown-failure" {
			event(name, 2, "local.future.Response", map[string]any{"future": map[string]any{"completion": "not evidence"}}, "session", "turn", nil)
			emit(record(name, 2, "error", "session", "turn", map[string]any{"code": "provider_broke", "message": "fixture failure", "severity": "error", "daemon_code": 42, "requested_session_id": "session", "observed_session_id": "session", "target_turn_id": "turn", "call_id": "call", "decision_indeterminate": true}))
			emit(record(name, 2, "terminal", "session", "turn", map[string]any{"status": "failed", "outcome": "failed", "cleanup": "stopped"}))
			os.Exit(1)
		}
		activity("AGENT_COMPLETION", "completion", map[string]any{"content": "v2 answer", "tokens_input": 3, "tokens_output": 2, "cached_tokens": "9007199254740993"})
		activity("AGENT_TURN_FINISHED", "turn_finished", map[string]any{"status": "TURN_COMPLETED"})
		event(name, 2, "local.v1.GetTurnResponse", map[string]any{"turn": map[string]any{"id": "turn", "session_id": "session", "campaign_id": handleCamp, "status": "TURN_COMPLETED", "latest_attempt": map[string]any{"id": "attempt", "turn_id": "turn", "state": "TURN_ATTEMPT_COMPLETED", "attempt_number": "1"}}}, "session", "turn", nil)
		emit(record(name, 2, "terminal", "session", "turn", map[string]any{"status": "completed", "outcome": "turn_completed", "cleanup": "stopped"}))
		os.Exit(0)
	}
	attach := args[0] == "session" && len(args) > 1 && args[1] == "attach"
	if attach || args[0] == "observe" {
		name := "obey.observe"
		session := ""
		scope := map[string]any{}
		if attach {
			name = "obey.session.attach"
			session = "session"
			if mode == "redirect" {
				session = "successor"
			}
			scope = map[string]any{"requested_session_id": "session", "observed_session_id": session}
		}
		if mode == "remote-timeout" {
			emit(record(name, 1, "error", session, "", map[string]any{"code": "timeout", "message": "remote read timed out", "severity": "error"}))
			emit(record(name, 1, "terminal", session, "", map[string]any{"status": "failed", "outcome": "failed", "cleanup": "not_owned"}))
			os.Exit(1)
		}
		if !attach && !has("--follow") {
			events := []any{map[string]any{"id": "history", "session_id": "session", "campaign_id": handleCamp, "sequence_number": "9007199254740993", "kind": "future", "message": "kept"}}
			event(name, 1, "local.v1.ListRecentActivityResponse", map[string]any{"events": events, "page": map[string]any{"has_more": true, "next_cursor": "opaque"}}, "", "", nil)
			emit(record(name, 1, "terminal", "", "", map[string]any{"status": "completed", "outcome": "observed", "cleanup": "not_owned"}))
			os.Exit(0)
		}
		packet := func(payload any, turn string) {
			copy := map[string]any{}
			for k, v := range scope {
				copy[k] = v
			}
			wrapper := "local.v1.AttachSessionResponse"
			if !attach {
				wrapper = "local.v1.WatchAllActivityResponse"
			}
			event(name, 1, wrapper, payload, session, turn, copy)
		}
		marker := map[string]any{"marker": map[string]any{"type": "ATTACH_MARKER_TYPE_BACKFILL_COMPLETE", "high_water_sequence": "0", "snapshot_version": "9007199254740993"}}
		if attach {
			packet(marker, "")
		} else {
			packet(map[string]any{"event": map[string]any{"meta_event": "META_EVENT_HELLO", "durable_sequence": "0"}}, "")
		}
		if mode == "unexpected-eof" {
			os.Exit(0)
		}
		if mode == "bad-second-scope" || mode == "bad-second-payload" {
			payload := map[string]any{"live": map[string]any{"session_id": session, "turn_id": "sibling", "type": "AGENT_TOOL_CALL"}}
			bad := record(name, 1, "event", session, "sibling", map[string]any{"payload_type": "local.v1.AttachSessionResponse", "payload": payload, "requested_session_id": "session", "observed_session_id": session})
			if mode == "bad-second-scope" {
				bad["campaign_id"] = handleForeignCamp
			}
			emit(bad)
		} else if mode != "block" && mode != "slow" {
			if attach {
				if !has("--no-backfill") {
					packet(map[string]any{"backfill": map[string]any{"id": "entry", "session_id": session, "campaign_id": handleCamp, "sequence_number": "12", "turn_id": "sibling", "attempt_id": "attempt", "kind": "completion", "activity_type": "AGENT_COMPLETION", "content": "backfill"}}, "sibling")
				}
				packet(marker, "")
				packet(map[string]any{"live": map[string]any{"session_id": session, "turn_id": "sibling", "durable_sequence": "0", "type": "AGENT_MESSAGE_DELTA", "message_delta": map[string]any{"content": "live"}}}, "sibling")
			} else {
				packet(map[string]any{"event": map[string]any{"session_id": "session", "dropped_count": "9007199254740993", "durable_sequence": "0", "meta_event": "META_EVENT_KEEPALIVE"}}, "")
			}
		}
		<-signals
		terminal := map[string]any{"status": "canceled", "outcome": "detached", "cleanup": "not_owned"}
		for k, v := range scope {
			terminal[k] = v
		}
		emit(record(name, 1, "terminal", session, "", terminal))
		os.Exit(1)
	}
	os.Exit(98)
}
func richClient(t *testing.T, mode string, options ...ClientOption) (*Client, string) {
	t.Helper()
	publicGuard(t)
	root := t.TempDir()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	base := []ClientOption{WithLimits(Limits{EventQueueSize: 1, ShutdownGrace: 3 * time.Second}), WithSessionDefaults(SessionOptions{Provider: "provider", Model: "model"})}
	base = append(base, options...)
	base = append(base, WithEnvironment(map[string]string{"OBEY_RICH_FAKE": "1", "OBEY_RICH_CASE": mode, "OBEY_RICH_ROOT": root}))
	client, err := NewClient(binary, base...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, root
}
func TestProcessRichRunSelectionAndKnownFailure(t *testing.T) {
	for _, tc := range []struct {
		mode       string
		camp, live bool
		version    int
		code       string
	}{{"modern", true, true, 2, ""}, {"modern", false, false, 1, ""}, {"modern", false, true, 0, "unsupported_capability"}, {"v1-only", true, false, 1, ""}, {"missing-identity", true, false, 1, ""}, {"wrong-key", true, true, 0, "unsupported_capability"}, {"unknown-failure", true, false, 2, "provider_broke"}, {"malformed-run", true, false, 2, "protocol_error"}, {"foreign-cleanup", true, false, 2, "protocol_error"}} {
		t.Run(tc.mode+fmt.Sprint(tc.camp, tc.live), func(t *testing.T) {
			opts := []ClientOption{}
			if tc.camp {
				opts = append(opts, WithCamp(handleCamp))
			}
			client, root := richClient(t, tc.mode, opts...)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			stream, err := client.StreamPrompt(ctx, "private prompt", &RunOptions{RequireLiveEvents: tc.live})
			var typed *Error
			if tc.version == 0 {
				if !errors.As(err, &typed) || typed.Code != tc.code {
					t.Fatalf("preflight=%+v", err)
				}
				calls, _ := os.ReadFile(filepath.Join(root, "calls"))
				if strings.Contains(string(calls), "run --input") {
					t.Fatalf("preflight mutated: %s", calls)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if (stream.Fidelity() == FidelityLive) != (tc.version == 2) {
				t.Fatal(stream.Fidelity())
			}
			unknown := false
			for stream.Next() {
				e := stream.Event()
				if e.PayloadType == "local.future.Response" {
					unknown = len(e.RawPayload) > 0 && e.Completion == nil
				}
			}
			result, err := stream.Result()
			if tc.code == "" {
				if err != nil || result.Protocol.Version != tc.version || result.Cleanup != CleanupStopped {
					t.Fatalf("result=%+v err=%v", result, err)
				}
			} else if !errors.As(err, &typed) || typed.Code != tc.code {
				t.Fatalf("failure=%+v", err)
			}
			if tc.mode == "unknown-failure" {
				if !unknown || len(typed.RawRecord) == 0 || typed.CallID != "call" || !typed.DecisionIndeterminate || typed.TargetTurnID != "turn" || typed.DaemonCode == nil || *typed.DaemonCode != 42 {
					t.Fatalf("lost raw/error metadata=%+v", typed)
				}
			}
			if tc.mode == "malformed-run" && result.Cleanup != CleanupStopped {
				t.Fatalf("known cleanup lost=%+v", result)
			}
			if tc.mode == "foreign-cleanup" && result.Cleanup != CleanupUnknown {
				t.Fatalf("foreign cleanup trusted=%+v", result)
			}
		})
	}
}
func TestProcessRichLateCancelPreflightClassification(t *testing.T) {
	client, root := richClient(t, "late-cancel", WithCamp(handleCamp))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		for ctx.Err() == nil {
			if _, err := os.Stat(filepath.Join(root, "status-ready")); err == nil {
				cancel()
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	_, err := client.StreamPrompt(ctx, "prompt", nil)
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != "canceled" || !errors.Is(err, context.Canceled) {
		t.Fatalf("late cancel=%+v", err)
	}
	calls, _ := os.ReadFile(filepath.Join(root, "calls"))
	if strings.Contains(string(calls), "run --input") {
		t.Fatalf("canceled preflight spawned run=%s", calls)
	}
}
func TestProcessEventStreamScopesProjectionAndRemoteTimeout(t *testing.T) {
	client, _ := richClient(t, "modern", WithCamp(handleCamp))
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	finite, err := client.Observe(ctx, ObserveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !finite.Next() || finite.Event().History == nil || finite.Event().History.NextCursor != "opaque" {
		t.Fatal("finite history missing")
	}
	for finite.Next() {
	}
	if finite.Err() != nil {
		t.Fatal(finite.Err())
	}
	session := &Session{client: client, ref: SessionRef{handleCamp, "session"}}
	attached, err := session.Attach(ctx, AttachOptions{NoBackfill: true})
	if err != nil {
		t.Fatal(err)
	}
	markers, delta := 0, false
	for i := 0; i < 3; i++ {
		if !attached.Next() {
			t.Fatal(attached.Err())
		}
		e := attached.Event()
		if e.Marker != nil {
			markers++
			if e.Marker.SnapshotVersion != 9007199254740993 {
				t.Fatal(e.Marker)
			}
		}
		if e.Delta != nil {
			delta = e.DurableSequence == nil
		}
	}
	if markers != 2 || !delta {
		t.Fatalf("markers%d delta%v", markers, delta)
	}
	_ = attached.Close()
	timeoutClient, _ := richClient(t, "remote-timeout", WithCamp(handleCamp))
	remote, err := timeoutClient.Observe(ctx, ObserveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for remote.Next() {
	}
	if !errors.Is(remote.Err(), context.DeadlineExceeded) || ctx.Err() != nil {
		t.Fatalf("remote timeout cause=%v parent=%v", remote.Err(), ctx.Err())
	}
}
func TestProcessEventStreamRollbackRedirectAndEOF(t *testing.T) {
	for _, mode := range []string{"bad-second-scope", "bad-second-payload", "redirect", "unexpected-eof"} {
		t.Run(mode, func(t *testing.T) {
			client, _ := richClient(t, mode)
			session := &Session{client: client, ref: SessionRef{handleCamp, "session"}}
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			options := AttachOptions{}
			if mode == "redirect" {
				options.FromSequence = 12
			}
			stream, err := session.Attach(ctx, options)
			var typed *Error
			if mode == "redirect" {
				if !errors.As(err, &typed) || typed.Code != "scope_mismatch" || typed.ObservedSessionID != "successor" {
					t.Fatalf("cursor redirect=%+v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !stream.Next() || stream.Event().Kind != EventMarker {
				t.Fatal("prefetched marker lost")
			}
			before := stream.Event()
			if stream.Next() {
				t.Fatal("invalid next frame accepted")
			}
			if stream.Err() == nil {
				t.Fatal("failure lost")
			}
			after := stream.Event()
			if after.Kind != before.Kind || after.Session != before.Session {
				t.Fatal("invalid frame replaced trusted event")
			}
			_ = stream.Close()
			if mode == "unexpected-eof" {
				if !errors.As(stream.Err(), &typed) || typed.Code != "incomplete_stream" {
					t.Fatal(stream.Err())
				}
			}
		})
	}
}
func TestProcessEventStreamDelayedConsumerPastEstablishment(t *testing.T) {
	client, _ := richClient(t, "slow", WithCamp(handleCamp))
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Second)
	defer cancel()
	session := &Session{client: client, ref: SessionRef{handleCamp, "session"}}
	attach, err := session.Attach(ctx, AttachOptions{NoBackfill: true})
	if err != nil {
		t.Fatal(err)
	}
	observe, err := client.Observe(ctx, ObserveOptions{Follow: true})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(unaryTimeout + time.Second)
	for _, stream := range []*EventStream{attach, observe} {
		if stream.process.Finished() {
			t.Fatal("healthy queued receipt got consumer-time deadline")
		}
		if !stream.Next() {
			t.Fatal(stream.Err())
		}
		_ = stream.Close()
	}
}

func TestProcessRichWireContextCausesWithLiveCaller(t *testing.T) {
	for _, mode := range []string{"v1-remote-timeout", "remote-timeout", "v1-remote-canceled", "remote-canceled"} {
		t.Run(mode, func(t *testing.T) {
			client, _ := richClient(t, mode, WithCamp(handleCamp))
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			timeout := 500 * time.Millisecond
			result, err := client.RunPrompt(ctx, "prompt", &RunOptions{Timeout: &timeout})
			var typed *Error
			expected := context.DeadlineExceeded
			code := "timeout"
			if strings.Contains(mode, "canceled") {
				expected = context.Canceled
				code = "canceled"
			}
			if !errors.As(err, &typed) || typed.Code != code || !errors.Is(err, expected) || ctx.Err() != nil || result.Cleanup != CleanupStopped {
				t.Fatalf("result=%+v err=%+v parent%v", result, err, ctx.Err())
			}
		})
	}
}
