package agentrun

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
)

var (
	ErrDocumentReferenceInvalid    = fmt.Errorf("%w: document references are invalid", ErrInvalid)
	ErrDocumentReferenceUnreadable = fmt.Errorf("%w: a document reference is not readable", ErrInvalid)
	ErrDocumentResolverUnavailable = errors.New("documents are not available in this workspace")
)

type taskDocumentReferencesContextKey struct{}

// WithDocumentReferences binds admission-validated request references to the
// task creation call. The runtime copies them into the digest-bound plan.
func WithDocumentReferences(ctx context.Context, refs []agentdocref.Reference) (context.Context, error) {
	if ctx == nil || agentdocref.Validate(refs, agentdocref.MaxRequestReferences) != nil {
		return nil, ErrDocumentReferenceInvalid
	}
	return context.WithValue(ctx, taskDocumentReferencesContextKey{}, cloneDocumentReferences(refs)), nil
}

func documentReferencesFromContext(ctx context.Context) []agentdocref.Reference {
	if ctx == nil {
		return nil
	}
	refs, _ := ctx.Value(taskDocumentReferencesContextKey{}).([]agentdocref.Reference)
	return cloneDocumentReferences(refs)
}

func cloneDocumentReferences(in []agentdocref.Reference) []agentdocref.Reference {
	return slices.Clone(in)
}

func cloneDocumentOmissions(in []agentdocref.Omission) []agentdocref.Omission {
	return slices.Clone(in)
}

// ValidateTaskDocumentOmissions accepts only content-free resolver reasons
// associated with one of the task's admitted labels (or a generic omission).
func ValidateTaskDocumentOmissions(refs []agentdocref.Reference, omissions []agentdocref.Omission) error {
	labels := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		labels[ref.Label] = struct{}{}
	}
	seen := make(map[string]struct{}, len(omissions))
	for _, omission := range omissions {
		if omission.Reason != agentdocref.NotReadable && omission.Reason != agentdocref.NotFound && omission.Reason != agentdocref.NotPublished && omission.Reason != agentdocref.OverBudget {
			return ErrDocumentReferenceInvalid
		}
		label := strings.TrimSpace(omission.Label)
		if label != "" {
			if _, ok := labels[label]; !ok {
				return ErrDocumentReferenceInvalid
			}
			key := label + "\x00" + omission.Reason
			if _, ok := seen[key]; ok {
				return ErrDocumentReferenceInvalid
			}
			seen[key] = struct{}{}
		}
	}
	return nil
}

type taskDocumentOmissionStore interface {
	RecordDocumentOmissions(context.Context, string, []agentdocref.Reference, []agentdocref.Omission) error
}

// RecordDocumentOmissions records the resolver's current content-free
// omissions without changing the task state-machine revision.
func (r *Runtime) RecordDocumentOmissions(ctx context.Context, id string, refs []agentdocref.Reference, omissions []agentdocref.Omission) error {
	if r == nil || strings.TrimSpace(id) == "" || agentdocref.Validate(refs, agentdocref.MaxRequestReferences) != nil || ValidateTaskDocumentOmissions(refs, omissions) != nil {
		return ErrDocumentReferenceInvalid
	}
	store, ok := r.store.(taskDocumentOmissionStore)
	if !ok {
		return ErrDocumentReferenceInvalid
	}
	return store.RecordDocumentOmissions(ctx, id, cloneDocumentReferences(refs), cloneDocumentOmissions(omissions))
}

func (s *MemoryStore) RecordDocumentOmissions(_ context.Context, id string, refs []agentdocref.Reference, omissions []agentdocref.Omission) error {
	if s == nil || agentdocref.Validate(refs, agentdocref.MaxRequestReferences) != nil || ValidateTaskDocumentOmissions(refs, omissions) != nil {
		return ErrDocumentReferenceInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if !slices.Equal(task.Plan.DocumentReferences, refs) {
		return ErrDocumentReferenceInvalid
	}
	task.Plan.DocumentOmissions = cloneDocumentOmissions(omissions)
	s.tasks[id] = cloneTask(task)
	return nil
}
