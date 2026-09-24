package crm

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var ErrCRM005Rejected = errors.New("CRM_005_REJECTED")

const (
	CRM005IntentType    = "hcm.recruiting.create_candidate"
	CRM005IntentVersion = "v1"
)

type ConversionRejection struct {
	Field, State string
	Version      values.RevisionToken
}

// CandidateConversionProposal is a zero-write preflight artifact. It is not
// evidence that identity was resolved or that recruiting created either role;
// those outcomes must come from their semantic owners.
type CandidateConversionProposal struct {
	Prospect             ProspectRevision
	ProposedCandidate    values.EntityRef
	ProposedApplication  values.EntityRef
	ProposedIdentityLink values.EntityRef
	IntentType           string
	IntentVersion        string
	At                   values.Instant
	Source               ProspectSourceAttribution
	Consent              ProspectConsent
}

func PrepareProspectConversion(p ProspectRevision, candidate, application, identityLink values.EntityRef, intentType, intentVersion string, at values.Instant) (CandidateConversionProposal, *ConversionRejection, error) {
	reject := func(field, state string) (CandidateConversionProposal, *ConversionRejection, error) {
		rejection := &ConversionRejection{Field: field, State: state, Version: p.Revision}
		return CandidateConversionProposal{}, rejection, fmt.Errorf("%w: field=%s state=%s version=%s", ErrCRM005Rejected, field, state, p.Revision.String())
	}
	if err := at.Validate(); err != nil {
		return reject("at", "INVALID")
	}
	if err := p.Validate(); err != nil {
		return reject("prospect", "INVALID")
	}
	if strings.TrimSpace(p.Attribution.Campaign) == "" {
		return reject("source.campaign", "UNBOUND")
	}
	if d := p.Consent.OutreachAt(at); !d.Allowed {
		return reject("consent", string(d.Status))
	}
	if err := candidate.Validate(); err != nil || candidate.Tenant != p.ProspectID.Tenant || candidate.Kind != "candidate" {
		return reject("candidate", "INVALID")
	}
	if err := application.Validate(); err != nil || application.Tenant != p.ProspectID.Tenant || application.Kind != "application" {
		return reject("application", "INVALID")
	}
	if err := identityLink.Validate(); err != nil || identityLink.Tenant != p.ProspectID.Tenant || identityLink.Kind != "identity_link" {
		return reject("identity_link", "INVALID")
	}
	if intentType != CRM005IntentType || intentVersion != CRM005IntentVersion {
		return reject("intent", "UNBOUND")
	}
	return CandidateConversionProposal{Prospect: p, ProposedCandidate: candidate, ProposedApplication: application, ProposedIdentityLink: identityLink, IntentType: intentType, IntentVersion: intentVersion, At: at, Source: p.Attribution, Consent: p.Consent}, nil, nil
}

// ConversionCommitFence binds a conversion write to the prospect revision
// observed during preflight. The writer must compare it atomically with the
// current revision before publishing any of the three role records.
type ConversionCommitFence struct {
	ProspectRevision values.RevisionToken
	Token            string
}

// ConversionWrite is the durable recruiting command produced by CRM.
type ConversionWrite struct {
	Proposal       CandidateConversionProposal
	IdempotencyKey string
	Fence          ConversionCommitFence
}

// ConversionResult reports the durable role references and whether this call
// replayed an earlier write with the same idempotency key and content.
type ConversionResult struct {
	Candidate, Application, IdentityLink values.EntityRef
	Replayed                             bool
}

// ConversionAuthority evaluates the governed intent and the proposal's
// consent evidence. Implementations must consult the current authority state.
type ConversionAuthority interface {
	AuthorizeProspectConversion(context.Context, CandidateConversionProposal) error
}

// ConversionWriter owns the atomic recruiting write. Implementations must
// enforce the fence and idempotency key transactionally, rejecting a reused
// key with different content and returning the original result on exact replay.
type ConversionWriter interface {
	CommitProspectConversion(context.Context, ConversionWrite) (ConversionResult, error)
}

// ExecuteProspectConversion validates the proposal and authority before
// calling the recruiting owner. The recruiting owner is responsible for
// atomically writing candidate, application and identity-link roles.
func ExecuteProspectConversion(ctx context.Context, proposal CandidateConversionProposal, authority ConversionAuthority, writer ConversionWriter, fence ConversionCommitFence, idempotencyKey string) (ConversionResult, error) {
	if err := validateConversionProposal(proposal); err != nil {
		return ConversionResult{}, err
	}
	if isNilConversionDependency(authority) || isNilConversionDependency(writer) || strings.TrimSpace(fence.Token) == "" || !fence.ProspectRevision.Equal(proposal.Prospect.Revision) || strings.TrimSpace(idempotencyKey) == "" {
		return ConversionResult{}, fmt.Errorf("%w: authority, writer, matching fence and idempotency key are required", ErrCRM005Rejected)
	}
	if err := authority.AuthorizeProspectConversion(ctx, proposal); err != nil {
		return ConversionResult{}, fmt.Errorf("%w: governed intent authorization rejected", ErrCRM005Rejected)
	}
	result, err := writer.CommitProspectConversion(ctx, ConversionWrite{Proposal: proposal, IdempotencyKey: idempotencyKey, Fence: fence})
	if err != nil {
		return ConversionResult{}, fmt.Errorf("%w: recruiting conversion commit failed", ErrCRM005Rejected)
	}
	if result.Candidate != proposal.ProposedCandidate || result.Application != proposal.ProposedApplication || result.IdentityLink != proposal.ProposedIdentityLink {
		return ConversionResult{}, fmt.Errorf("%w: recruiting conversion returned inconsistent roles", ErrCRM005Rejected)
	}
	return result, nil
}

func validateConversionProposal(p CandidateConversionProposal) error {
	if p.IntentType != CRM005IntentType || p.IntentVersion != CRM005IntentVersion || p.At.Validate() != nil || p.Prospect.Validate() != nil || !p.Prospect.Consent.OutreachAt(p.At).Allowed || strings.TrimSpace(p.Prospect.Attribution.Campaign) == "" || p.Source != p.Prospect.Attribution || p.Consent != p.Prospect.Consent {
		return fmt.Errorf("%w: invalid or altered conversion proposal", ErrCRM005Rejected)
	}
	for _, ref := range []struct {
		value values.EntityRef
		kind  string
	}{{p.ProposedCandidate, "candidate"}, {p.ProposedApplication, "application"}, {p.ProposedIdentityLink, "identity_link"}} {
		if ref.value.Validate() != nil || ref.value.Tenant != p.Prospect.ProspectID.Tenant || ref.value.Kind != values.Kind(ref.kind) {
			return fmt.Errorf("%w: invalid proposed role", ErrCRM005Rejected)
		}
	}
	return nil
}

func isNilConversionDependency(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}
