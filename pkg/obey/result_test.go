package obey

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/obediencecorp/obey-go-sdk/internal/execclient"
	"github.com/obediencecorp/obey-go-sdk/internal/protocol"
)

func TestResultCorpus(t *testing.T) {
	for _, name := range []string{"v1_success", "v1_empty_unknown_usage", "v1_cleanup_warning", "v2_completed", "v2_cleanup_warning", "v2_daemon_failure", "v1_canceled", "v1_timeout", "unknown_raw_payload", "unknown_enum", "missing_terminal", "nonzero_completed"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile("../../internal/protocol/testdata/fixtures/" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var f struct {
				Schema, Expected string
				Records          []json.RawMessage
				Exit             int `json:"exit_code"`
			}
			if err := json.Unmarshal(data, &f); err != nil {
				t.Fatal(err)
			}
			version := 2
			if f.Schema == "run-v1" {
				version = 1
			}
			b := newResultBuilder("obey.session.run", version, 0)
			var parseErr error
			for _, r := range f.Records {
				if _, parseErr = b.add(r); parseErr != nil {
					break
				}
			}
			result, err := b.finish(execclient.Outcome{ExitCode: f.Exit}, parseErr)
			if f.Expected == "success" || f.Expected == "success_with_cleanup_warning" {
				if err != nil {
					t.Fatal(err)
				}
				if f.Expected == "success_with_cleanup_warning" && result.Cleanup != CleanupStopFailed {
					t.Fatal(result.Cleanup)
				}
				if name == "v2_completed" && (result.Usage.CachedTokens != 9007199254740993 || result.Usage.CostUSDTicks == nil || *result.Usage.CostUSDTicks != 10000000000) {
					t.Fatalf("%+v", result)
				}
				if result.Usage.Complete != nil {
					t.Fatal("synthesized usage completeness")
				}
			} else {
				var e *Error
				if !errors.As(err, &e) || e.Code != f.Expected {
					t.Fatalf("%v", err)
				}
			}
		})
	}
}
func TestResultContentBoundAndPartial(t *testing.T) {
	b := newResultBuilder("obey.session.run", 1, 3)
	for _, data := range []string{`{"protocol":"obey.session.run","version":1,"type":"session_created","session_id":"s","campaign_id":"c"}`, `{"protocol":"obey.session.run","version":1,"type":"assistant_content","session_id":"s","content":"four","final":true}`} {
		_, err := b.add([]byte(data))
		if err != nil {
			result, err := b.finish(execclient.Outcome{ExitCode: 9, StderrTail: "tail", Err: context.Canceled}, err)
			var e *Error
			if !errors.As(err, &e) || e.Code != "output_limit" || !errors.Is(err, context.Canceled) || result.Session.SessionID != "s" || e.ExitCode == nil || *e.ExitCode != 9 || e.StderrTail != "tail" {
				t.Fatalf("%+v %v", result, err)
			}
			return
		}
	}
	t.Fatal("oversized content accepted")
}
func TestCapabilityCacheCopiesAndIdentity(t *testing.T) {
	data := []byte(`{"schema":"obey.doctor","version":9,"status":"fail","capabilities":{"x":{"protocol":"unknown.future","versions":[7]}}}`)
	var c capabilityCache
	if err := c.store("binary1", data); err != nil {
		t.Fatal(err)
	}
	data[0] = 'x'
	a := c.load("binary1")
	a.Protocols["x"] = ProtocolSupport{}
	a.Raw[0] = 'x'
	if b := c.load("binary1"); b.Protocols["x"].Protocol != "unknown.future" || !json.Valid(b.Raw) || b.HealthStatus != "fail" {
		t.Fatal(b)
	}
	if c.load("binary2") != nil {
		t.Fatal("stale executable cache")
	}
}
func TestErrorPreservesUnknownDetails(t *testing.T) {
	b := newResultBuilder("obey.session.send", 1, 0)
	records := []string{`{"protocol":"obey.session.send","version":1,"type":"error","severity":"error","code":"future_code","message":"precise daemon reason","daemon_code":991,"grpc_code":"PermissionDenied","details":{"opaque":9007199254740993},"idempotency_key":"key","client_ref":"ref","acceptance_indeterminate":true}`, `{"protocol":"obey.session.send","version":1,"type":"terminal","status":"failed","outcome":"indeterminate","cleanup":"not_owned"}`}
	for _, r := range records {
		if _, err := b.add([]byte(r)); err != nil {
			t.Fatal(err)
		}
	}
	_, err := b.finish(execclient.Outcome{ExitCode: 17, Err: context.Canceled}, nil)
	var e *Error
	var pe *protocol.Error
	if !errors.As(err, &e) || e.Code != "future_code" || !errors.As(err, &pe) || !errors.Is(err, context.Canceled) || e.DaemonCode == nil || *e.DaemonCode != 991 || e.IdempotencyKey != "key" || !e.AcceptanceIndeterminate || string(e.Details) != `{"opaque":9007199254740993}` {
		t.Fatalf("%+v %v", e, err)
	}
}

