package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/intelligence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/repair"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// repairApprovalPolicy is the governance requirement a P1A repair plan
// declares. It is pinned rather than derived: a plan that could choose its own
// approval roles would be a plan that could choose to need none.
func repairApprovalPolicy() repair.ApprovalPolicy {
	return repair.ApprovalPolicy{
		Version:               ApprovalPolicyVersion,
		RolesForExternalWrite: []string{"hr_operations_lead"},
		RolesForReview:        []string{"hr_partner"},
		SoDExcludedRoles:      []string{"reconciliation_operator"},
	}
}

// freshnessPolicy is the observation-age policy every comparison is judged
// under.
func freshnessPolicy() dataops.FreshnessPolicy {
	return dataops.FreshnessPolicy{Version: FreshnessPolicyVersion, MaxAgeSeconds: FreshnessMaxAgeSeconds}
}

// resolveDrift decodes a detect_drift payload into the bounded cross-system
// comparison the operations domain runs.
func (f *CorpusInputs) resolveDrift(ctx context.Context, req ResolveRequest, payload *structValue) (DomainCall, error) {
	inst := req.Instance
	if f.externalSource == "" {
		return DomainCall{}, fmt.Errorf("app: this cell has no incumbent connection to compare against")
	}
	population, err := f.population(ctx, inst, payload)
	if err != nil {
		return DomainCall{}, err
	}
	effective, err := localDate(payload, "as_of")
	if err != nil {
		return DomainCall{}, err
	}
	asOf, err := asOfFrom(inst, effective, optionalStr(payload, "known_at"))
	if err != nil {
		return DomainCall{}, err
	}

	fields := ComparisonFields()
	// One decision covers the whole population: the comparison reads the same
	// projection of every subject, so a caller either may read that projection
	// under this purpose or may not. The subject the decision names is the
	// first of the population, which is the one whose scope resolution is
	// reported if it is refused.
	decision, err := authorizeRead(req.Principal, req.Purpose, authorizationRequest{
		Subject:     population[0],
		EvaluatedAt: inst.CreatedAt,
		Read:        peopleFields(comparisonPeopleFields()),
	})
	if err != nil {
		return DomainCall{}, err
	}

	revisions, resolved, err := f.pinPopulation(ctx, inst, population, asOf)
	if err != nil {
		return DomainCall{}, err
	}

	return DomainCall{
		Drift: &dataops.DetectDriftRequest{
			Tenant:        inst.Tenant,
			Source:        f.externalSource,
			Population:    population,
			Fields:        fields,
			AsOfEffective: effective,
			AsKnownAt:     asOf.KnownAt,
			// The comparison is judged at the instant the intent was recorded,
			// not at whatever the wall clock says when the handler runs. That
			// is what makes a replay of this intent produce the same freshness
			// verdicts as the original run.
			EvaluatedAt:   inst.CreatedAt,
			Authorization: dataopsDecision(decision, fields),
			Freshness:     freshnessPolicy(),
			PageLimit:     ObservationPageLimit,
		},
		Baseline: diagnosticBaseline(inst, revisions, resolved,
			[]string{"comparison_set_ref", "watermarks"}),
	}, nil
}

