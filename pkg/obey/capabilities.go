package obey

import (
	"encoding/json"
	"sync"

	"github.com/Obedience-Corp/obey-go-sdk/internal/protocol"
)

// capabilityCache is owned by a Client, never shared globally. The executable
// identity is provided by the invoking client; changes invalidate projections.
type capabilityCache struct {
	mu       sync.Mutex
	identity string
	document []byte
}

func (c *capabilityCache) load(identity string) *Capabilities {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.identity != identity || c.document == nil {
		return nil
	}
	value, _ := decodeCapabilities(c.document)
	return value
}
func (c *capabilityCache) store(identity string, data []byte) error {
	if _, err := decodeCapabilities(data); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.identity = identity
	c.document = append([]byte(nil), data...)
	return nil
}
func decodeCapabilities(data []byte) (*Capabilities, error) {
	d, err := protocol.DecodeDoctor(data)
	if err != nil {
		return nil, err
	}
	c := &Capabilities{Schema: d.Schema, ReportVersion: d.Version, HealthStatus: d.Status, Protocols: make(map[string]ProtocolSupport), Raw: append(json.RawMessage(nil), d.Raw...)}
	for key, s := range d.Capabilities {
		c.Protocols[key] = ProtocolSupport{Protocol: s.Protocol, Versions: append([]int(nil), s.Versions...)}
	}
	return c, nil
}
