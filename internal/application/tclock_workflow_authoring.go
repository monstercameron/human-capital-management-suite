package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/clockpunch"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designerpalette"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/releasefixture"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

const (
	localDevPunchTemplateID = "hcmnext.templates.time_punch_session"
	localDevPunchVersion    = "1.0.0"
	localDevPunchFixture    = "fixture:workflow.time-punch-session/reproducible-compile@v1"
)

var errTimeclockBootstrapClock = errors.New("timeclock workflow bootstrap: a trusted clock is required")

// localDevelopmentTimeclockWorkflowEntry returns the server-owned punch
// session template for the local-development designer. The definition and
// digest come from the executable timeclock package, so authoring cannot drift
// from the plan that the runtime resolves.
func localDevelopmentTimeclockWorkflowEntry(cfg ServeConfig) (designerpalette.Entry, bool) {
	if cfg.Profile != ServeProfileLocalDev || cfg.Tenant == "" {
		return designerpalette.Entry{}, false
	}
	definition := clockpunch.Definition()
	plan, err := clockpunch.Compile()
	if err != nil {
		return designerpalette.Entry{}, false
	}
	return designerpalette.Entry{
		ID: localDevPunchTemplateID, Version: 1, Name: "Clock in and clock out", Kind: designerpalette.KindTemplate,
		Domain: "Time", Description: "Commit clock-in, await clock-out, commit clock-out and close the session, with missing-out and repair paths.",
		EffectClass: capability.EffectInternalMutation, Reversal: "COMPENSATION_REQUIRED", Status: "ACTIVE",
		RequiredCapabilities: requiredWorkflowCapabilities(definition.Nodes), PublishedPlanDigest: plan.Digest(),
		Expansion: designerpalette.Expansion{Template: &definition},
	}, true
}

// bootstrapLocalDevTimeclockWorkflowVersion publishes and activates the
// reference punch session on the same governed durable path as other shipped
// workflows. It is intentionally a separate call so composition can invoke it
// after the existing local-development workflow bootstrap without changing
// that shared bootstrap contract.
func bootstrapLocalDevTimeclockWorkflowVersion(ctx context.Context, cfg ServeConfig, versions version.Store, now func() time.Time) ([]version.CompiledVersion, error) {
	if cfg.Profile != ServeProfileLocalDev || !cfg.DevBrowserLogin || cfg.Tenant == "" {
		return nil, nil
	}
	if _, ok := demoworkforce.PackFor(cfg.Tenant); !ok {
		return nil, nil
	}
	registry, ok := versions.(platformexecution.VersionRegistry)
	if !ok {
		return nil, nil
	}
	if now == nil {
		return nil, errTimeclockBootstrapClock
	}
	at := now().UTC()
	if at.IsZero() {
		return nil, errTimeclockBootstrapClock
	}
	definition := clockpunch.Definition()
	plan, err := clockpunch.Compile()
	if err != nil {
		return nil, fmt.Errorf("compile the local development punch session workflow: %w", err)
	}
	published, err := version.Publish(registry, definition, plan, clockpunch.CompileOptions(), version.PublishMeta{
		SemanticVersion: localDevPunchVersion, PublishedAt: at, PublishedBy: "internal/application:local-development",
		ToolVersions: map[string]string{"go": runtime.Version(), "publisher": "internal/application"},
		FixtureRefs:  []string{localDevPunchFixture},
	})
	if err != nil {
		return nil, fmt.Errorf("publish the local development punch session workflow: %w", err)
	}
	if published.Status == version.StatusDraft {
		suite := releasefixture.Suite{localDevPunchFixture: reproduceLocalDevPunchPlan}
		report := releasefixture.Run(suite, published, "internal/application:local-development", at)
		if _, err := platformexecution.ApproveRelease(ctx, registry, suite, platformexecution.ReleaseApproval{
			CompiledPlanDigest: published.CompiledPlanDigest, ApprovedBy: platformexecution.DevReleaseApprover,
			Authority: "authority:local-development-bootstrap", Reason: "local development bootstrap of time punch session",
			Report: report, ApprovedAt: at,
		}); err != nil {
			return nil, fmt.Errorf("approve the local development punch session workflow: %w", err)
		}
		published, err = registry.ActivateApproved(ctx, published.CompiledPlanDigest, true)
		if err != nil {
			return nil, fmt.Errorf("activate the local development punch session workflow: %w", err)
		}
	}
	return []version.CompiledVersion{published}, nil
}

func reproduceLocalDevPunchPlan(v version.CompiledVersion) error {
	if err := v.Verify(); err != nil {
		return err
	}
	plan, err := clockpunch.Compile()
	if err != nil {
		return err
	}
	if plan.WorkflowID != v.WorkflowID || plan.Digest() != v.CompiledPlanDigest {
		return fmt.Errorf("compiled punch session plan %s does not match published %s", plan.Digest(), v.CompiledPlanDigest)
	}
	if !bytes.Equal(canonicalPlanBytesForApplication(plan), v.CanonicalPlanBytes) {
		return fmt.Errorf("compiled punch session canonical plan differs from published bytes")
	}
	return nil
}

func canonicalPlanBytesForApplication(plan *workflow.CompiledWorkflow) []byte {
	encoded, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return nil
	}
	return append(encoded, '\n')
}
