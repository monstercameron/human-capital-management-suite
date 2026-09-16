package execution

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/decision"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/revalidate"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// governanceProposal is the approved binding the governance record is taken
// for: the promotion's worker and target position, its effective interval and
// its material digest.
func governanceProposal(t *testing.T) runtime.ProposalBinding {
	t.Helper()
	start := values.NewInstant(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC))
	effective, err := values.NewOpenInstantInterval(start)
	if err != nil {
		t.Fatal(err)
	}
	return runtime.ProposalBinding{Revision: intent.ProposalRevision{
		IntentID: wfrun034Intent.String(), ProposalRevisionID: wfrun034Proposal.String(), Revision: 1,
		Tenant: "harborcare-demo", OrganizationScopeID: "org:harborcare-demo:people-ops",
		EffectiveTime: effective, MaterialDigest: digest.Reference{Digest: "sha256:material"},
		Subjects: []intent.SubjectReference{
			{Kind: "EMPLOYMENT", SubjectID: uuid.NewString(), AuthorityDomain: "PEOPLE"},
			{Kind: "POSITION", SubjectID: uuid.NewString(), AuthorityDomain: "POSITION"},
		},
		Writes: []intent.PlannedWrite{{FieldPath: "assignment.assignment.job_code", SourceAuthorityDecision: "authority.local_master/v1"}},
	}}
}

func governanceStanding() app.GovernanceStanding {
	return app.GovernanceStanding{
		Authorized: true, Subject: "hc-050-rafael-torres", SessionRef: "session-1", Assurance: "substantial",
		RequiredRole: "promotion_operator", Purpose: "compensation_review",
		PolicyBundleDigest: "sha256:policy", LegalContextDigest: "sha256:legal", ClassificationDigest: "sha256:classification",
		CapabilityDigest: "sha256:capability", ControlDigest: "sha256:control", SourceAuthorityDigest: "sha256:source",
		RiskClass: "R3",
	}
}

// guardless is a transaction whose only scripted read is the competing-promotion
// count, which real PostgreSQL always answers.
func guardless() *scriptTx {
	return &scriptTx{rows: map[string][]any{"FROM promotion_active_intent_guard": {0}}}
}

func governancePorts(tx *scriptTx) *promotionStepPorts {
	return &promotionStepPorts{
		db: scriptDB{tx: tx}, cellID: "cell-test", authorityDigest: "sha256:authority",
		planDigest: "sha256:plan", clock: func() time.Time { return time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC) },
	}
}

// TestGovernanceFactsAreReadFromDurableRowsOnly proves every fact is a read of
// a recorded row: no reservation, no pool, no position subject and an
// unresolvable subject each deny or fail closed rather than pass, and the
// observation identities name the rows they came from.
func TestGovernanceFactsAreReadFromDurableRowsOnly(t *testing.T) {
	ctx := context.Background()
	proposal := governanceProposal(t)
	in := governanceInputs{
		tenantID: wfrun034Tenant, instanceID: uuid.New(), proposal: proposal,
		planDigest: "sha256:plan", standing: governanceStanding(),
	}
	ports := governancePorts(guardless())

	facts, err := ports.governanceFacts(ctx, guardless(), in)
	if err != nil {
		t.Fatalf("governanceFacts: %v", err)
	}
	if facts.AuthZ.Effect != decision.Allow || facts.Session.Effect != decision.Allow {
		t.Fatalf("authorization facts = %+v", facts)
	}
	if facts.BudgetPosition.Budget.Effect != decision.Deny || !strings.HasPrefix(facts.BudgetPosition.Budget.ObservationID, "budget:no-reservation:") {
		t.Fatalf("budget fact with no reservation = %+v, want a denial naming the absence", facts.BudgetPosition.Budget)
	}
	if facts.BudgetPosition.Position.Effect != decision.Deny || !strings.HasPrefix(facts.BudgetPosition.Position.ObservationID, "position:absent:") {
		t.Fatalf("position fact with no recorded position = %+v", facts.BudgetPosition.Position)
	}
	if facts.Conflict.Effect != decision.UnknownFailClosed || facts.Conflict.Classification != conflictSubjectUnresolved {
		t.Fatalf("conflict fact with no resolvable subject = %+v", facts.Conflict)
	}
	if facts.SourceAuthority.Decision != "sha256:source|authority.local_master/v1" ||
		facts.FieldClassification.Version != "sha256:classification" || facts.LegalPolicy.LegalPackVersion != "sha256:legal" {
		t.Fatalf("control facts = %+v", facts)
	}

	// A proposal naming no position at all denies on the position fact
	// instead of skipping it.
	noPosition := in
	noPosition.proposal.Revision.Subjects = proposal.Revision.Subjects[:1]
	positionless, err := ports.governanceFacts(ctx, guardless(), noPosition)
	if err != nil || positionless.BudgetPosition.Position.ObservationID != "position:none" {
		t.Fatalf("position fact with no POSITION subject = %+v, %v", positionless.BudgetPosition.Position, err)
	}

	// An unauthorized standing is a recorded denial, not an error.
	revoked := in
	revoked.standing.Authorized = false
	refused, err := ports.governanceFacts(ctx, guardless(), revoked)
	if err != nil || refused.AuthZ.Effect != decision.Deny || refused.Session.Effect != decision.Deny {
		t.Fatalf("revoked standing facts = %+v, %v", refused, err)
	}

	// A revision with no effective start cannot be governed at any instant.
	undated := in
	undated.proposal.Revision.EffectiveTime = values.EffectiveInterval{}
	if _, err := ports.governanceFacts(ctx, guardless(), undated); err == nil {
		t.Fatal("facts were composed for a revision with no effective start")
	}
}

