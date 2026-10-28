package protocol

import (
	"encoding/json"
	"strconv"
)

// Unsigned accepts protobuf decimal strings and handwritten exact integers.
type Unsigned uint64

func (n *Unsigned) UnmarshalJSON(data []byte) error {
	value := string(data)
	if len(data) > 0 && data[0] == '"' {
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return err
	}
	*n = Unsigned(parsed)
	return nil
}

type TranscriptDocument struct {
	Entries []json.RawMessage `json:"entries"`
	Page    struct {
		NextCursor string `json:"next_cursor"`
		HasMore    bool   `json:"has_more"`
	} `json:"page"`
}
type TranscriptRow struct {
	ID           string          `json:"id"`
	SessionID    string          `json:"session_id"`
	CampID       string          `json:"campaign_id"`
	Sequence     Unsigned        `json:"sequence_number"`
	Timestamp    string          `json:"timestamp"`
	Actor        string          `json:"actor"`
	Kind         string          `json:"kind"`
	ActivityType json.RawMessage `json:"activity_type"`
	Content      string          `json:"content"`
	ToolName     string          `json:"tool_name"`
	ToolCallID   string          `json:"tool_call_id"`
	Success      *bool           `json:"success"`
	Error        string          `json:"error"`
	Metadata     string          `json:"metadata_json"`
	TurnID       string          `json:"turn_id"`
	AttemptID    string          `json:"attempt_id"`
}
