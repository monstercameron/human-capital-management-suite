package admin

import (
	"context"
	"strconv"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/authzsim"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/explorer"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// ListLedgerEvents is REV-037-01's operator stream listing: it calls
// internal/operations/explorer.StreamListing unchanged and maps the view
// onto the wire, preserving the explorer's redaction flags field for field.
func (s *server) ListLedgerEvents(ctx context.Context, req *adminv1.ListLedgerEventsRequest) (*adminv1.ListLedgerEventsResponse, error) {
	principal, inv, opErr := requireOperator(ctx)
	if opErr != nil {
		return nil, opErr
	}
	if s.deps.LedgerQuerier == nil {
		return nil, envelope.New(envelope.CodeUnavailable,
			"admin.ledger_querier_unconfigured",
			"the ledger read port is not configured").
			WithCorrelation(inv.RequestID()).
			WithEvidence(envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"})
	}
	tenant, err := uuid.Parse(req.GetTenantId())
	if err != nil {
		return nil, envelope.New(envelope.CodeInvalidArgument,
			"admin.ledger_tenant_invalid",
			"tenant_id is not a UUID").
			WithCorrelation(inv.RequestID()).
			WithEvidence(envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"})
	}
	view, err := explorer.StreamListing(ctx, s.deps.LedgerQuerier, tenant, req.GetStreamKey(), nil)
	if err != nil {
		return nil, envelope.Coerce(err)
	}
	out := &adminv1.ListLedgerEventsResponse{Digest: view.Digest, EvidenceRef: evidenceRef(principal, "admin.list_ledger_events")}
	for _, ev := range view.Events {
		out.Events = append(out.Events, &adminv1.LedgerEventProfile{
			TenantId:        ev.Tenant.String(),
			StreamKey:       ev.StreamKey,
			Sequence:        ev.Sequence,
			EventId:         ev.EventID.String(),
			AssertionClass:  ev.AssertionClass,
			SchemaRef:       ev.SchemaRef,
			Digest:          ev.Digest,
			DigestAlgorithm: ev.DigestAlgorithm,
			OccurredAt:      timestamppb.New(ev.OccurredAt),
			EffectiveAt:     timestamppb.New(ev.EffectiveAt),
			RecordedAt:      timestamppb.New(ev.RecordedAt),
			CorrelationId:   ev.CorrelationID.String(),
			IdempotencyKey:  ev.IdempotencyKey,
			Authority:       ev.Authority,
			SourceRef:       ev.SourceRef,
			Payload:         append([]byte(nil), ev.Payload...),
			ArtifactRef:     ev.ArtifactRef,
			PayloadWithheld: ev.PayloadWithheld,
			SubjectWithheld: ev.SubjectWithheld,
		})
	}
	return out, nil
}

// GetChainVerification is REV-037-01's operator chain replay: it calls
// internal/operations/explorer.VerifyChain unchanged. Verification metadata
// is never redacted: an operator diagnosing a reported tamper sees the
// exact break sequence even when event payloads are denied.
func (s *server) GetChainVerification(ctx context.Context, req *adminv1.GetChainVerificationRequest) (*adminv1.GetChainVerificationResponse, error) {
	principal, inv, opErr := requireOperator(ctx)
	if opErr != nil {
		return nil, opErr
	}
	if s.deps.ChainQuerier == nil || s.deps.ChainDigester == nil {
		return nil, envelope.New(envelope.CodeUnavailable,
			"admin.chain_verifier_unconfigured",
			"the chain verification port is not configured").
			WithCorrelation(inv.RequestID()).
			WithEvidence(envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"})
	}
	tenant, err := uuid.Parse(req.GetTenantId())
	if err != nil {
		return nil, envelope.New(envelope.CodeInvalidArgument,
			"admin.ledger_tenant_invalid",
			"tenant_id is not a UUID").
			WithCorrelation(inv.RequestID()).
			WithEvidence(envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"})
	}
	view, err := explorer.VerifyChain(ctx, s.deps.ChainQuerier, s.deps.ChainDigester, tenant, req.GetStreamKey())
	if err != nil {
		return nil, envelope.Coerce(err)
	}
	out := &adminv1.GetChainVerificationResponse{
		StreamKey:   view.StreamKey,
		Verified:    view.Verified,
		Empty:       view.Empty,
		EvidenceRef: evidenceRef(principal, "admin.get_chain_verification"),
	}
	if view.Verified {
		out.Head = &adminv1.ChainHeadProfile{
			StreamKey: view.Head.StreamKey,
			Sequence:  view.Head.Sequence,
			ChainHash: view.Head.ChainHash,
			Algorithm: view.Head.Algorithm,
		}
	}
	if view.Broken != nil {
		out.BrokenSequence = strconv.FormatInt(view.Broken.Sequence, 10)
		out.BrokenReason = view.Broken.Reason
	}
	return out, nil
}

// SimulateAuthorization is REV-037-01's operator AuthZ simulator: it calls
// internal/operations/authzsim.Simulate unchanged, simulating the caller's
// own principal (taken from the trusted context, never from the request)
// against the named subject, purpose and fields.
func (s *server) SimulateAuthorization(ctx context.Context, req *adminv1.SimulateAuthorizationRequest) (*adminv1.SimulateAuthorizationResponse, error) {
	principal, inv, opErr := requireOperator(ctx)
	if opErr != nil {
		return nil, opErr
	}
	subjectTenant, err := uuid.Parse(req.GetSubjectTenantId())
	if err != nil {
		return nil, envelope.New(envelope.CodeInvalidArgument,
			"admin.simulate_subject_tenant_invalid",
			"subject_tenant_id is not a UUID").
			WithCorrelation(inv.RequestID()).
			WithEvidence(envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"})
	}
	kind := values.Kind(req.GetSubjectKind())
	if err := kind.Validate(); err != nil {
		return nil, envelope.New(envelope.CodeInvalidArgument,
			"admin.simulate_subject_kind_invalid",
			"subject_kind is not a known entity kind").
			WithCorrelation(inv.RequestID()).
			WithEvidence(envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"})
	}
	fields := make([]authz.FieldID, 0, len(req.GetFields()))
	for _, name := range req.GetFields() {
		fields = append(fields, authz.FieldID(name))
	}
	res, err := authzsim.Simulate(authz.Request{
		Principal: principal,
		Purpose:   req.GetPurpose(),
		Subject: values.EntityRef{
			Tenant: values.TenantId(subjectTenant.String()),
			Kind:   kind,
			Id:     req.GetSubjectId(),
		},
		Fields: fields,
	}, req.GetPolicyVersion())
	if err != nil {
		return nil, envelope.Coerce(err)
	}
	rulings := make(map[string]string, len(res.Decision.Fields))
	for field, ruling := range res.Decision.Fields {
		rulings[string(field)] = ruling.Effect.String()
	}
	return &adminv1.SimulateAuthorizationResponse{
		SubjectDisclosable: res.Decision.SubjectDisclosable,
		DenialReason:       res.Decision.SubjectDenialReason,
		TenantEffect:       res.Decision.Tenant.Effect.String(),
		ScopeEffect:        res.Decision.Scope.Effect.String(),
		FieldRulings:       rulings,
		MatchedRules:       append([]string(nil), res.Decision.MatchedRules...),
		PolicyVersionMatch: res.PolicyVersionMatch,
		Explanation:        res.Explanation,
		Digest:             res.Digest,
		EvidenceRef:        evidenceRef(principal, "admin.simulate_authorization"),
	}, nil
}
