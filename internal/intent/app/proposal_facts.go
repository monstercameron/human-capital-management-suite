package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection/critical"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// WF-RUN-027's durable half: the adapter [runtime.Start] resolves a bound
// proposal revision's approval and currency through, and the two recorders
// that put the facts it reads into the database in the first place.
//
// # Why this exists
//
// Before this file, internal/workflow/runtime.Start could only learn "this
// revision is approved" from [runtime.ProposalBinding.Approved] -- a boolean
// its own caller set -- because no durable approval-decision table existed.
// Migration 00024 materialized intent_decision (a decision bound to an exact
// proposal revision, its material digest and the control context it was seen
// under) and intent_relationship (typed SUPERSEDES edges), and
// internal/data/intentcontrol is the store over them. [DurableProposalFacts]
// is the adapter that turns those two tables into the two consumer-owned
// ports runtime declares, so a caller can no longer assert its way past
// either check: an Execute presenting Approved=true with no recorded decision
// is [runtime.CodeUnapprovedProposal], and a revision the relationship graph
// has superseded is [runtime.CodeSupersededProposal] whatever the caller says.
//
// # Why it lives here rather than in internal/platform/execution
//
// WF-RUN-027's REFACTOR clause puts the adapters in internal/platform/execution.
// This package is where the caller that constructs the [runtime.StartRequest]
// lives ([IntentService.executionStart]) and where the two recorders have to
// run -- inside the journey's own tenant-scoped transactions -- so keeping the
// reader beside them is what makes "what Start reads" and "what the journey
// writes" one reviewable pair rather than two halves that can drift. Moving
// the reader to internal/platform/execution later is a rename: nothing here
// depends on this package's own state.

// executionProposalSchemaRef names the schema the materialized
// proposal_revision payload validates against. It is the canonical proposal
// message this cell's simulation produces.
const executionProposalSchemaRef = critical.SchemaRefFullProposalSnapshot

// executionAuthorityRequirementID is the requirement id the execution
// authority gate's own AUTHZ decision is recorded under. It is the same rule
// reference the gate's refusals name, so a reader of intent_decision and a
// reader of a refusal envelope are looking at one identifier.
const executionAuthorityRequirementID = ruleExecutionAuthorityGate

// executionDecisionNamespace is the fixed UUIDv5 namespace every decision id
// this package mints is derived under. Deriving rather than allocating is what
// makes re-recording the same decision collide on intent_decision's primary
// key -- an idempotent no-op -- instead of minting a second vote.
var executionDecisionNamespace = uuid.MustParse("6b1f2d84-9c37-4a15-8e63-0d5a7c9b21ef")

// ExecutionFacts is the pair of consumer-owned ports
// [internal/workflow/runtime.Start] resolves a bound proposal revision
// through. A composition that supplies one supplies both, because Start
// refuses a request carrying only one of them.
type ExecutionFacts interface {
	runtime.ProposalFacts
	runtime.ApprovalFacts
}

// AcceptedActionLookup is the exact prepared-plan identity the execution
// commit boundary asks durable acceptance facts to resolve.
type AcceptedActionLookup struct {
	Tenant             values.TenantId
	TenantID           uuid.UUID
	IntentID           string
	ActionID           string
	ProposalRevisionID string
	ProposalDigest     string
	IdempotencyKey     string
}

// AcceptedActionFacts is the service-owned port for reading a durable action
// acceptance inside the transaction which prepares and commits its plan.
type AcceptedActionFacts interface {
	ResolveAcceptedAction(context.Context, intentcontrol.Executor, AcceptedActionLookup) (AcceptedAction, error)
}

var _ AcceptedActionFacts = DurableProposalFacts{}

// ResolveAcceptedAction reads the approved action row linked to the exact
// durable HUMAN_APPROVAL decision. The only caller supplies plan coordinates;
// actor, acceptance time and decision identity are read from stored facts.
func (DurableProposalFacts) ResolveAcceptedAction(
	ctx context.Context, ex intentcontrol.Executor, q AcceptedActionLookup,
) (AcceptedAction, error) {
	intentID, err := executionIntentUUID(q.IntentID)
	if err != nil {
		return AcceptedAction{}, err
	}
	stored, err := (intentcontrol.AcceptedActionStore{}).ForPlan(
		ctx, ex, q.TenantID, intentID, q.ActionID, q.ProposalRevisionID,
		q.ProposalDigest, q.IdempotencyKey)
	if err != nil {
		return AcceptedAction{}, err
	}
	return AcceptedAction{
		Tenant: q.Tenant, DecisionID: stored.DecisionID.String(), ActionID: stored.ActionID,
		IntentID: stored.IntentID.String(), ProposalRevisionID: stored.ProposalRevisionID,
		ProposalDigest: stored.ProposalDigest, AcceptedBy: stored.AcceptedBy,
		AcceptedAt: values.NewInstant(stored.AcceptedAt), IdempotencyKey: stored.IdempotencyKey,
	}, nil
}

