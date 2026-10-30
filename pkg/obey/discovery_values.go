package obey

import (
	"encoding/json"
	"time"
)

// CheckState retains unknown future CLI states as strings.
type CheckState string

const (
	CheckReady       CheckState = "ready"
	CheckUnavailable CheckState = "unavailable"
	CheckUnsupported CheckState = "unsupported"
	CheckUnknown     CheckState = "unknown"
)

type ToolPolicy string

const (
	ToolPolicyDaemonManaged ToolPolicy = "daemon"
	ToolPolicyOff           ToolPolicy = "off"
)

type CatalogMetadata struct {
	Schema        string    `json:"schema"`
	Version       int       `json:"version"`
	ObservedAt    time.Time `json:"observed_at"`
	DaemonVersion string    `json:"daemon_version"`
}
type DiscoveryDiagnostic struct {
	Code  string `json:"code"`
	Scope string `json:"scope"`
}
type ProviderInfo struct {
	ID             string     `json:"id"`
	DisplayName    string     `json:"display_name"`
	Type           string     `json:"type"`
	Revision       string     `json:"revision"`
	DefaultModel   string     `json:"default_model"`
	Enabled        bool       `json:"enabled"`
	Health         CheckState `json:"health"`
	ModelDiscovery *bool      `json:"model_discovery"`
	ModelRefresh   *bool      `json:"model_refresh"`
}
type ProviderCatalog struct {
	CatalogMetadata
	DefaultProvider *string               `json:"default_provider"`
	DefaultModel    *string               `json:"default_model"`
	Providers       []ProviderInfo        `json:"providers"`
	Diagnostics     []DiscoveryDiagnostic `json:"diagnostics"`
	Raw             json.RawMessage       `json:"-"`
}
type ModelInfo struct {
	ID              string `json:"id"`
	DisplayName     string `json:"display_name"`
	ToolsOff        *bool  `json:"tools_off"`
	AssistantDeltas *bool  `json:"assistant_deltas"`
}
type ModelCatalog struct {
	CatalogMetadata
	ProviderID       string                `json:"provider_id"`
	ProviderRevision string                `json:"provider_revision"`
	DefaultModel     *string               `json:"default_model"`
	Source           string                `json:"catalog_source"`
	State            string                `json:"catalog_state"`
	FetchedAt        *time.Time            `json:"fetched_at"`
	Exhaustive       *bool                 `json:"exhaustive"`
	Models           []ModelInfo           `json:"models"`
	Diagnostics      []DiscoveryDiagnostic `json:"diagnostics"`
	Raw              json.RawMessage       `json:"-"`
}
type PreflightRequest struct {
	Provider, Model string
	// Camp/workdir preflight is not supported by the current CLI. Nonempty
	// values refuse before invocation rather than silently skipping validation.
	Camp, WorkingDirectory string
	ToolPolicy             ToolPolicy
	RequireLiveEvents      bool
}
type ReadinessCheck struct {
	Stage       string     `json:"stage"`
	Code        string     `json:"code"`
	State       CheckState `json:"state"`
	Remediation string     `json:"remediation,omitempty"`
}
type Readiness struct {
	CatalogMetadata
	ProviderID string           `json:"provider_id"`
	ModelID    string           `json:"model_id"`
	Fidelity   StreamFidelity   `json:"fidelity"`
	Checks     []ReadinessCheck `json:"checks"`
	Raw        json.RawMessage  `json:"-"`
}

// HasFailures reports explicit failed requirements/availability. Unknown
// evidence is preserved; it is not proof of readiness or a definite failure.
func (r *Readiness) HasFailures() bool {
	if r == nil {
		return true
	}
	for _, check := range r.Checks {
		if check.State == CheckUnavailable || check.State == CheckUnsupported {
			return true
		}
	}
	return false
}

// Ready requires every reported check to be ready. Preflight is a snapshot,
// not a reservation or proof that inference will succeed.
func (r *Readiness) Ready() bool {
	if r == nil || len(r.Checks) == 0 {
		return false
	}
	for _, check := range r.Checks {
		if check.State != CheckReady {
			return false
		}
	}
	return true
}
