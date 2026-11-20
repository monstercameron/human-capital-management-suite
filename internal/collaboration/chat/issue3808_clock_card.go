package chat

import (
	"context"
	"strings"
	"time"
)

// ClockCardMarker opens the body of a time-clock system card (#3808): a
// missing punch the worker should fix, or that a supervisor should know about.
// Like the membership line, the body is the marker and a few identifiers; the
// page writes the sentence in the reader's language from names it already
// holds. The marker starts with an invisible separator (U+2063), so a client
// that does not know it shows an unremarkable line rather than markup.
const ClockCardMarker = "⁣clock-card:"

// ClockCardMissingPunch is the one card kind of the first slice.
const ClockCardMissingPunch = "missing-punch"

// The two audiences a card is written for. A worker's card says "you"; a
// supervisor's names the worker and carries no action of its own.
const (
	ClockCardSelf   = "self"
	ClockCardReview = "review"
)

// ClockCard is what the body of a time-clock card says. Subject is the
// worker's chat identifier, Day the calendar day the exception belongs to
// (YYYY-MM-DD) and Ref the clock session the card stands for.
type ClockCard struct {
	Kind, Audience, Subject, Day, Ref string
}

func (c ClockCard) valid() bool {
	if c.Kind != ClockCardMissingPunch || (c.Audience != ClockCardSelf && c.Audience != ClockCardReview) {
		return false
	}
	for _, part := range []string{c.Subject, c.Ref} {
		if strings.TrimSpace(part) == "" || part != strings.TrimSpace(part) || len(part) > 128 || strings.ContainsAny(part, "|\r\n\x00") || strings.Contains(part, ClockCardMarker) {
			return false
		}
	}
	_, err := time.Parse("2006-01-02", c.Day)
	return err == nil
}

// ClockCardBody is the post body that records c.
func ClockCardBody(c ClockCard) (string, error) {
	if !c.valid() {
		return "", ErrInvalidArgument
	}
	return ClockCardMarker + strings.Join([]string{c.Kind, c.Audience, c.Subject, c.Day, c.Ref}, "|"), nil
}

// ParseClockCard reports whether body is a time-clock card, and what it says.
func ParseClockCard(body string) (ClockCard, bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(body), ClockCardMarker)
	if !found {
		return ClockCard{}, false
	}
	parts := strings.Split(rest, "|")
	if len(parts) != 5 {
		return ClockCard{}, false
	}
	c := ClockCard{Kind: parts[0], Audience: parts[1], Subject: parts[2], Day: parts[3], Ref: parts[4]}
	return c, c.valid()
}

// carriesClockCardMarker reports whether a body holds the marker anywhere, so a
// hand-typed or edited-in card is refused whatever surrounds it.
func carriesClockCardMarker(body string) bool {
	return strings.Contains(body, ClockCardMarker)
}

type clockCardAuthorityKey struct{}

func hasClockCardAuthority(ctx context.Context) bool {
	granted, _ := ctx.Value(clockCardAuthorityKey{}).(bool)
	return granted
}

// ClockCardPost is one card for one recipient. Sender is the system identity
// the deployment posts as; IdempotencyKey makes a replayed event the same post.
type ClockCardPost struct {
	TenantID       string
	Sender         Principal
	Recipient      string
	Card           ClockCard
	IdempotencyKey string
}

// PostClockCard writes the card into the direct conversation between the system
// sender and the recipient, opening it the first time. The caller is the
// adapter that already decided who may be told; this method owns only the
// shape: same tenant, a distinct recipient, a valid card, and a body no person
// could have typed.
func (s *Service) PostClockCard(ctx context.Context, r ClockCardPost) (Post, error) {
	if err := validatePrincipal(r.Sender, r.TenantID); err != nil {
		return Post{}, err
	}
	recipient := strings.TrimSpace(r.Recipient)
	key := strings.TrimSpace(r.IdempotencyKey)
	if r.Sender.TenantID != r.TenantID || recipient == "" || recipient == r.Sender.SubjectID || key == "" || len(key) > 160 {
		return Post{}, ErrInvalidArgument
	}
	body, err := ClockCardBody(r.Card)
	if err != nil {
		return Post{}, err
	}
	dm, err := s.CreateConversation(ctx, CreateConversationRequest{Principal: r.Sender, TenantID: r.TenantID, Kind: Direct,
		Members: []MemberRef{{TenantID: r.TenantID, SubjectID: recipient}}, IdempotencyKey: "clock-card-dm:" + r.Sender.SubjectID + ":" + recipient})
	if err != nil {
		return Post{}, err
	}
	return s.sendPost(context.WithValue(ctx, clockCardAuthorityKey{}, true),
		SendPostRequest{Principal: r.Sender, TenantID: r.TenantID, ConversationID: dm.ID, Body: body, IdempotencyKey: "clock-card:" + key}, false)
}

// IsSystemBody reports whether a post body is a system line or card (a person
// added, a time-clock card) rather than something a person wrote. Search, the
// context handed to agents and the translation and rewrite queues leave these
// out; only the recipient's own timeline shows them.
func IsSystemBody(body string) bool {
	if _, ok := ParseMembershipAdded(body); ok {
		return true
	}
	if _, ok := ParseFormCard(body); ok {
		return true
	}
	if _, ok := ParseJourneyStage(body); ok {
		return true
	}
	if _, ok := ParseOnboardingCard(body); ok {
		return true
	}
	_, ok := ParseClockCard(body)
	return ok
}
