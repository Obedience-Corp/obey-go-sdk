package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
)

type Envelope struct {
	RequestedSessionID      string          `json:"requested_session_id"`
	Protocol                string          `json:"protocol"`
	Version                 int             `json:"version"`
	Type                    string          `json:"type"`
	PayloadType             string          `json:"payload_type"`
	Payload                 json.RawMessage `json:"payload"`
	SessionID               string          `json:"session_id"`
	CampID                  string          `json:"campaign_id"`
	TurnID                  string          `json:"turn_id"`
	IdempotencyKey          string          `json:"idempotency_key"`
	ClientRef               string          `json:"client_ref"`
	Content                 *string         `json:"content"`
	Final                   *bool           `json:"final"`
	InputTokens             *int64          `json:"input_tokens"`
	OutputTokens            *int64          `json:"output_tokens"`
	CachedTokens            *int64          `json:"cached_tokens"`
	LatencyMS               *int64          `json:"latency_ms"`
	Code                    string          `json:"code"`
	Message                 string          `json:"message"`
	Severity                string          `json:"severity"`
	DaemonCode              *int32          `json:"daemon_code"`
	GRPCCode                string          `json:"grpc_code"`
	Details                 json.RawMessage `json:"details"`
	AcceptanceIndeterminate bool            `json:"acceptance_indeterminate"`
	Status                  string          `json:"status"`
	Outcome                 string          `json:"outcome"`
	Cleanup                 string          `json:"cleanup"`
	Raw                     json.RawMessage `json:"-"`
}

type Error struct {
	Code   string
	Record *Envelope
	Cause  error
}

func (e *Error) Error() string {
	if e.Record != nil {
		return e.Code + ": " + e.Record.Message
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Code, e.Cause)
	}
	return e.Code
}
func (e *Error) Unwrap() error           { return e.Cause }
func failure(code, message string) error { return &Error{Code: code, Cause: errors.New(message)} }

func Decode(data []byte, protocol string, version int) (Envelope, error) {
	var r Envelope
	if err := json.Unmarshal(data, &r); err != nil {
		return r, &Error{Code: "protocol_error", Cause: err}
	}
	r.Raw = append(json.RawMessage(nil), data...)
	if r.Protocol != protocol || r.Version != version {
		return r, failure("unsupported_protocol", "unexpected protocol or version")
	}
	if r.Type == "" {
		return r, failure("protocol_error", "missing record type")
	}
	if version != 1 && !(protocol == "obey.session.run" && version == 2) {
		return r, failure("unsupported_protocol", "unsupported version")
	}
	if r.Type == "terminal" {
		switch r.Status {
		case "completed", "failed", "canceled":
		default:
			return r, failure("unsupported_protocol", "unknown terminal status")
		}
	}
	if !(protocol == "obey.session.run" && version == 1) {
		switch r.Type {
		case "event", "session_created":
			if r.PayloadType == "" || len(r.Payload) == 0 || r.Payload[0] != '{' {
				return r, failure("protocol_error", "missing typed payload object")
			}
		case "error":
			if r.Code == "" || r.Message == "" || (r.Severity != "error" && r.Severity != "warning") {
				return r, failure("protocol_error", "invalid error record")
			}
		case "terminal":
			if r.Outcome == "" || (r.Cleanup != "stopped" && r.Cleanup != "stop_failed" && r.Cleanup != "unknown" && r.Cleanup != "not_owned") {
				return r, failure("protocol_error", "invalid terminal outcome/cleanup")
			}
		}
	}
	return r, nil
}