type fullProposalVerifier struct{ digester intent.Digester }

func (v fullProposalVerifier) VerifyProposalDigest(p intent.ProposalRevision) error {
	if v.digester == nil {
		return fmt.Errorf("proposal digester required")
	}
	r, err := v.digester.ProposalDigest(p)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(r, p.MaterialDigest) {
		return fmt.Errorf("material digest reference mismatch")
	}
	return nil
}

// DurableProposalFacts reads WF-RUN-027's two facts out of migration 00024's
// tables through internal/data/intentcontrol.
//
// It holds no state and opens no connection: both methods run on the
// [runtime.Executor] Start hands them, which is the very transaction Start is
// fencing. That is what makes "the decision that admits this start" and "the
// start" one atomic read -- a decision inserted by a concurrent writer after
// the check could not admit a start that already committed.
type DurableProposalFacts struct{}

var _ ExecutionFacts = DurableProposalFacts{}

// Supersession implements [runtime.ProposalFacts] over intent_relationship.
//
// A revision whose intent carries an inbound SUPERSEDES edge is superseded,
// whatever the caller asserted. CurrentRevision is deliberately left nil: the
// only consumer that reads it is internal/workflow/execute.CurrencyGuard,
// which treats a nil current revision as a material change and blocks -- the
// safe answer for a store that records supersession between intents without
// also carrying the superseding intent's own proposal content.
func (DurableProposalFacts) Supersession(
	ctx context.Context, ex runtime.Executor, tenantID uuid.UUID, rev intent.ProposalRevision,
) (runtime.ProposalSupersessionFact, error) {
	intentID, err := executionIntentUUID(rev.IntentID)
	if err != nil {
		return runtime.ProposalSupersessionFact{}, err
	}
	by, superseded, err := (intentcontrol.RelationshipStore{}).SupersededBy(ctx, ex, tenantID, intentID)
	if err != nil {
		return runtime.ProposalSupersessionFact{}, err
	}
	if !superseded {
		return runtime.ProposalSupersessionFact{}, nil
	}
	return runtime.ProposalSupersessionFact{
		Superseded:             true,
		SupersededByRevisionID: "intent:" + by.String(),
	}, nil
}

// Decisions implements [runtime.ApprovalFacts] over intent_decision.
//
// Every decision recorded against this exact (intent, revision) is returned
// whole, including the material digest it was bound to: Start refuses
// [runtime.CodeApprovalBindingMismatch] the instant a decision names a digest
// that is not the started revision's own, so a decision recorded against an
// earlier content of the same revision id can never admit a later one.
//
// Invalidated is always false here. intent_decision is append-only and has no
// invalidation column: INTENT-006's materiality assessment invalidates an
// approval by superseding the revision it was bound to, which this adapter
// already reports through [DurableProposalFacts.Supersession], and a
// re-bound decision is a new row with a new digest rather than a mutated one.
func (DurableProposalFacts) Decisions(
	ctx context.Context, ex runtime.Executor, tenantID uuid.UUID, rev intent.ProposalRevision,
) ([]runtime.ApprovalDecisionFact, error) {
	intentID, err := executionIntentUUID(rev.IntentID)
	if err != nil {
		return nil, err
	}
	revision := rev.Revision
	if revision == 0 {
		revision = simulationRevision
	}
	stored, err := (intentcontrol.DecisionStore{}).ForRevision(ctx, ex, tenantID, intentID, revision)
	if err != nil {
		return nil, err
	}
	facts := make([]runtime.ApprovalDecisionFact, 0, len(stored))
	for _, d := range stored {
		facts = append(facts, runtime.ApprovalDecisionFact{
			DecisionID:     d.DecisionID.String(),
			Outcome:        runtime.ApprovalOutcome(d.Outcome),
			ProposalDigest: d.ProposalDigest,
		})
	}
	return facts, nil
}

// executionIntentUUID parses an intent id onto the uuid every intent-control
// table keys on. A cell whose intent ids are not uuids cannot record or read
// these facts at all, and saying so is better than reading an empty decision
// set and calling the proposal unapproved.
func executionIntentUUID(intentID string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(intentID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("app: intent id %q is not a uuid the intent-control tables can key on: %w",
			intentID, err)
	}
	return parsed, nil
}

