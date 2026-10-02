package application

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
	"net/url"
)

func (r *AgentAnnouncementRuntime) outputRun(ctx context.Context, output agentsecurity.FinalOutputPersistence) (agentrun.Record, runstate.Run, runstate.Store, error) {
	i := output.Identity()
	repo, err := r.admissionRepository(i.TenantID)
	if err != nil {
		return agentrun.Record{}, runstate.Run{}, nil, err
	}
	record, err := repo.GetByID(ctx, i.AdmissionID)
	if err != nil || record.Request.Source.Kind != agentrun.SourceAnnouncement || record.Request.Source.Key != i.InvocationID || record.Request.Principal.RequesterID != i.InvokerID || record.Request.InstallationID != i.InstallationID || record.Request.Persona == nil || record.Request.Persona.ID != i.PersonaID || record.Request.Audience.ID != i.ConversationID || record.Request.Context.ID != i.ThreadID {
		return record, runstate.Run{}, nil, ErrAgentAnnouncementDenied
	}
	states, err := agentrunstate.New(r.Agents, func(tenant string) uuid.UUID { return r.Work.tenantUUID(values.TenantId(tenant)) })
	if err != nil {
		return record, runstate.Run{}, nil, err
	}
	store, err := states.ForTenant(i.TenantID)
	if err != nil {
		return record, runstate.Run{}, nil, err
	}
	run, err := store.Get(ctx, i.RunID)
	if err != nil || run.AdmissionID != record.ID {
		return record, run, nil, ErrAgentAnnouncementDenied
	}
	return record, run, store, nil
}

func (r *AgentAnnouncementRuntime) AuthorizeSealedPublicPersonaReply(context.Context, agentsecurity.FinalOutputPersistence) (workload.Identity, error) {
	return workload.Identity{}, chat.ErrPermissionDenied
}

func (r *AgentAnnouncementRuntime) AuthorizeSealedAnnouncement(ctx context.Context, output agentsecurity.FinalOutputPersistence) (workload.Identity, error) {
	record, run, _, err := r.outputRun(ctx, output)
	if err != nil {
		return workload.Identity{}, err
	}
	draft, _, err := output.Payload()
	if err != nil {
		return workload.Identity{}, err
	}
	reply, ok := draft.Result.Value.(PersonaChatReply)
	if !ok {
		return workload.Identity{}, ErrPersonaRunOutputRejected
	}
	fresh, err := r.seal(ctx, record, run, reply.Text)
	if err != nil || fresh.Identity() != output.Identity() || fresh.SemanticDigest() != output.SemanticDigest() {
		return workload.Identity{}, ErrPersonaRunOutputRejected
	}
	return r.Worker.ResolvePersonaChatWorker(ctx)
}

