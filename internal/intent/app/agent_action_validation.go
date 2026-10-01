package app

import (
	"context"
	"errors"
	"fmt"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/protobuf/proto"
)

var ErrAgentActionDefinition = errors.New("app: agent action does not match the served intent definition")
var ErrAgentActionDraft = errors.New("app: agent action draft is not admissible")

// ValidateAgentIntentDraft resolves the exact served definition and uses the
// ordinary kernel, authorization and domain input owners before any draft is
// recorded. The pinned definition includes schemas, governance, capabilities,
// effects, risk and approval requirements; no private agent catalog is copied.
func (s *IntentService) ValidateAgentIntentDraft(ctx context.Context, pinned *intentsv1.IntentDefinition, req *intentsv1.CreateIntentRequest) error {
	if s == nil || pinned == nil || req == nil {
		return ErrAgentActionDraft
	}
	principal, inv, ownedErr := caller(ctx)
	if ownedErr != nil {
		return ownedErr
	}
	if principal.SubjectKind() != trust.SubjectKindHuman || !principal.Assurance().AtLeast(trust.AssuranceLow) {
		return ErrAgentActionDraft
	}
	def, ownedErr := s.resolveDefinition(req.GetDefinition(), true)
	if ownedErr != nil {
		return ownedErr
	}
	published, err := protomap.DefinitionToProto(def)
	if err != nil {
		return err
	}
	if !proto.Equal(published, pinned) {
		return ErrAgentActionDefinition
	}
	if req.GetInitiator().GetPrincipalId() != principal.Subject() || req.GetInitiator().GetKind() != intentsv1.InitiatorKind_INITIATOR_KIND_HUMAN || req.GetInitiator().GetIdentityAssuranceRef() != principal.EvidenceID() {
		return ErrAgentActionDraft
	}
	if scope := req.GetScope(); scope != nil && (scope.GetTenantId() != principal.Tenant().String() || scope.GetOrganizationScopeId() != principal.OrganizationScopeID() || scope.GetPurpose() != purposeOf(principal, inv)) {
		return ErrAgentActionDraft
	}
	spec, ownedErr := s.specFor(req, def, principal, inv)
	if ownedErr != nil {
		return ownedErr
	}
	if spec.Request.Schema != def.InputSchema {
		return ErrAgentActionDefinition
	}
	if err := s.denyIntentAction(ctx, principal, purposeOf(principal, inv), def, spec.Tenant, spec.Subjects, "", s.clock()); err != nil {
		return err
	}
	// Validation identities are never persisted. The normal owner resolver needs
	// an instance to validate required arguments and current subject references.
	instance, _, err := intent.NewInstance(spec, def, s.digester, func() (string, error) { return "4c56fa83-2d76-47bb-9d15-b5b7e717e031", nil }, s.clock)
	if err != nil {
		return kernelRejection(err)
	}
	if s.inputs == nil {
		return ErrAgentActionDraft
	}
	if _, err := s.inputs.Resolve(ctx, ResolveRequest{Instance: instance, Definition: def, Principal: principal, Purpose: purposeOf(principal, inv)}); err != nil {
		return fmt.Errorf("%w: %w", ErrAgentActionDraft, err)
	}
	return nil
}

// AgentIntentObservation includes the owner's terminal references which are
// projected beside the immutable creation envelope rather than in wire fields.
type AgentIntentObservation struct {
	Instance   *intentsv1.IntentInstance
	ReceiptRef string
	RepairRef  string
}

func (s *IntentService) ObserveAgentIntent(ctx context.Context, intentID string) (AgentIntentObservation, error) {
	_, err := s.GetIntent(ctx, &intentsv1.GetIntentRequest{IntentId: intentID})
	if err != nil {
		return AgentIntentObservation{}, err
	}
	p, _, ownedErr := caller(ctx)
	if ownedErr != nil {
		return AgentIntentObservation{}, ownedErr
	}
	inst, _, ownedErr := s.loadInstance(ctx, p.Tenant().String(), intentID)
	if ownedErr != nil {
		return AgentIntentObservation{}, ownedErr
	}
	latest, err := protomap.InstanceToProto(inst)
	if err != nil {
		return AgentIntentObservation{}, err
	}
	return AgentIntentObservation{Instance: latest, ReceiptRef: inst.CommitReceiptRef, RepairRef: inst.RepairRef}, nil
}
