package execution

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// VersionRegistry is a durable [version.Store] that activates a version only
// on a recorded approval (internal/data/workflowversionstore.Store).
type VersionRegistry interface {
	version.Store
	RecordApproval(ctx context.Context, approval workflowversionstore.Approval) error
	ActivateApproved(ctx context.Context, planDigest string, supersede bool) (version.CompiledVersion, error)
}

// versionApprovalNamespace derives a stable approval id per (version,
// approver), so a recomposed server records its release approval once.
var versionApprovalNamespace = uuid.MustParse("7d3c1d0e-5b7a-4f55-9d3e-2a8c0f6e9b41")

// composeVersions returns the version store this composition publishes into
// and the activation step each shipped version goes through.
//
// With a durable [VersionRegistry] a DRAFT version is approved under the
// configured release approver -- never the publisher, which the registry
// refuses -- and activated on that recorded approval, superseding the version
// a previous release left active. A version an operator quarantined or
// retired is left exactly as it is: a restart must never undo a quarantine,
// so starts on it stay refused until it is governed back into service. An
// ACTIVE version is left alone, so a restart activates nothing twice.
//
// With no registry the composition keeps the private in-memory registry unit
// compositions use, self-activated as before.
func composeVersions(cfg PromotionExecutionConfig, at time.Time) (version.Store, func(version.CompiledVersion) error) {
	if cfg.Versions == nil {
		registry := version.NewRegistry()
		return registry, func(published version.CompiledVersion) error {
			_, err := version.Activate(registry, published.CompiledPlanDigest, version.ActivationEvidence{
				Authorized: true, ApprovedBy: versionPublisher,
				Authority: "authority:execution-authority-flag", ApprovedAt: at,
				ReviewedPlanDigest: published.CompiledPlanDigest, TestsPassed: true,
			})
			return err
		}
	}
	approver := strings.TrimSpace(cfg.VersionApprover)
	if approver == "" {
		approver = defaultVersionApprover
	}
	registry := cfg.Versions
	return registry, func(published version.CompiledVersion) error {
		if published.Status != version.StatusDraft {
			return nil
		}
		ctx := context.Background()
		digest := published.CompiledPlanDigest
		if err := registry.RecordApproval(ctx, workflowversionstore.Approval{
			ApprovalID:         uuid.NewSHA1(versionApprovalNamespace, []byte(digest+"\x00"+approver)),
			CompiledPlanDigest: digest, ReviewedPlanDigest: digest,
			ApprovedBy: approver, Authority: "authority:workflow-release",
			Reason:      fmt.Sprintf("release approval of %s %s", published.WorkflowID, published.SemanticVersion),
			TestsPassed: true, FixtureRefs: published.FixtureRefs, ApprovedAt: at,
		}); err != nil {
			return err
		}
		_, err := registry.ActivateApproved(ctx, digest, true)
		return err
	}
}
