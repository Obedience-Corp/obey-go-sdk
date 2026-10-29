package obey

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Obedience-Corp/obey-go-sdk/internal/protocol"
)

func TestSendOptionsOwnKeyAndLocalValidationBeforeSpawn(t *testing.T) {
	options := &SendOptions{Files: []string{"original"}, Mode: "plan", Model: "chosen", ClientRef: "ref"}
	prepared, err := prepareSendOptions(options)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.IdempotencyKey == "" || options.IdempotencyKey != "" {
		t.Fatal("generated key mutated caller or missing")
	}
	options.Files[0] = "changed"
	if prepared.Files[0] != "original" {
		t.Fatal("file options aliased")
	}
	for _, options := range []*SendOptions{{IdempotencyKey: " edge"}, {IdempotencyKey: strings.Repeat("x", 129)}, {ClientRef: strings.Repeat("x", 65)}, {Mode: "stop"}, {Mode: "unknown"}, {Model: " "}} {
		if _, err := prepareSendOptions(options); err == nil {
			t.Fatalf("invalid options accepted: %+v", options)
		}
	}
	for _, mode := range []string{"", "autonomous", "supervised", "plan", "discussion", "fest", "vendor:provider/value:sandbox"} {
		if _, err := prepareSendOptions(&SendOptions{Mode: mode}); err != nil {
			t.Fatal(err)
		}
	}
	client := &Client{}
	session := &Session{client: client, ref: SessionRef{CampID: "camp", SessionID: "session"}}
	for _, prompt := range []string{"", strings.Repeat("x", maxSendBytes+1)} {
		turn, err := session.Send(context.Background(), prompt, nil)
		var failure *Error
		if turn != nil || !errors.As(err, &failure) || failure.Code != "invalid_input" || failure.IdempotencyKey == "" || failure.AcceptanceIndeterminate {
			t.Fatalf("pre-spawn invalid result: turn=%v err=%+v", turn, failure)
		}
	}
}
func TestSendPartialIdentityIsNotSuccessfulAcceptance(t *testing.T) {
	session := &Session{client: &Client{}, ref: SessionRef{CampID: "camp", SessionID: "session"}}
	raw := func(camp, session, outer, inner, status string) json.RawMessage {
		data, _ := json.Marshal(map[string]any{"protocol": "obey.session.send", "version": 1, "type": "event", "campaign_id": camp, "session_id": session, "turn_id": outer, "payload_type": "local.v1.SendMessageResponse", "payload": map[string]any{"accepted": map[string]any{"turn_id": inner, "status": status}}})
		return data
	}
	for _, tc := range []struct {
		camp, session, outer, inner string
		known                       bool
	}{{"camp", "session", "turn", "turn", true}, {"foreign", "session", "turn", "turn", false}, {"camp", "foreign", "turn", "turn", false}, {"camp", "session", "foreign", "turn", false}} {
		code := 0
		ref := TurnRef{SessionRef: session.ref, IdempotencyKey: "original-key", ClientRef: "original-ref"}
		failure := &Error{Code: "protocol_error", Operation: "session.send", ExitCode: &code, RawRecord: raw(tc.camp, tc.session, tc.outer, tc.inner, "FUTURE_STATUS")}
		partial, err := session.sendFailure(ref, nil, failure)
		var typed *Error
		if !errors.As(err, &typed) || typed.Code != "indeterminate_acceptance" || !typed.AcceptanceIndeterminate || typed.IdempotencyKey != "original-key" || typed.ClientRef != "original-ref" {
			t.Fatalf("uncertainty/key lost: %+v", typed)
		}
		if tc.known {
			if partial == nil || partial.Ref().TurnID != "turn" || partial.Acceptance() != nil || partial.client != session.client {
				t.Fatalf("known partial missing: %v", partial)
			}
		} else if partial != nil {
			t.Fatal("foreign/malformed scope promoted")
		}
	}
	original := TurnRef{SessionRef: session.ref, TurnID: "original-turn", IdempotencyKey: "key"}
	partial, err := session.sendFailure(original, nil, &Error{Code: "protocol_error", RawRecord: raw("camp", "session", "foreign", "foreign", "FUTURE")})
	if partial.Ref().TurnID != "original-turn" || err.(*Error).TurnID != "original-turn" {
		t.Fatal("later invalid frame replaced known turn")
	}
}
func TestReceiptMetadataAndRawCopy(t *testing.T) {
	for _, status := range []string{"TURN_QUEUED", "TURN_COMPLETED"} {
		parsed, err := protocol.DecodeAcceptance([]byte(`{"accepted":{"turn_id":"turn","status":"` + status + `","future":{"zero":0}}}`))
		if err != nil {
			t.Fatal(err)
		}
		position := ptrCopy(parsed.QueuePosition)
		if status == string(TurnQueued) && position == nil {
			position = new(uint32)
		}
		if status != string(TurnQueued) {
			position = nil
		}
		turn := &Turn{acceptance: &Acceptance{Status: TurnStatus(status), QueuePosition: position, Raw: parsed.Raw}}
		copy := turn.Acceptance()
		copy.Raw[0] = 'x'
		if copy.QueuePosition != nil {
			*copy.QueuePosition = 8
		}
		if turn.Acceptance().Raw[0] != '{' || (status == string(TurnQueued) && *turn.Acceptance().QueuePosition != 0) {
			t.Fatal("receipt snapshot aliased")
		}
	}
}