// executionDecision is one decision this package records against an exact
// proposal revision, together with the revision row it has to exist beside.
type executionDecision struct {
	TenantID uuid.UUID
	IntentID uuid.UUID
	Revision uint64

	// MaterialDigest is the revision's own minted material digest. It is
	// written to proposal_revision's proposal_digest and material_digest
	// alike, and to the decision's proposal_digest, because that single
	// digest is what runtime.Start compares a decision's binding against.
	MaterialDigest string
	ControlDigest  string

	RequirementID    string
	Kind             string
	Outcome          string
	DecidedBy        string
	AuthorityRef     string
	Reason           string
	DecidedAt        time.Time
	Proposal         *intent.ProposalRevision
	ProposalVerifier intent.Digester
	TenantUUID       func(values.TenantId) uuid.UUID
}

// decisionID derives this decision's identity from the tuple that defines it,
// so re-recording the same decision is an idempotent no-op rather than a
// second vote.
func (d executionDecision) decisionID() uuid.UUID {
	name := strings.Join([]string{
		d.TenantID.String(), d.IntentID.String(), fmt.Sprint(d.Revision),
		d.RequirementID, d.DecidedBy, d.Outcome, d.MaterialDigest,
	}, "\x00")
	return uuid.NewSHA1(executionDecisionNamespace, []byte(name))
}

// record materializes the proposal_revision row this decision hangs off and
// then inserts the decision, on the caller's own transaction.
//
// Both writes are idempotent. proposal_revision is append-only and
// intent_decision allows one vote per (revision, requirement, principal), so a
// replayed journey step re-derives the same identities and changes nothing;
// [intentcontrol.ErrDuplicate] is that outcome, not a failure.
func (d executionDecision) record(ctx context.Context, tx dbport.Tx) error {
	if d.Proposal == nil {
		return fmt.Errorf("app: complete proposal revision snapshot is required")
	}
	p := *d.Proposal
	if d.TenantUUID == nil || d.TenantUUID(p.Tenant) != d.TenantID || p.IntentID != d.IntentID.String() || p.Revision != d.Revision || p.MaterialDigest.Digest != d.MaterialDigest {
		return fmt.Errorf("app: proposal revision snapshot does not match decision binding")
	}
	if d.ProposalVerifier == nil {
		return fmt.Errorf("app: proposal revision verifier is required")
	}
	ref, err := d.ProposalVerifier.ProposalDigest(p)
	if err != nil {
		return fmt.Errorf("app: proposal revision material digest verification failed: %w", err)
	}
	if !reflect.DeepEqual(ref, p.MaterialDigest) {
		return fmt.Errorf("app: proposal revision material digest verification failed: digest reference mismatch")
	}
	if err := recordProposalRevision(ctx, tx, d.TenantID, p, d.ProposalVerifier); err != nil {
		return fmt.Errorf("app: record the proposal revision through the ledger: %w", err)
	}
	stored, err := (intentcontrol.RevisionStore{}).Load(ctx, tx, d.TenantID, d.IntentID, d.Revision)
	if err != nil {
		return fmt.Errorf("app: read back the proposal revision: %w", err)
	}
	if stored.TenantID != d.TenantID || stored.IntentID != d.IntentID || stored.Revision != d.Revision || stored.ProposalDigest != d.MaterialDigest || stored.MaterialDigest != d.MaterialDigest || stored.SchemaRef != executionProposalSchemaRef {
		return fmt.Errorf("app: stored proposal revision identity or digest does not match decision binding")
	}
	storedProposal, err := intentcontrol.DecodeFullProposal(stored.Payload, fullProposalVerifier{d.ProposalVerifier})
	if err != nil {
		return fmt.Errorf("app: legacy proposal revision cannot be resumed: %w", err)
	}
	if !bytes.Equal(storedProposal.MaterialPayload().WireBytes, p.MaterialPayload().WireBytes) {
		return fmt.Errorf("app: stored proposal revision material does not match the decision snapshot")
	}

	decision := intentcontrol.Decision{
		TenantID:         d.TenantID,
		DecisionID:       d.decisionID(),
		IntentID:         d.IntentID,
		Revision:         d.Revision,
		RequirementID:    d.RequirementID,
		Kind:             d.Kind,
		Outcome:          d.Outcome,
		ProposalDigest:   d.MaterialDigest,
		ControlDigest:    d.ControlDigest,
		MaterialityClass: intentcontrol.Material,
		DecidedBy:        d.DecidedBy,
		AuthorityRef:     d.AuthorityRef,
		Reason:           d.Reason,
		DecidedAt:        d.DecidedAt,
	}
	err = (intentcontrol.DecisionStore{}).Record(ctx, tx, decision)
	if err != nil && !errors.Is(err, intentcontrol.ErrDuplicate) {
		return fmt.Errorf("app: record the %s decision: %w", d.Kind, err)
	}
	if d.Kind == intentcontrol.DecisionHumanApproval && d.Outcome == intentcontrol.OutcomeApproved {
		if _, acceptanceErr := (intentcontrol.AcceptedActionStore{}).RecordApprovedDecision(
			ctx, tx, decision, p.ProposalRevisionID, AcceptedIntentExecutionActionID,
			acceptedExecutionIdempotencyKey(p.IntentID, p.ProposalRevisionID)); acceptanceErr != nil {
			return fmt.Errorf("app: record accepted intent action: %w", acceptanceErr)
		}
	}
	return nil
}

