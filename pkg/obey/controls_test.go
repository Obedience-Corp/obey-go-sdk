package obey

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestControlTurnSnapshotExactAndScoped(t *testing.T) {
	ref := TurnRef{SessionRef: SessionRef{CampID: "camp", SessionID: "session"}, TurnID: "turn", IdempotencyKey: "key"}
	raw := json.RawMessage(`{"id":"turn","session_id":"session","campaign_id":"camp","status":"TURN_RECOVERY_REQUIRED","version":"9007199254740993","requested_model":"selected","latest_attempt":{"id":"attempt","turn_id":"turn"},"usage":{"tokens_input":"9007199254740993","cost_usd_ticks":"9007199254740993"},"recovery":{"reason":"post_dispatch_ambiguous"}}`)
	state, err := decodeControlTurn(raw, ref)
	if err != nil || state.Version != 9007199254740993 || state.Usage.InputTokens != 9007199254740993 || state.Usage.CostUSDTicks == nil || *state.Usage.CostUSDTicks != 9007199254740993 || state.Ref != ref || !state.RecoveryRequired || state.RecoveryReason != "post_dispatch_ambiguous" || state.LatestAttemptID != "attempt" || state.RequestedModel != "selected" {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	raw[0] = '!'
	if state.Raw[0] != '{' {
		t.Fatal("raw aliases caller")
	}
	for _, foreign := range []string{`{"id":"other","session_id":"session","campaign_id":"camp","status":"TURN_COMPLETED"}`, `{"id":"turn","session_id":"other","campaign_id":"camp","status":"TURN_COMPLETED"}`, `{"id":"turn","session_id":"session","campaign_id":"camp","status":"TURN_COMPLETED","latest_attempt":{"turn_id":"other"}}`} {
		_, err := decodeControlTurn(json.RawMessage(foreign), ref)
		var typed *Error
		if !errors.As(err, &typed) || typed.Code != "scope_mismatch" || typed.TurnID != "turn" {
			t.Fatalf("foreign=%v", err)
		}
	}
}
func TestControlClosedClientPrecedesValidation(t *testing.T) {
	c := &Client{closed: true}
	s := &Session{client: c}
	turn := &Turn{client: c}
	checks := []func() error{func() error { _, err := s.PendingApprovals(t.Context()); return err }, func() error { return s.RespondApproval(t.Context(), ApprovalDecision{}) }, func() error { return s.Stop(t.Context()) }, func() error { _, err := turn.Cancel(t.Context()); return err }}
	for _, check := range checks {
		var typed *Error
		if err := check(); !errors.As(err, &typed) || typed.Code != "closed_client" {
			t.Fatalf("closed=%v", err)
		}
	}
}
