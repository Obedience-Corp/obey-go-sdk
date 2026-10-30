//go:build process_tests

package obey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func init() {
	if os.Getenv("OBEY_SDK_CATALOG_FAKE") != "1" {
		return
	}
	if os.Getenv("OBEY_SDK_TEST_CONTAINER") != "1" {
		os.Exit(99)
	}
	args := os.Args[1:]
	if len(args) > 1 && args[0] == "--socket" {
		args = args[2:]
	}
	ledger := os.Getenv("SDK_CATALOG_LEDGER")
	old, _ := os.ReadFile(ledger)
	entry, _ := json.Marshal(os.Args[1:])
	if err := os.WriteFile(ledger, append(old, append(entry, '\n')...), 0600); err != nil {
		os.Exit(98)
	}
	mode := os.Getenv("SDK_CATALOG_MODE")
	if args[0] == "doctor" {
		if mode == "legacy" {
			fmt.Fprintln(os.Stderr, "unknown flag: --capabilities-only")
			os.Exit(2)
		}
		status := "unknown"
		if mode == "health-fallback" {
			status = "pass"
		}
		fmt.Printf(`{"schema":"obey.doctor","version":3,"status":%q,"checks":[],"capabilities":{"provider_list":{"protocol":"obey.providers.list","versions":[1]},"provider_models":{"protocol":"obey.providers.models","versions":[1]},"provider_check":{"protocol":"obey.providers.check","versions":[1]}}}`+"\n", status)
		os.Exit(0)
	}
	name := providerListProtocol
	if len(args) > 1 && args[1] == "models" {
		name = providerModelsProtocol
	}
	if len(args) > 1 && args[1] == "check" {
		name = providerCheckProtocol
	}
	if mode == "block" {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	if mode == "malformed" {
		fmt.Println("{bad")
		os.Exit(0)
	}
	if mode == "oversize" {
		fmt.Println(strings.Repeat("x", 8192))
		os.Exit(0)
	}
	if mode == "timeout" || mode == "canceled" {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"schema": "obey.machine.error", "version": 1, "operation": name, "code": mode, "message": "Fixture failure"})
		os.Exit(2)
	}
	header := discoveryTestHeader(name)
	if mode == "version" {
		header.Version = 99
	}
	id := "profile-a"
	if mode == "wrong-provider" {
		id = "other-profile"
	}
	switch name {
	case providerListProtocol:
		p, m := "profile-a", "model-a"
		_ = json.NewEncoder(os.Stdout).Encode(ProviderCatalog{CatalogMetadata: header, DefaultProvider: &p, DefaultModel: &m, Providers: []ProviderInfo{{ID: p, DisplayName: "Profile A", Type: "openai", Revision: "r1", DefaultModel: m, Enabled: true, Health: CheckUnknown}}, Diagnostics: []DiscoveryDiagnostic{}})
	case providerModelsProtocol:
		count := strings.Count(string(old), `"providers"`) + 1
		model := ModelInfo{ID: fmt.Sprintf("model-%d", count), DisplayName: "Fixture model"}
		_ = json.NewEncoder(os.Stdout).Encode(ModelCatalog{CatalogMetadata: header, ProviderID: id, ProviderRevision: "r1", Source: "live", State: "fresh", Models: []ModelInfo{model}, Diagnostics: []DiscoveryDiagnostic{}})
	case providerCheckProtocol:
		checks := []ReadinessCheck{{Stage: "cli", Code: "provider_check", State: CheckReady}, {Stage: "daemon", Code: "daemon_reachable", State: CheckReady}, {Stage: "provider", Code: "provider_catalog_reachable", State: CheckReady}, {Stage: "model", Code: "model_advertised", State: CheckReady}}
		if strings.Contains(strings.Join(args, " "), "--tools off") {
			checks = append(checks, ReadinessCheck{Stage: "requirements", Code: "tools_off_unsupported", State: CheckUnsupported})
		} else {
			checks = append(checks, ReadinessCheck{Stage: "requirements", Code: "daemon_managed_tools", State: CheckReady})
		}
		if strings.Contains(strings.Join(args, " "), "--require-live-events") {
			checks = append(checks, ReadinessCheck{Stage: "requirements", Code: "live_events_unsupported", State: CheckUnsupported})
		}
		if mode == "missing-requirement" {
			checks = checks[:4]
		}
		_ = json.NewEncoder(os.Stdout).Encode(Readiness{CatalogMetadata: header, ProviderID: id, ModelID: "model-a", Fidelity: FidelityFinalOnly, Checks: checks})
	}
	if mode == "nonzero" {
		os.Exit(9)
	}
	os.Exit(0)
}

