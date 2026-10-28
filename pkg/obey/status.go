package obey

import (
	"context"
	"encoding/json"
	"errors"
)

func (c *Client) Status(ctx context.Context) (*DaemonStatus, error) {
	if err := c.requireProtocol(ctx, "daemon_status", "obey.daemon.status", 1); err != nil {
		return nil, err
	}
	records, err := c.machineDocument(ctx, []string{"status", "--json", "--protocol-version", "1"}, "status", "obey.daemon.status", TurnRef{})
	if err != nil {
		c.invalidateCapabilities()
		return nil, err
	}
	var raw json.RawMessage
	for _, record := range records {
		if record.Type == "event" && record.PayloadType == "local.v1.PingResponse" {
			raw = record.Payload
		}
	}
	var doc struct {
		Version      string                     `json:"version"`
		Hostname     string                     `json:"hostname"`
		Capabilities map[string]json.RawMessage `json:"capabilities"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, &Error{Code: "protocol_error", Operation: "status", Cause: err}
	}
	status := &DaemonStatus{Version: doc.Version, Hostname: doc.Hostname, CapabilitiesPresent: doc.Capabilities != nil, Features: map[string]bool{}, Raw: append(json.RawMessage(nil), raw...)}
	for key, rawValue := range doc.Capabilities {
		var value bool
		if json.Unmarshal(rawValue, &value) == nil {
			status.Features[key] = value
		}
	}
	return status, nil
}
func (c *Client) requireFeatures(ctx context.Context, names ...string) error {
	status, err := c.Status(ctx)
	if err != nil {
		return err
	}
	for _, name := range names {
		if !status.Features[name] {
			return &Error{Code: "unsupported_capability", Operation: "preflight", Cause: errors.New("daemon does not advertise " + name)}
		}
	}
	return nil
}
