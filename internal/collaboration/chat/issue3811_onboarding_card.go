package chat

import (
	"context"
	"strings"
)

// OnboardingCardMarker opens the body of a new-hire welcome card (#3811). The
// body names only who the card is for and which employment it belongs to; it
// never holds a task, a status or a document. The checklist is read from the
// onboarding owner each time the card is drawn, so Chat is never the record of
// what is done. The marker starts with an invisible separator (U+2063).
const OnboardingCardMarker = "⁣onboarding-card:"

// OnboardingCardWelcome is the one card kind of the first slice.
const OnboardingCardWelcome = "welcome"

// OnboardingCard is what the body of a welcome card says. Subject is the new
// hire's chat identifier, Worker the owner's worker identifier and Employment
// the employment instance the welcome is for, so a rehire or a second
// concurrent employment is a different card.
type OnboardingCard struct {
	Subject, Worker, Employment string
}

func (c OnboardingCard) valid() bool {
	for _, part := range []string{c.Subject, c.Worker, c.Employment} {
		if strings.TrimSpace(part) == "" || part != strings.TrimSpace(part) || len(part) > 128 || strings.ContainsAny(part, "|\r\n\x00") || strings.Contains(part, OnboardingCardMarker) {
			return false
		}
	}
	return true
}

// OnboardingCardBody is the post body that records c.
func OnboardingCardBody(c OnboardingCard) (string, error) {
	if !c.valid() {
		return "", ErrInvalidArgument
	}
	return OnboardingCardMarker + strings.Join([]string{OnboardingCardWelcome, c.Subject, c.Worker, c.Employment}, "|"), nil
}

// ParseOnboardingCard reports whether body is a welcome card, and what it says.
func ParseOnboardingCard(body string) (OnboardingCard, bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(body), OnboardingCardMarker)
	if !found {
		return OnboardingCard{}, false
	}
	parts := strings.Split(rest, "|")
	if len(parts) != 4 || parts[0] != OnboardingCardWelcome {
		return OnboardingCard{}, false
	}
	c := OnboardingCard{Subject: parts[1], Worker: parts[2], Employment: parts[3]}
	return c, c.valid()
}

func carriesOnboardingCardMarker(body string) bool {
	return strings.Contains(body, OnboardingCardMarker)
}

type onboardingCardAuthorityKey struct{}

func hasOnboardingCardAuthority(ctx context.Context) bool {
	granted, _ := ctx.Value(onboardingCardAuthorityKey{}).(bool)
	return granted
}

// OnboardingCardPost is one welcome card for one new hire. Sender is the system
// identity the deployment posts as; IdempotencyKey names the onboarding
// instance, so a replayed event is the same post.
type OnboardingCardPost struct {
	TenantID       string
	Sender         Principal
	Card           OnboardingCard
	IdempotencyKey string
}

// PostOnboardingCard writes the card into the private direct conversation
// between the system sender and the new hire, opening it the first time. The
// conversation has exactly those two members: nobody else is added, whatever
// their relationship to the hire. The caller is the adapter that already
// decided the hire may be told; this method owns the shape.
func (s *Service) PostOnboardingCard(ctx context.Context, r OnboardingCardPost) (Post, error) {
	if err := validatePrincipal(r.Sender, r.TenantID); err != nil {
		return Post{}, err
	}
	key := strings.TrimSpace(r.IdempotencyKey)
	if r.Sender.TenantID != r.TenantID || r.Card.Subject == r.Sender.SubjectID || key == "" || len(key) > 160 {
		return Post{}, ErrInvalidArgument
	}
	body, err := OnboardingCardBody(r.Card)
	if err != nil {
		return Post{}, err
	}
	dm, err := s.CreateConversation(ctx, CreateConversationRequest{Principal: r.Sender, TenantID: r.TenantID, Kind: Direct,
		Members: []MemberRef{{TenantID: r.TenantID, SubjectID: r.Card.Subject}}, IdempotencyKey: "onboarding-card-dm:" + r.Sender.SubjectID + ":" + r.Card.Subject})
	if err != nil {
		return Post{}, err
	}
	return s.sendPost(context.WithValue(ctx, onboardingCardAuthorityKey{}, true),
		SendPostRequest{Principal: r.Sender, TenantID: r.TenantID, ConversationID: dm.ID, Body: body, IdempotencyKey: "onboarding-card:" + key}, false)
}
