package protocol

import (
	"encoding/json"
	"errors"
	"strings"
)

type AcceptedTurn struct {
	TurnID        string          `json:"turn_id"`
	Status        string          `json:"status"`
	QueuePosition *uint32         `json:"queue_position"`
	Deduplicated  bool            `json:"deduplicated"`
	Raw           json.RawMessage `json:"-"`
}

func DecodeAcceptance(payload []byte) (*AcceptedTurn, error) {
	var wrapper struct {
		Accepted json.RawMessage `json:"accepted"`
		Event    json.RawMessage `json:"event"`
		Queued   json.RawMessage `json:"queued"`
		Note     json.RawMessage `json:"undelivered_note"`
	}
	if err := json.Unmarshal(payload, &wrapper); err != nil {
		return nil, err
	}
	if len(wrapper.Accepted) == 0 || string(wrapper.Accepted) == "null" {
		return nil, errors.New("typed acceptance receipt missing")
	}
	if len(wrapper.Event) > 0 || len(wrapper.Queued) > 0 || len(wrapper.Note) > 0 {
		return nil, errors.New("multiple SendMessageResponse variants")
	}
	var accepted AcceptedTurn
	if err := json.Unmarshal(wrapper.Accepted, &accepted); err != nil {
		return nil, err
	}
	if strings.TrimSpace(accepted.TurnID) == "" || strings.ContainsRune(accepted.TurnID, '\x00') {
		return nil, errors.New("receipt turn ID missing")
	}
	switch accepted.Status {
	case "TURN_QUEUED", "TURN_RUNNING", "TURN_COMPLETED", "TURN_FAILED", "TURN_CANCELLED", "TURN_RECOVERY_REQUIRED", "TURN_HISTORICAL_UNKNOWN":
	default:
		return nil, errors.New("unsupported acceptance status")
	}
	accepted.Raw = append(json.RawMessage(nil), wrapper.Accepted...)
	return &accepted, nil
}

// ReceiptIdentity is deliberately weaker than successful acceptance: it only
// identifies a structurally scoped durable turn when status/metadata is unknown.
func ReceiptIdentity(record Envelope, camp, session, key, clientRef string) string {
	if record.Protocol != "obey.session.send" || record.Version != 1 || record.Type != "event" || record.PayloadType != "local.v1.SendMessageResponse" || record.CampID != camp || record.SessionID != session {
		return ""
	}
	if (record.IdempotencyKey != "" && record.IdempotencyKey != key) || (record.ClientRef != "" && record.ClientRef != clientRef) {
		return ""
	}
	var wrapper struct {
		Accepted *struct {
			TurnID string `json:"turn_id"`
		} `json:"accepted"`
		Event  json.RawMessage `json:"event"`
		Queued json.RawMessage `json:"queued"`
		Note   json.RawMessage `json:"undelivered_note"`
	}
	if json.Unmarshal(record.Payload, &wrapper) != nil || wrapper.Accepted == nil || (strings.TrimSpace(wrapper.Accepted.TurnID) == "" || strings.ContainsRune(wrapper.Accepted.TurnID, '\x00')) || len(wrapper.Event) > 0 || len(wrapper.Queued) > 0 || len(wrapper.Note) > 0 {
		return ""
	}
	if record.TurnID != "" && record.TurnID != wrapper.Accepted.TurnID {
		return ""
	}
	return wrapper.Accepted.TurnID
}
