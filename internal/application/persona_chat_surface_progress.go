package application

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstate"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

func personaSurfaceRunID(invocation agentinvoke.Invocation) (string, error) {
	return agentrun.AdmissionRequestID(agentrun.SourceIdentity{TenantID: invocation.TenantID, Kind: agentrun.SourcePersonaMention, Key: invocation.ID, Ref: invocation.PostID})
}

// Progress discloses no rows owned by another invoker, including task names,
// checkpoint references and failure details. Unknown state is unavailable.
func (s *PersonaChatSurface) Progress(ctx context.Context, conversationID string) (personachat.Progress, error) {
	p, room, err := s.member(ctx, conversationID)
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
	if s.Receipts != nil {
		receipts, err := s.Receipts.ListReplyReceipts(ctx, room.TenantID, p.Subject(), room.ID)
		if err != nil {
			return personachat.Progress{}, personachat.ErrUnavailable
		}
		for _, receipt := range receipts {
			if receipt.TenantID != room.TenantID {
				return personachat.Progress{}, personachat.ErrUnavailable
			}
			if receipt.InvokerID != p.Subject() || receipt.ConversationID != room.ID || receipt.PrivatePostID == "" || receipt.PrivateConversationID == "" {
				continue
			}
			if _, _, err := s.member(ctx, receipt.PrivateConversationID); err == nil {
				privateReplies[receipt.InvocationID] = receipt
			}
		}
	}
	names := make(map[string]string)
	if s.References != nil && s.Personas != nil && s.Skills != nil {
		if directory, err := s.Directory(ctx, conversationID); err == nil {
			for _, profile := range directory.Personas {
				facts, err := s.References.LookupPersonaReference(ctx, room.TenantID, room.ID, profile.Reference.ID)
				if err == nil {
					names[facts.PersonaID] = profile.Reference.Display
				}
			}
		}
	}
	for _, invocation := range rows {
		if invocation.TenantID != room.TenantID || invocation.InvokerID != p.Subject() || invocation.ConversationID != room.ID {
			return personachat.Progress{}, personachat.ErrUnavailable
		}
		id, err := personaSurfaceRunID(invocation)
		if err != nil {
			return personachat.Progress{}, personachat.ErrUnavailable
		}
		run, err := store.Get(ctx, id)
		projection := personachat.Invocation{InvocationID: invocation.ID, PostID: invocation.PostID, ThreadID: invocation.ThreadID, ConversationID: room.ID, InvokerID: p.Subject(), AgentName: "Persona", Status: string(invocation.State), Activity: "Admission", CurrentStep: 1, TotalSteps: 4}
		seenPosts[invocation.PostID] = true
		if name := names[invocation.PersonaID]; name != "" {
			projection.AgentName = name
		}
		if receipt, ok := privateReplies[invocation.ID]; ok {
			projection.PrivateConversationID, projection.PrivatePostID = receipt.PrivateConversationID, receipt.PrivatePostID
		}
		if errors.Is(err, agentrunstate.ErrNotFound) {
			if failure, ok := failures[invocation.PostID]; ok {
				projectPersonaPostFailure(&projection, failure)
			}
			result.Invocations = append(result.Invocations, projection)
			continue
		}
		if err != nil || run.TenantID != room.TenantID || run.ID != id || run.AdmissionID != id {
			return personachat.Progress{}, personachat.ErrUnavailable
		}
		projectPersonaSurfaceRun(&projection, run)
		if err := s.projectPersonaInvocationTask(ctx, invocation, run, &projection); err != nil {
			return personachat.Progress{}, err
		}
		result.Invocations = append(result.Invocations, projection)
	}
	for postID, failure := range failures {
		if seenPosts[postID] {
			continue
		}
		projection := personachat.Invocation{InvocationID: "post-failure:" + postID, PostID: postID, ThreadID: failure.ThreadID, ConversationID: room.ID, InvokerID: p.Subject(), AgentName: "Persona", TotalSteps: 4, CurrentStep: 1}
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
		out.Retryable = run.State == runstate.StateFailed && run.TerminalCode == "MODEL_UNAVAILABLE"
	}
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
	if strings.HasPrefix(invocationID, "post-failure:") && s.Failures != nil {
		failure, failureErr := s.Failures.LookupPostFailure(ctx, p.Tenant().String(), p.Subject(), strings.TrimPrefix(invocationID, "post-failure:"))
		if failureErr != nil || failure.TenantID != p.Tenant().String() || failure.InvokerID != p.Subject() || !failure.Retryable {
			return personachat.RetryResult{}, personachat.ErrDenied
		}
		postFailure = &failure
		invocation = agentinvoke.Invocation{ID: invocationID, TenantID: failure.TenantID, InvokerID: failure.InvokerID, ConversationID: failure.ConversationID, ThreadID: failure.ThreadID, PostID: failure.PostID}
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
	if postFailure == nil {
		run, err := store.Get(ctx, id)
		if errors.Is(err, agentrunstate.ErrNotFound) && s.Failures != nil {
			failure, failureErr := s.Failures.LookupPostFailure(ctx, p.Tenant().String(), p.Subject(), invocation.PostID)
			if failureErr != nil || !failure.Retryable {
				return personachat.RetryResult{}, personachat.ErrConflict
			}
		} else {
			if err != nil || run.ID != id || run.TenantID != room.TenantID {
				return personachat.RetryResult{}, personachat.ErrUnavailable
			}
			if run.State != runstate.StateFailed || run.TerminalCode != "MODEL_UNAVAILABLE" {
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
			for _, candidate := range directory.Personas {
				facts, err := s.References.LookupPersonaReference(ctx, room.TenantID, room.ID, candidate.Reference.ID)
				if err != nil || invocation.PersonaID != "" && facts.PersonaID != invocation.PersonaID {
					continue
				}
				for _, reference := range post.References {
					if reference.Kind == chat.AgentMention && reference.ID == candidate.Reference.ID && reference.TenantID == room.TenantID {
						references = append(references, reference)
						break
					}
				}
			}
			if len(references) != 1 {
				return personachat.RetryResult{}, personachat.ErrDenied
			}
			created, err := s.Chat.SendPost(ctx, chat.SendPostRequest{Principal: principal, TenantID: room.TenantID, ConversationID: room.ID, ParentID: invocation.ThreadID, Body: post.Body, References: references, IdempotencyKey: "persona-retry:" + invocationID + ":" + idempotencyKey})
			if err != nil {
				return personachat.RetryResult{}, surfaceChatError(err)
			}
			return personachat.RetryResult{PostID: created.ID, ConversationID: room.ID, ThreadID: invocation.ThreadID}, nil
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