// resolveRepair decodes a create_repair_plan or simulate_repair payload. Both
// intents rest on the same pinned comparison; the second one simulates the
// plan the first one would produce, so resolving them differently would let
// the simulation be about a world the plan never saw.
func (f *CorpusInputs) resolveRepair(ctx context.Context, req ResolveRequest, payload *structValue) (DomainCall, error) {
	inst, def := req.Instance, req.Definition
	if f.externalSource == "" {
		return DomainCall{}, fmt.Errorf("app: this cell has no incumbent connection to compare against")
	}
	workerRef, err := str(payload, "worker_ref")
	if err != nil {
		return DomainCall{}, err
	}
	subject, ok := f.worker(ctx, inst.Tenant, workerRef)
	if !ok {
		return DomainCall{}, fmt.Errorf("app: worker_ref %q is not a resolvable worker reference", workerRef)
	}
	effective, err := localDate(payload, "as_of")
	if err != nil {
		return DomainCall{}, err
	}
	asOf, err := asOfFrom(inst, effective, optionalStr(payload, "known_at"))
	if err != nil {
		return DomainCall{}, err
	}

	fields := ComparisonFields()
	decision, err := authorizeRead(req.Principal, req.Purpose, authorizationRequest{
		Subject:     subject,
		EvaluatedAt: inst.CreatedAt,
		Read:        peopleFields(comparisonPeopleFields()),
	})
	if err != nil {
		return DomainCall{}, err
	}

	revisions, resolved, err := f.pinPopulation(ctx, inst, []values.EntityRef{subject}, asOf)
	if err != nil {
		return DomainCall{}, err
	}

	present := []string{"diagnosis_digest", "policy_snapshot_ref"}
	if def.Ref.TypeID == repair.SimulateRepairIntentType {
		present = []string{"repair_plan_digest", "state_watermarks"}
	}

	return DomainCall{
		Repair: &RepairInputs{
			PlanID:                 derivedPlanID(inst, subject),
			Tenant:                 inst.Tenant,
			Subject:                subject,
			Source:                 f.externalSource,
			Fields:                 fields,
			AsOfEffective:          effective,
			AsKnownAt:              asOf.KnownAt,
			EvaluatedAt:            inst.CreatedAt,
			Authorization:          dataopsDecision(decision, fields),
			Freshness:              freshnessPolicy(),
			Approval:               repairApprovalPolicy(),
			AuthorityPolicyVersion: FieldAuthorityPolicyVersion,
			LocalSystem:            LocalSystem,
			ExternalSystem:         f.externalSource,
			PageLimit:              ObservationPageLimit,
		},
		Baseline: diagnosticBaseline(inst, revisions, resolved, present),
	}, nil
}

// resolveTransaction decodes an explain_transaction payload.
//
// It reads nothing itself: the transaction's evidence is the ledger, and the
// ledger is read inside the governed capability. What it resolves is the
// coordinate - which transaction, at what knowledge cut-off, over which
// sections - and the authorization decision that says whether this caller may
// learn the transaction exists at all.
func (f *CorpusInputs) resolveTransaction(req ResolveRequest, payload *structValue) (DomainCall, error) {
	inst := req.Instance
	ref, err := str(payload, "transaction_ref")
	if err != nil {
		return DomainCall{}, err
	}
	transaction := values.EntityRef{Tenant: inst.Tenant, Kind: intelligence.KindTransaction, Id: ref}
	if validateErr := transaction.Validate(); validateErr != nil {
		return DomainCall{}, fmt.Errorf("app: transaction_ref %q is not a valid transaction reference: %w", ref, validateErr)
	}
	asOf, err := asOfFrom(inst, dayOf(inst.CreatedAt), optionalStr(payload, "known_at"))
	if err != nil {
		return DomainCall{}, err
	}

	sections := explainableSections()
	// A transaction's chronology is worker-scoped evidence, so the gate is the
	// same one every governed worker read passes: may this caller reach a
	// record in this tenant at all. The section projection is not a second
	// policy - see intelligenceDecision.
	decision, err := authorizeRead(req.Principal, req.Purpose, authorizationRequest{
		Subject:     transaction,
		EvaluatedAt: inst.CreatedAt,
		Read:        []authz.FieldID{authz.FieldWorkerNumber},
	})
	if err != nil {
		return DomainCall{}, err
	}

	return DomainCall{
		Transaction: &intelligence.ExplainTransactionRequest{
			Tenant:        inst.Tenant,
			Transaction:   transaction,
			Sections:      sections,
			AsKnownAt:     asOf.KnownAt,
			Authorization: intelligenceDecision(decision, sections),
		},
		Baseline: diagnosticBaseline(inst, nil, map[string]bool{ref: true},
			[]string{"transaction_ref"}),
	}, nil
}

// explainableSections is the projection this cell can actually reconstruct
// from a P1A chronology, plus the sections it reports as declared gaps.
//
// It is the full section list rather than a narrowed one on purpose: an
// explanation that quietly omitted the write set would read as "nothing was
// written", and the whole point of the release is that the absence of writes
// is a stated, evidenced fact.
func explainableSections() []intelligence.Section { return intelligence.AllSections() }

