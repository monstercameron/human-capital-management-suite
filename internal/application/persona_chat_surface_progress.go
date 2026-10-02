package application

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstate"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// personaAnswerFeedbackReader reads the ratings a person holds on the answers
// to their own questions in one conversation, by invocation.
type personaAnswerFeedbackReader interface {
	ListAnswerFeedback(context.Context, string, string, string) (map[string]agentinvocationstore.AnswerFeedback, error)
}

// The two ratings as the page's rating controls name them.
const (
	personaFeedbackHelpful  = "helpful"
	personaFeedbackNotRight = "not-right"
)

func personaSurfaceRunID(invocation agentinvoke.Invocation) (string, error) {
	return agentrun.AdmissionRequestID(agentrun.SourceIdentity{TenantID: invocation.TenantID, Kind: agentrun.SourcePersonaMention, Key: invocation.ID, Ref: invocation.PostID})
}

// Progress discloses no rows owned by another invoker, including task names,
// checkpoint references and failure details. Unknown state is unavailable.
func (s *PersonaChatSurface) Progress(ctx context.Context, conversationID string) (personachat.Progress, error) {
	// A read: the member of an archived conversation still sees what became of
	// their own questions there. Asking again goes through member, which refuses.
	p, room, err := s.reader(ctx, conversationID)
	if err != nil {
		return personachat.Progress{}, err
	}
	if s.Invocations == nil || s.Executions == nil {
		return personachat.Progress{}, personachat.ErrUnavailable
	}
	rows, err := s.Invocations.ListPersonaInvocations(ctx, room.TenantID, p.Subject(), room.ID)
	if err != nil {
		return personachat.Progress{}, personachat.ErrUnavailable
	}
	store, err := s.Executions(ctx, room.TenantID)
	if err != nil || store == nil {
		return personachat.Progress{}, personachat.ErrUnavailable
	}
	result := personachat.Progress{Invocations: make([]personachat.Invocation, 0, len(rows))}
	failures := make(map[string]agentinvocationstore.PostFailure)
	if s.Failures != nil {
		failed, err := s.Failures.ListPostFailures(ctx, room.TenantID, p.Subject(), room.ID)
		if err != nil {
			return personachat.Progress{}, personachat.ErrUnavailable
		}
		for _, failure := range failed {
			if failure.TenantID != room.TenantID || failure.InvokerID != p.Subject() || failure.ConversationID != room.ID {
				return personachat.Progress{}, personachat.ErrUnavailable
			}
			failures[failure.PostID] = failure
		}
	}
	seenPosts := make(map[string]bool)
	privateReplies := make(map[string]agentinvocationstore.ReplyReceipt)
	publicReplies := make(map[string]string)
	if s.Receipts != nil {
		receipts, err := s.Receipts.ListReplyReceipts(ctx, room.TenantID, p.Subject(), room.ID)
		if err != nil {
			return personachat.Progress{}, personachat.ErrUnavailable
		}
		for _, receipt := range receipts {
			if receipt.TenantID != room.TenantID {
				return personachat.Progress{}, personachat.ErrUnavailable
			}
			if receipt.InvokerID == p.Subject() && receipt.ConversationID == room.ID && receipt.PublicPostID != "" {
				publicReplies[receipt.InvocationID] = receipt.PublicPostID
			}
			if receipt.InvokerID != p.Subject() || receipt.ConversationID != room.ID || receipt.PrivatePostID == "" || receipt.PrivateConversationID == "" {
				continue
			}
			if _, _, err := s.member(ctx, receipt.PrivateConversationID); err == nil {
				privateReplies[receipt.InvocationID] = receipt
			}
		}
	}
	// CHATBUG-066: the asker's own stored ratings travel with the activity, so
	// the page draws an answer already rated from its first paint after a load.
	ratings := map[string]agentinvocationstore.AnswerFeedback{}
	if reader, ok := s.Invocations.(personaAnswerFeedbackReader); ok {
		if ratings, err = reader.ListAnswerFeedback(ctx, room.TenantID, p.Subject(), room.ID); err != nil {
			return personachat.Progress{}, personachat.ErrUnavailable
		}
	}
	// CHATBUG-075: the agents are named only when there is a run to name, and
	// from names kept for a minute: this read happens every second for as long
	// as the conversation is open, and most conversations have no run at all.
	var names map[string]string
	if len(rows) > 0 {
		names = s.personaNames(ctx, p, room)
	}
	// CHATBUG-047: a question asked again has one row for each attempt, newest
	// first. Only the newest is drawn: it takes the failed one's place under the
	// question. A post-keyed failure belongs to the first attempt alone.
	superseded, retried := personaAttemptRows(rows)
	for index, invocation := range rows {
		if invocation.TenantID != room.TenantID || invocation.InvokerID != p.Subject() || invocation.ConversationID != room.ID {
			return personachat.Progress{}, personachat.ErrUnavailable
		}
		if superseded[index] {
			continue
		}
		id, err := personaSurfaceRunID(invocation)
		if err != nil {
			return personachat.Progress{}, personachat.ErrUnavailable
		}
		run, err := store.Get(ctx, id)
		// CHATBUG-063: an agent that cannot be named here is sent with no name. The
		// page names it from the question's own mention, or draws no card.
		projection := personachat.Invocation{InvocationID: invocation.ID, PostID: invocation.PostID, ThreadID: invocation.ThreadID, ConversationID: room.ID, InvokerID: p.Subject(), Status: string(invocation.State), Activity: "Admission", CurrentStep: 1, TotalSteps: 4}
		seenPosts[invocation.PostID] = true
		if name := names[invocation.PersonaID]; name != "" {
			projection.AgentName = name
		}
		if receipt, ok := privateReplies[invocation.ID]; ok {
			projection.PrivateConversationID, projection.PrivatePostID = receipt.PrivateConversationID, receipt.PrivatePostID
		}
		projection.PublicPostID = publicReplies[invocation.ID]
		if rating, rated := ratings[invocation.ID]; rated && rating.Active {
			projection.Feedback = personaFeedbackNotRight
			if rating.Helpful {
				projection.Feedback = personaFeedbackHelpful
			}
		}
		if errors.Is(err, agentrunstate.ErrNotFound) {
			if failure, ok := failures[invocation.PostID]; ok && !retried[index] {
				projectPersonaPostFailure(&projection, failure)
			} else {
				s.projectPersonaStep(&projection, invocation, runstate.Run{}, false)
			}
			result.Invocations = append(result.Invocations, projection)
			continue
		}
		if err != nil || run.TenantID != room.TenantID || run.ID != id || run.AdmissionID != id {
			return personachat.Progress{}, personachat.ErrUnavailable
		}
		projectPersonaSurfaceRun(&projection, run)
		projection.ElapsedSeconds = personaRunElapsedSeconds(run, s.now())
		s.projectPersonaStep(&projection, invocation, run, true)
		if err := s.projectPersonaInvocationTask(ctx, invocation, run, &projection); err != nil {
			return personachat.Progress{}, err
		}
		result.Invocations = append(result.Invocations, projection)
	}
	for postID, failure := range failures {
		if seenPosts[postID] {
			continue
		}
		projection := personachat.Invocation{InvocationID: "post-failure:" + postID, PostID: postID, ThreadID: failure.ThreadID, ConversationID: room.ID, InvokerID: p.Subject(), TotalSteps: 4, CurrentStep: 1}
		projectPersonaPostFailure(&projection, failure)
		result.Invocations = append(result.Invocations, projection)
	}
	slices.SortFunc(result.Invocations, func(a, b personachat.Invocation) int { return strings.Compare(a.InvocationID, b.InvocationID) })
	return result, nil
}

