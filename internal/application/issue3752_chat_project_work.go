package application

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// chatProjectWorkDirectory is the chat service's view of the project service
// (issue 3752). It answers one question for a reference: may this person read
// this board or task now, and what is it called. Authorization is the project
// service's own read rule; the chat caller's claims are never an input.
type chatProjectWorkDirectory struct{ service projectservice.Service }

// ProjectWorkVisible implements chat.ProjectWorkDirectory.
func (d chatProjectWorkDirectory) ProjectWorkVisible(ctx context.Context, p chat.Principal, projectID, taskID string) (string, error) {
	trusted, ok := trust.FromContext(ctx)
	if !ok || trusted == nil || trusted.Tenant().String() != p.TenantID || trusted.Subject() != p.SubjectID {
		return "", chat.ErrPermissionDenied
	}
	if d.service.Auth == nil || d.service.Reads == nil || d.service.Workflows == nil {
		return "", chat.ErrUnavailable
	}
	var title string
	var err error
	if taskID == "" {
		var record projectservice.ProjectRecord
		record, err = d.service.GetProject(ctx, trusted, projectID)
		title = record.Name
	} else {
		var record projectservice.TaskRecord
		record, err = d.service.GetTask(ctx, trusted, projectID, taskID)
		title = record.Title
	}
	switch {
	case err == nil:
		if strings.TrimSpace(title) == "" {
			return "", chat.ErrNotFound
		}
		return title, nil
	case errors.Is(err, projectservice.ErrUnavailable):
		return "", chat.ErrUnavailable
	default:
		// A missing item and a refused read look the same to the caller, so a
		// reference never confirms that something exists.
		return "", chat.ErrPermissionDenied
	}
}
