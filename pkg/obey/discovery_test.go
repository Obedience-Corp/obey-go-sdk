package obey

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Obedience-Corp/obey-go-sdk/internal/execclient"
)

func discoveryTestHeader(schema string) CatalogMetadata {
	return CatalogMetadata{Schema: schema, Version: 1, ObservedAt: time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC), DaemonVersion: "0.5.0"}
}

func TestDiscoveryProviderBooleanAndIdentityValidation(t *testing.T) {
	data := []byte(`{"schema":"obey.providers.list","version":1,"observed_at":"2026-10-30T00:00:00Z","daemon_version":"0.5.0","providers":[{"id":"p","type":"openai","revision":"r","health":"future_state"}],"diagnostics":[]}`)
	var catalog ProviderCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	if err := validateProviderCatalog(catalog, data); err == nil {
		t.Fatal("missing enabled state silently became false")
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	wire["providers"].([]any)[0].(map[string]any)["enabled"] = false
	data, _ = json.Marshal(wire)
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	if err := validateProviderCatalog(catalog, data); err != nil {
		t.Fatal(err)
	}
	if catalog.Providers[0].Health != "future_state" || catalog.Providers[0].ModelDiscovery != nil {
		t.Fatal("unknown evidence was lost")
	}
	catalog.Providers = append(catalog.Providers, catalog.Providers[0])
	if err := validateProviderCatalog(catalog, data); err == nil {
		t.Fatal("duplicate profile accepted")
	}
}

func TestDiscoveryReadinessUnknownIsNotReadyOrDefiniteFailure(t *testing.T) {
	r := &Readiness{Checks: []ReadinessCheck{{Stage: "provider", Code: "model_unverified", State: CheckUnknown}}}
	if r.Ready() || r.HasFailures() {
		t.Fatal("unknown check collapsed into a verdict")
	}
	r.Checks[0].State = CheckUnavailable
	if !r.HasFailures() || r.Ready() {
		t.Fatal("unavailable check lost")
	}
	r.Checks[0].State = CheckReady
	if !r.Ready() || r.HasFailures() {
		t.Fatal("ready check lost")
	}
}

func TestDiscoveryErrorEnvelopeRequiresNonzeroExitAndExactOperation(t *testing.T) {
	data := []byte(`{"schema":"obey.machine.error","version":1,"operation":"obey.providers.models","code":"timeout","message":"Timed out"}`)
	err := validateDiscoveryInvocation(data, providerModelsProtocol, "models", execclient.Outcome{ExitCode: 1})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		exit int
	}{{providerListProtocol, 1}, {providerModelsProtocol, 0}} {
		err := validateDiscoveryInvocation(data, test.name, "models", execclient.Outcome{ExitCode: test.exit})
		var typed *Error
		if !errors.As(err, &typed) || typed.Code != "protocol_error" {
			t.Fatal(err)
		}
	}
}

func TestDiscoveryReadinessDoesNotSilentlyDropRequestedRequirements(t *testing.T) {
	r := Readiness{CatalogMetadata: discoveryTestHeader(providerCheckProtocol), Fidelity: FidelityFinalOnly, Checks: []ReadinessCheck{
		{Stage: "cli", Code: "provider_check", State: CheckReady}, {Stage: "daemon", Code: "daemon_reachable", State: CheckReady},
		{Stage: "provider", Code: "provider_catalog_reachable", State: CheckReady}, {Stage: "model", Code: "model_advertised", State: CheckReady},
		{Stage: "requirements", Code: "tools_off_unsupported", State: CheckUnsupported},
	}}
	request := PreflightRequest{ToolPolicy: ToolPolicyOff, RequireLiveEvents: true}
	if err := validateReadiness(r, request); err == nil {
		t.Fatal("missing live requirement accepted")
	}
	r.Checks = append(r.Checks, ReadinessCheck{Stage: "requirements", Code: "live_events_unsupported", State: CheckUnsupported})
	if err := validateReadiness(r, request); err != nil {
		t.Fatal(err)
	}
	r.Checks[4].State = CheckReady
	if err := validateReadiness(r, request); err == nil {
		t.Fatal("unsupported tools were declared ready")
	}
}
