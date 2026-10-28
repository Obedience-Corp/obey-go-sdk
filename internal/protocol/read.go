package protocol

import "encoding/json"

type ReadError struct {
	Schema     string          `json:"schema"`
	Version    int             `json:"version"`
	Operation  string          `json:"operation"`
	Code       string          `json:"code"`
	Message    string          `json:"message"`
	CampID     string          `json:"campaign_id"`
	SessionID  string          `json:"session_id"`
	TurnID     string          `json:"turn_id"`
	DaemonCode *int32          `json:"daemon_code"`
	GRPCCode   string          `json:"grpc_code"`
	Details    json.RawMessage `json:"details"`
	Raw        json.RawMessage `json:"-"`
}

func DecodeReadError(data []byte, operation string) (ReadError, error) {
	var r ReadError
	if err := json.Unmarshal(data, &r); err != nil {
		return r, &Error{Code: "protocol_error", Cause: err}
	}
	if r.Schema != "obey.machine.error" || r.Version != 1 || r.Operation != operation || r.Code == "" || r.Message == "" {
		return r, failure("protocol_error", "invalid machine read error")
	}
	r.Raw = append(json.RawMessage(nil), data...)
	return r, nil
}
