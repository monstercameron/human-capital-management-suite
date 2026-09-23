package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const (
	// reasonPromotionActive is the stable refusal reference returned when a
	// second promotion would overlap an active promotion for the same worker.
	reasonPromotionActive        = "promotion.active_conflict"
	promotionGuardPageSize int32 = 200
)

// FindActivePromotion reports an existing promotion for workerID that still
// participates in the request lifecycle.  It is intentionally a pure scan so
// both the page and intent-only proposal paths can apply the same guard before
// creating a new intent. Terminal request states are not duplicates: a
// rejected, cancelled, superseded, withdrawn or closed promotion may be
// proposed again as a new intent with its own evidence.
//
// The scan requires the tenant and exact EMPLOYMENT subject to match. A worker
// identifier by itself is not a tenant boundary, and matching a POSITION
// subject would incorrectly reject two workers who happen to target one
// position on different effective dates.
func FindActivePromotion(instances []intent.Instance, tenant string, workerID string) (intent.Instance, bool) {
	tenant = strings.TrimSpace(tenant)
	workerID = strings.TrimSpace(workerID)
	if tenant == "" || workerID == "" {
		return intent.Instance{}, false
	}
	for _, instance := range instances {
		if instance.Tenant.String() != tenant || instance.Definition.TypeID != promotion.IntentType || !activePromotionLifecycle(instance.Lifecycle) {
			continue
		}
		for _, subject := range instance.Subjects {
			if subject.Kind == "EMPLOYMENT" && subject.SubjectID == workerID {
				return instance, true
			}
		}
	}
	return intent.Instance{}, false
}

func activePromotionLifecycle(d lifecycle.Dimensions) bool {
	switch d.Request {
	case lifecycle.RequestDraft, lifecycle.RequestPreflighted, lifecycle.RequestSimulated,
		lifecycle.RequestSubmitted, lifecycle.RequestApproved, lifecycle.RequestReopened:
		return true
	default:
		return false
	}
}

func promotionEmploymentSubject(subjects []intent.SubjectReference) string {
	for _, subject := range subjects {
		if subject.Kind == "EMPLOYMENT" && strings.TrimSpace(subject.SubjectID) != "" {
			return strings.TrimSpace(subject.SubjectID)
		}
	}
	return ""
}

// findActivePromotion reads the tenant's complete intent population in
// bounded pages and applies the same subject/lifecycle predicate used by the
// journey projections. It is called while CreateIntent holds the service's
// promotion admission lock, so two local callers cannot both pass the scan
// and append competing promotions.
func (s *IntentService) findActivePromotion(ctx context.Context, tenant, workerID string) (intent.Instance, bool, error) {
	if s == nil || s.store == nil {
		return intent.Instance{}, false, fmt.Errorf("app: promotion admission store is unavailable")
	}
	cursor := ""
	for {
		page, err := s.store.ListIntents(ctx, tenant, promotionGuardPageSize, cursor)
		if err != nil {
			return intent.Instance{}, false, fmt.Errorf("app: scan active promotions: %w", err)
		}
		for _, record := range page.Records {
			if record.Definition.TypeID != promotion.IntentType {
				continue
			}
			instance, decodeErr := decodeEnvelope(record.Envelope)
			if decodeErr != nil {
				return intent.Instance{}, false, fmt.Errorf("app: decode active promotion: %w", decodeErr)
			}
			if active, ok := FindActivePromotion([]intent.Instance{instance}, tenant, workerID); ok {
				return active, true, nil
			}
		}
		if page.NextCursor == "" {
			return intent.Instance{}, false, nil
		}
		if page.NextCursor == cursor {
			return intent.Instance{}, false, fmt.Errorf("app: active promotion scan returned a repeated cursor")
		}
		cursor = page.NextCursor
	}
}

// The durable half of a propose: EP-PROMO-001's "immutable
// snapshot/simulation/proposal candidates". Minting the proposal in memory is
// half the contract; the other half is that the exact snapshot the governed
// read produced, the simulation result it computed and the proposal revision
// it minted survive the process that produced them.
//
// Recording is deliberately a propose-time act and never an inspect-time one:
// Inspect and ListJourneys re-simulate a never-executed journey to report its
// current truth, and a read path that wrote candidate rows on every page view
// would fill append-only tables with rows nobody asked for. Propose records
// once; every later reader either finds these rows or reads nothing durable
// at all.
//
// Every identity here is derived, never allocated: a replayed propose
// re-derives the same snapshot, simulation and revision keys, so the stores'
// own ON CONFLICT handling makes the second write a no-op rather than a
// second candidate set.

