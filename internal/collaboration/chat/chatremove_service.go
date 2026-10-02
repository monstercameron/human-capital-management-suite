package chat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

const RemovedByAdministrator = "Removed by an administrator"

type RemovalSelection struct {
	ConversationID               string
	PostIDs                      []string
	AuthorID, AuthorHomeTenantID string
	From, Until                  time.Time
}

type RemovalRequest struct {
	Principal                Principal `json:"-"`
	TenantID                 string    `json:"-"`
	Selection                RemovalSelection
	ReasonCode, Note, Action string
	Confirmation             string
	ConfirmedCount           int
}

type RemovalCandidate struct {
	ID       string
	Revision uint64
}

type RemovalPreview struct {
	Count        int
	Confirmation string
}

type ModerationItem struct {
	ID, Kind, ConversationID, PostID, AuthorID, ReporterID, Reason, State string
	Message                                                               Post
	Context                                                               []Post
	At                                                                    time.Time
	CanRemove, CanRestore                                                 bool
	// Rule is the filter rule that matched, for an item that came from a
	// filter hit flagged for review ("" for reports, appeals and removals).
	Rule string
	// ConversationName is the name of the conversation the message is in.
	ConversationName string
	// Decision, DecidedBy, DecisionReason and DecidedAt say how a resolved item
	// was closed: the action, who took it, the reason they gave, and when.
	Decision, DecidedBy, DecisionReason string
	DecidedAt                           time.Time
}

type ModerationNotice struct {
	TenantID                                    string
	ID, ConversationID, PostID, Reason, Outcome string
	CanAppeal                                   bool
	At                                          time.Time
}

// ModerationStore rechecks current permissions inside each mutation transaction.
// CommitRemoval compares the whole preview and persists the private notice with
// the action. Neither a stale preview nor a partial bulk removal may commit.
type ModerationStore interface {
	PreviewRemoval(context.Context, RemovalRequest) ([]RemovalCandidate, error)
	CommitRemoval(context.Context, RemovalRequest, time.Time) (int, error)
	ReviewRemoved(context.Context, Principal, string, string, string, string, time.Time) (Post, error)
	SearchModeration(context.Context, Principal, string, string) ([]ModerationItem, error)
	ModerationNotices(context.Context, Principal, string) ([]ModerationNotice, error)
	AppealRemoval(context.Context, Principal, string, string, string, time.Time) error
	ResolveModeration(context.Context, Principal, string, string, string, string, time.Time) error
}

// FilterModerationPort avoids a dependency on the filter implementation. It
// must filter by current access before returning hits, including their counts.
type FilterModerationPort interface {
	SearchModeration(context.Context, Principal, string, string) ([]ModerationItem, error)
	ResolveModeration(context.Context, Principal, string, string, string, string, time.Time) error
}

type ModerationService struct {
	Store   ModerationStore
	Filters FilterModerationPort
	Clock   Clock
}

func (s *ModerationService) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func ValidRemovalReason(reason string) bool {
	switch reason {
	case "harassment", "sensitive_information", "spam", "policy_violation":
		return true
	}
	return false
}

func ValidateRemoval(r RemovalRequest) error {
	if err := validatePrincipal(r.Principal, r.TenantID); err != nil {
		return err
	}
	if r.Principal.TenantID != r.TenantID {
		return ErrPermissionDenied
	}
	// A removal needs a reason from the short list; a restore needs none.
	if strings.TrimSpace(r.Selection.ConversationID) == "" || (r.Action == "remove" && !ValidRemovalReason(r.ReasonCode)) || (r.Action == "restore" && r.ReasonCode != "" && !ValidRemovalReason(r.ReasonCode)) || len(r.Note) > 2000 || (r.Action != "remove" && r.Action != "restore") {
		return ErrInvalidArgument
	}
	if len(r.Selection.PostIDs) > 200 {
		return ErrInvalidArgument
	}
	if len(r.Selection.PostIDs) == 0 {
		if r.Selection.AuthorID == "" || r.Selection.AuthorHomeTenantID == "" || r.Selection.From.IsZero() || !r.Selection.Until.After(r.Selection.From) {
			return ErrInvalidArgument
		}
	} else {
		if r.Selection.AuthorID != "" || !r.Selection.From.IsZero() || !r.Selection.Until.IsZero() {
			return ErrInvalidArgument
		}
		seen := map[string]bool{}
		for _, id := range r.Selection.PostIDs {
			if strings.TrimSpace(id) == "" || seen[id] {
				return ErrInvalidArgument
			}
			seen[id] = true
		}
	}
	return nil
}

