package obey

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestHandlesRejectBlankIdentityWithoutClientOrSpawn(t *testing.T) {
	client := &Client{}
	for _, ref := range []TurnRef{{}, {SessionRef: SessionRef{CampID: "camp"}}, {SessionRef: SessionRef{SessionID: "session"}}, {SessionRef: SessionRef{CampID: "camp", SessionID: "session"}}} {
		turn, err := client.OpenTurn(context.Background(), ref)
		var typed *Error
		if turn != nil || !errors.As(err, &typed) || typed.Code != "invalid_input" {
			t.Fatalf("ref=%+v turn=%v err=%v", ref, turn, err)
		}
	}
	for _, ref := range []SessionRef{{}, {CampID: "camp"}, {SessionID: "session"}} {
		if session, err := client.OpenSession(context.Background(), ref); session != nil || err == nil {
			t.Fatal("blank session accepted")
		}
	}
}
func TestTurnStateExactScopeNumbersUnknownAndMetadata(t *testing.T) {
	ref := TurnRef{SessionRef: SessionRef{CampID: "camp", SessionID: "session"}, TurnID: "turn", IdempotencyKey: "caller-key", ClientRef: "caller-ref"}
	for _, status := range []string{"completed", "recovery_required", "historical_unknown", "future_status", "42"} {
		data, _ := json.Marshal(map[string]any{"schema_version": "obey.turn.v1", "turn": map[string]any{"id": "turn", "session_id": "session", "campaign_id": "camp", "status": status, "version": int64(9007199254740993), "latest_attempt": map[string]any{"id": "attempt", "turn_id": "turn"}, "recovery": map[string]any{"reason": "post_dispatch_ambiguous"}, "usage": map[string]any{"tokens_input": int64(9007199254740993), "tokens_output": 0, "usage_is_incomplete": false}, "future": map[string]any{"unchanged": true}}})
		state, err := decodeTurnState(data, ref)
		if err != nil {
			t.Fatal(err)
		}
		if state.Status != canonicalTurnStatus(status) || state.Ref != ref || state.Version != 9007199254740993 || state.Usage.InputTokens != 9007199254740993 || state.LatestAttemptID != "attempt" || state.Usage.Complete == nil || !*state.Usage.Complete || state.Usage.CostUSDTicks != nil {
			t.Fatalf("state=%+v", state)
		}
		var retained map[string]any
		if json.Unmarshal(state.Raw, &retained) != nil {
			t.Fatal("raw missing")
		}
		other := ref
		other.SessionID = "foreign"
		if _, err := decodeTurnState(data, other); err == nil {
			t.Fatal("foreign session accepted")
		}
	}
	turn := &Turn{ref: ref}
	if turn.Acceptance() != nil || turn.Ref() != ref {
		t.Fatal("reopen invented acceptance or lost optional metadata")
	}
	turn.acceptance = &Acceptance{Ref: ref, Status: TurnQueued, QueuePosition: new(uint32)}
	receipt := turn.Acceptance()
	*receipt.QueuePosition = 9
	receipt.Ref.TurnID = "changed"
	if turn.Acceptance().Ref != ref || *turn.Acceptance().QueuePosition != 0 {
		t.Fatal("receipt exposed internal state")
	}
}
func TestErrorCopiesPreserveNewScopeAndRaw(t *testing.T) {
	original := &Error{Code: "indeterminate_acceptance", ObservedSessionID: "observed", RequestedSessionID: "requested", TargetTurnID: "target", CallID: "call", DecisionIndeterminate: true, RawRecord: json.RawMessage(`{"future":1}`)}
	copy := cloneError(original).(*Error)
	copy.RawRecord[0] = 'x'
	if string(original.RawRecord) != `{"future":1}` || copy.ObservedSessionID != "observed" || !copy.DecisionIndeterminate {
		t.Fatal("raw/scope copy failed")
	}
}

func TestClosedHandleClientPrecedesInputValidationWithoutSpawn(t *testing.T) {
	client := &Client{closed: true}
	calls := []func() error{
		func() error { _, err := client.OpenTurn(context.Background(), TurnRef{}); return err },
		func() error {
			_, err := client.OpenTurn(context.Background(), TurnRef{SessionRef: SessionRef{CampID: "c", SessionID: "s"}, TurnID: "t"})
			return err
		},
		func() error { _, err := client.OpenSession(context.Background(), SessionRef{}); return err },
		func() error { _, err := client.CreateSession(context.Background(), nil); return err },
		func() error {
			_, err := client.CreateSession(context.Background(), &SessionOptions{Provider: "p", Model: "m", Camp: "c"})
			return err
		},
		func() error { _, err := client.Status(context.Background()); return err },
	}
	for _, call := range calls {
		var typed *Error
		if err := call(); !errors.As(err, &typed) || typed.Code != "closed_client" {
			t.Fatalf("closed client validation precedence=%v", err)
		}
	}
}
