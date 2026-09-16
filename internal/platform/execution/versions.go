package execution

import (
	"context"
	"time"

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

// composeVersions returns the version store this composition publishes the
// shipped workflows into.
//
// With a durable [VersionRegistry] the shipped versions are published as
// DRAFT (idempotently) and nothing else: composition records no approval and
// activates nothing, so a version reaches service only through the governed
// fixtures -> approve -> activate path in release.go. An ACTIVE, QUARANTINED
// or RETIRED version is left exactly as it is across restart. Until a version
// is activated a served start is refused [ErrNoActiveWorkflowVersion].
//
// With no registry the composition keeps a private in-memory registry for
// unit compositions only; each version is activated there on its own
// in-process fixture run ([activateInMemory]).
func composeVersions(cfg PromotionExecutionConfig, at time.Time) (version.Store, error) {
	if cfg.Versions != nil {
		if _, err := PublishShippedVersions(cfg.Versions, at); err != nil {
			return nil, err
		}
		return cfg.Versions, nil
	}
	registry := version.NewRegistry()
	published, err := PublishShippedVersions(registry, at)
	if err != nil {
		return nil, err
	}
	for _, v := range published {
		if err := activateInMemory(registry, v, at); err != nil {
			return nil, err
		}
	}
	return registry, nil
}