func RemovalConfirmation(r RemovalRequest, candidates []RemovalCandidate) string {
	// Bind the preview to the actor, selection, reason and current revisions.
	value := struct {
		Tenant, Actor, Home  string
		Selection            RemovalSelection
		Reason, Note, Action string
		Candidates           []RemovalCandidate
	}{r.TenantID, r.Principal.SubjectID, r.Principal.TenantID, r.Selection, r.ReasonCode, r.Note, r.Action, candidates}
	b, _ := json.Marshal(value)
	digest := sha256.Sum256(b)
	return hex.EncodeToString(digest[:])
}

func (s *ModerationService) Preview(ctx context.Context, r RemovalRequest) (RemovalPreview, error) {
	if err := ValidateRemoval(r); err != nil {
		return RemovalPreview{}, err
	}
	if s.Store == nil {
		return RemovalPreview{}, ErrUnavailable
	}
	rows, err := s.Store.PreviewRemoval(ctx, r)
	if err != nil {
		return RemovalPreview{}, err
	}
	return RemovalPreview{Count: len(rows), Confirmation: RemovalConfirmation(r, rows)}, nil
}

func (s *ModerationService) Apply(ctx context.Context, r RemovalRequest) (int, error) {
	if err := ValidateRemoval(r); err != nil {
		return 0, err
	}
	if r.Confirmation == "" || r.ConfirmedCount <= 0 || r.ConfirmedCount > 200 {
		return 0, ErrInvalidArgument
	}
	if s.Store == nil {
		return 0, ErrUnavailable
	}
	return s.Store.CommitRemoval(ctx, r, s.now())
}

func (s *ModerationService) Queue(ctx context.Context, p Principal, tenant, query string) ([]ModerationItem, error) {
	if err := validatePrincipal(p, tenant); err != nil {
		return nil, err
	}
	if p.TenantID != tenant {
		return nil, ErrPermissionDenied
	}
	if s.Store == nil {
		return nil, ErrUnavailable
	}
	rows, err := s.Store.SearchModeration(ctx, p, tenant, query)
	if err != nil {
		return nil, err
	}
	if s.Filters != nil {
		hits, e := s.Filters.SearchModeration(ctx, p, tenant, query)
		if e != nil {
			return nil, e
		}
		rows = append(rows, hits...)
	}
	return rows, nil
}

func (s *ModerationService) Resolve(ctx context.Context, p Principal, tenant, id, action, reason string) error {
	if err := validatePrincipal(p, tenant); err != nil {
		return err
	}
	if p.TenantID != tenant {
		return ErrPermissionDenied
	}
	if strings.TrimSpace(reason) == "" || len(reason) > 2000 {
		return ErrInvalidArgument
	}
	switch action {
	case "remove", "dismiss", "restore", "message_author":
	default:
		return ErrInvalidArgument
	}
	if s.Store == nil {
		return ErrUnavailable
	}
	if strings.HasPrefix(id, "filter:") {
		if s.Filters == nil {
			return ErrUnavailable
		}
		return s.Filters.ResolveModeration(ctx, p, tenant, id, action, reason, s.now())
	}
	return s.Store.ResolveModeration(ctx, p, tenant, id, action, reason, s.now())
}