func projectPersonaPostFailure(out *personachat.Invocation, failure agentinvocationstore.PostFailure) {
	out.Status, out.FailureCode, out.Retryable = "FAILED", failure.Code, failure.Retryable
	out.Activity = "Unable to finish"
	out.FailureMessage = "The persona could not finish this request."
	if failure.Code == "ADMISSION_REFUSED" {
		out.FailureMessage = "This persona is unavailable for this request with your current access."
	}
}

func projectPersonaSurfaceRun(out *personachat.Invocation, run runstate.Run) {
	out.Status = string(run.State)
	out.Activity = "Preparing context"
	out.CurrentStep = 1
	for _, checkpoint := range run.Checkpoints {
		switch checkpoint.Phase {
		case runstate.PhaseContext:
			out.Activity, out.CurrentStep = "Preparing answer", 2
		case runstate.PhaseModelCall:
			out.Activity, out.CurrentStep = "Validating answer", 3
		case runstate.PhaseToolCall:
			out.Activity, out.CurrentStep = "Reading sources", 2
		case runstate.PhaseValidation:
			out.Activity, out.CurrentStep = "Delivering answer", 4
		case runstate.PhaseDelivery:
			out.Activity, out.CurrentStep = "Delivered", 4
		}
	}
	if run.State == runstate.StateFailed || run.State == runstate.StateExpired || run.State == runstate.StateCancelled || run.State == runstate.StateNeedsRepair {
		out.FailureCode = run.TerminalCode
		out.FailureMessage = "The persona could not finish this request."
		// Retry starts a fresh invocation, with current authority, rather than
		// mutating a terminal execution or replaying old access.
		out.Retryable = personaRunRetryable(run)
	}
}

