package application

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// CHATBUG-040: a private answer used to reach the page only on the
// conversation's event stream, one answer at a time and seconds after the
// record that said its run had finished. On a reload every answered question
// was therefore drawn as a placeholder first. The first read of a
// conversation's activity now carries the caller's stored answers with it, in
// one batch; the once-a-second reads that follow do not, and an answer
// delivered while the page is open still arrives on the stream.

var _ personachat.OpeningSurface = (*PersonaChatSurface)(nil)

// OpeningProgress is Progress with the caller's own stored private answers of
// this conversation beside it. Only answers named by the receipts of the
// caller's own runs are sent, read through the card reader, which checks the
// caller's membership and leaves out what has expired. A failed read of the
// answers does not fail the activity: the page keeps the placeholder and the
// stream delivers them, as before.
func (s *PersonaChatSurface) OpeningProgress(ctx context.Context, conversationID string) (personachat.Progress, error) {
	result, err := s.Progress(ctx, conversationID)
	if err != nil || s.Receipts == nil {
		return result, err
	}
	answers, err := s.openingAnswers(ctx, conversationID)
	if errors.Is(err, personachat.ErrDenied) || errors.Is(err, personachat.ErrUnauthenticated) {
		return personachat.Progress{}, err
	}
	if err == nil && len(answers) > 0 {
		result.Answers = answers
	}
	return result, nil
}

// openingAnswers reads the caller's own stored private answers of one
// conversation, oldest first, each with the message it was shared to the
// channel as when it was (CHATUX-026).
func (s *PersonaChatSurface) openingAnswers(ctx context.Context, conversationID string) ([]personachat.PrivateAnswer, error) {
	p, room, err := s.reader(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	receipts, err := s.Receipts.ListReplyReceipts(ctx, room.TenantID, p.Subject(), room.ID)
	if err != nil {
		return nil, personachat.ErrUnavailable
	}
	cards, err := s.deliveredPrivateCards(ctx, p, room, receipts)
	if err != nil {
		return nil, err
	}
	answers := make([]personachat.PrivateAnswer, 0, len(cards))
	for _, receipt := range receipts {
		card, delivered := cards[receipt.EphemeralPostID]
		if !delivered || receipt.InvokerID != p.Subject() || receipt.ConversationID != room.ID {
			continue
		}
		if !card.OnlyVisibleToYou || card.ThreadID == "" || strings.TrimSpace(card.Body) == "" || card.CreatedAt.IsZero() || !card.ExpiresAt.After(card.CreatedAt) {
			continue
		}
		answers = append(answers, personachat.PrivateAnswer{ID: card.ID, ThreadID: card.ThreadID, Body: card.Body, CreatedAt: card.CreatedAt, ExpiresAt: card.ExpiresAt, ThreadLink: card.ThreadLink, InvocationID: receipt.InvocationID})
	}
	slices.SortFunc(answers, func(a, b personachat.PrivateAnswer) int {
		if order := a.CreatedAt.Compare(b.CreatedAt); order != 0 {
			return order
		}
		return strings.Compare(a.ID, b.ID)
	})
	// A failed search for the shared copies leaves the answers as private cards,
	// which is what they are until the page is told otherwise.
	if shared, err := s.sharedAnswerCopies(ctx, p.Subject(), room, receipts, answers); err == nil {
		for index := range answers {
			answers[index].SharedPostID = shared[answers[index].ID]
		}
	}
	return answers, nil
}
