package application

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// CHATBUG-047: asking an agent again admits the question that already stands as
// another attempt. It posts nothing: the failed attempt stays what it was, and
// the new attempt is the one the person's page shows under the question.

// personaQuestionReattempter admits an existing question again. attempt is the
// number of the attempt to start: zero for a question that was committed and
// never admitted, one for its first retry, and so on.
type personaQuestionReattempter interface {
	ReattemptPersonaQuestion(ctx context.Context, post chatcore.Post, attempt int) error
}

// personaQuestionAttempts are the invocations of one question by one person in
// one conversation, newest first as the invocation list returns them. An empty
// personaID matches every agent asked in the question.
func personaQuestionAttempts(rows []agentinvoke.Invocation, postID, personaID, invoker, conversationID string) []agentinvoke.Invocation {
	var attempts []agentinvoke.Invocation
	for _, row := range rows {
		if row.PostID == postID && row.InvokerID == invoker && row.ConversationID == conversationID && (personaID == "" || row.PersonaID == personaID) {
			attempts = append(attempts, row)
		}
	}
	return attempts
}

// personaAttemptRows marks, for each invocation of a newest-first list, whether
// a newer attempt at the same question by the same agent stands (superseded: the
// page draws the newer one in its place) and whether it is itself a retry, that
// is, not the oldest attempt listed.
func personaAttemptRows(rows []agentinvoke.Invocation) (superseded, retried []bool) {
	superseded, retried = make([]bool, len(rows)), make([]bool, len(rows))
	type question struct{ post, persona string }
	newest := make(map[question]bool, len(rows))
	oldest := make(map[question]int, len(rows))
	for index, row := range rows {
		oldest[question{row.PostID, row.PersonaID}] = index
	}
	for index, row := range rows {
		key := question{row.PostID, row.PersonaID}
		superseded[index] = newest[key]
		newest[key] = true
		retried[index] = oldest[key] != index
	}
	return superseded, retried
}

type personaRetryAttemptKey struct{}

// withPersonaRetryAttempt marks the admission that follows as attempt n at the
// post it is given, rather than the post's first admission.
func withPersonaRetryAttempt(ctx context.Context, attempt int) context.Context {
	return context.WithValue(ctx, personaRetryAttemptKey{}, attempt)
}

// personaRetryAttempt is the attempt the admission in ctx belongs to; zero is
// the first admission of a post.
func personaRetryAttempt(ctx context.Context) int {
	attempt, _ := ctx.Value(personaRetryAttemptKey{}).(int)
	return attempt
}

// admitPostCommit is the one place a committed post reaches admission: a first
// admission, or an explicit retry of the post.
func (c *personaChatInvocation) admitPostCommit(ctx context.Context, post agentinvoke.PostCommit) error {
	var err error
	if attempt := personaRetryAttempt(ctx); attempt > 0 {
		_, err = c.resolver.RetryMention(ctx, post, attempt)
	} else {
		_, err = c.resolver.OnPostCommit(ctx, post)
	}
	return err
}

// reattempt runs the steps that follow a committed question for a question that
// stands already. It reads the question as it is stored: the caller passes the
// stored post, and the checks made after a commit (the author is the signed-in
// person, the post is a new, unedited, human post) are made on it again.
func (c *personaChatInvocation) reattempt(ctx context.Context, post chatcore.Post, attempt int) error {
	if c == nil || c.resolver == nil || attempt < 0 || post.ID == "" {
		return errPersonaChatInvocation
	}
	request := chatcore.SendPostRequest{
		Principal: chatcore.Principal{TenantID: post.TenantID, SubjectID: post.AuthorID},
		TenantID:  post.TenantID, ConversationID: post.ConversationID,
	}
	ctx = withPersonaRetryAttempt(ctx, attempt)
	if c.detached {
		// As after a first question, model work never holds the response.
		go c.afterCommit(context.WithoutCancel(ctx), request, post)
		return nil
	}
	c.afterCommit(ctx, request, post)
	return nil
}

// ReattemptPersonaQuestion hands the question to the post-commit admission this
// chat service was composed with.
func (w *PersonaInvocationServeWiring) ReattemptPersonaQuestion(ctx context.Context, post chatcore.Post, attempt int) error {
	if w == nil || w.Chat == nil {
		return errPersonaInvocationServeWiring
	}
	return w.Chat.reattempt(ctx, post, attempt)
}

// ReattemptPersonaQuestion is the served chat service's way to the same
// admission: the persona surface holds only the chat service.
func (s *streamingChatService) ReattemptPersonaQuestion(ctx context.Context, post chatcore.Post, attempt int) error {
	if s == nil || s.personaInvocation == nil {
		return errors.New("application: persona invocation is not composed")
	}
	return s.personaInvocation.ReattemptPersonaQuestion(ctx, post, attempt)
}

var (
	_ personaQuestionReattempter = (*PersonaInvocationServeWiring)(nil)
	_ personaQuestionReattempter = (*streamingChatService)(nil)
)
