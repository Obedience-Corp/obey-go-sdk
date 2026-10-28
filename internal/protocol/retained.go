package protocol

import "encoding/json"

type retainedProof struct {
	attempt              string
	completed            bool
	high                 uint64
	last                 uint64
	completion, boundary bool
}

func (p retainedProof) valid() bool {
	return p.completed && p.attempt != "" && p.high > 0 && p.completion && p.boundary
}

// CLI send --wait preserves typed authoritative state and raw transcript pages;
// it need not fabricate a live AgentCompletion for an already-completed turn.
func (d *Decoder) retainedEvent(r Envelope) error {
	switch r.PayloadType {
	case "local.v1.GetTurnResponse":
		var doc struct {
			Turn struct {
				ID      string          `json:"id"`
				Session string          `json:"session_id"`
				Camp    string          `json:"campaign_id"`
				Status  json.RawMessage `json:"status"`
				Attempt *struct {
					ID    string          `json:"id"`
					Turn  string          `json:"turn_id"`
					State json.RawMessage `json:"state"`
				} `json:"latest_attempt"`
			} `json:"turn"`
		}
		if err := json.Unmarshal(r.Payload, &doc); err != nil {
			return failure("protocol_error", "invalid retained turn state")
		}
		t := doc.Turn
		if t.ID != d.TurnID || t.Session != d.SessionID || t.Camp != d.CampID {
			return failure("protocol_error", "retained state identity mismatch")
		}
		d.retained = retainedProof{}
		if string(t.Status) == `"TURN_COMPLETED"` && t.Attempt != nil && t.Attempt.ID != "" && t.Attempt.Turn == d.TurnID && string(t.Attempt.State) == `"TURN_ATTEMPT_COMPLETED"` {
			d.retained = retainedProof{completed: true, attempt: t.Attempt.ID}
		}
	case "local.v1.ListSessionTranscriptResponse":
		if !d.retained.completed || r.HighWaterSequence == nil || *r.HighWaterSequence == 0 {
			return nil
		}
		high := *r.HighWaterSequence
		if d.retained.high != 0 && d.retained.high != high {
			return failure("protocol_error", "retained high-water changed")
		}
		d.retained.high = high
		var page TranscriptDocument
		if err := json.Unmarshal(r.Payload, &page); err != nil {
			return failure("protocol_error", "invalid retained transcript page")
		}
		for _, raw := range page.Entries {
			var row TranscriptRow
			if err := json.Unmarshal(raw, &row); err != nil {
				return failure("protocol_error", "invalid retained transcript entry")
			}
			if row.ID == "" || row.CampID != d.CampID || row.SessionID != d.SessionID || row.TurnID != d.TurnID || row.AttemptID != d.retained.attempt {
				return failure("protocol_error", "retained entry scope/attempt mismatch")
			}
			seq := uint64(row.Sequence)
			if seq == 0 || seq <= d.retained.last {
				return failure("protocol_error", "retained sequence did not advance")
			}
			d.retained.last = seq
			if seq > high {
				continue
			}
			if seq == high {
				d.retained.boundary = true
			}
			if row.Kind == "completion" && string(row.ActivityType) == `"AGENT_COMPLETION"` {
				d.retained.completion = true
			}
		}
	}
	return nil
}
