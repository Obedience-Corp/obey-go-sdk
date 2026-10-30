package obey

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Obedience-Corp/obey-go-sdk/internal/execclient"
)

func discoveryDecodeError(operation string, data []byte, cause error) error {
	return &Error{Code: "protocol_error", Operation: operation, RawRecord: append(json.RawMessage(nil), data...), Cause: cause}
}

func validateDiscoveryInvocation(data []byte, name, operation string, out execclient.Outcome) error {
	var header struct {
		Schema    string `json:"schema"`
		Version   int    `json:"version"`
		Operation string `json:"operation"`
		Code      string `json:"code"`
		Message   string `json:"message"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return discoveryDecodeError(operation, data, err)
	}
	if header.Schema == "obey.machine.error" {
		if header.Version != 1 || header.Operation != name || header.Code == "" || out.ExitCode == 0 {
			return discoveryDecodeError(operation, data, errors.New("invalid discovery error document or exit status"))
		}
		cause := errors.Join(out.Err, errors.New(header.Message))
		if header.Code == "timeout" {
			cause = errors.Join(cause, context.DeadlineExceeded)
		}
		if header.Code == "canceled" {
			cause = errors.Join(cause, context.Canceled)
		}
		return &Error{Code: header.Code, Operation: operation, Protocol: ProtocolVersion{Name: name, Version: 1}, ExitCode: ptrCopy(&out.ExitCode), StderrTail: out.StderrTail, RawRecord: append(json.RawMessage(nil), data...), Cause: cause}
	}
	if header.Schema != name || header.Version != 1 {
		return &Error{Code: "unsupported_protocol", Operation: operation, Protocol: ProtocolVersion{Name: header.Schema, Version: header.Version}, RawRecord: append(json.RawMessage(nil), data...)}
	}
	if out.ExitCode != 0 {
		return &Error{Code: "command_error", Operation: operation, ExitCode: ptrCopy(&out.ExitCode), StderrTail: out.StderrTail, Cause: out.Err}
	}
	return nil
}

func validateCatalogMetadata(metadata CatalogMetadata) error {
	if metadata.ObservedAt.IsZero() || metadata.DaemonVersion == "" {
		return errors.New("missing discovery observation or daemon version")
	}
	return nil
}

func validateProviderCatalog(catalog ProviderCatalog, data []byte) error {
	if err := validateCatalogMetadata(catalog.CatalogMetadata); err != nil {
		return err
	}
	if catalog.Providers == nil || catalog.Diagnostics == nil {
		return errors.New("missing provider or diagnostic array")
	}
	var wire struct {
		Providers []map[string]json.RawMessage `json:"providers"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	seen := map[string]bool{}
	for i, p := range catalog.Providers {
		if p.ID == "" || p.Type == "" || p.Revision == "" || p.Health == "" || seen[p.ID] {
			return errors.New("invalid or duplicate provider identity/metadata")
		}
		var enabled *bool
		if err := json.Unmarshal(wire.Providers[i]["enabled"], &enabled); err != nil || enabled == nil {
			return errors.New("missing enabled state")
		}
		seen[p.ID] = true
	}
	return nil
}

func validateModelCatalog(catalog ModelCatalog) error {
	if err := validateCatalogMetadata(catalog.CatalogMetadata); err != nil {
		return err
	}
	if catalog.ProviderRevision == "" || catalog.Source == "" || catalog.State == "" || catalog.Models == nil || catalog.Diagnostics == nil {
		return errors.New("missing model catalog metadata/arrays")
	}
	seen := map[string]bool{}
	for _, model := range catalog.Models {
		if model.ID == "" || seen[model.ID] {
			return errors.New("invalid or duplicate model ID")
		}
		seen[model.ID] = true
	}
	return nil
}

func validateReadiness(readiness Readiness, request PreflightRequest) error {
	if err := validateCatalogMetadata(readiness.CatalogMetadata); err != nil {
		return err
	}
	if readiness.Fidelity == "" || readiness.Checks == nil {
		return errors.New("missing readiness fidelity/checks")
	}
	stages := map[string]bool{}
	tools, live := false, false
	for _, check := range readiness.Checks {
		if check.Stage == "" || check.Code == "" || check.State == "" {
			return errors.New("incomplete readiness check")
		}
		stages[check.Stage] = true
		if check.Stage == "requirements" {
			if check.Code == "tools_off_unsupported" && check.State != CheckUnsupported {
				return errors.New("contradictory tools-off requirement result")
			}
			if check.Code == "live_events_unsupported" && check.State != CheckUnsupported {
				return errors.New("contradictory live-event requirement result")
			}
			if request.ToolPolicy == ToolPolicyOff && check.Code == "tools_off_unsupported" {
				tools = true
			}
			if request.ToolPolicy == ToolPolicyDaemonManaged && check.Code == "daemon_managed_tools" {
				tools = true
			}
			if check.Code == "live_events_supported" && (check.State != CheckReady || readiness.Fidelity != FidelityLive) {
				return errors.New("live events lack verified fidelity")
			}
			if check.Code == "live_events_unsupported" || check.Code == "live_events_supported" {
				live = true
			}
		}
	}
	for _, stage := range []string{"cli", "daemon", "provider", "model", "requirements"} {
		if !stages[stage] {
			return errors.New("missing required readiness stage")
		}
	}
	if !tools || (request.RequireLiveEvents && !live) {
		return errors.New("requested execution requirement was not checked")
	}
	return nil
}
