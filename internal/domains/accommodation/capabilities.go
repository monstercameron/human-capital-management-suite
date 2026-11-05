package accommodation

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// Capability identities: the six governed operations on an accommodation
// case. Records mutate only through these handlers.
const (
	CapRecordRequest  = "hcmnext.accommodation.record_request"
	CapRecordDialogue = "hcmnext.accommodation.record_dialogue"
	CapConsiderOption = "hcmnext.accommodation.consider_option"
	CapRecordDecision = "hcmnext.accommodation.record_decision"
	CapRecordReview   = "hcmnext.accommodation.record_review"
	CapProjectCase    = "hcmnext.accommodation.project_case"
)

// Scopes the gateway requires on the caller's authorization decision.
const (
	ScopeRead  = "scope:accommodation.read"
	ScopeWrite = "scope:accommodation.write"
)

func accommodationSchema(id, slot string) capability.SchemaRef {
	return capability.SchemaRef{
		SchemaID:         id + "." + slot + "/v1",
		Version:          1,
		ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition",
	}
}

func accommodationDefinition(id string, effect capability.EffectClass, read, write []string, idempotency, scope string) capability.Definition {
	return capability.Definition{
		ID:                   id,
		Version:              1,
		OwnerDomain:          "accommodation",
		RequestSchema:        accommodationSchema(id, "request"),
		ResponseSchema:       accommodationSchema(id, "response"),
		ErrorSchema:          accommodationSchema(id, "error"),
		EffectClass:          effect,
		ReadData:             capability.DataDomainFieldSet{DataDomains: read},
		WriteData:            capability.DataDomainFieldSet{DataDomains: write},
		RiskClass:            "HIGH",
		IdempotencyPolicyRef: idempotency,
		AgentEligible:        false,
		AuthZScopeRef:        scope,
		LegalBasisRef:        "legal.accommodation.interactive-process.v1",
		EntitlementRef:       "entitlement.accommodation.case.v1",
		SLOClassRef:          "slo.interactive.p95-2s.v1",
		TestRef:              "conformance:" + id + "/v1",
	}
}

// Definitions returns the six governed accommodation operations. The five
// mutations declare internal-mutation effects; projection is read-only.
func Definitions() []capability.Definition {
	return []capability.Definition{
		accommodationDefinition(CapRecordRequest, capability.EffectInternalMutation,
			[]string{"case"}, []string{"case"}, "idempotency.effect-key.v1", ScopeWrite),
		accommodationDefinition(CapRecordDialogue, capability.EffectInternalMutation,
			[]string{"case"}, []string{"case"}, "idempotency.effect-key.v1", ScopeWrite),
		accommodationDefinition(CapConsiderOption, capability.EffectInternalMutation,
			[]string{"case"}, []string{"case"}, "idempotency.effect-key.v1", ScopeWrite),
		accommodationDefinition(CapRecordDecision, capability.EffectInternalMutation,
			[]string{"case"}, []string{"case"}, "idempotency.effect-key.v1", ScopeWrite),
		accommodationDefinition(CapRecordReview, capability.EffectInternalMutation,
			[]string{"case"}, []string{"case"}, "idempotency.effect-key.v1", ScopeWrite),
		accommodationDefinition(CapProjectCase, capability.EffectReadOnly,
			[]string{"case", "medical"}, nil, "idempotency.read-safe.v1", ScopeRead),
	}
}

// RecordRequestPayload opens a case: the sealed request plus the wall it
// opens under. The wall is caller-declared and policy-validated, never
// inferred.
type RecordRequestPayload struct {
	Request        Request
	Classification string
	Purpose        string
	Participants   []runtime.RunParticipant
}

// RecordDialoguePayload appends one dialogue entry.
type RecordDialoguePayload struct {
	CaseID string
	Entry  DialogueEntry
}

// ConsiderOptionPayload records one evaluated option.
type ConsiderOptionPayload struct {
	CaseID string
	Option Option
}

// RecordDecisionPayload records the decision.
type RecordDecisionPayload struct {
	CaseID   string
	Decision Decision
}

// RecordReviewPayload records a review: scheduled when CompletedAt is zero,
// completed otherwise.
type RecordReviewPayload struct {
	CaseID string
	Review Review
}

// ProjectCasePayload renders one viewer's case projection.
type ProjectCasePayload struct {
	CaseID  string
	Viewer  *trust.Principal
	Purpose string
}