// comparisonPeopleFields is the comparison projection expressed in the
// worker-state vocabulary, for the authorization decision.
func comparisonPeopleFields() []people.FieldID {
	fields := ComparisonFields()
	out := make([]people.FieldID, 0, len(fields))
	for _, f := range fields {
		out = append(out, people.FieldID(f))
	}
	return out
}

// population decodes the bounded subject set a drift run examines, accepting
// either a list of worker references or a single one.
func (f *CorpusInputs) population(ctx context.Context, inst intent.Instance, payload *structValue) ([]values.EntityRef, error) {
	refs := optionalStrings(payload, "worker_refs")
	if len(refs) == 0 {
		single, err := str(payload, "worker_ref")
		if err != nil {
			return nil, err
		}
		refs = []string{single}
	}
	out := make([]values.EntityRef, 0, len(refs))
	seen := make(map[values.EntityRef]struct{}, len(refs))
	for _, ref := range refs {
		subject, ok := f.worker(ctx, inst.Tenant, ref)
		if !ok {
			return nil, fmt.Errorf("app: worker reference %q does not resolve", ref)
		}
		if _, dup := seen[subject]; dup {
			continue
		}
		seen[subject] = struct{}{}
		out = append(out, subject)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("app: the comparison population is empty")
	}
	return out, nil
}

// pinPopulation performs the governed read that pins the baseline: which
// subjects resolve at all, and the revision each was read at.
func (f *CorpusInputs) pinPopulation(
	ctx context.Context,
	inst intent.Instance,
	population []values.EntityRef,
	asOf people.AsOf,
) (map[string]values.RevisionToken, map[string]bool, error) {
	revisions := make(map[string]values.RevisionToken, len(population))
	resolved := make(map[string]bool, len(population))
	for _, subject := range population {
		facts, err := f.read(ctx, inst.Tenant, subject, asOf)
		if err != nil {
			return nil, nil, fmt.Errorf("app: governed worker read: %w", err)
		}
		if !facts.Exists {
			continue
		}
		revisions[subject.String()] = facts.Watermark
		resolved[subject.Id] = true
		resolved[subject.String()] = true
	}
	return revisions, resolved, nil
}

// diagnosticBaseline builds the kernel's input snapshot for an intent whose
// subjects are diagnostic references (a comparison set, a drift, a repair
// plan, a transaction) rather than worker records.
//
// Subject resolution stays real: a declared subject whose identifier the
// resolver did not actually resolve is simply absent from KnownSubjects, and
// the kernel then reports UNKNOWN_SUBJECT rather than the cell asserting that
// everything the caller named exists.
func diagnosticBaseline(
	inst intent.Instance,
	revisions map[string]values.RevisionToken,
	resolved map[string]bool,
	present []string,
) intent.BaselineSnapshot {
	if revisions == nil {
		revisions = map[string]values.RevisionToken{}
	}
	snapshot := intent.BaselineSnapshot{
		SnapshotID:    "snapshot:" + inst.IntentID,
		ObservedAt:    inst.CreatedAt,
		Revisions:     revisions,
		PresentInputs: present,
	}
	for _, s := range inst.Subjects {
		if resolved[s.SubjectID] {
			snapshot.KnownSubjects = append(snapshot.KnownSubjects, s)
		}
	}
	return snapshot
}

// derivedPlanID mints the stable identity of the repair plan one intent is
// about.
//
// It is derived from the intent and its subject rather than freshly minted so
// that planning the same evidence twice produces one plan: the identity is
// hashed into the plan's own digest, and a fresh UUID would make two runs over
// identical evidence disagree for a reason that has nothing to do with the
// evidence.
func derivedPlanID(inst intent.Instance, subject values.EntityRef) string {
	seed := strings.Join([]string{inst.IntentID, inst.CanonicalRequestDigest.Digest, subject.String()}, "|")
	return uuid.NewSHA1(artifactNamespace, []byte(seed)).String()
}

// dayOf renders an instant as the business date it falls on, for the intents
// that carry a knowledge cut-off but no business date of their own.
func dayOf(at values.Instant) values.LocalDate {
	utc := at.Time().UTC()
	date, err := values.NewLocalDate(utc.Year(), utc.Month(), utc.Day())
	if err != nil {
		return values.LocalDate{}
	}
	return date
}