func catalogFakeClient(t *testing.T, mode string, options ...ClientOption) (*Client, string) {
	t.Helper()
	publicGuard(t)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ledger := filepath.Join(t.TempDir(), "calls.jsonl")
	options = append(options, WithEnvironment(map[string]string{"OBEY_SDK_CATALOG_FAKE": "1", "SDK_CATALOG_MODE": mode, "SDK_CATALOG_LEDGER": ledger}))
	client, err := NewClient(binary, options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, ledger
}

func TestProcessDiscoveryFreshSnapshotsAndSocketRouting(t *testing.T) {
	c, ledger := catalogFakeClient(t, "normal", WithSocket("/selected-daemon.sock"))
	p, err := c.Providers(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if p.DefaultProvider == nil || *p.DefaultProvider != "profile-a" || p.Providers[0].ModelDiscovery != nil {
		t.Fatal(p)
	}
	*p.DefaultProvider = "mutated"
	p.Raw[0] = 'x'
	first, err := c.Models(t.Context(), "profile-a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.Models(t.Context(), "profile-a")
	if err != nil {
		t.Fatal(err)
	}
	if first.Models[0].ID == second.Models[0].ID || first.Models[0].ToolsOff != nil {
		t.Fatal("catalog was cached or unknown capability changed")
	}
	if _, err := c.RefreshModels(t.Context(), "profile-a"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var args []string
		if err := json.Unmarshal([]byte(line), &args); err != nil {
			t.Fatal(err)
		}
		if len(args) < 3 || args[0] != "--socket" || args[1] != "/selected-daemon.sock" {
			t.Fatal(args)
		}
		if args[2] == "doctor" && !strings.Contains(string(line), "--capabilities-only") {
			t.Fatal("broad health fallback")
		}
	}
	if !strings.Contains(string(data), "--refresh") || strings.Contains(string(data), `"run"`) {
		t.Fatal("wrong operation set")
	}
}

func TestProcessDiscoveryOldCLIRefusesBeforeProviderOperation(t *testing.T) {
	for _, mode := range []string{"legacy", "health-fallback"} {
		c, ledger := catalogFakeClient(t, mode)
		_, err := c.Providers(t.Context())
		var typed *Error
		if !errors.As(err, &typed) || typed.Code != "unsupported_protocol" {
			t.Fatalf("%s: %v", mode, err)
		}
		data, _ := os.ReadFile(ledger)
		if strings.Contains(string(data), `"providers"`) {
			t.Fatal("old CLI received provider command")
		}
	}
}

func TestProcessDiscoveryTypedErrorsAndOutputLimits(t *testing.T) {
	for mode, code := range map[string]string{"malformed": "protocol_error", "version": "unsupported_protocol", "wrong-provider": "scope_mismatch", "nonzero": "command_error", "timeout": "timeout", "canceled": "canceled", "oversize": "output_limit"} {
		t.Run(mode, func(t *testing.T) {
			c, _ := catalogFakeClient(t, mode, WithLimits(Limits{MaxRecordBytes: 1024, ShutdownGrace: 100 * time.Millisecond}))
			_, err := c.Models(t.Context(), "profile-a")
			var typed *Error
			if !errors.As(err, &typed) || typed.Code != code {
				t.Fatalf("got %v, want %s", err, code)
			}
			if mode == "timeout" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			if mode == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}

func TestProcessDiscoveryPreflightPreservesRequirements(t *testing.T) {
	c, _ := catalogFakeClient(t, "normal")
	r, err := c.Preflight(t.Context(), PreflightRequest{Provider: "profile-a", Model: "model-a"})
	if err != nil || !r.Ready() {
		t.Fatal(r, err)
	}
	r, err = c.Preflight(t.Context(), PreflightRequest{Provider: "profile-a", ToolPolicy: ToolPolicyOff, RequireLiveEvents: true})
	if err != nil || !r.HasFailures() || r.Ready() {
		t.Fatal(r, err)
	}
	bad, _ := catalogFakeClient(t, "missing-requirement")
	if _, err := bad.Preflight(t.Context(), PreflightRequest{Provider: "profile-a"}); err == nil {
		t.Fatal("missing checks accepted")
	}
}

func TestProcessDiscoveryUnsupportedScopeAndClosedClientDoNotInvoke(t *testing.T) {
	c, ledger := catalogFakeClient(t, "normal")
	_, err := c.Preflight(t.Context(), PreflightRequest{Provider: "profile-a", Camp: "camp-a"})
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != "unsupported_capability" {
		t.Fatal(err)
	}
	if _, err := os.Stat(ledger); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unsupported scope invoked CLI")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = c.Providers(t.Context())
	if !errors.As(err, &typed) || typed.Code != "closed_client" {
		t.Fatal(err)
	}
}

func TestProcessDiscoveryCancellationOwnsProbeLifetime(t *testing.T) {
	c, _ := catalogFakeClient(t, "block", WithLimits(Limits{ShutdownGrace: 100 * time.Millisecond}))
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	_, err := c.Models(ctx, "profile-a")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
