package protocol

import (
	"encoding/json"
	"strconv"
)

// Integer accepts exact handwritten JSON numbers and protojson decimal strings.
// Floating-point/exponent encodings are rejected instead of rounded.
type Integer int64

func (n *Integer) UnmarshalJSON(data []byte) error {
	s := string(data)
	if len(data) > 0 && data[0] == '"' {
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
	}
	value, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return err
	}
	*n = Integer(value)
	return nil
}
