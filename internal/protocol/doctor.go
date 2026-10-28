package protocol

import "encoding/json"

type Support struct {
	Protocol string `json:"protocol"`
	Versions []int  `json:"versions"`
}
type Doctor struct {
	Schema       string             `json:"schema"`
	Version      int                `json:"version"`
	Status       string             `json:"status"`
	Capabilities map[string]Support `json:"capabilities"`
	Raw          json.RawMessage    `json:"-"`
}

func DecodeDoctor(data []byte) (Doctor, error) {
	var d Doctor
	if err := json.Unmarshal(data, &d); err != nil {
		return d, &Error{Code: "unsupported_protocol", Cause: err}
	}
	if d.Schema != "obey.doctor" || d.Version < 1 || d.Capabilities == nil {
		return d, failure("unsupported_protocol", "missing doctor schema/capabilities")
	}
	for _, s := range d.Capabilities {
		if s.Protocol == "" || len(s.Versions) == 0 {
			return d, failure("unsupported_protocol", "malformed capability")
		}
		for _, v := range s.Versions {
			if v < 1 {
				return d, failure("unsupported_protocol", "invalid capability version")
			}
		}
	}
	d.Raw = append(json.RawMessage(nil), data...)
	return d, nil
}
func (d Doctor) Supports(key, protocol string, version int) bool {
	s, ok := d.Capabilities[key]
	if !ok || s.Protocol != protocol {
		return false
	}
	for _, v := range s.Versions {
		if v == version {
			return true
		}
	}
	return false
}
