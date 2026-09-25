package projectservice

import (
	"context"
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/data/projectconfigstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
)

// StoreMigrationPreviewer reads the current published workflow and a bounded,
// tenant-scoped active-task snapshot before invoking the pure migration planner.
// The planner receives field values only for validation; they are never copied
// into application preview diagnostics.
type StoreMigrationPreviewer struct {
	Projects ActiveTaskSnapshotRepository
	Configs  WorkflowPreviewConfigurationRepository
}

type ActiveTaskSnapshotRepository interface {
	ActiveTaskSnapshots(context.Context, string, string) ([]projectworkflow.TaskSnapshot, error)
}

type WorkflowPreviewConfigurationRepository interface {
	GetPublished(context.Context, string, string) (projectconfigstore.Published, error)
	GetDraft(context.Context, string, string) (projectworkflow.Config, uint64, error)
}

var _ MigrationPreviewer = StoreMigrationPreviewer{}
var _ ActiveTaskSnapshotRepository = (*projectstore.Store)(nil)
var _ WorkflowPreviewConfigurationRepository = (*projectconfigstore.Store)(nil)

func (p StoreMigrationPreviewer) Preview(ctx context.Context, tenantID, projectID string, pending projectworkflow.Config, expectedDraftRevision uint64, mappings projectworkflow.MigrationMappings) (WorkflowPreview, error) {
	if p.Projects == nil || p.Configs == nil || tenantID == "" || projectID == "" {
		return WorkflowPreview{}, ErrUnavailable
	}
	current, err := p.Configs.GetPublished(ctx, tenantID, projectID)
	if err != nil {
		return WorkflowPreview{}, err
	}
	draft, draftRevision, err := p.Configs.GetDraft(ctx, tenantID, projectID)
	if err != nil {
		return WorkflowPreview{}, err
	}
	if draftRevision != expectedDraftRevision {
		return WorkflowPreview{}, projectconfigstore.ErrRevisionConflict
	}
	// The config supplied by the caller must be exactly the revision that is
	// being reviewed, even if a concurrent draft save happened meanwhile.
	requestedDigest, publishErr := projectworkflow.Publish(pending, 1)
	if publishErr != nil {
		return WorkflowPreview{}, publishErr
	}
	draftDigest, publishErr := projectworkflow.Publish(draft, 1)
	if publishErr != nil {
		return WorkflowPreview{}, publishErr
	}
	if requestedDigest.Digest() != draftDigest.Digest() {
		return WorkflowPreview{}, projectconfigstore.ErrRevisionConflict
	}
	digestConfig, err := projectworkflow.Publish(pending, 1)
	if err != nil {
		if validation, ok := err.(projectworkflow.ValidationErrors); ok {
			return WorkflowPreview{Config: pending, Errors: validation}, nil
		}
		return WorkflowPreview{}, err
	}
	tasks, err := p.Projects.ActiveTaskSnapshots(ctx, tenantID, projectID)
	if err != nil {
		return WorkflowPreview{}, err
	}
	result, previewErr := projectworkflow.PreviewMigration(projectworkflow.MigrationRequest{
		Current:        current.Config,
		Pending:        pending,
		Tasks:          tasks,
		StatusMappings: mappings.Statuses,
		FieldMappings:  mappings.Fields,
	})
	out := WorkflowPreview{Config: pending, DraftRevision: expectedDraftRevision, Digest: digestConfig.Digest(), AffectedTaskCount: result.AffectedTaskCount, AffectedTaskIDs: append([]string(nil), result.AffectedTaskIDs...), Safe: result.Safe}
	if len(result.Issues) > 0 {
		out.Errors = make(projectworkflow.ValidationErrors, 0, len(result.Issues))
		for _, issue := range result.Issues {
			path := "tasks"
			if issue.TaskID != "" {
				path += "." + issue.TaskID
			}
			if issue.FieldID != "" {
				path += ".fields." + issue.FieldID
			}
			if issue.StatusID != "" {
				path += ".status." + issue.StatusID
			}
			out.Errors = append(out.Errors, projectworkflow.ValidationError{Code: issue.Code, Path: path, Message: issue.Message})
		}
	}
	if previewErr != nil && len(out.Errors) == 0 {
		return WorkflowPreview{}, fmt.Errorf("projectservice: migration preview failed: %w", previewErr)
	}
	var migrationErr projectworkflow.MigrationIssues
	if previewErr != nil && !errors.As(previewErr, &migrationErr) {
		return WorkflowPreview{}, fmt.Errorf("projectservice: migration preview failed: %w", previewErr)
	}
	if previewErr == nil {
		out.PlanDigest = projectworkflow.DigestMigrationPlan(current.Digest, digestConfig.Digest(), uint64(current.Version), expectedDraftRevision, mappings, result, tasks)
	}
	return out, nil
}