// RequestHandler opens cases from sealed requests.
func RequestHandler(s *Store) capability.Handler {
	return func(_ context.Context, payload any) (any, error) {
		p, ok := payload.(RecordRequestPayload)
		if !ok {
			return nil, fmt.Errorf("%w: record_request takes RecordRequestPayload", ErrInvalidRecord)
		}
		policy, err := runtime.NewOpenRunAccess(p.Classification, p.Purpose)
		if err != nil {
			return nil, err
		}
		walled, err := policy.Wall(p.Participants...)
		if err != nil {
			return nil, err
		}
		cs, err := OpenCase(p.Request, walled)
		if err != nil {
			return nil, err
		}
		if err := s.Create(*cs); err != nil {
			return nil, err
		}
		return *cs, nil
	}
}

// DialogueHandler appends dialogue entries.
func DialogueHandler(s *Store) capability.Handler {
	return func(_ context.Context, payload any) (any, error) {
		p, ok := payload.(RecordDialoguePayload)
		if !ok {
			return nil, fmt.Errorf("%w: record_dialogue takes RecordDialoguePayload", ErrInvalidRecord)
		}
		cs, ok := s.Get(p.CaseID)
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnknownCase, p.CaseID)
		}
		if err := cs.RecordDialogue(p.Entry); err != nil {
			return nil, err
		}
		if err := s.Update(cs); err != nil {
			return nil, err
		}
		return cs, nil
	}
}

// OptionHandler records considered options.
func OptionHandler(s *Store) capability.Handler {
	return func(_ context.Context, payload any) (any, error) {
		p, ok := payload.(ConsiderOptionPayload)
		if !ok {
			return nil, fmt.Errorf("%w: consider_option takes ConsiderOptionPayload", ErrInvalidRecord)
		}
		cs, ok := s.Get(p.CaseID)
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnknownCase, p.CaseID)
		}
		if err := cs.ConsiderOption(p.Option); err != nil {
			return nil, err
		}
		if err := s.Update(cs); err != nil {
			return nil, err
		}
		return cs, nil
	}
}

// DecisionHandler records the decision.
func DecisionHandler(s *Store) capability.Handler {
	return func(_ context.Context, payload any) (any, error) {
		p, ok := payload.(RecordDecisionPayload)
		if !ok {
			return nil, fmt.Errorf("%w: record_decision takes RecordDecisionPayload", ErrInvalidRecord)
		}
		cs, ok := s.Get(p.CaseID)
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnknownCase, p.CaseID)
		}
		if err := cs.Decide(p.Decision); err != nil {
			return nil, err
		}
		if err := s.Update(cs); err != nil {
			return nil, err
		}
		return cs, nil
	}
}

// ReviewHandler records scheduled or completed reviews.
func ReviewHandler(s *Store) capability.Handler {
	return func(_ context.Context, payload any) (any, error) {
		p, ok := payload.(RecordReviewPayload)
		if !ok {
			return nil, fmt.Errorf("%w: record_review takes RecordReviewPayload", ErrInvalidRecord)
		}
		cs, ok := s.Get(p.CaseID)
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnknownCase, p.CaseID)
		}
		if p.Review.CompletedAt == 0 {
			if err := cs.RecordReview(p.Review); err != nil {
				return nil, err
			}
		} else if err := cs.CompleteReview(p.Review); err != nil {
			return nil, err
		}
		if err := s.Update(cs); err != nil {
			return nil, err
		}
		return cs, nil
	}
}

// ProjectHandler renders viewer projections.
func ProjectHandler(s *Store) capability.Handler {
	return func(_ context.Context, payload any) (any, error) {
		p, ok := payload.(ProjectCasePayload)
		if !ok {
			return nil, fmt.Errorf("%w: project_case takes ProjectCasePayload", ErrInvalidRecord)
		}
		cs, ok := s.Get(p.CaseID)
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnknownCase, p.CaseID)
		}
		return ProjectCase(cs, p.Viewer, p.Purpose)
	}
}

// RegisterInto publishes the six operations bound to handlers over s. It
// fails closed on a nil store: capabilities without state resolve nowhere.
func RegisterInto(r *capability.Registry, s *Store) error {
	if r == nil || s == nil {
		return fmt.Errorf("%w: registry and store are required", ErrInvalidRecord)
	}
	bound := map[string]capability.Handler{
		CapRecordRequest:  RequestHandler(s),
		CapRecordDialogue: DialogueHandler(s),
		CapConsiderOption: OptionHandler(s),
		CapRecordDecision: DecisionHandler(s),
		CapRecordReview:   ReviewHandler(s),
		CapProjectCase:    ProjectHandler(s),
	}
	for _, d := range Definitions() {
		if err := r.Register(d, bound[d.ID]); err != nil {
			return err
		}
	}
	return nil
}