// personaRunElapsedSeconds is how long a run that is still going has been
// going since its admission (AGENTUX-026). A run that is over, or one whose
// admission time is not recorded, has no elapsed time to show.
func personaRunElapsedSeconds(run runstate.Run, at time.Time) int {
	switch run.State {
	case runstate.StateCompleted, runstate.StateFailed, runstate.StateExpired, runstate.StateCancelled, runstate.StateNeedsRepair:
		return 0
	}
	if run.CreatedAt.IsZero() || !at.After(run.CreatedAt) {
		return 0
	}
	return int(at.Sub(run.CreatedAt) / time.Second)
}

// now is the surface's clock.
func (s *PersonaChatSurface) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// personaRetryQuestionPrefix names a question to ask again by its own message,
// as "question:<conversation>:<message>".
const personaRetryQuestionPrefix = "question:"

// personaRetryQuestion splits that name. A message id holds no colon, so the
// last one separates it from the conversation.
func personaRetryQuestion(id string) (conversationID, postID string, ok bool) {
	rest, named := strings.CutPrefix(id, personaRetryQuestionPrefix)
	if !named {
		return "", "", false
	}
	cut := strings.LastIndex(rest, ":")
	if cut <= 0 || cut == len(rest)-1 {
		return "", "", false
	}
	return rest[:cut], rest[cut+1:], true
}

// personaRunEndedUnanswered reports whether a run is over without an answer:
// it failed, expired, was stopped or needs repair. Only such a run may be asked
// again; one still going, or one that answered, may not.
func personaRunEndedUnanswered(run runstate.Run) bool {
	switch run.State {
	case runstate.StateFailed, runstate.StateExpired, runstate.StateCancelled, runstate.StateNeedsRepair:
		return true
	}
	return false
}

