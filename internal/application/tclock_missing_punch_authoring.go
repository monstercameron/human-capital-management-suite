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
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/clockrepair"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designerpalette"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/releasefixture"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

const (
	localDevMissingPunchTemplateID = "hcmnext.templates.fix_missing_punch"
	localDevMissingPunchVersion    = "1.0.0"
	localDevMissingPunchFixture    = "fixture:workflow.fix-missing-punch/reproducible-compile@v1"
)

var errMissingPunchBootstrapClock = errors.New("missing punch workflow bootstrap: a trusted clock is required")

// localDevelopmentMissingPunchWorkflowEntry returns the executable missing
// punch repair template for the local-development designer.
func localDevelopmentMissingPunchWorkflowEntry(cfg ServeConfig) (designerpalette.Entry, bool) {
	if cfg.Profile != ServeProfileLocalDev || cfg.Tenant == "" {
		return designerpalette.Entry{}, false
	}
	definition := clockrepair.Definition()
	plan, err := clockrepair.Compile()
	if err != nil {
		return designerpalette.Entry{}, false
	}
	return designerpalette.Entry{
		ID: localDevMissingPunchTemplateID, Version: 1, Name: "Fix a missing punch", Kind: designerpalette.KindTemplate,
		Domain: "Time", Description: "Review a missing clock-out, capture the correction, and preserve the original record.",
		EffectClass: capability.EffectInternalMutation, Reversal: "COMPENSATION_REQUIRED", Status: "ACTIVE",
		RequiredCapabilities: requiredWorkflowCapabilities(definition.Nodes), PublishedPlanDigest: plan.Digest(),
		Expansion: designerpalette.Expansion{Template: &definition},
	}, true
}

// bootstrapLocalDevMissingPunchWorkflowVersion publishes and activates the
// reference repair workflow through the governed durable release path.
func bootstrapLocalDevMissingPunchWorkflowVersion(ctx context.Context, cfg ServeConfig, versions version.Store, now func() time.Time) ([]version.CompiledVersion, error) {
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
		return nil, errMissingPunchBootstrapClock
	}
	at := now().UTC()
	if at.IsZero() {
		return nil, errMissingPunchBootstrapClock
	}
	definition := clockrepair.Definition()
	plan, err := clockrepair.Compile()
	if err != nil {
		return nil, fmt.Errorf("compile the local development missing punch workflow: %w", err)
	}
	published, err := version.Publish(registry, definition, plan, clockrepair.CompileOptions(), version.PublishMeta{
		SemanticVersion: localDevMissingPunchVersion, PublishedAt: at, PublishedBy: "internal/application:local-development",
		ToolVersions: map[string]string{"go": runtime.Version(), "publisher": "internal/application"},
		FixtureRefs:  []string{localDevMissingPunchFixture},
	})
	if err != nil {
		return nil, fmt.Errorf("publish the local development missing punch workflow: %w", err)
	}
	if published.Status == version.StatusDraft {
		suite := releasefixture.Suite{localDevMissingPunchFixture: reproduceLocalDevMissingPunchPlan}
		report := releasefixture.Run(suite, published, "internal/application:local-development", at)
		if _, err := platformexecution.ApproveRelease(ctx, registry, suite, platformexecution.ReleaseApproval{
			CompiledPlanDigest: published.CompiledPlanDigest, ApprovedBy: platformexecution.DevReleaseApprover,
			Authority: "authority:local-development-bootstrap", Reason: "local development bootstrap of missing punch workflow",
			Report: report, ApprovedAt: at,
		}); err != nil {
			return nil, fmt.Errorf("approve the local development missing punch workflow: %w", err)
		}
		published, err = registry.ActivateApproved(ctx, published.CompiledPlanDigest, true)
		if err != nil {
			return nil, fmt.Errorf("activate the local development missing punch workflow: %w", err)
		}
	}
	return []version.CompiledVersion{published}, nil
}

func reproduceLocalDevMissingPunchPlan(v version.CompiledVersion) error {
	if err := v.Verify(); err != nil {
		return err
	}
	plan, err := clockrepair.Compile()
	if err != nil {
		return err
	}
	if plan.WorkflowID != v.WorkflowID || plan.Digest() != v.CompiledPlanDigest {
		return fmt.Errorf("compiled missing punch plan %s does not match published %s", plan.Digest(), v.CompiledPlanDigest)
	}
	canonical, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	return compareMissingPunchCanonical(append(canonical, '\n'), v.CanonicalPlanBytes)
}

func compareMissingPunchCanonical(want, got []byte) error {
	if !bytes.Equal(want, got) {
		return errors.New("compiled missing punch canonical plan differs from published bytes")
	}
	return nil
}
