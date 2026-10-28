package protocol

import "encoding/json"

// Fresh owned run-v2 never reconnects or replays a mutation. Its live event has
// no attempt ID, so only the original single completed attempt can corroborate it.
func (d *Decoder) runState(r Envelope) error {
	var doc struct {
		Turn struct {
			ID      string          `json:"id"`
			Session string          `json:"session_id"`
			Camp    string          `json:"campaign_id"`
			Status  json.RawMessage `json:"status"`
			Attempt *struct {
				ID     string          `json:"id"`
				Turn   string          `json:"turn_id"`
				State  json.RawMessage `json:"state"`
				Number Integer         `json:"attempt_number"`
			} `json:"latest_attempt"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(r.Payload, &doc); err != nil {
		return failure("protocol_error", "invalid run completed state")
	}
	t := doc.Turn
	if t.ID == "" || t.ID != d.TurnID || t.Session != d.SessionID || t.Camp != d.CampID {
		return failure("protocol_error", "run state scope mismatch")
	}
	d.runStateCompleted = string(t.Status) == `"TURN_COMPLETED"` && t.Attempt != nil && t.Attempt.ID != "" && t.Attempt.Turn == t.ID && t.Attempt.Number == 1 && string(t.Attempt.State) == `"TURN_ATTEMPT_COMPLETED"`
	return nil
}