// acceptedExecutionIdempotencyKey is the server-derived semantic key granted
// with a durable approval. It is the exact START key ExecuteIntent later uses,
// so a plan cannot substitute a caller-selected key after acceptance.
func acceptedExecutionIdempotencyKey(intentID, proposalRevisionID string) string {
	return "execute:" + intentID + ":" + proposalRevisionID
}

// controlSnapshotDigest mints the 64-hex control_digest an intent-control row
// pins, over exactly the thirteen control snapshots an intent instance
// carries, in a fixed field order.
//
// It is deterministic and total: two decisions recorded under the same control
// context digest to the same value, and a cell that pins no snapshots at all
// still produces a well-formed digest rather than a constraint violation.
func controlSnapshotDigest(c intent.ControlSnapshots) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		c.CapabilityRegistryDigest,
		c.PolicyBundleDigest,
		c.LegalContextDigest,
		c.EntitlementDigest,
		c.ReferenceDataDigest,
		c.WorkflowDefinitionDigest,
		c.ConnectorConfigurationDigest,
		c.ClassificationTaxonomyDigest,
		c.ClassificationLabelSetDigest,
		c.ClassificationPropagationWatermark,
		c.DLPDecisionDigest,
		c.DestinationTrustDigest,
		c.PurposeAndResidencyDigest,
	}, "\x00")))
	return hex.EncodeToString(sum[:])
}

// nonEmptyReason keeps a decision's stored reason non-blank: intent_decision's
// decision_reason is required, and a decision recorded with an empty reason
// would be refused by the store's own validation rather than by anything the
// caller could act on.
func nonEmptyReason(reason, fallback string) string {
	if trimmed := strings.TrimSpace(reason); trimmed != "" {
		return trimmed
	}
	return fallback
}

// executionFactsFor is the composition rule [NewCell] applies: a cell handed
// the execution database can read WF-RUN-027's durable facts out of it, and a
// cell without one cannot.
//
// It returns a nil interface rather than a typed-nil so that
// [IntentService.executionStart]'s own nil check is meaningful, which is the
// same reason [NewCell] leaves Cell.Journey unassigned instead of storing a
// nil *journeyEngine.
func executionFactsFor(cfg CellConfig) ExecutionFacts {
	if cfg.ExecutionDB == nil || cfg.TenantUUID == nil {
		return nil
	}
	return DurableProposalFacts{}
}

func executionFactsForConfig(cfg CellConfig) ExecutionFacts {
	if cfg.ExecutionFacts != nil {
		return cfg.ExecutionFacts
	}
	return executionFactsFor(cfg)
}

// proposalFactsOf and approvalFactsOf project an [ExecutionFacts] onto the two
// ports runtime.StartRequest declares separately, mapping a nil pair onto two
// nil ports rather than onto two non-nil interfaces wrapping a nil value.
//
// runtime.Start refuses a request that supplies one port without the other, so
// the projection has to be all-or-nothing; doing it in one place is what makes
// that impossible to get half right at a call site.
func proposalFactsOf(facts ExecutionFacts) runtime.ProposalFacts {
	if facts == nil {
		return nil
	}
	return facts
}

func approvalFactsOf(facts ExecutionFacts) runtime.ApprovalFacts {
	if facts == nil {
		return nil
	}
	return facts
}

// decisionRef is the authority reference an AUTHZ decision this gate admitted
// is recorded under: the signed P1B authority amendment the gate was
// configured with.
//
// intent_decision.authority_ref is a non-blank semantic key, so a gate
// configured with no amendment digest still has to name something a reader can
// resolve; naming the rule itself says exactly as much as the configuration
// does, and no more.
func (a *ExecutionAuthority) decisionRef() string {
	if a == nil || strings.TrimSpace(a.AuthorityDigest) == "" {
		return ruleExecutionAuthorityGate
	}
	return strings.TrimSpace(a.AuthorityDigest)
}