const (
	// proposalSnapshotSchemaRef and proposalSimulationSchemaRef name the
	// schema each recorded body validates against. They are semantic keys
	// in the same family as [executionProposalSchemaRef].
	proposalSnapshotSchemaRef   = "hcmnext.intents.v1.IntentInputSnapshot"
	proposalSimulationSchemaRef = "hcmnext.intents.v1.SimulationResult"
)

// proposalCandidateNamespace is the fixed UUID namespace candidate ids are
// derived under. Derivation is what makes a replayed propose converge on the
// rows its first call recorded instead of minting second copies.
var proposalCandidateNamespace = uuid.MustParse("3f7a9c2e-5b1d-4e6f-8a0c-9d2e4f6a8b0c")

// proposalSnapshotDocument is the canonical body intent_input_snapshot
// records: the intent identity, the request digest it was created from and
// the source baselines the minted proposal pins. The baselines are the
// load-bearing part: they are the answer to "what did the governed read say"
// at the moment this proposal was computed.
type proposalSnapshotDocument struct {
	IntentID               string                        `json:"intent_id"`
	Revision               uint64                        `json:"revision"`
	RequestDigestAlgorithm string                        `json:"request_digest_algorithm"`
	RequestDigest          string                        `json:"request_digest"`
	Purpose                string                        `json:"purpose"`
	Subjects               []proposalSnapshotSubject     `json:"subjects"`
	SourceBaselines        []proposalSnapshotBaselineRef `json:"source_baselines"`
	ControlDigest          string                        `json:"control_digest"`
}

type proposalSnapshotSubject struct {
	Kind            string `json:"kind"`
	SubjectID       string `json:"subject_id"`
	AuthorityDomain string `json:"authority_domain"`
}

type proposalSnapshotBaselineRef struct {
	StreamID         string `json:"stream_id"`
	ExpectedRevision string `json:"expected_revision"`
}

// proposalSimulationDocument is the canonical body intent_simulation_result
// records: the proposal the simulation minted, the digest an approval would
// bind, and the shape of the answer (planned writes and finding codes) so a
// stored result can be read without re-running the simulation.
type proposalSimulationDocument struct {
	IntentID           string   `json:"intent_id"`
	ProposalRevisionID string   `json:"proposal_revision_id"`
	MaterialDigest     string   `json:"material_digest"`
	SimulationStatus   string   `json:"simulation_status"`
	PlannedWrites      []string `json:"planned_writes"`
	FindingCodes       []string `json:"finding_codes"`
}

