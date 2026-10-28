package protocol

import "encoding/json"

// TurnDocument keeps the existing handwritten numeric DTO separate from
// protojson envelopes. Unknown fields/status remain available in Raw.
type TurnDocument struct {
	Schema string `json:"schema_version"`
	Turn   struct {
		ID        string          `json:"id"`
		SessionID string          `json:"session_id"`
		CampID    string          `json:"campaign_id"`
		Ordinal   int64           `json:"ordinal"`
		Version   int64           `json:"version"`
		Status    string          `json:"status"`
		Usage     json.RawMessage `json:"usage"`
	} `json:"turn"`
	Raw json.RawMessage `json:"-"`
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
