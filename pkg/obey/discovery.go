package obey

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Obedience-Corp/obey-go-sdk/internal/protocol"
)

const (
	providerListProtocol   = "obey.providers.list"
	providerModelsProtocol = "obey.providers.models"
	providerCheckProtocol  = "obey.providers.check"
)

// Providers reads daemon-owned routing metadata and defaults without contacting
// provider endpoints. Returned values are fresh independent snapshots.
func (c *Client) Providers(ctx context.Context) (*ProviderCatalog, error) {
	data, err := c.discovery(ctx, "provider_list", providerListProtocol, "providers", []string{"providers"})
	if err != nil {
		return nil, err
	}
	var catalog ProviderCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, discoveryDecodeError("providers", data, err)
	}
	if err := validateProviderCatalog(catalog, data); err != nil {
		return nil, discoveryDecodeError("providers", data, err)
	}
	catalog.Raw = append(json.RawMessage(nil), data...)
	return &catalog, nil
}

func (c *Client) Models(ctx context.Context, provider string) (*ModelCatalog, error) {
	return c.models(ctx, provider, false)
}

// RefreshModels explicitly requests a new catalog snapshot; regular Models
// follows the daemon's existing catalog TTL and cache-miss population policy.
func (c *Client) RefreshModels(ctx context.Context, provider string) (*ModelCatalog, error) {
	return c.models(ctx, provider, true)
}

func (c *Client) models(ctx context.Context, provider string, refresh bool) (*ModelCatalog, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return nil, &Error{Code: "invalid_options", Operation: "models", Cause: errors.New("provider is required")}
	}
	args := []string{"providers", "models", provider}
	operation := "models"
	if refresh {
		args = append(args, "--refresh")
		operation = "refresh_models"
	}
	data, err := c.discovery(ctx, "provider_models", providerModelsProtocol, operation, args)
	if err != nil {
		return nil, err
	}
	var catalog ModelCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, discoveryDecodeError(operation, data, err)
	}
	if catalog.ProviderID != provider {
		return nil, &Error{Code: "scope_mismatch", Operation: operation, RawRecord: append(json.RawMessage(nil), data...)}
	}
	if err := validateModelCatalog(catalog); err != nil {
		return nil, discoveryDecodeError(operation, data, err)
	}
	catalog.Raw = append(json.RawMessage(nil), data...)
	return &catalog, nil
}

func (c *Client) Preflight(ctx context.Context, request PreflightRequest) (*Readiness, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	request.Provider = strings.TrimSpace(request.Provider)
	if request.Provider == "" {
		return nil, &Error{Code: "invalid_options", Operation: "preflight", Cause: errors.New("provider is required")}
	}
	if request.Camp != "" || request.WorkingDirectory != "" {
		return nil, &Error{Code: "unsupported_capability", Operation: "preflight", Cause: errors.New("camp and working-directory preflight are not supported by this CLI contract")}
	}
	if request.ToolPolicy == "" {
		request.ToolPolicy = ToolPolicyDaemonManaged
	}
	if request.ToolPolicy != ToolPolicyDaemonManaged && request.ToolPolicy != ToolPolicyOff {
		return nil, &Error{Code: "invalid_options", Operation: "preflight", Cause: errors.New("unknown tool policy")}
	}
	args := []string{"providers", "check", request.Provider, "--tools", string(request.ToolPolicy)}
	if request.Model != "" {
		args = append(args, "--model", request.Model)
	}
	if request.RequireLiveEvents {
		args = append(args, "--require-live-events")
	}
	data, err := c.discovery(ctx, "provider_check", providerCheckProtocol, "preflight", args)
	if err != nil {
		return nil, err
	}
	var readiness Readiness
	if err := json.Unmarshal(data, &readiness); err != nil {
		return nil, discoveryDecodeError("preflight", data, err)
	}
	if readiness.ProviderID != request.Provider || (request.Model != "" && readiness.ModelID != request.Model) {
		return nil, &Error{Code: "scope_mismatch", Operation: "preflight", RawRecord: append(json.RawMessage(nil), data...)}
	}
	if err := validateReadiness(readiness, request); err != nil {
		return nil, discoveryDecodeError("preflight", data, err)
	}
	readiness.Raw = append(json.RawMessage(nil), data...)
	return &readiness, nil
}

func (c *Client) discovery(ctx context.Context, key, name, operation string, args []string) ([]byte, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, unaryTimeout)
	defer cancel()
	// Never substitute broad doctor health checks: older CLIs reject this flag
	// before running a command, while supported CLIs perform no config/daemon I/O.
	capData, capOut, err := c.finiteDocument(ctx, []string{"doctor", "--json", "--capabilities-only"}, nil, operation)
	if err != nil {
		return nil, err
	}
	caps, err := protocol.DecodeDoctor(capData)
	if err != nil || capOut.ExitCode != 0 || caps.Status != "unknown" || !caps.Supports(key, name, 1) {
		return nil, &Error{Code: "unsupported_protocol", Operation: operation, Protocol: ProtocolVersion{Name: name, Version: 1}, ExitCode: ptrCopy(&capOut.ExitCode), StderrTail: capOut.StderrTail, Cause: errors.Join(err, capOut.Err)}
	}
	deadline, _ := ctx.Deadline()
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return nil, &Error{Code: "timeout", Operation: operation, Cause: context.DeadlineExceeded}
	}
	args = append(args, "--json", "--protocol-version", "1", "--timeout", remaining.String())
	data, out, err := c.finiteDocument(ctx, args, nil, operation)
	if err != nil {
		return nil, err
	}
	if err := validateDiscoveryInvocation(data, name, operation, out); err != nil {
		return nil, err
	}
	return data, nil
}
