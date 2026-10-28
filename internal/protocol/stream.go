package protocol

import (
	"encoding/json"
)

type Decoder struct {
	Protocol                                                string
	Version                                                 int
	SessionID, CampID, TurnID                               string
	IdempotencyKey, ClientRef                               string
	Created, Content, Usage, Completion, Accepted, Terminal bool
	Last                                                    Envelope
	Errors                                                  []Envelope
	payloads                                                map[string]bool
	MaxDiagnosticBytes                                      int64
	diagnosticBytes                                         int64
}

func (d *Decoder) Add(data []byte) (Envelope, error) {
	r, err := Decode(data, d.Protocol, d.Version)
	if err != nil {
		return r, err
	}
	if d.Terminal {
		return r, failure("protocol_error", "record after terminal")
	}
	observation := d.Protocol == "obey.session.attach" || d.Protocol == "obey.observe"
	scope := func(target *string, value string) bool {
		if value == "" {
			return true
		}
		if *target != "" && *target != value {
			return false
		}
		*target = value
		return true
	}
	if !observation && (!scope(&d.SessionID, r.SessionID) || !scope(&d.CampID, r.CampID) || !scope(&d.TurnID, r.TurnID) || !scope(&d.IdempotencyKey, r.IdempotencyKey) || !scope(&d.ClientRef, r.ClientRef)) {
		return r, failure("protocol_error", "contradictory scope")
	}
	legacy := d.Protocol == "obey.session.run" && d.Version == 1
	switch r.Type {
	case "session_created":
		if d.Created || r.SessionID == "" || r.CampID == "" {
			return r, failure("protocol_error", "invalid session creation")
		}
		d.Created = true
		if !legacy {
			var p struct {
				Session struct {
					ID     string `json:"id"`
					CampID string `json:"campaign_id"`
				} `json:"session"`
			}
			if err := json.Unmarshal(r.Payload, &p); err != nil || r.PayloadType != "local.v1.CreateSessionResponse" || p.Session.ID != r.SessionID || p.Session.CampID != r.CampID {
				return r, failure("protocol_error", "creation payload scope mismatch")
			}
		}
	case "assistant_content":
		if !legacy {
			return r, failure("protocol_error", "legacy content in new envelope")
		}
		if !d.Created || d.Content || r.Content == nil || r.Final == nil || !*r.Final {
			return r, failure("protocol_error", "invalid final content")
		}
		d.Content = true
		d.Completion = true
	case "usage":
		if !legacy || !d.Content || d.Usage || r.InputTokens == nil || r.OutputTokens == nil || r.CachedTokens == nil || r.LatencyMS == nil {
			return r, failure("protocol_error", "invalid usage ordering")
		}
		d.Usage = true
	case "error":
		limit := d.MaxDiagnosticBytes
		if limit == 0 {
			limit = 64 << 20
		}
		d.diagnosticBytes += int64(len(r.Raw))
		if d.diagnosticBytes > limit {
			return r, failure("output_limit", "diagnostic retention limit exceeded")
		}
		d.Errors = append(d.Errors, r)
	case "terminal":
		if !legacy && d.Protocol != "obey.session.attach" && d.Protocol != "obey.observe" {
			if (d.SessionID != "" && r.SessionID != d.SessionID) || (d.CampID != "" && r.CampID != d.CampID) || (d.Accepted && r.TurnID != d.TurnID) {
				return r, failure("protocol_error", "terminal scope missing")
			}
		}
		d.Terminal = true
		d.Last = r
	case "event":
		if legacy {
			return r, failure("protocol_error", "new event in legacy stream")
		}
		if d.payloads == nil {
			d.payloads = make(map[string]bool)
		}
		switch r.PayloadType {
		case "local.v1.PingResponse", "local.v1.RespondApprovalResponse", "local.v1.StopSessionResponse", "local.v1.CancelTurnResponse", "local.v1.ListRecentActivityResponse":
			d.payloads[r.PayloadType] = true
		}
		if observation || r.PayloadType != "local.v1.SendMessageResponse" || (d.Protocol != "obey.session.run" && d.Protocol != "obey.session.send") {
			break
		}
		var p struct {
			Accepted *struct {
				TurnID string          `json:"turn_id"`
				Status json.RawMessage `json:"status"`
			} `json:"accepted"`
			Event *struct {
				SessionID  string          `json:"session_id"`
				TurnID     string          `json:"turn_id"`
				Type       json.RawMessage `json:"type"`
				Completion json.RawMessage `json:"completion"`
			} `json:"event"`
		}
		if err := json.Unmarshal(r.Payload, &p); err != nil {
			return r, failure("protocol_error", "invalid protobuf wrapper")
		}
		if p.Accepted != nil && r.PayloadType == "local.v1.SendMessageResponse" {
			var status string
			_ = json.Unmarshal(p.Accepted.Status, &status)
			switch status {
			case "TURN_QUEUED", "TURN_RUNNING", "TURN_COMPLETED", "TURN_FAILED", "TURN_CANCELLED", "TURN_RECOVERY_REQUIRED", "TURN_HISTORICAL_UNKNOWN":
			default:
				return r, failure("protocol_error", "unknown acceptance status")
			}
			if p.Accepted.TurnID == "" || !scope(&d.TurnID, p.Accepted.TurnID) {
				return r, failure("protocol_error", "receipt identity mismatch")
			}
			d.Accepted = true
		}
		if p.Event != nil {
			if !scope(&d.SessionID, p.Event.SessionID) || !scope(&d.TurnID, p.Event.TurnID) {
				return r, failure("protocol_error", "event identity mismatch")
			}
			if string(p.Event.Type) == `"AGENT_COMPLETION"` && len(p.Event.Completion) > 0 && string(p.Event.Completion) != "null" {
				if p.Event.SessionID != d.SessionID || p.Event.TurnID == "" || p.Event.TurnID != d.TurnID || p.Event.Completion[0] != '{' {
					return r, failure("protocol_error", "completion payload scope/type mismatch")
				}
				if d.Completion {
					return r, failure("protocol_error", "duplicate completion")
				}
				d.Completion = true
			}
		}
	}
	return r, nil
}
func (d *Decoder) Finish(exit int) error {
	if !d.Terminal {
		switch d.Protocol {
		case "obey.session.attach", "obey.observe":
			return failure("incomplete_stream", "open observation ended unexpectedly")
		case "obey.session.create":
			return failure("indeterminate_creation", "creation terminal missing")
		case "obey.session.send":
			return failure("indeterminate_acceptance", "send terminal/receipt missing")
		}
		return failure("protocol_error", "terminal missing")
	}
	if d.Last.Status == "completed" {
		if !(d.Protocol == "obey.session.run" && d.Version == 1) {
			if d.Protocol == "obey.session.run" {
				if d.Last.Cleanup == "not_owned" {
					return failure("protocol_error", "owned run cleanup missing")
				}
			} else if d.Last.Cleanup != "not_owned" {
				return failure("protocol_error", "persistent/control cleanup ownership mismatch")
			}
		}
		if exit != 0 {
			return failure("protocol_error", "completed terminal with nonzero exit")
		}
		for i := range d.Errors {
			e := d.Errors[i]
			if e.Severity == "warning" || (d.Protocol == "obey.session.run" && d.Version == 1 && e.Code == "stop_session") {
				continue
			}
			return failure("protocol_error", "completed terminal contradicts error")
		}
		switch d.Protocol {
		case "obey.session.run":
			if !d.Created || !d.Completion || (d.Version == 1 && !d.Usage) || (d.Version == 2 && (d.TurnID == "" || d.Last.Outcome != "turn_completed")) {
				return failure("protocol_error", "run completion evidence missing")
			}
		case "obey.session.create":
			if !d.Created || d.Last.Outcome != "session_created" {
				return failure("protocol_error", "creation evidence missing")
			}
		case "obey.session.send":
			if !d.Accepted {
				return failure("indeterminate_acceptance", "typed receipt missing")
			}
			if d.Last.Outcome == "turn_completed" {
				if !d.Completion {
					return failure("protocol_error", "matching completion missing")
				}
			} else if d.Last.Outcome != "accepted" {
				return failure("protocol_error", "invalid send outcome")
			}
		case "obey.daemon.status", "obey.session.approve", "obey.session.stop", "obey.turn.cancel":
			expected := map[string]string{"obey.daemon.status": "local.v1.PingResponse", "obey.session.approve": "local.v1.RespondApprovalResponse", "obey.session.stop": "local.v1.StopSessionResponse", "obey.turn.cancel": "local.v1.CancelTurnResponse"}[d.Protocol]
			if d.Last.Outcome != "rpc_completed" || !d.payloads[expected] {
				return failure("protocol_error", "RPC evidence missing")
			}
		case "obey.observe":
			if d.Last.Outcome != "observed" || !d.payloads["local.v1.ListRecentActivityResponse"] {
				return failure("protocol_error", "finite observation evidence missing")
			}
		default:
			return failure("protocol_error", "unexpected successful open stream terminal")
		}
		return nil
	}
	if exit == 0 {
		return failure("protocol_error", "failed terminal with zero exit")
	}
	for i := range d.Errors {
		if d.Errors[i].Severity != "warning" {
			e := d.Errors[i]
			return &Error{Code: e.Code, Record: &e}
		}
	}
	if d.Last.Status == "canceled" {
		return failure("canceled", "observation detached")
	}
	return failure("daemon_error", "failed invocation")
}
