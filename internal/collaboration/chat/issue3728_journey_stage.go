package chat

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

// JourneyStageMarker opens the body of the system line posted into a
// conversation when a workflow request that was shared there is decided
// (issue 3728). Like the membership line, the body is the marker and a few
// identifiers: the outcome and the person who decided. The page writes the
// sentence in the reader's language from names it already holds. The line holds
// no reason, no pay figure and no worker: the typed journey reference on the
// post names the request, and every reader's own access to it decides what that
// reader sees. The marker starts with an invisible separator (U+2063), so a
// client that does not know it shows an unremarkable line rather than markup.
const JourneyStageMarker = "⁣journey-stage:"

// The outcomes a stage line can announce.
const (
	JourneyStageApproved = "approved"
	JourneyStageRejected = "rejected"
	JourneyStageBlocked  = "blocked"
)

// JourneyStageLine is what the body of a stage line says. Decider is the chat
// identifier of the person whose decision moved the request.
type JourneyStageLine struct{ Outcome, Decider string }

func (l JourneyStageLine) valid() bool {
	switch l.Outcome {
	case JourneyStageApproved, JourneyStageRejected, JourneyStageBlocked:
	default:
		return false
	}
	d := l.Decider
	return strings.TrimSpace(d) != "" && d == strings.TrimSpace(d) && len(d) <= 128 && !strings.ContainsAny(d, "|\r\n\x00") && !strings.Contains(d, JourneyStageMarker)
}

// JourneyStageBody is the post body that records l.
func JourneyStageBody(l JourneyStageLine) (string, error) {
	if !l.valid() {
		return "", ErrInvalidArgument
	}
	return JourneyStageMarker + l.Outcome + "|" + l.Decider, nil
}

// ParseJourneyStage reports whether body is a stage line, and what it says.
func ParseJourneyStage(body string) (JourneyStageLine, bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(body), JourneyStageMarker)
	if !found {
		return JourneyStageLine{}, false
	}
	outcome, decider, ok := strings.Cut(rest, "|")
	l := JourneyStageLine{Outcome: outcome, Decider: decider}
	return l, ok && l.valid()
}

// carriesJourneyStageMarker reports whether a body holds the marker anywhere,
// so a hand-typed or edited-in line is refused whatever surrounds it.
func carriesJourneyStageMarker(body string) bool {
	return strings.Contains(body, JourneyStageMarker)
}

// SystemLineWriter is the optional store capability that writes a system line
// attributed to a person who is not necessarily a member of the conversation:
// a workflow approver decides on the journey page, not in the room. Only the
// service calls it, with a body no person could have typed.
type SystemLineWriter interface {
	SendSystemLine(ctx context.Context, r SendPostRequest, p Post) (Post, error)
}

// JourneyStagePost is one stage line for one conversation. Decider is the
// person who decided; the context must carry that person's trusted identity,
// because the journey authority is asked whether they may open the request.
type JourneyStagePost struct {
	TenantID, ConversationID, JourneyID, Outcome, IdempotencyKey string
	Decider                                                      Principal
}

// PostJourneyStage writes the line into the conversation the journey was shared
// into. The adapter that calls it already knows the decision happened; this
// method owns the shape: same tenant, a known outcome, a conversation that
// still exists and is not archived, a decider the journey authority lets open
// the request, and a body no person could have typed. It never opens a
// conversation and never reads or writes a reason.
func (s *Service) PostJourneyStage(ctx context.Context, r JourneyStagePost) (Post, error) {
	if err := validatePrincipal(r.Decider, r.TenantID); err != nil {
		return Post{}, err
	}
	key := strings.TrimSpace(r.IdempotencyKey)
	if r.Decider.TenantID != r.TenantID || strings.TrimSpace(r.ConversationID) == "" || key == "" || len(key) > 160 {
		return Post{}, ErrInvalidArgument
	}
	body, err := JourneyStageBody(JourneyStageLine{Outcome: r.Outcome, Decider: r.Decider.SubjectID})
	if err != nil {
		return Post{}, err
	}
	writer, ok := s.store.(SystemLineWriter)
	if !ok {
		return Post{}, ErrUnavailable
	}
	ref := Reference{Kind: JourneyReference, TenantID: r.TenantID, ID: r.JourneyID}
	// The authority decides, as the decider, that the request exists and may be
	// opened by them; the stored name is the authority's.
	if err := s.validateJourneyReference(ctx, r.Decider, r.TenantID, &ref); err != nil {
		return Post{}, err
	}
	c, err := s.store.GetConversation(ctx, r.TenantID, r.ConversationID)
	if err != nil {
		return Post{}, err
	}
	if c.ID != r.ConversationID || c.TenantID != r.TenantID {
		return Post{}, ErrNotFound
	}
	if c.Archived {
		return Post{}, ErrNotFound
	}
	post := Post{ID: uuid.NewString(), ConversationID: c.ID, TenantID: c.TenantID, AuthorID: r.Decider.SubjectID, AuthorHomeTenantID: r.Decider.TenantID, Body: body, Revision: 1, CreatedAt: s.now(), References: []Reference{ref}}
	sent, err := writer.SendSystemLine(ctx, SendPostRequest{Principal: r.Decider, TenantID: r.TenantID, ConversationID: c.ID, Body: body, IdempotencyKey: key, References: post.References}, post)
	if err != nil {
		return Post{}, err
	}
	return sent, nil
}