// Retry re-reads the original human post through the history authority, then
// commits a fresh typed mention. SendPost performs current persona admission.
func (s *PersonaChatSurface) Retry(ctx context.Context, invocationID, idempotencyKey string) (personachat.RetryResult, error) {
	if s == nil || s.Invocations == nil || s.Executions == nil {
		return personachat.RetryResult{}, personachat.ErrUnavailable
	}
	if invocationID == "" || strings.TrimSpace(invocationID) != invocationID || strings.TrimSpace(idempotencyKey) != idempotencyKey || len(idempotencyKey) < 8 || len(idempotencyKey) > 128 {
		return personachat.RetryResult{}, personachat.ErrInvalid
	}
	p, ok := personaSurfacePrincipal(ctx)
	if !ok {
		if p != nil {
			return personachat.RetryResult{}, personachat.ErrDenied
		}
		return personachat.RetryResult{}, personachat.ErrUnauthenticated
	}
	invocation, err := s.Invocations.Lookup(ctx, p.Tenant().String(), p.Subject(), invocationID)
	var postFailure *agentinvocationstore.PostFailure
	// CHATBUG-054: the person who asked may ask again whatever ended the first
	// answer and however long ago. Asking again starts a fresh admission under
	// current authority, so a question that may no longer be answered is refused
	// there, as a first question would be.
	askedByQuestion := false
	if strings.HasPrefix(invocationID, "post-failure:") && s.Failures != nil {
		failure, failureErr := s.Failures.LookupPostFailure(ctx, p.Tenant().String(), p.Subject(), strings.TrimPrefix(invocationID, "post-failure:"))
		if failureErr != nil || failure.TenantID != p.Tenant().String() || failure.InvokerID != p.Subject() {
			return personachat.RetryResult{}, personachat.ErrDenied
		}
		postFailure = &failure
		invocation = agentinvoke.Invocation{ID: invocationID, TenantID: failure.TenantID, InvokerID: failure.InvokerID, ConversationID: failure.ConversationID, ThreadID: failure.ThreadID, PostID: failure.PostID}
		err = nil
	} else if conversationID, postID, named := personaRetryQuestion(invocationID); named {
		// A question the server holds no outcome for (it was committed and never
		// admitted) is named by its own message. If a run does stand behind it,
		// that run decides, below, whether it may be asked again.
		rows, listErr := s.Invocations.ListPersonaInvocations(ctx, p.Tenant().String(), p.Subject(), conversationID)
		if listErr != nil {
			return personachat.RetryResult{}, personachat.ErrUnavailable
		}
		invocation, askedByQuestion = agentinvoke.Invocation{ID: invocationID, TenantID: p.Tenant().String(), InvokerID: p.Subject(), ConversationID: conversationID, ThreadID: postID, PostID: postID}, true
		for _, row := range rows {
			if row.PostID == postID && row.TenantID == p.Tenant().String() && row.InvokerID == p.Subject() && row.ConversationID == conversationID {
				invocation, invocationID, askedByQuestion = row, row.ID, false
				break
			}
		}
		err = nil
	}
	if err != nil || invocation.ID != invocationID || invocation.InvokerID != p.Subject() || invocation.TenantID != p.Tenant().String() {
		return personachat.RetryResult{}, personachat.ErrDenied
	}
	_, room, err := s.member(ctx, invocation.ConversationID)
	if err != nil {
		return personachat.RetryResult{}, err
	}
	id, err := personaSurfaceRunID(invocation)
	if err != nil {
		return personachat.RetryResult{}, personachat.ErrUnavailable
	}
	store, err := s.Executions(ctx, room.TenantID)
	if err != nil || store == nil {
		return personachat.RetryResult{}, personachat.ErrUnavailable
	}
	rows, err := s.Invocations.ListPersonaInvocations(ctx, room.TenantID, p.Subject(), room.ID)
	if err != nil {
		return personachat.RetryResult{}, personachat.ErrUnavailable
	}
	attempts := personaQuestionAttempts(rows, invocation.PostID, invocation.PersonaID, p.Subject(), room.ID)
	if postFailure == nil && !askedByQuestion && len(attempts) > 0 && attempts[0].ID != invocation.ID {
		// The card asked about is no longer the newest attempt: the question was
		// asked again after it. One attempt is live at a time, so a second ask
		// while the newest runs is the first ask repeated, and starts nothing.
		newest, err := personaSurfaceRunID(attempts[0])
		if err != nil {
			return personachat.RetryResult{}, personachat.ErrUnavailable
		}
		run, err := store.Get(ctx, newest)
		switch {
		case errors.Is(err, agentrunstate.ErrNotFound):
			return personachat.RetryResult{PostID: invocation.PostID, ConversationID: room.ID, ThreadID: invocation.ThreadID}, nil
		case err != nil || run.ID != newest || run.TenantID != room.TenantID:
			return personachat.RetryResult{}, personachat.ErrUnavailable
		case !personaRunEndedUnanswered(run):
			if run.State == runstate.StateCompleted {
				return personachat.RetryResult{}, personachat.ErrConflict
			}
			return personachat.RetryResult{PostID: invocation.PostID, ConversationID: room.ID, ThreadID: invocation.ThreadID}, nil
		}
		invocation, id = attempts[0], newest
	} else if postFailure == nil && !askedByQuestion {
		run, err := store.Get(ctx, id)
		if errors.Is(err, agentrunstate.ErrNotFound) && s.Failures != nil {
			if _, failureErr := s.Failures.LookupPostFailure(ctx, p.Tenant().String(), p.Subject(), invocation.PostID); failureErr != nil {
				return personachat.RetryResult{}, personachat.ErrConflict
			}
		} else {
			if err != nil || run.ID != id || run.TenantID != room.TenantID {
				return personachat.RetryResult{}, personachat.ErrUnavailable
			}
			if !personaRunEndedUnanswered(run) {
				return personachat.RetryResult{}, personachat.ErrConflict
			}
		}
	}
	principal := chat.Principal{TenantID: room.TenantID, SubjectID: p.Subject()}
	page := chat.Page{PageSize: 200}
	seen := make(map[string]bool)
	for {
		posts, err := s.Chat.ListPosts(ctx, chat.ListPostsRequest{Principal: principal, TenantID: room.TenantID, ConversationID: room.ID, Page: page})
		if err != nil {
			return personachat.RetryResult{}, surfaceChatError(err)
		}
		for _, post := range posts.Posts {
			if post.ID != invocation.PostID {
				continue
			}
			if post.AuthorID != p.Subject() || post.TenantID != room.TenantID || post.ConversationID != room.ID || post.Deleted || len(post.References) == 0 {
				return personachat.RetryResult{}, personachat.ErrDenied
			}
			// Preserve only the canonical persona that failed. Other mentions
			// from the original post must not trigger extra retry work.
			directory, err := s.Directory(ctx, room.ID)
			if err != nil {
				return personachat.RetryResult{}, err
			}
			var references []chat.Reference
			asked := ""
			for _, candidate := range directory.Personas {
				facts, err := s.References.LookupPersonaReference(ctx, room.TenantID, room.ID, candidate.Reference.ID)
				if err != nil || invocation.PersonaID != "" && facts.PersonaID != invocation.PersonaID {
					continue
				}
				for _, reference := range post.References {
					if reference.Kind == chat.AgentMention && reference.ID == candidate.Reference.ID && reference.TenantID == room.TenantID {
						references, asked = append(references, reference), facts.PersonaID
						break
					}
				}
			}
			if len(references) != 1 {
				return personachat.RetryResult{}, personachat.ErrDenied
			}
			// CHATBUG-047: the question that stands is admitted again as the next
			// attempt. No message is posted, so nothing is stored twice and the
			// failed attempt's card is replaced in place by the new one. The attempt
			// number is the count of attempts so far, so a second ask that read the
			// same state names the same attempt and starts nothing more.
			reattempt, ok := s.Chat.(personaQuestionReattempter)
			if !ok || isNilPersonaOutputPort(reattempt) {
				return personachat.RetryResult{}, personachat.ErrUnavailable
			}
			next := len(personaQuestionAttempts(rows, post.ID, asked, p.Subject(), room.ID))
			if err := reattempt.ReattemptPersonaQuestion(ctx, post, next); err != nil {
				return personachat.RetryResult{}, personachat.ErrUnavailable
			}
			return personachat.RetryResult{PostID: post.ID, ConversationID: room.ID, ThreadID: invocation.ThreadID}, nil
		}
		if posts.NextCursor == "" {
			return personachat.RetryResult{}, personachat.ErrDenied
		}
		if seen[posts.NextCursor] {
			return personachat.RetryResult{}, personachat.ErrUnavailable
		}
		seen[posts.NextCursor], page.Cursor = true, posts.NextCursor
	}
}
