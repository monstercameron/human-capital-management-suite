package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdeliver"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var errPersonaFinalOutputSourceUnavailable = errors.New("application: persona final output source unavailable")

type personaFinalOutputRecoveryStore interface {
	GetFinalOutput(context.Context, string) (agentpersonastore.FinalOutputRecord, error)
	RecoverFinalOutputForIdentity(context.Context, agentsecurity.FinalOutputIdentity, *agentsecurity.FinalOutputRecoveryVerifier, agentsecurity.FinalOutputRecoveryRehydrator) (agentsecurity.FinalOutputPersistence, error)
}

type personaFinalOutputRecoveryStoreFactory interface {
	ForTenant(context.Context, values.TenantId) (personaFinalOutputRecoveryStore, error)
}

type personaAgentStoreRecoveryFactory struct{ store *agentpersonastore.Store }

func (f personaAgentStoreRecoveryFactory) ForTenant(ctx context.Context, tenant values.TenantId) (personaFinalOutputRecoveryStore, error) {
	if f.store == nil || ctx == nil || strings.TrimSpace(tenant.String()) == "" {
		return nil, errPersonaFinalOutputSourceUnavailable
	}
	return f.store.ForTenant(ctx, tenant)
}

// PersonaFinalOutputSource recovers only exact, signed output projections and
// exposes the plain chat-reply schema as one safe text item.
type PersonaFinalOutputSource struct {
	stores     personaFinalOutputRecoveryStoreFactory
	verifier   *agentsecurity.FinalOutputRecoveryVerifier
	rehydrator agentsecurity.FinalOutputRecoveryRehydrator
}

// NewPersonaFinalOutputSource composes tenant-scoped durable recovery with the
// signature verifier and same-gateway rehydrator.
func NewPersonaFinalOutputSource(store *agentpersonastore.Store, verifier *agentsecurity.FinalOutputRecoveryVerifier, rehydrator agentsecurity.FinalOutputRecoveryRehydrator) (*PersonaFinalOutputSource, error) {
	if store == nil || verifier == nil || isNilPersonaOutputPort(rehydrator) {
		return nil, errPersonaFinalOutputSourceUnavailable
	}
	return newPersonaFinalOutputSource(personaAgentStoreRecoveryFactory{store: store}, verifier, rehydrator)
}

func newPersonaFinalOutputSource(stores personaFinalOutputRecoveryStoreFactory, verifier *agentsecurity.FinalOutputRecoveryVerifier, rehydrator agentsecurity.FinalOutputRecoveryRehydrator) (*PersonaFinalOutputSource, error) {
	if isNilPersonaOutputPort(stores) || verifier == nil || isNilPersonaOutputPort(rehydrator) {
		return nil, errPersonaFinalOutputSourceUnavailable
	}
	return &PersonaFinalOutputSource{stores: stores, verifier: verifier, rehydrator: rehydrator}, nil
}

// LoadPersonaOutput binds the requested tenant, invoker, destination, thread,
// and opaque output ID to current invocation facts before signed recovery.
func (s *PersonaFinalOutputSource) LoadPersonaOutput(ctx context.Context, request PersonaOutputRequest) (agentdeliver.Result, error) {
	if s == nil || ctx == nil || isNilPersonaOutputPort(s.stores) || s.verifier == nil || isNilPersonaOutputPort(s.rehydrator) || !validPersonaOutputRequest(request) {
		return agentdeliver.Result{}, errPersonaFinalOutputSourceUnavailable
	}
	store, err := s.stores.ForTenant(ctx, values.TenantId(request.TenantID))
	if err != nil {
		return agentdeliver.Result{}, fmt.Errorf("scope persona output store: %w", err)
	}
	if isNilPersonaOutputPort(store) {
		return agentdeliver.Result{}, errPersonaFinalOutputSourceUnavailable
	}
	record, err := store.GetFinalOutput(ctx, request.OutputID)
	if err != nil {
		return agentdeliver.Result{}, fmt.Errorf("load persona output record: %w", err)
	}
	identity := personaOutputRecordIdentity(record)
	if !personaOutputRecordMatchesRequest(identity, request) {
		return agentdeliver.Result{}, errPersonaFinalOutputSourceUnavailable
	}
	projection, err := store.RecoverFinalOutputForIdentity(ctx, identity, s.verifier, s.rehydrator)
	if err != nil {
		return agentdeliver.Result{}, fmt.Errorf("recover exact persona output: %w", err)
	}
	if projection.Identity() != identity {
		return agentdeliver.Result{}, errPersonaFinalOutputSourceUnavailable
	}
	return personaChatReplyDeliveryResult(projection)
}

func personaOutputRecordIdentity(record agentpersonastore.FinalOutputRecord) agentsecurity.FinalOutputIdentity {
	return agentsecurity.FinalOutputIdentity{
		TenantID: record.TenantID.String(), OutputID: record.OutputID, InvocationID: record.InvocationID,
		AdmissionID: record.AdmissionID, RunID: record.RunID, InvokerID: record.InvokerID,
		ConversationID: record.ConversationID, ThreadID: record.ThreadID, PostID: record.ParentPostID,
		PersonaID: record.PersonaID, PersonaVersion: record.PersonaVersion, InstallationID: record.InstallationID,
	}
}

func personaOutputRecordMatchesRequest(i agentsecurity.FinalOutputIdentity, request PersonaOutputRequest) bool {
	return i.TenantID == request.TenantID && i.OutputID == request.OutputID &&
		i.InvokerID == request.Invoker.SubjectID && i.ConversationID == request.ConversationID && i.ThreadID == request.ParentPostID &&
		strings.TrimSpace(i.InvocationID) != "" && strings.TrimSpace(i.AdmissionID) != "" && strings.TrimSpace(i.RunID) != "" &&
		strings.TrimSpace(i.PostID) != "" && strings.TrimSpace(i.PersonaID) != "" && strings.TrimSpace(i.PersonaVersion) != "" && strings.TrimSpace(i.InstallationID) != ""
}

func personaChatReplyDeliveryResult(projection agentsecurity.FinalOutputPersistence) (agentdeliver.Result, error) {
	draft, answer, err := projection.Payload()
	if err != nil || draft.Result.Schema != PersonaChatReplySchema {
		return agentdeliver.Result{}, errPersonaFinalOutputSourceUnavailable
	}
	reply, ok := draft.Result.Value.(PersonaChatReply)
	if !ok || validatePersonaChatReply(reply.Text) != nil || len(answer.Parts) != 1 || answer.Parts[0].Text != reply.Text {
		return agentdeliver.Result{}, errPersonaFinalOutputSourceUnavailable
	}
	citations := make([]agentdeliver.Citation, 0, len(projection.Citations()))
	for _, citation := range projection.Citations() {
		citations = append(citations, agentdeliver.Citation{SourceID: citation.SourceID})
	}
	return agentdeliver.Result{PersonaLabel: "Persona", Items: []agentdeliver.ResultItem{{ID: "answer", Text: reply.Text, Citations: citations}}}, nil
}

var _ PersonaOutputSource = (*PersonaFinalOutputSource)(nil)
var _ personaFinalOutputRecoveryStore = (*agentpersonastore.TenantStore)(nil)