func TestIndeterminateReceiptPreservesRetryIdentity(t *testing.T) {
	b := newResultBuilder("obey.session.send", 1, 0)
	data := []byte(`{"protocol":"obey.session.send","version":1,"type":"event","session_id":"s","campaign_id":"c","turn_id":"t","idempotency_key":"original-key","client_ref":"job","payload_type":"local.v1.SendMessageResponse","payload":{"accepted":{"turn_id":"t","status":"TURN_RUNNING"}}}`)
	if _, err := b.add(data); err != nil {
		t.Fatal(err)
	}
	result, err := b.finish(execclient.Outcome{ExitCode: 5}, nil)
	var e *Error
	if !errors.As(err, &e) || e.Code != "indeterminate_acceptance" || e.IdempotencyKey != "original-key" || e.ClientRef != "job" || result.TurnID != "t" || !e.AcceptanceIndeterminate {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestCompletedDoesNotMaskJoinedTransportFailure(t *testing.T) {
	data, err := os.ReadFile("../../internal/protocol/testdata/fixtures/v1_success.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct{ Records []json.RawMessage }
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	ioFailure := errors.New("distinct read failure")
	for _, cause := range []error{execclient.ErrOutputLimit, ioFailure} {
		b := newResultBuilder("obey.session.run", 1, 0)
		for _, r := range f.Records {
			if _, err := b.add(r); err != nil {
				t.Fatal(err)
			}
		}
		_, err := b.finish(execclient.Outcome{ExitCode: 0, Err: errors.Join(context.Canceled, cause)}, nil)
		if err == nil || !errors.Is(err, context.Canceled) || !errors.Is(err, cause) {
			t.Fatal(err)
		}
		if cause == execclient.ErrOutputLimit {
			var e *Error
			if !errors.As(err, &e) || e.Code != "output_limit" {
				t.Fatal(err)
			}
		}
	}
	b := newResultBuilder("obey.session.run", 1, 0)
	for _, r := range f.Records {
		if _, err := b.add(r); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.finish(execclient.Outcome{ExitCode: 0, Err: errors.Join(context.Canceled)}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestCallerDeadlineNormalizesCLICanceled(t *testing.T) {
	b := newResultBuilder("obey.session.run", 1, 0)
	for _, record := range []string{`{"protocol":"obey.session.run","version":1,"type":"session_created","session_id":"s","campaign_id":"c"}`, `{"protocol":"obey.session.run","version":1,"type":"error","session_id":"s","code":"canceled","message":"CLI received SIGTERM"}`, `{"protocol":"obey.session.run","version":1,"type":"terminal","session_id":"s","status":"canceled"}`} {
		if _, err := b.add([]byte(record)); err != nil {
			t.Fatal(err)
		}
	}
	result, err := b.finish(execclient.Outcome{ExitCode: 1, Err: context.DeadlineExceeded}, nil)
	var failure *Error
	if !errors.As(err, &failure) || failure.Code != "timeout" || !errors.Is(err, context.DeadlineExceeded) || result.Cleanup != CleanupStopped || result.Diagnostics[0].Code != "canceled" {
		t.Fatalf("%+v %v", result, err)
	}
}
