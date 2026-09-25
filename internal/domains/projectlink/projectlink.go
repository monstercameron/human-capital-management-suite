// Package projectlink resolves the small, explicit set of references that
// project boards may display. A reference never carries target permissions.
package projectlink

import (
	"context"
	"errors"
	"regexp"
	"strings"
)

type Kind string

const (
	ChatConversation Kind = "CHAT_CONVERSATION"
	ChatPost         Kind = "CHAT_POST"
	DeployedDocument Kind = "DEPLOYED_DOCUMENT"
	WorkItem         Kind = "WORK_ITEM"
	// WorkOrder is a governed field-work record. Project links expose only its
	// identity after the owning work-order authority confirms access.
	WorkOrder Kind = "WORK_ORDER"
	// Journey is an in-progress workflow run (for example a promotion). The
	// project side stores and returns only its ID: the viewer reads the
	// journey itself through the journey service, whose own authorization
	// decides what is shown, so a project link never discloses a journey.
	Journey Kind = "JOURNEY"
)

var (
	ErrInvalidReference    = errors.New("invalid project link reference")
	ErrResolverUnavailable = errors.New("project link resolver unavailable")
	idPattern              = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

// Reference identifies one target in the first-release allowlist. Version is
// required for deployed documents so a link cannot silently follow a draft or
// a later deployment. ConversationID is required for chat posts.
type Reference struct {
	Kind           Kind
	ID             string
	ConversationID string
	Version        string
	ScopeID        string
}

func (r Reference) Validate() error {
	if !validID(r.ID) {
		return ErrInvalidReference
	}
	switch r.Kind {
	case ChatConversation:
		if r.ConversationID != "" || r.Version != "" || r.ScopeID != "" {
			return ErrInvalidReference
		}
	case ChatPost:
		if !validID(r.ConversationID) || r.Version != "" || r.ScopeID != "" {
			return ErrInvalidReference
		}
	case DeployedDocument:
		if !validID(r.Version) || !validID(r.ScopeID) || r.ConversationID != "" {
			return ErrInvalidReference
		}
	case WorkItem, WorkOrder, Journey:
		if r.ConversationID != "" || r.Version != "" || r.ScopeID != "" {
			return ErrInvalidReference
		}
	default:
		return ErrInvalidReference
	}
	return nil
}

func validID(s string) bool { return s == strings.TrimSpace(s) && idPattern.MatchString(s) }

type State string

const (
	Available  State = "AVAILABLE"
	Restricted State = "RESTRICTED"
)

// Preview contains only target data that the owning domain allows the current
// viewer to see. WorkItem fields are deliberately limited to its safe board
// projection.
type Preview struct {
	Kind           Kind
	ID             string
	Title          string
	Snippet        string
	Version        string
	ScopeID        string
	Status         string
	Freshness      string
	ConversationID string
}

type Result struct {
	State   State
	Preview *Preview
}

// Authorizer checks current target authorization. Resolve calls it before any
// target lookup and again before returning data, so access loss during lookup
// discards the result.
type Authorizer interface {
	Authorize(context.Context, string, Reference) (bool, error)
}

// TargetReader resolves already-authorized targets. Implementations must
// return only the fields approved for the corresponding reference kind.
type TargetReader interface {
	ReadAuthorized(context.Context, Reference) (Preview, bool, error)
}

type Resolver struct {
	Authorization Authorizer
	Targets       TargetReader
}

// Resolve returns the same empty restricted state for denial and absence,
// preventing titles and existence from being disclosed through project links.
func (r Resolver) Resolve(ctx context.Context, principal string, ref Reference) (Result, error) {
	if ref.Validate() != nil || !validID(principal) {
		return Result{}, ErrInvalidReference
	}
	if r.Authorization == nil || r.Targets == nil {
		return Result{}, ErrResolverUnavailable
	}
	allowed, err := r.Authorization.Authorize(ctx, principal, ref)
	if err != nil {
		return Result{}, err
	}
	if !allowed {
		return restricted(), nil
	}

	preview, found, err := r.Targets.ReadAuthorized(ctx, ref)
	if err != nil {
		return Result{}, err
	}
	if !found {
		return restricted(), nil
	}
	if err := validatePreview(ref, preview); err != nil {
		return Result{}, err
	}

	allowed, err = r.Authorization.Authorize(ctx, principal, ref)
	if err != nil {
		return Result{}, err
	}
	if !allowed {
		return restricted(), nil
	}
	return Result{State: Available, Preview: &preview}, nil
}

func restricted() Result { return Result{State: Restricted} }

func validatePreview(ref Reference, p Preview) error {
	if p.Kind != ref.Kind || p.ID != ref.ID {
		return ErrResolverUnavailable
	}
	switch ref.Kind {
	case ChatConversation:
		if p.ConversationID != "" || p.Version != "" || p.ScopeID != "" || p.Status != "" || p.Freshness != "" {
			return ErrResolverUnavailable
		}
	case ChatPost:
		if p.ConversationID != ref.ConversationID || p.Version != "" || p.ScopeID != "" || p.Status != "" || p.Freshness != "" {
			return ErrResolverUnavailable
		}
	case DeployedDocument:
		if p.Version != ref.Version || p.ScopeID != ref.ScopeID || p.ConversationID != "" || p.Status != "" || p.Freshness != "" {
			return ErrResolverUnavailable
		}
	case WorkItem:
		if p.Version == "" || p.Status == "" || p.Freshness == "" || p.Title != "" || p.Snippet != "" || p.ConversationID != "" || p.ScopeID != "" {
			return ErrResolverUnavailable
		}
	case Journey:
		// Identity only: every display field comes from the viewer's own
		// journey read, never from the project service.
		if p.Title != "" || p.Snippet != "" || p.Version != "" || p.Status != "" || p.Freshness != "" || p.ConversationID != "" || p.ScopeID != "" {
			return ErrResolverUnavailable
		}
	case WorkOrder:
		// Identity only. Work order scope, crew, evidence, financial data, and
		// phase details remain in the owning domain's authorized read surface.
		if p.Title != "" || p.Snippet != "" || p.Version != "" || p.Status != "" || p.Freshness != "" || p.ConversationID != "" || p.ScopeID != "" {
			return ErrResolverUnavailable
		}
	default:
		return ErrInvalidReference
	}
	return nil
}
