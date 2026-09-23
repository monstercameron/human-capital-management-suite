package application

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// ResolveWorkflowMigrationPlans loads the source and target version records by
// compiled-plan digest and rehydrates the exact compiled plans the instance
// migrates between. The decoded plans must digest back to the digests the
// registry stored, or the registry row is refused rather than trusted.
func ResolveWorkflowMigrationPlans(sourceDigest, targetDigest string, versions version.Store) (source, target *workflow.CompiledWorkflow, err error) {
	sourceRec, found, err := versions.GetByDigest(sourceDigest)
	if err != nil {
		return nil, nil, fmt.Errorf("load source version: %w", err)
	}
	if !found {
		return nil, nil, fmt.Errorf("no published version carries source digest %q", sourceDigest)
	}
	targetRec, found, err := versions.GetByDigest(targetDigest)
	if err != nil {
		return nil, nil, fmt.Errorf("load target version: %w", err)
	}
	if !found {
		return nil, nil, fmt.Errorf("no published version carries target digest %q", targetDigest)
	}
	if sourceRec.WorkflowID != targetRec.WorkflowID {
		return nil, nil, fmt.Errorf("source workflow %q and target workflow %q differ", sourceRec.WorkflowID, targetRec.WorkflowID)
	}
	source, err = workflow.DecodeCanonicalPlan(sourceRec.CanonicalPlanBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("decode source plan: %w", err)
	}
	target, err = workflow.DecodeCanonicalPlan(targetRec.CanonicalPlanBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("decode target plan: %w", err)
	}
	if source.Digest() != sourceRec.CompiledPlanDigest || target.Digest() != targetRec.CompiledPlanDigest {
		return nil, nil, fmt.Errorf("decoded plan digest does not match the registry record")
	}
	return source, target, nil
}