// TestGovernanceBaseNamesTheDecisionsMaterialContext proves the composed
// inputs name the proposal, principal, capability, subject, organization and
// approvals the decision was taken over, and that an approval-less instance
// composes an UNKNOWN requirement rather than an implicit satisfaction.
func TestGovernanceBaseNamesTheDecisionsMaterialContext(t *testing.T) {
	proposal := governanceProposal(t)
	in := governanceInputs{
		tenantID: wfrun034Tenant, instanceID: uuid.New(), proposal: proposal,
		planDigest: "sha256:plan", standing: governanceStanding(),
	}
	base := governanceBase(in, promotionterminalDecisions{ApprovalIDs: []string{"d-1", "d-2"}})
	if base.ProposalRevisionDigest != "sha256:material" || base.Context.Principal != "hc-050-rafael-torres" ||
		base.Context.Purpose != "compensation_review" || base.Context.Risk != "R3" ||
		base.Context.Resource != proposal.Revision.Subjects[0].SubjectID || len(base.Context.Fields) != 1 {
		t.Fatalf("base context = %+v", base.Context)
	}
	if base.ControlSnapshot.Digest != "sha256:control" || base.ControlSnapshot.PolicyBundle != "sha256:policy" {
		t.Fatalf("base control snapshot = %+v", base.ControlSnapshot)
	}
	if len(base.ApprovalRequirements) != 2 {
		t.Fatalf("approval requirements = %+v, want one per recorded decision", base.ApprovalRequirements)
	}
	for _, requirement := range base.ApprovalRequirements {
		if requirement.Satisfaction != decision.ApprovalSatisfied {
			t.Fatalf("requirement %+v, want SATISFIED", requirement)
		}
	}
	none := governanceBase(in, promotionterminalDecisions{})
	if len(none.ApprovalRequirements) != 1 || none.ApprovalRequirements[0].Satisfaction != decision.ApprovalUnknown {
		t.Fatalf("approval requirements with no decisions = %+v, want one UNKNOWN", none.ApprovalRequirements)
	}
	empty := governanceBase(governanceInputs{proposal: runtime.ProposalBinding{}}, promotionterminalDecisions{})
	if len(empty.Context.Fields) != 1 || empty.Context.Fields[0] != "promotion.no_declared_write" {
		t.Fatalf("fields for a revision with no writes = %v", empty.Context.Fields)
	}
}

// TestRecordAndLoadApprovalGovernance proves one approval writes one
// digest-bound record, that the record reproduces from its own inputs when it
// is read back, and that an absent record is reported as such rather than as
// an empty confirmation.
func TestRecordAndLoadApprovalGovernance(t *testing.T) {
	ctx := context.Background()
	tx := guardless()
	ports := governancePorts(tx)
	in := governanceInputs{
		tenantID: wfrun034Tenant, instanceID: uuid.New(), proposal: governanceProposal(t),
		planDigest: "sha256:plan", standing: governanceStanding(),
	}
	recordedAt := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	written, err := ports.RecordApprovalGovernance(ctx, tx, in, "approve_manager", uuid.New(),
		promotionterminalDecisions{ApprovalIDs: []string{"d-1", "d-2"}}, recordedAt)
	if err != nil || !written {
		t.Fatalf("RecordApprovalGovernance = %t, %v", written, err)
	}
	if tx.execs != 1 {
		t.Fatalf("governance writes = %d, want exactly one insert", tx.execs)
	}

	// The record a caller reads back must reproduce from its own inputs, and
	// revalidating it against the same facts confirms.
	facts, err := ports.governanceFacts(ctx, guardless(), in)
	if err != nil {
		t.Fatal(err)
	}
	historical, err := revalidate.NewHistoricalApproval(governanceBase(in, promotionterminalDecisions{ApprovalIDs: []string{"d-1", "d-2"}}), facts, in.planDigest)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(approvalGovernanceRecord{Historical: historical, NodeID: "approve_manager", RecordedAt: recordedAt})
	if err != nil {
		t.Fatal(err)
	}
	stored := &scriptTx{rows: map[string][]any{"FROM promotion_approval_governance": {body}}}
	loaded, err := loadApprovalGovernance(ctx, stored, in.tenantID, in.instanceID)
	if err != nil {
		t.Fatalf("loadApprovalGovernance: %v", err)
	}
	if loaded.NodeID != "approve_manager" || loaded.Historical.Decision.Digest != historical.Decision.Digest {
		t.Fatalf("loaded record = %+v", loaded)
	}
	if err := loaded.Historical.Validate(); err != nil {
		t.Fatalf("the stored record does not reproduce from its own inputs: %v", err)
	}
	if loaded.Historical.Allows() {
		t.Fatal("a decision composed over a denied budget fact reports as allowing")
	}
	if _, err := loadApprovalGovernance(ctx, &scriptTx{}, in.tenantID, in.instanceID); !errors.Is(err, ErrNoApprovalGovernance) {
		t.Fatalf("loading an absent record = %v, want ErrNoApprovalGovernance", err)
	}

	// A proposal with no parsable revision identity records nothing.
	broken := in
	broken.proposal.Revision.ProposalRevisionID = "not-a-uuid"
	if _, err := ports.RecordApprovalGovernance(ctx, guardless(), broken, "approve_manager", uuid.New(), promotionterminalDecisions{}, recordedAt); err == nil {
		t.Fatal("a record was written for a revision naming no proposal")
	}
}
