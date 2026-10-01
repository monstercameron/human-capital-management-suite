package application

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// personaRuntimeOutputRecovery restores only a signed, exact plain reply after
// current admission, schema, workload, grant and source revalidation. It never
// converts retained identity fields into an authenticated human context.
type personaRuntimeOutputRecovery struct {
	store      *agentstore.Store
	tenantUUID func(values.TenantId) uuid.UUID
	authority  PersonaRunChatReplyAuthoritySource
	verifier   *agentsecurity.FinalOutputRecoveryVerifier
}

func (r *personaRuntimeOutputRecovery) RehydrateFinalOutput(ctx context.Context, record agentsecurity.FinalOutputRecoveryRecord) (agentsecurity.FinalOutputPersistence, error) {
	if r == nil || ctx == nil || r.store == nil || r.tenantUUID == nil || isNilPersonaOutputPort(r.authority) || r.verifier == nil {
		return agentsecurity.FinalOutputPersistence{}, agentsecurity.ErrFinalOutputRecoveryUnavailable
	}
	return agentsecurity.RestoreFinalOutputPersistence(ctx, record, r.verifier, r, r)
}

func (r *personaRuntimeOutputRecovery) current(ctx context.Context, identity agentsecurity.FinalOutputIdentity) (agentrun.Record, runstate.Run, error) {
	if r == nil || ctx == nil || r.store == nil || r.tenantUUID == nil || isNilPersonaOutputPort(r.authority) {
		return agentrun.Record{}, runstate.Run{}, agentsecurity.ErrFinalOutputRecoveryUnavailable
	}
	tenant := values.TenantId(identity.TenantID)
	repo, err := agentrunstore.NewAdmissionRepository(r.store, r.tenantUUID(tenant), tenant)
	if err != nil {
		return agentrun.Record{}, runstate.Run{}, err
	}
	record, err := repo.GetByID(ctx, identity.AdmissionID)
	if err != nil {
		return agentrun.Record{}, runstate.Run{}, err
	}
	states, err := agentrunstate.New(r.store, func(t string) uuid.UUID { return r.tenantUUID(values.TenantId(t)) })
	if err != nil {
		return agentrun.Record{}, runstate.Run{}, err
	}
	view, err := states.ForTenant(identity.TenantID)
	if err != nil {
		return agentrun.Record{}, runstate.Run{}, err
	}
	run, err := view.Get(ctx, identity.RunID)
	if err != nil || validatePersonaModelWorkBinding(record, run) != nil || personaRunFinalOutputIdentity(record, run) != identity {
		return agentrun.Record{}, runstate.Run{}, agentsecurity.ErrFinalOutputRecoveryUnavailable
	}
	return record, run, nil
}

func (r *personaRuntimeOutputRecovery) RevalidateFinalOutputRecovery(ctx context.Context, retained agentsecurity.FinalOutputRecoveryRecord) (agentsecurity.FinalOutputPersistence, error) {
	// The security restoration boundary has verified the receipt before calling
	// this method. Only text is decoded; all seals and evidence come from owners.
	text, err := personaRuntimeRecoveryText(retained.SealedPayload)
	if err != nil {
		return agentsecurity.FinalOutputPersistence{}, err
	}
	record, run, err := r.current(ctx, retained.Identity)
	if err != nil {
		return agentsecurity.FinalOutputPersistence{}, err
	}
	current, err := r.authority.ResolvePersonaRunChatReplyAuthority(ctx, record, run)
	if err != nil || !validPersonaRunChatReplyAuthority(record, run, current) {
		return agentsecurity.FinalOutputPersistence{}, agentsecurity.ErrFinalOutputRecoveryUnavailable
	}
	return sealPersonaRunChatReply(ctx, record, run, current, text)
}

func (r *personaRuntimeOutputRecovery) AuthorizeRecoveredFinalOutput(ctx context.Context, output agentsecurity.FinalOutputPersistence) error {
	record, run, err := r.current(ctx, output.Identity())
	if err != nil {
		return err
	}
	current, err := r.authority.ResolvePersonaRunChatReplyAuthority(ctx, record, run)
	if err != nil || !validPersonaRunChatReplyAuthority(record, run, current) {
		return agentsecurity.ErrFinalOutputRecoveryUnavailable
	}
	payload, _, err := output.Payload()
	if err != nil {
		return err
	}
	reply, ok := payload.Result.Value.(PersonaChatReply)
	if !ok {
		return agentsecurity.ErrFinalOutputRecoveryUnavailable
	}
	fresh, err := sealPersonaRunChatReply(ctx, record, run, current, reply.Text)
	if err != nil || fresh.SemanticDigest() != output.SemanticDigest() {
		return agentsecurity.ErrFinalOutputRecoveryUnavailable
	}
	return nil
}

func personaRuntimeRecoveryText(raw json.RawMessage) (string, error) {
	var retained struct {
		Payload struct {
			Result struct {
				Schema string
				Value  PersonaChatReply
			}
			Narrative string
		} `json:"payload"`
	}
	if json.Unmarshal(raw, &retained) != nil || retained.Payload.Result.Schema != PersonaChatReplySchema || validatePersonaChatReply(retained.Payload.Result.Value.Text) != nil || retained.Payload.Narrative != retained.Payload.Result.Value.Text {
		return "", agentsecurity.ErrFinalOutputRecoveryUnavailable
	}
	return retained.Payload.Result.Value.Text, nil
}

var _ agentsecurity.FinalOutputRecoveryRehydrator = (*personaRuntimeOutputRecovery)(nil)

// personaRuntimeCurrentReply rechecks source and delegation authority before
// either public delivery or invoker-only fallback reaches the chat owner.
type personaRuntimeCurrentReply struct {
	next      PersonaRunReplyDeliverer
	authority agentsecurity.FinalOutputRecoveryCurrentAuthority
}

func (d *personaRuntimeCurrentReply) Deliver(ctx context.Context, req PersonaReplyDeliveryRequest) (PersonaReplyDeliveryReceipt, error) {
	if d == nil || ctx == nil || isNilPersonaOutputPort(d.next) || isNilPersonaOutputPort(d.authority) || req.Output.Digest() == "" {
		return PersonaReplyDeliveryReceipt{}, ErrPersonaReplyDeliveryUnavailable
	}
	if err := d.authority.AuthorizeRecoveredFinalOutput(ctx, req.Output); err != nil {
		return PersonaReplyDeliveryReceipt{}, err
	}
	return d.next.Deliver(ctx, req)
}
