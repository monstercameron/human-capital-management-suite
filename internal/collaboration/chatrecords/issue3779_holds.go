package chatrecords

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Issue 3779, slice 1: a legal hold on one conversation. Placing, releasing and
// listing holds are governance side effects, so each one asks the Authorizer
// first, and placing and releasing commit the hold change and its audit event
// together. A hold on a conversation covers every record in it, including
// records created after the hold was placed, and blocks disposition of them.

// ErrHoldNotFound is returned when a release names a hold the tenant does not have.
var ErrHoldNotFound = errors.New("chatrecords: hold not found")

const (
	// HoldMaxReason and HoldMaxMatter bound the free text of a hold.
	HoldMaxReason = 500
	HoldMaxMatter = 120

	ActionHoldPlace   = "hold.place"
	ActionHoldRelease = "hold.release"
	ActionHoldList    = "hold.list"
)

// HoldRepository stores a hold or its release together with the audit event.
// The repository assigns the tenant event sequence, event id and digest. A
// hold id placed twice is ErrConflict; releasing an unknown hold is
// ErrHoldNotFound and releasing a released one is ErrConflict.
type HoldRepository interface {
	PutHoldAudited(ctx context.Context, hold Hold, event AuditEvent) (AuditEvent, error)
	ReleaseHoldAudited(ctx context.Context, tenantID, holdID, releasedBy, reason string, at time.Time, event AuditEvent) (Hold, AuditEvent, error)
}

func holdText(v string, max int) (string, bool) {
	v = strings.TrimSpace(v)
	return v, v != "" && utf8.RuneCountInString(v) <= max && !strings.ContainsRune(v, 0)
}

// PlaceConversationHold places an active hold on one conversation.
func (s *Service) PlaceConversationHold(ctx context.Context, actor, tenant, conversation, holdID, matterRef, reason string) (Hold, error) {
	evidence, err := s.authorize(ctx, actor, tenant, ActionHoldPlace, holdID)
	if err != nil {
		return Hold{}, err
	}
	conversation, okConversation := holdText(conversation, 200)
	holdID, okID := holdText(holdID, 200)
	reason, okReason := holdText(reason, HoldMaxReason)
	matterRef = strings.TrimSpace(matterRef)
	if !okConversation || !okID || !okReason || utf8.RuneCountInString(matterRef) > HoldMaxMatter {
		return Hold{}, ErrInvalid
	}
	repo, ok := s.Repo.(HoldRepository)
	if !ok {
		return Hold{}, ErrAuditUnavailable
	}
	at := s.now()
	hold := Hold{TenantID: tenant, HoldID: holdID, MatterRef: matterRef, Reason: reason, PlacedBy: actor, PlacedAt: at, ConversationID: conversation}
	event := AuditEvent{TenantID: tenant, ActorID: actor, Action: ActionHoldPlace, TargetType: "chat_hold", TargetID: holdID, Reason: reason, PolicyEvidence: evidence, At: at}
	if _, err = repo.PutHoldAudited(ctx, hold, event); err != nil {
		return Hold{}, err
	}
	return hold, nil
}

// ReleaseConversationHold lifts a hold. The reason is required and recorded.
func (s *Service) ReleaseConversationHold(ctx context.Context, actor, tenant, holdID, reason string) (Hold, error) {
	evidence, err := s.authorize(ctx, actor, tenant, ActionHoldRelease, holdID)
	if err != nil {
		return Hold{}, err
	}
	holdID, okID := holdText(holdID, 200)
	reason, okReason := holdText(reason, HoldMaxReason)
	if !okID || !okReason {
		return Hold{}, ErrInvalid
	}
	repo, ok := s.Repo.(HoldRepository)
	if !ok {
		return Hold{}, ErrAuditUnavailable
	}
	at := s.now()
	event := AuditEvent{TenantID: tenant, ActorID: actor, Action: ActionHoldRelease, TargetType: "chat_hold", TargetID: holdID, Reason: reason, PolicyEvidence: evidence, At: at}
	hold, _, err := repo.ReleaseHoldAudited(ctx, tenant, holdID, actor, reason, at, event)
	return hold, err
}

// ListHolds returns the tenant's holds, active first and then newest first.
func (s *Service) ListHolds(ctx context.Context, actor, tenant string) ([]Hold, error) {
	if _, err := s.authorize(ctx, actor, tenant, ActionHoldList, tenant); err != nil {
		return nil, err
	}
	holds, err := s.Repo.Holds(ctx, tenant)
	if err != nil {
		return nil, err
	}
	out := make([]Hold, 0, len(holds))
	for _, h := range holds {
		if h.TenantID == "" || h.TenantID == tenant {
			out = append(out, h)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Active() != out[j].Active() {
			return out[i].Active()
		}
		if !out[i].PlacedAt.Equal(out[j].PlacedAt) {
			return out[i].PlacedAt.After(out[j].PlacedAt)
		}
		return out[i].HoldID < out[j].HoldID
	})
	return out, nil
}

// ActiveConversationHolds returns the active holds that cover one conversation.
func (s *Service) ActiveConversationHolds(ctx context.Context, actor, tenant, conversation string) ([]Hold, error) {
	holds, err := s.ListHolds(ctx, actor, tenant)
	if err != nil {
		return nil, err
	}
	out := []Hold{}
	for _, h := range holds {
		if h.Active() && h.ConversationID != "" && h.ConversationID == conversation {
			out = append(out, h)
		}
	}
	return out, nil
}

// activeConversationHoldIDs maps a conversation to the ids of its active holds.
func activeConversationHoldIDs(holds []Hold) map[string][]string {
	out := map[string][]string{}
	for _, h := range holds {
		if h.Active() && h.ConversationID != "" {
			out[h.ConversationID] = append(out[h.ConversationID], h.HoldID)
		}
	}
	return out
}