// recordProposalCandidates persists one propose's durable candidates: the
// input snapshot the simulation consumed, the proposal revision it minted
// with its four item sets, and the simulation result that binds them. All
// four writes run inside one tenant-scoped transaction, so a candidate set is
// either recorded whole or not at all.
//
// Idempotency is structural: the identities are derived from (tenant, intent,
// purpose, sequence) and every store collides rather than duplicates, so a
// replayed propose returns the original intent and rewrites nothing.
func (e *journeyEngine) recordProposalCandidates(
	ctx context.Context, principal *trust.Principal, inst intent.Instance, simulated simulationResult,
) error {
	rev := simulated.Revision
	if rev == nil {
		// A simulation that minted no proposal revision has no candidate set
		// to record; the BLOCKED answer is its own evidence.
		return nil
	}
	if e.db == nil || e.svc == nil || e.svc.tenantUUID == nil {
		// A cell composed without the durable database has nowhere to put
		// the candidates; the minted revision still answers this process.
		return nil
	}
	tenantID := e.svc.tenantUUID(inst.Tenant)
	intentID, err := executionIntentUUID(inst.IntentID)
	if err != nil {
		return err
	}
	observed := e.now()

	snapshotBody, err := proposalSnapshotDocument{
		IntentID:               inst.IntentID,
		Revision:               rev.Revision,
		RequestDigestAlgorithm: inst.CanonicalRequestDigest.AlgorithmID,
		RequestDigest:          inst.CanonicalRequestDigest.Digest,
		Purpose:                inst.Purpose,
		Subjects:               proposalSnapshotSubjects(inst),
		SourceBaselines:        proposalSnapshotBaselines(*rev),
		ControlDigest:          controlSnapshotDigest(rev.ControlSnapshots),
	}.marshal()
	if err != nil {
		return err
	}
	snapshotID := proposalCandidateUUID(tenantID, intentID, "snapshot:"+intentcontrol.PurposeSimulation)

	simulationBody, err := proposalSimulationDocument{
		IntentID:           inst.IntentID,
		ProposalRevisionID: rev.ProposalRevisionID,
		MaterialDigest:     rev.MaterialDigest.Digest,
		SimulationStatus:   intentcontrol.SimulationReady,
		PlannedWrites:      proposalPlannedWrites(simulated.Artifact),
		FindingCodes:       proposalFindingCodes(simulated.Artifact),
	}.marshal()
	if err != nil {
		return err
	}
	simulationID := proposalCandidateUUID(tenantID, intentID, "simulation")

	tx, err := e.beginTenant(ctx, principal)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	if _, err := (intentcontrol.SnapshotStore{}).Record(ctx, tx, intentcontrol.InputSnapshot{
		TenantID:   tenantID,
		SnapshotID: snapshotID,
		IntentID:   intentID,
		Purpose:    intentcontrol.PurposeSimulation,
		Sequence:   1,
		ObservedAt: observed,
		Digest:     candidateDigest(snapshotBody),
		SchemaRef:  proposalSnapshotSchemaRef,
		Body:       snapshotBody,
	}); err != nil && !errors.Is(err, intentcontrol.ErrDuplicate) {
		return fmt.Errorf("app: record the proposal input snapshot: %w", err)
	}

	payload, err := intentcontrol.EncodeFullProposal(*rev)
	if err != nil {
		return fmt.Errorf("app: encode the proposal revision: %w", err)
	}
	if _, err := (intentcontrol.RevisionStore{}).Materialize(ctx, tx, intentcontrol.Revision{
		TenantID:       tenantID,
		IntentID:       intentID,
		Revision:       rev.Revision,
		ProposalDigest: rev.MaterialDigest.Digest,
		MaterialDigest: rev.MaterialDigest.Digest,
		SchemaRef:      executionProposalSchemaRef,
		Payload:        payload,
		ProducedBy:     "hcmnext:intent-cell",
		ProducedAt:     rev.CreatedAt.Time(),
	}); err != nil {
		return fmt.Errorf("app: materialize the proposal revision: %w", err)
	}

	// WF-RUN-034: the proposal holds its raise against its organization
	// unit's compensation pool in the same transaction its revision is
	// materialized in, recorded no later than the revision's ProducedAt.
	if hold, ok, holdErr := proposalReservationFor(*rev); holdErr != nil {
		return holdErr
	} else if ok {
		if _, err := promotionbudget.ReserveProposalBudget(ctx, tx, tenantID, hold); err != nil {
			return fmt.Errorf("app: hold the proposal's budget: %w", err)
		}
	}

	// REV-006-02: the admitted proposal holds its target head the same
	// way. Two promotions for the last open headcount each read a
	// capacity-available snapshot, so the check-then-act has to happen
	// here, once, under the fence: the loser gets the stores' typed
	// conflict and never reaches a commit. A revision with no POSITION
	// target implies no hold, exactly like the budget skip above.
	if err := e.holdProposalPosition(ctx, inst, *rev, observed); err != nil {
		return err
	}

	sets, err := proposalCandidateSets(*rev)
	if err != nil {
		return err
	}
	if err := (intentcontrol.ProposalSetStore{}).Record(ctx, tx, tenantID, intentID, rev.Revision, sets); err != nil && !errors.Is(err, intentcontrol.ErrDuplicate) {
		return fmt.Errorf("app: record the proposal sets: %w", err)
	}

	// The simulation result foreign-keys to both rows above, so it records
	// last: a result that named a snapshot or revision nobody stored would
	// be evidence pointing at nothing.
	if _, err := (intentcontrol.SimulationStore{}).RecordResult(ctx, tx, intentcontrol.SimulationResult{
		TenantID:        tenantID,
		SimulationID:    simulationID,
		IntentID:        intentID,
		Revision:        rev.Revision,
		Sequence:        1,
		InputSnapshotID: snapshotID,
		Status:          intentcontrol.SimulationReady,
		ResultDigest:    candidateDigest(simulationBody),
		ProposalDigest:  rev.MaterialDigest.Digest,
		ControlDigest:   controlSnapshotDigest(rev.ControlSnapshots),
		SchemaRef:       proposalSimulationSchemaRef,
		Body:            simulationBody,
		SimulatedAt:     observed,
	}); err != nil && !errors.Is(err, intentcontrol.ErrDuplicate) {
		return fmt.Errorf("app: record the simulation result: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("app: commit the proposal candidates: %w", err)
	}
	committed = true
	return nil
}

// proposalCandidateUUID derives one candidate row's identity from the tenant,
// the intent and the candidate kind. The sequence the schema keys on is the
// other half of the identity and is always the first of its kind here.
func proposalCandidateUUID(tenantID, intentID uuid.UUID, kind string) uuid.UUID {
	return uuid.NewSHA1(proposalCandidateNamespace,
		[]byte(tenantID.String()+":"+intentID.String()+":"+kind+":1"))
}

// candidateDigest is the 64-hex content digest of one canonical candidate
// body, in the same shape every other intent-control digest takes.
func candidateDigest(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func (d proposalSnapshotDocument) marshal() (json.RawMessage, error) {
	body, err := json.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("app: encode the input snapshot body: %w", err)
	}
	return body, nil
}

func (d proposalSimulationDocument) marshal() (json.RawMessage, error) {
	body, err := json.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("app: encode the simulation result body: %w", err)
	}
	return body, nil
}

