package protocol

import "encoding/json"

// TurnDocument keeps the existing handwritten numeric DTO separate from
// protojson envelopes. Unknown fields/status remain available in Raw.
type TurnDocument struct {
	Schema string          `json:"schema_version"`
	Turn   TurnView        `json:"turn"`
	Raw    json.RawMessage `json:"-"`
}
type TurnView struct {
	ID                string            `json:"id"`
	SessionID         string            `json:"session_id"`
	CampID            string            `json:"campaign_id"`
	Ordinal           int64             `json:"ordinal"`
	Version           int64             `json:"version"`
	Status            string            `json:"status"`
	QueuePosition     *uint32           `json:"queue_position"`
	RequestedProvider string            `json:"requested_provider"`
	RequestedModel    string            `json:"requested_model"`
	RequestedMode     string            `json:"requested_mode"`
	LatestAttempt     *TurnAttemptView  `json:"latest_attempt"`
	Recovery          *TurnRecoveryView `json:"recovery"`
	ErrorCode         string            `json:"error_code"`
	ErrorMessage      string            `json:"error_message"`
	DurationMS        int64             `json:"duration_ms"`
	Usage             json.RawMessage   `json:"usage"`
}
type TurnAttemptView struct {
	ID     string `json:"id"`
	TurnID string `json:"turn_id"`
	Number int64  `json:"attempt_number"`
	State  string `json:"state"`
}
type TurnRecoveryView struct {
	Reason string `json:"reason"`
}

type NumericUsage struct {
	Input       int64  `json:"tokens_input"`
	Output      int64  `json:"tokens_output"`
	Cached      int64  `json:"cached_tokens"`
	Cost        *int64 `json:"cost_usd_ticks"`
	CostPartial *bool  `json:"cost_is_partial"`
	Incomplete  *bool  `json:"usage_is_incomplete"`
}

func DecodeTurn(data []byte) (TurnDocument, error) {
	var d TurnDocument
	if err := json.Unmarshal(data, &d); err != nil {
		return d, &Error{Code: "protocol_error", Cause: err}
	}
	if d.Schema != "obey.turn.v1" || d.Turn.ID == "" || d.Turn.SessionID == "" || d.Turn.CampID == "" || d.Turn.Status == "" {
		return d, failure("protocol_error", "invalid turn document")
	}
	d.Raw = append(json.RawMessage(nil), data...)
	return d, nil
}
