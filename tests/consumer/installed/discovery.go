package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	obey "github.com/Obedience-Corp/obey-go-sdk/pkg/obey"
)

func (f *completionFixture) catalogCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.catalogRequests
}

func (p *probe) discovery(ctx context.Context, client *obey.Client) error {
	before := p.fixture.catalogCount()
	providers, err := client.Providers(ctx)
	if err != nil {
		return err
	}
	if p.fixture.catalogCount() != before || p.fixture.count() != 0 {
		return errors.New("provider inventory contacted a provider endpoint")
	}
	found := false
	for _, profile := range providers.Providers {
		if profile.ID == providerName {
			found = profile.Type == "openai" && profile.Enabled && profile.DefaultModel == modelName && profile.Health == obey.CheckUnknown && profile.Revision != ""
		}
	}
	if !found {
		return errors.New("provider inventory omitted daemon-owned route")
	}
	if err := writeJSON(filepath.Join(p.opts.evidence, "providers.json"), providers); err != nil {
		return err
	}
	models, err := client.Models(ctx, providerName)
	if err != nil {
		return err
	}
	if models.ProviderID != providerName || len(models.Models) != 1 || models.Models[0].ID != modelName || models.Models[0].ToolsOff != nil || models.Models[0].AssistantDeltas != nil {
		return errors.New("model catalog lost raw ID or invented capability evidence")
	}
	if err := writeJSON(filepath.Join(p.opts.evidence, "models.json"), models); err != nil {
		return err
	}
	before = p.fixture.catalogCount()
	refreshed, err := client.RefreshModels(ctx, providerName)
	if err != nil {
		return err
	}
	if p.fixture.catalogCount() != before+1 || refreshed.ProviderID != providerName || len(refreshed.Models) != 1 || refreshed.Models[0].ID != modelName {
		return errors.New("explicit catalog refresh did not perform exactly one selected-profile fetch")
	}
	if err := writeJSON(filepath.Join(p.opts.evidence, "models-refreshed.json"), refreshed); err != nil {
		return err
	}
	readiness, err := client.Preflight(ctx, obey.PreflightRequest{Provider: providerName, Model: modelName, ToolPolicy: obey.ToolPolicyDaemonManaged})
	if err != nil {
		return err
	}
	if readiness.HasFailures() || readiness.Fidelity != obey.FidelityFinalOnly {
		return errors.New("configured route failed preflight or claimed live fidelity")
	}
	if err := writeJSON(filepath.Join(p.opts.evidence, "preflight.json"), readiness); err != nil {
		return err
	}
	restricted, err := client.Preflight(ctx, obey.PreflightRequest{Provider: providerName, Model: modelName, ToolPolicy: obey.ToolPolicyOff, RequireLiveEvents: true})
	if err != nil {
		return err
	}
	off, live := false, false
	for _, check := range restricted.Checks {
		if check.Code == "tools_off_unsupported" && check.State == obey.CheckUnsupported {
			off = true
		}
		if check.Code == "live_events_unsupported" && check.State == obey.CheckUnsupported {
			live = true
		}
	}
	if !off || !live || !restricted.HasFailures() || restricted.Ready() {
		return errors.New("preflight silently accepted unsupported execution requirements")
	}
	if err := writeJSON(filepath.Join(p.opts.evidence, "preflight-restricted.json"), restricted); err != nil {
		return err
	}
	rows, err := p.sessions(ctx, p.opts.camp, "sessions-after-discovery.json")
	if err != nil {
		return err
	}
	if len(rows) != 0 || p.fixture.count() != 0 {
		return errors.New("discovery created sessions or invoked inference")
	}
	fmt.Println("Discovery: daemon-owned provider/model IDs; metadata-only inventory; selected refresh; tools-off/live refusal; no sessions or inference")
	return nil
}
