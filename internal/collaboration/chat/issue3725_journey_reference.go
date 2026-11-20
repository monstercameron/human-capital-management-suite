package chat

import (
	"context"
	"errors"
	"regexp"
	"strings"
)

// JourneyReference is a typed pointer to a workflow request (a journey) that
// "Send to chat" put in a conversation (issue 3725). Its ID is the journey's
// intent id. The post stores that id and a snapshot of the workflow's name and
// nothing else: the stage, the requester and every other detail are read live,
// per reader, from the journey service, so the post never carries a worker or a
// pay figure and a stage change shows without rewriting the post.
const JourneyReference ReferenceKind = "JOURNEY"

const restrictedJourneyReference = "Restricted workflow request"

var journeyReferenceIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~-]{0,127}$`)

// JourneyDirectory is the read-only port the journey service satisfies.
// JourneyVisible returns the workflow's name when the principal may open the
// journey, and an error otherwise.
type JourneyDirectory interface {
	JourneyVisible(ctx context.Context, p Principal, intentID string) (name string, err error)
}

// JourneyShareLinker remembers which conversations a journey was shared into,
// so the journey can list where it was discussed. It stores a pointer only;
// whether a reader may open the conversation is decided when the link is read.
type JourneyShareLinker interface {
	LinkJourneyShare(ctx context.Context, tenant, conversation, intentID, sharedBy string) error
}

// SetJourneyDirectory installs the journey authority. Without it a journey
// reference is refused, the fail-closed answer for a composition with no
// journey service.
func (s *Service) SetJourneyDirectory(d JourneyDirectory) { s.journeys = d }

// SetJourneyShareLinker installs the store of journey-to-conversation links.
func (s *Service) SetJourneyShareLinker(l JourneyShareLinker) { s.journeyLinks = l }

// validateJourneyReference authorizes the sender against the journey service at
// commit time and replaces the caller's display text with the authority's name.
func (s *Service) validateJourneyReference(ctx context.Context, p Principal, tenant string, ref *Reference) error {
	if !journeyReferenceIDPattern.MatchString(ref.ID) {
		return ErrInvalidArgument
	}
	if ref.TenantID != tenant || p.TenantID != tenant {
		return ErrPermissionDenied
	}
	if s.journeys == nil {
		return ErrUnavailable
	}
	name, err := s.journeys.JourneyVisible(ctx, p, ref.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrPermissionDenied) {
			return ErrPermissionDenied
		}
		return ErrUnavailable
	}
	// Only the id and the authority's name are kept; any other field the caller
	// filled in is dropped.
	*ref = Reference{Kind: JourneyReference, TenantID: ref.TenantID, ID: ref.ID, Display: strings.TrimSpace(name)}
	return nil
}

// journeyVisibleTo reports whether the reader may open the journey now, and its
// current workflow name.
func (s *Service) journeyVisibleTo(ctx context.Context, p Principal, ref Reference) (string, bool) {
	if !journeyReferenceIDPattern.MatchString(ref.ID) || s.journeys == nil || ref.TenantID != p.TenantID {
		return "", false
	}
	name, err := s.journeys.JourneyVisible(ctx, p, ref.ID)
	return strings.TrimSpace(name), err == nil
}

// linkSentJourneys records, after the post is stored, that each journey it
// points at was shared into this conversation. The post is already committed
// and the link only feeds the journey's "discussed in" list, so a failed write
// does not turn a delivered message into an error; a retried send links again.
func (s *Service) linkSentJourneys(ctx context.Context, sent Post) {
	if s.journeyLinks == nil {
		return
	}
	for _, ref := range sent.References {
		if ref.Kind == JourneyReference && ref.ID != "" {
			_ = s.journeyLinks.LinkJourneyShare(ctx, sent.TenantID, sent.ConversationID, ref.ID, sent.AuthorID)
		}
	}
}