func (r *AgentAnnouncementRuntime) PostAnnouncement(ctx context.Context, request AgentAnnouncementRunRequest, result AgentAnnouncementRunResult, _ string) (string, error) {
	if r == nil || r.Chat == nil || r.Names == nil {
		return "", ErrAgentAnnouncementUnavailable
	}
	ctx = context.WithValue(ctx, announcementRuntimeKey{}, r)
	// The message is committed by the store directly, so this path takes the
	// conversation's write lease itself, exactly as the routed Chat service
	// does for a person's message. A stale or moved route refuses the post.
	if r.Routes != nil && r.RouteCache != nil {
		lease, err := r.RouteCache.Resolve(ctx, r.Routes, request.ConversationID, request.TenantID)
		if err != nil {
			return "", fmt.Errorf("announcement route: %w", err)
		}
		if err = r.RouteCache.CheckWrite(ctx, r.Routes, lease, request.TenantID, r.Now()); err != nil {
			r.RouteCache.Invalidate(request.ConversationID, lease.Route.Epoch)
			return "", fmt.Errorf("announcement route: %w", err)
		}
		ctx = chatrouting.WithWriteLease(ctx, lease)
	}
	record, run, execution, err := r.outputRun(ctx, result.Output)
	if err != nil {
		return "", err
	}
	definition, err := r.record(ctx, record.Request)
	if err != nil {
		return "", err
	}
	if request.OccurrenceID != result.Output.Identity().InvocationID || request.OwnerID != definition.OwnerID || request.AnnouncementID != definition.ID || request.Preview {
		return "", ErrAgentAnnouncementDenied
	}
	names, err := r.Names(ctx, request.TenantID, definition.OwnerID)
	if err != nil {
		return "", err
	}
	profile, _, _, _, _, err := r.facts(ctx, definition)
	if err != nil {
		return "", err
	}
	message := chatui.AgentAnnouncementMessage{AgentName: profile.DisplayName, OwnerName: names, Scheduled: definition.Cadence != "NOW" && definition.Cadence != "ONCE", Text: result.Text, PostedAt: r.Now().UTC()}
	for _, source := range result.Sources {
		message.Sources = append(message.Sources, chatui.AgentAnnouncementSource{Title: source.Title, Href: "/workspace/app/docs?" + url.Values{"document": {source.DocumentID}}.Encode()})
	}
	body, err := chatui.AnnouncementMessageBody(message)
	if err != nil {
		return "", err
	}
	ids := make([]string, 0, len(result.Sources))
	for _, source := range result.Sources {
		ids = append(ids, source.DocumentID)
	}
	var post chat.Post
	err = r.Agents.WithAnnouncementSecurityFence(ctx, r.Work.tenantUUID(values.TenantId(request.TenantID)), record.ID, func() error {
		return r.agentuxDemoSourceReadFence(ctx, definition, ids, func() error {
			current, err := r.currentConversation(ctx, request.TenantID, request.ConversationID)
			if err != nil {
				return err
			}
			snapshot, err := r.Audience.CurrentAudience(ctx, current)
			if err != nil {
				return err
			}
			class, err := r.bodyClass(ctx, request.TenantID, result.Text)
			if err != nil || r.Audience.AllowDataClass(ctx, current, class) != nil {
				return ErrAgentAnnouncementNotPublic
			}
			if definition.PersonaID == AgentUXDemoBirthdayPersonaID {
				if err := r.agentuxDemoRecheckBirthday(ctx, definition, result.Output); err != nil {
					return err
				}
			} else {
				documents, citations, err := outputAnnouncementDocuments(result.Output)
				if err != nil {
					return err
				}
				allowed, _, err := authorizeAnnouncementAudience(ctx, request.TenantID, request.ConversationID, r.DocumentAuthority, snapshot, documents, citations)
				if err != nil || !allowed {
					return ErrAgentAnnouncementNotPublic
				}
			}
			proof, err := chat.IssueAnnouncementDeliveryProof(ctx, result.Output, personaFixedAudienceDecision{decision: chat.PersonaAudienceDecision{Revision: snapshot.Revision, Body: body}})
			if err != nil {
				return err
			}
			committer, err := chatstore.NewSealedPublicPersonaDelivery(r.Chat, r, r.Now)
			if err != nil {
				return err
			}
			post, err = committer.CommitSealedAnnouncement(ctx, result.Output, chat.PersonaReplyCommitRequest{TenantID: request.TenantID, ConversationID: request.ConversationID, AuthorID: request.PersonaID, AuthorHomeTenantID: request.TenantID, Body: body, IdempotencyKey: record.ID, ExpectedAudienceRevision: snapshot.Revision, OutputDigest: result.Output.Digest(), Proof: proof})
			return err
		})
	})
	if err != nil {
		return "", err
	}
	repo, err := r.admissionRepository(request.TenantID)
	if err != nil {
		return "", err
	}
	recheck, err := NewPersonaRunAdmissionRechecker(request.TenantID, repo, r)
	if err != nil {
		return "", err
	}
	state, err := runstate.New(execution, recheck)
	if err != nil {
		return "", err
	}
	deliveryDigest := personaRunBytesDigest([]byte(fmt.Sprint(post.ID, "\x00", body)))
	_, err = state.Checkpoint(withPersonaRunReplyDelivered(ctx, record.ID), run.ID, r.Base.WorkerID, run.Fence, run.Version, runstate.PhaseDelivery, 1, post.ID, deliveryDigest, r.Now())
	return post.ID, err
}
