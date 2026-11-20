package chat

import (
	"context"
	"errors"
	"strings"
)

// ProjectWorkReference is a typed pointer to a project board or one task on
// it, attached to a post by "Send to chat" (issue 3752). Its ID is
// ProjectWorkReferenceID: the project, then "/" and the task for a task.
// The post stores identifiers only; Display is a snapshot of the title the
// project authority returned to the sender, and every reader is re-checked.
const ProjectWorkReference ReferenceKind = "PROJECT_TASK"

const restrictedProjectWorkReference = "Restricted project item"

// ProjectWorkDirectory is the read-only port the project service satisfies.
// ProjectWorkVisible returns the title of the board or task when the principal
// may read it, and an error otherwise. An empty taskID asks about the board.
type ProjectWorkDirectory interface {
	ProjectWorkVisible(ctx context.Context, p Principal, projectID, taskID string) (title string, err error)
}

// SetProjectWorkDirectory installs the project authority. Without it a project
// reference is refused, the fail-closed answer for a composition with no
// project service.
func (s *Service) SetProjectWorkDirectory(d ProjectWorkDirectory) { s.projectWork = d }

// ProjectWorkReferenceID builds the reference id for a board (empty taskID) or task.
func ProjectWorkReferenceID(projectID, taskID string) string {
	if taskID == "" {
		return projectID
	}
	return projectID + "/" + taskID
}

// ParseProjectWorkReferenceID splits a reference id; ok is false when it is not
// one project id or a project id and a task id.
func ParseProjectWorkReferenceID(id string) (projectID, taskID string, ok bool) {
	parts := strings.Split(id, "/")
	if len(parts) > 2 {
		return "", "", false
	}
	projectID = parts[0]
	if len(parts) == 2 {
		taskID = parts[1]
	}
	if strings.TrimSpace(projectID) == "" || (len(parts) == 2 && strings.TrimSpace(taskID) == "") {
		return "", "", false
	}
	return projectID, taskID, true
}

// validateProjectWorkReference authorizes the sender against the project
// service at commit time and replaces the caller's display text with the
// authority's title.
func (s *Service) validateProjectWorkReference(ctx context.Context, p Principal, tenant string, ref *Reference) error {
	projectID, taskID, ok := ParseProjectWorkReferenceID(ref.ID)
	if !ok {
		return ErrInvalidArgument
	}
	if ref.TenantID != tenant || p.TenantID != tenant {
		return ErrPermissionDenied
	}
	if s.projectWork == nil {
		return ErrUnavailable
	}
	title, err := s.projectWork.ProjectWorkVisible(ctx, p, projectID, taskID)
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrPermissionDenied) {
			return ErrPermissionDenied
		}
		return ErrUnavailable
	}
	// Only the id and the authority's title are kept; any other field the
	// caller filled in (media facts, a conversation id) is dropped.
	*ref = Reference{Kind: ProjectWorkReference, TenantID: ref.TenantID, ID: ref.ID, Display: strings.TrimSpace(title)}
	return nil
}

// projectWorkVisibleTo reports whether the reader may open the referenced item
// now, and the item's current title, so a rename shows without rewriting the
// stored post.
func (s *Service) projectWorkVisibleTo(ctx context.Context, p Principal, ref Reference) (string, bool) {
	projectID, taskID, ok := ParseProjectWorkReferenceID(ref.ID)
	if !ok || s.projectWork == nil || ref.TenantID != p.TenantID {
		return "", false
	}
	title, err := s.projectWork.ProjectWorkVisible(ctx, p, projectID, taskID)
	return strings.TrimSpace(title), err == nil
}