func proposalSnapshotSubjects(inst intent.Instance) []proposalSnapshotSubject {
	out := make([]proposalSnapshotSubject, 0, len(inst.Subjects))
	for _, s := range inst.Subjects {
		out = append(out, proposalSnapshotSubject{
			Kind:            s.Kind,
			SubjectID:       s.SubjectID,
			AuthorityDomain: s.AuthorityDomain,
		})
	}
	return out
}

func proposalSnapshotBaselines(rev intent.ProposalRevision) []proposalSnapshotBaselineRef {
	out := make([]proposalSnapshotBaselineRef, 0, len(rev.SourceBaselines))
	for _, b := range rev.SourceBaselines {
		out = append(out, proposalSnapshotBaselineRef{
			StreamID:         b.StreamID,
			ExpectedRevision: b.ExpectedRevision.String(),
		})
	}
	return out
}

func proposalPlannedWrites(artifact *intentsv1.SimulationArtifact) []string {
	if artifact == nil {
		return nil
	}
	out := make([]string, 0, len(artifact.GetPlannedWrites()))
	for _, w := range artifact.GetPlannedWrites() {
		out = append(out, w.GetOperation()+" "+w.GetTargetRef())
	}
	return out
}

func proposalFindingCodes(artifact *intentsv1.SimulationArtifact) []string {
	if artifact == nil {
		return nil
	}
	out := make([]string, 0, len(artifact.GetFindings()))
	for _, f := range artifact.GetFindings() {
		out = append(out, f.GetCode())
	}
	return out
}

// proposalCandidateSets maps the minted revision onto the four append-only
// item tables. A member the store's own contract cannot express - an
// obligation carrying no deadline, which the kernel's Obligation type does
// not model - is an error rather than a silently dropped row: a stored
// candidate set that is missing a member is a proposal that reads as less
// than it is.
func proposalCandidateSets(rev intent.ProposalRevision) (intentcontrol.ProposalSets, error) {
	writes, err := intentcontrol.NewWriteItems(rev.Writes)
	if err != nil {
		return intentcontrol.ProposalSets{}, fmt.Errorf("app: map the proposal writes: %w", err)
	}
	if len(rev.Obligations) > 0 {
		return intentcontrol.ProposalSets{}, fmt.Errorf("app: proposal %s carries %d obligation(s) and none carry a due_at the schema requires",
			rev.ProposalRevisionID, len(rev.Obligations))
	}
	sets := intentcontrol.ProposalSets{Writes: writes}
	for _, e := range rev.Effects {
		sets.Effects = append(sets.Effects, intentcontrol.EffectItem{
			EffectID:        e.EffectID,
			Kind:            e.Kind,
			DestinationRef:  e.DestinationRef,
			Reversibility:   e.Reversibility,
			CompensationRef: e.CompensationRef,
			ObservationRef:  e.ObservationRef,
		})
	}
	for _, a := range rev.RequiredApprovals {
		sets.Approvals = append(sets.Approvals, intentcontrol.ApprovalRequirementItem{
			RequirementID:        a.RequirementID,
			SeparationConstraint: a.SeparationConstraint,
			MaterialityClass:     intentcontrol.Material,
		})
	}
	return sets, nil
}
