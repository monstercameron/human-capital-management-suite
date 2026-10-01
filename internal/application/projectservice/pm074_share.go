package projectservice

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	ErrTaskShareUnavailable        = errors.New("projectservice: task share port unavailable")
	ErrInvalidTaskShareDestination = errors.New("projectservice: invalid task share destination")
)

type TaskShareKind string

const (
	TaskShareChatPost          TaskShareKind = "CHAT_POST"
	TaskShareDocumentCandidate TaskShareKind = "DOCUMENT_CANDIDATE"
)

// TaskShareDestination identifies the owning surface that will perform its
// own current write/read check. Project only supplies the stable task link.
type TaskShareDestination struct {
	Kind                      TaskShareKind
	ConversationID            string
	DocumentID                string
	CandidateID               string
	ExpectedCandidateRevision uint64
}

func (d TaskShareDestination) Validate() error {
	switch d.Kind {
	case TaskShareChatPost:
		if !safeShareID(d.ConversationID) || d.DocumentID != "" || d.CandidateID != "" || d.ExpectedCandidateRevision != 0 {
			return ErrInvalidTaskShareDestination
		}
	case TaskShareDocumentCandidate:
		if !safeShareID(d.DocumentID) || !safeShareID(d.CandidateID) || d.ConversationID != "" || d.ExpectedCandidateRevision == 0 {
			return ErrInvalidTaskShareDestination
		}
	default:
		return ErrInvalidTaskShareDestination
	}
	return nil
}

// TaskSharePort is implemented by the Chat and Docs composition adapters.
// The adapter owns destination authorization and candidate/post mutation;
// this package never writes either product's records.
type TaskSharePort interface {
	ShareTaskLink(context.Context, string, string, TaskShareDestination, string, string) error
}

type ShareTaskLinkRequest struct {
	ProjectID, TaskID, IdempotencyKey string
	ExpectedTaskRevision              uint64
	Destination                       TaskShareDestination
}

func (s Service) ShareTaskLink(ctx context.Context, principal *trust.Principal, req ShareTaskLinkRequest) error {
	if err := validPrincipal(principal); err != nil {
		return err
	}
	if s.Auth == nil || s.Reads == nil {
		return ErrUnavailable
	}
	port, ok := s.Links.(TaskSharePort)
	if !ok {
		return ErrTaskShareUnavailable
	}
	if !safeShareID(req.ProjectID) || !safeShareID(req.TaskID) || strings.TrimSpace(req.IdempotencyKey) == "" || req.ExpectedTaskRevision == 0 || req.Destination.Validate() != nil {
		return ErrInvalidRequest
	}
	if err := s.Auth.Authorize(ctx, principal, req.ProjectID, projectaccess.EditTask); err != nil {
		return err
	}
	task, err := s.Reads.GetTask(ctx, tenant(principal), req.ProjectID, req.TaskID)
	if err != nil {
		return err
	}
	if task.TenantID != tenant(principal) || task.ProjectID != req.ProjectID {
		return projectaccess.ErrTenantMismatch
	}
	if task.Revision != req.ExpectedTaskRevision {
		return project.ErrRevisionConflict
	}
	if err := s.Auth.Authorize(ctx, principal, req.ProjectID, projectaccess.EditTask); err != nil {
		return err
	}
	return port.ShareTaskLink(ctx, tenant(principal), principal.Subject(), req.Destination, taskLinkHref(req.ProjectID, req.TaskID), req.IdempotencyKey)
}

func taskLinkHref(projectID, taskID string) string {
	return "/workspace/app/project?" + url.Values{"project": []string{projectID}, "task": []string{taskID}}.Encode()
}

func safeShareID(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' || r == ':') {
			return false
		}
	}
	return true
}
