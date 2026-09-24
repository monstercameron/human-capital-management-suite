package skill

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const skillTenant values.TenantId = "acme"

func skillRef(kind values.Kind, id string) values.EntityRef {
	return values.EntityRef{Tenant: skillTenant, Kind: kind, Id: id}
}
func skillRevision(t *testing.T, stream string, sequence uint64) values.RevisionToken {
	t.Helper()
	revision, err := values.NewSequenceRevision(stream, sequence)
	if err != nil {
		t.Fatal(err)
	}
	return revision
}
func skillDate(t *testing.T, text string) values.LocalDate {
	t.Helper()
	date, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatal(err)
	}
	return date
}
func skillInterval(t *testing.T, start, end string) values.EffectiveInterval {
	t.Helper()
	interval, err := values.NewLocalDateInterval(skillDate(t, start), skillDate(t, end), values.CalendarRef{Ref: "gregorian", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	return interval
}

func skillOntology(t *testing.T) (SkillOntologyRevision, values.EntityRef, values.EntityRef) {
	t.Helper()
	parent := skillRef(KindSkill, "00000000-0000-0000-0000-000000000001")
	child := skillRef(KindSkill, "00000000-0000-0000-0000-000000000002")
	makeDefinition := func(ref values.EntityRef, parents []values.EntityRef) SkillDefinitionRevision {
		definition, err := NewSkillDefinition(SkillDefinitionRevision{SkillRef: ref, Revision: skillRevision(t, "skill/"+ref.Id, 1), Name: "skill", ParentRefs: parents, Aliases: []string{"common"}, ProficiencyScale: DefaultProficiencyScale()})
		if err != nil {
			t.Fatal(err)
		}
		if got := definition.computedDigest(); got != definition.CanonicalDigest {
			t.Fatalf("definition digest changed: got %s want %s", got, definition.CanonicalDigest)
		}
		return definition
	}
	ontology, err := NewSkillOntology(SkillOntologyRevision{OntologyID: skillRef(values.Kind("skill_ontology"), "00000000-0000-0000-0000-000000000010"), Revision: skillRevision(t, "ontology", 1), Skills: []SkillDefinitionRevision{makeDefinition(parent, nil), makeDefinition(child, []values.EntityRef{parent})}})
	if err != nil {
		t.Fatal(err)
	}
	return ontology, parent, child
}

func skillEvidence(t *testing.T, worker, ref values.EntityRef, end string, verified bool, kind EvidenceKind) WorkerSkillEvidence {
	t.Helper()
	evidence, err := NewWorkerSkillEvidence(WorkerSkillEvidence{EvidenceID: skillRef(values.Kind("skill_evidence"), "00000000-0000-0000-0000-000000000020"), Worker: worker, SkillRef: ref, Level: 4, EvidenceKind: kind, EvidenceRef: "evidence-private", Verified: verified, Effective: skillInterval(t, "2026-01-01", end)})
	if err != nil {
		t.Fatal(err)
	}
	return evidence
}

func skillConsumerAdapters(calls *atomic.Int32) []ConsumerResolver {
	return []ConsumerResolver{
		ConsumerResolverFunc{Kind: ConsumerQualification, Fn: func(ctx context.Context, resolver Resolver, req ResolveRequest) (Resolution, string, error) {
			if calls != nil {
				calls.Add(1)
			}
			resolution, err := resolver.Resolve(ctx, req)
			return resolution, "qualification purpose-safe pinned explanation", err
		}},
		ConsumerResolverFunc{Kind: ConsumerRecruiting, Fn: func(ctx context.Context, resolver Resolver, req ResolveRequest) (Resolution, string, error) {
			if calls != nil {
				calls.Add(1)
			}
			resolution, err := resolver.Resolve(ctx, req)
			return resolution, "recruiting purpose-safe pinned explanation", err
		}},
		ConsumerResolverFunc{Kind: ConsumerLearning, Fn: func(ctx context.Context, resolver Resolver, req ResolveRequest) (Resolution, string, error) {
			if calls != nil {
				calls.Add(1)
			}
			resolution, err := resolver.Resolve(ctx, req)
			return resolution, "learning purpose-safe pinned explanation", err
		}},
		ConsumerResolverFunc{Kind: ConsumerPlanning, Fn: func(ctx context.Context, resolver Resolver, req ResolveRequest) (Resolution, string, error) {
			if calls != nil {
				calls.Add(1)
			}
			resolution, err := resolver.Resolve(ctx, req)
			return resolution, "planning purpose-safe pinned explanation", err
		}},
	}
}

// TestSkillOntologyRejectsCyclesUnverifiedProficiencyAndUnsafeEquivalence is
// the primary SKILL-001 contract test from planning/todos.md.
func TestSkillOntologyRejectsCyclesUnverifiedProficiencyAndUnsafeEquivalence(t *testing.T) {
	ontology, parent, child := skillOntology(t)
	cycle := ontology
	cycle.Skills = append([]SkillDefinitionRevision(nil), ontology.Skills...)
	cycle.Skills[0].ParentRefs = append([]values.EntityRef(nil), ontology.Skills[0].ParentRefs...)
	cycle.Skills[0].ParentRefs = []values.EntityRef{child}
	if _, err := NewSkillOntology(cycle); !errors.Is(err, ErrInvalidOntology) {
		t.Fatalf("cycle error = %v", err)
	}

	worker := skillRef(KindWorker, "00000000-0000-0000-0000-000000000030")
	asserted := skillEvidence(t, worker, child, "2027-01-01", false, EvidenceSelfReport)
	resolved, err := NewResolver(FakeSkillEvidenceReader{Evidence: []WorkerSkillEvidence{asserted}}).Resolve(context.Background(), ResolveRequest{Worker: worker, AsOf: skillDate(t, "2026-06-01"), SkillRefs: []values.EntityRef{child}, Ontology: ontology})
	if err != nil {
		t.Fatal(err)
	}
	if got := resolved.Proficiencies[0].Status; got != StatusAsserted {
		t.Fatalf("status = %s", got)
	}
	if strings.Contains(Explain(asserted), asserted.EvidenceRef) {
		t.Fatal("Explain repeated an evidence reference")
	}

	unsafe, err := NewEquivalenceRule(EquivalenceRule{RuleID: skillRef(values.Kind("skill_equivalence"), "00000000-0000-0000-0000-000000000040"), Revision: skillRevision(t, "equivalence", 1), SourceSkill: parent, TargetSkill: child, SourceLevel: 3, TargetLevel: 3, Effective: skillInterval(t, "2026-01-01", "2027-01-01"), EvidenceRef: "review", Approved: false})
	if err == nil || !errors.Is(err, ErrInvalidEquivalence) {
		t.Fatalf("unsafe equivalence error = %v", err)
	}
	_ = unsafe
}

func TestSkillExpirationAndReviewedEquivalence(t *testing.T) {
	ontology, parent, child := skillOntology(t)
	worker := skillRef(KindWorker, "00000000-0000-0000-0000-000000000050")
	expired := skillEvidence(t, worker, child, "2026-03-01", true, EvidenceCredential)
	equivalence, err := NewEquivalenceRule(EquivalenceRule{RuleID: skillRef(values.Kind("skill_equivalence"), "00000000-0000-0000-0000-000000000051"), Revision: skillRevision(t, "equivalence", 2), SourceSkill: parent, TargetSkill: child, SourceLevel: 4, TargetLevel: 3, Effective: skillInterval(t, "2026-01-01", "2027-01-01"), EvidenceRef: "review", Approved: true})
	if err != nil {
		t.Fatal(err)
	}
	source := skillEvidence(t, worker, parent, "2027-01-01", true, EvidenceCredential)
	resolver := NewPinnedResolver(ontology, []EquivalenceRule{equivalence}, FakeSkillEvidenceReader{Evidence: []WorkerSkillEvidence{expired, source}})
	result, err := resolver.Resolve(context.Background(), ResolveRequest{Worker: worker, AsOf: skillDate(t, "2026-06-01"), SkillRefs: []values.EntityRef{child}, Ontology: ontology, Equivalences: []EquivalenceRule{equivalence}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Proficiencies[0].Status != StatusVerified || !result.Proficiencies[0].ViaEquivalence {
		t.Fatalf("equivalent result = %+v", result.Proficiencies[0])
	}
	result, err = resolver.Resolve(context.Background(), ResolveRequest{Worker: worker, AsOf: skillDate(t, "2026-06-01"), SkillRefs: []values.EntityRef{child}, Ontology: ontology})
	if err != nil {
		t.Fatal(err)
	}
	if result.Proficiencies[0].Status != StatusExpired {
		t.Fatalf("expired result = %+v", result.Proficiencies[0])
	}
}

func TestTodo_SKILL_001_Conformance(t *testing.T) {
	ontology, _, child := skillOntology(t)
	worker := skillRef(KindWorker, "00000000-0000-0000-0000-000000000050")
	expired := skillEvidence(t, worker, child, "2026-03-01", true, EvidenceCredential)
	resolver := NewPinnedResolver(ontology, nil, FakeSkillEvidenceReader{Evidence: []WorkerSkillEvidence{expired}})
	result, err := resolver.Resolve(context.Background(), ResolveRequest{Worker: worker, AsOf: skillDate(t, "2026-06-01"), SkillRefs: []values.EntityRef{child}, Ontology: ontology})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Proficiencies) != 1 || result.Proficiencies[0].Status != StatusExpired {
		t.Fatalf("expired evidence resolution=%+v, want one expired proficiency", result.Proficiencies)
	}
}
func TestTodo_SKILL_001_Fault(t *testing.T) {
	ontology, _, child := skillOntology(t)
	cycle := ontology
	cycle.Skills = append([]SkillDefinitionRevision(nil), ontology.Skills...)
	cycle.Skills[0].ParentRefs = []values.EntityRef{child}
	if _, err := NewSkillOntology(cycle); !errors.Is(err, ErrInvalidOntology) {
		t.Fatalf("cyclic ontology error=%v, want ErrInvalidOntology", err)
	}
}
func TestTodo_SKILL_001_Golden(t *testing.T) {
	ontology, _, _ := skillOntology(t)
	if ontology.CanonicalDigest == "" || ontology.Canonical() == nil {
		t.Fatal("ontology is not canonical")
	}
}
func TestTodo_SKILL_001_Mutation(t *testing.T) {
	ontology, _, _ := skillOntology(t)
	original := append([]SkillDefinitionRevision(nil), ontology.Skills...)
	ontology.Skills[0].Name = "changed"
	if original[0].Name == ontology.Skills[0].Name {
		t.Fatal("fixture copy was not independent")
	}
}
func TestTodo_SKILL_001_Property(t *testing.T) {
	ontology, _, _ := skillOntology(t)
	first := ontology.CanonicalDigest
	second, err := NewSkillOntology(ontology)
	if err != nil || first != second.CanonicalDigest {
		t.Fatalf("digest not stable: %v", err)
	}
}
func TestTodo_SKILL_001_Race(t *testing.T) {
	ontology, _, child := skillOntology(t)
	worker := skillRef(KindWorker, "00000000-0000-0000-0000-000000000060")
	evidence := skillEvidence(t, worker, child, "2027-01-01", true, EvidenceCredential)
	resolver := NewPinnedResolver(ontology, nil, FakeSkillEvidenceReader{Evidence: []WorkerSkillEvidence{evidence}})
	asOf := skillDate(t, "2026-06-01")
	request := ResolveRequest{Worker: worker, AsOf: asOf, Ontology: ontology}
	const workers = 8
	results := make(chan Resolution, workers)
	errors := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := resolver.Resolve(context.Background(), request)
			if err != nil {
				errors <- err
				return
			}
			results <- got
		}()
	}
	wg.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	count := 0
	for got := range results {
		count++
		verifiedChild := false
		for _, proficiency := range got.Proficiencies {
			if proficiency.SkillRef == child && proficiency.Status == StatusVerified {
				verifiedChild = true
			}
		}
		if !verifiedChild {
			t.Fatalf("concurrent resolution=%+v", got.Proficiencies)
		}
	}
	if count != workers {
		t.Fatalf("successful resolutions=%d, want %d", count, workers)
	}
}
func TestTodo_SKILL_001_Security(t *testing.T) {
	evidence := WorkerSkillEvidence{EvidenceRef: "secret"}
	if strings.Contains(Explain(evidence), "secret") {
		t.Fatal("explanation leaked evidence")
	}
}

// TestSkillConformanceUsesOnePinnedEvidenceRevisionAcrossConsumers is the
// primary SKILL-002 contract test from planning/todos.md.
func TestSkillConformanceUsesOnePinnedEvidenceRevisionAcrossConsumers(t *testing.T) {
	ot, _, child := skillOntology(t)
	worker := skillRef(KindWorker, "00000000-0000-0000-0000-000000000070")
	original := skillEvidence(t, worker, child, "2027-01-01", true, EvidenceAssessment)
	// A correction has a new immutable identity and explicitly points at the
	// predecessor; the predecessor remains in the snapshot for auditability.
	successor, err := NewWorkerSkillEvidence(WorkerSkillEvidence{EvidenceID: skillRef(values.Kind("skill_evidence"), "00000000-0000-0000-0000-000000000071"), Worker: worker, SkillRef: child, Level: 2, EvidenceKind: EvidenceAssessment, EvidenceRef: "corrected", Verified: true, Supersedes: original.EvidenceID, Effective: skillInterval(t, "2026-01-01", "2027-01-01")})
	if err != nil {
		t.Fatal(err)
	}
	correction, err := CorrectEvidence(original, successor, "assessment correction", []Consumer{ConsumerQualification, ConsumerRecruiting, ConsumerLearning, ConsumerPlanning})
	if err != nil {
		t.Fatal(err)
	}
	if correction.OriginalDigest != original.CanonicalDigest || correction.SuccessorDigest != successor.CanonicalDigest {
		t.Fatal("correction lost immutable lineage")
	}
	snapshot, err := NewEvidenceRevision(4, []WorkerSkillEvidence{original, successor})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	report, err := ResolveConsumers(context.Background(), ot, nil, snapshot, ResolveRequest{Worker: worker, AsOf: skillDate(t, "2026-06-01"), SkillRefs: []values.EntityRef{child}}, skillConsumerAdapters(&calls)...)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Results) != 4 {
		t.Fatalf("consumer results = %d, want 4", len(report.Results))
	}
	if calls.Load() != 4 {
		t.Fatalf("consumer adapter calls = %d, want 4", calls.Load())
	}
	for _, result := range report.Results {
		if result.EvidenceDigest != snapshot.Digest || result.OntologyDigest != ot.CanonicalDigest {
			t.Fatalf("%s was not pinned", result.Consumer)
		}
		if result.Resolution.Proficiencies[0].Level != 2 {
			t.Fatalf("%s used superseded evidence: %+v", result.Consumer, result.Resolution.Proficiencies[0])
		}
		if strings.Contains(result.Explanation, original.EvidenceRef) || strings.Contains(result.Explanation, successor.EvidenceRef) {
			t.Fatalf("%s explanation leaked evidence reference", result.Consumer)
		}
	}
}

func TestTodo_SKILL_002_Conformance(t *testing.T) {
	ot, _, child := skillOntology(t)
	worker := skillRef(KindWorker, "00000000-0000-0000-0000-000000000070")
	original := skillEvidence(t, worker, child, "2027-01-01", true, EvidenceAssessment)
	successor, err := NewWorkerSkillEvidence(WorkerSkillEvidence{EvidenceID: skillRef(values.Kind("skill_evidence"), "00000000-0000-0000-0000-000000000071"), Worker: worker, SkillRef: child, Level: 2, EvidenceKind: EvidenceAssessment, EvidenceRef: "corrected", Verified: true, Supersedes: original.EvidenceID, Effective: skillInterval(t, "2026-01-01", "2027-01-01")})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewEvidenceRevision(4, []WorkerSkillEvidence{original, successor})
	if err != nil {
		t.Fatal(err)
	}
	report, err := ResolveConsumers(context.Background(), ot, nil, snapshot, ResolveRequest{Worker: worker, AsOf: skillDate(t, "2026-06-01"), SkillRefs: []values.EntityRef{child}}, skillConsumerAdapters(nil)...)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Results) != 4 {
		t.Fatalf("consumer results=%d, want 4", len(report.Results))
	}
	for _, result := range report.Results {
		if result.EvidenceDigest != snapshot.Digest || result.Resolution.Proficiencies[0].Level != 2 {
			t.Fatalf("consumer %s did not use the pinned successor: %+v", result.Consumer, result.Resolution)
		}
	}
}
func TestTodo_SKILL_002_Golden(t *testing.T) {
	ot, _, child := skillOntology(t)
	worker := skillRef(KindWorker, "00000000-0000-0000-0000-000000000072")
	e := skillEvidence(t, worker, child, "2027-01-01", true, EvidenceCredential)
	s, err := NewEvidenceRevision(1, []WorkerSkillEvidence{e})
	if err != nil {
		t.Fatal(err)
	}
	if s.Digest == "" || s.Digest != evidenceRevisionDigest(s.Sequence, s.Evidence) {
		t.Fatal("evidence digest is not stable")
	}
	_ = ot
}
func TestTodo_SKILL_002_Security(t *testing.T) {
	if _, err := CorrectEvidence(WorkerSkillEvidence{}, WorkerSkillEvidence{}, "", nil); !errors.Is(err, ErrCorrection) {
		t.Fatalf("invalid correction = %v", err)
	}
}
func TestTodo_SKILL_002_Mutation(t *testing.T) {
	ot, _, child := skillOntology(t)
	worker := skillRef(KindWorker, "00000000-0000-0000-0000-000000000073")
	e := skillEvidence(t, worker, child, "2027-01-01", true, EvidenceCredential)
	s, err := NewEvidenceRevision(1, []WorkerSkillEvidence{e})
	if err != nil {
		t.Fatal(err)
	}
	before := s.Digest
	s.Evidence[0].EvidenceRef = "changed"
	if s.Digest != before || evidenceRevisionDigest(s.Sequence, s.Evidence) == before {
		t.Fatal("snapshot mutation was not detectable")
	}
	if _, err := ResolveConsumers(context.Background(), ot, nil, s, ResolveRequest{Worker: worker, AsOf: skillDate(t, "2026-06-01"), SkillRefs: []values.EntityRef{child}}, skillConsumerAdapters(nil)...); !errors.Is(err, ErrPinnedEvidence) {
		t.Fatalf("mutated snapshot error = %v", err)
	}
	_ = ot
}
func TestTodo_SKILL_002_Property(t *testing.T) {
	_, _, child := skillOntology(t)
	worker := skillRef(KindWorker, "00000000-0000-0000-0000-000000000072")
	evidence := skillEvidence(t, worker, child, "2027-01-01", true, EvidenceCredential)
	snapshot, err := NewEvidenceRevision(3, []WorkerSkillEvidence{evidence})
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := NewEvidenceRevision(snapshot.Sequence, snapshot.Evidence)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.Digest != snapshot.Digest || rebuilt.Digest != evidenceRevisionDigest(snapshot.Sequence, snapshot.Evidence) {
		t.Fatalf("evidence revision digest changed across reconstruction: %q vs %q", snapshot.Digest, rebuilt.Digest)
	}
}
func TestTodo_SKILL_002_Fault(t *testing.T) {
	ot, _, child := skillOntology(t)
	worker := skillRef(KindWorker, "00000000-0000-0000-0000-000000000074")
	e := skillEvidence(t, worker, child, "2027-01-01", true, EvidenceCredential)
	s, _ := NewEvidenceRevision(1, []WorkerSkillEvidence{e})
	s.Digest = "forged"
	if _, err := ResolveConsumers(context.Background(), ot, nil, s, ResolveRequest{Worker: worker, AsOf: skillDate(t, "2026-06-01"), SkillRefs: []values.EntityRef{child}}, skillConsumerAdapters(nil)...); !errors.Is(err, ErrPinnedEvidence) {
		t.Fatalf("forged snapshot = %v", err)
	}
}
func TestTodo_SKILL_002_Race(t *testing.T) {
	ot, _, child := skillOntology(t)
	worker := skillRef(KindWorker, "00000000-0000-0000-0000-000000000074")
	e := skillEvidence(t, worker, child, "2027-01-01", true, EvidenceCredential)
	snapshot, err := NewEvidenceRevision(1, []WorkerSkillEvidence{e})
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() {
			_, err := ResolveConsumers(context.Background(), ot, nil, snapshot, ResolveRequest{Worker: worker, AsOf: skillDate(t, "2026-06-01"), SkillRefs: []values.EntityRef{child}}, skillConsumerAdapters(nil)...)
			results <- err
		}()
	}
	for i := 0; i < 8; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}

func TestTodo_SKILL_002_FaultRejectsMaliciousLineage(t *testing.T) {
	_, _, skillRef := skillOntology(t)
	worker := skillRefForTest(KindWorker, "00000000-0000-0000-0000-000000000080")
	original := skillEvidence(t, worker, skillRef, "2027-01-01", true, EvidenceAssessment)
	makeSuccessor := func(id string, predecessor values.EntityRef, workerRef, skill values.EntityRef, start string) WorkerSkillEvidence {
		e, err := NewWorkerSkillEvidence(WorkerSkillEvidence{EvidenceID: skillRefForTest(values.Kind("skill_evidence"), id), Worker: workerRef, SkillRef: skill, Level: 2, EvidenceKind: EvidenceAssessment, EvidenceRef: "correction", Verified: true, Supersedes: predecessor, Effective: skillInterval(t, start, "2027-01-01")})
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	dangling := makeSuccessor("00000000-0000-0000-0000-000000000081", skillRefForTest(values.Kind("skill_evidence"), "00000000-0000-0000-0000-000000000099"), worker, skillRef, "2026-02-01")
	if _, err := NewEvidenceRevision(1, []WorkerSkillEvidence{original, dangling}); !errors.Is(err, ErrPinnedEvidence) {
		t.Fatalf("dangling lineage error = %v", err)
	}
	forkA := makeSuccessor("00000000-0000-0000-0000-000000000082", original.EvidenceID, worker, skillRef, "2026-02-01")
	forkB := makeSuccessor("00000000-0000-0000-0000-000000000083", original.EvidenceID, worker, skillRef, "2026-03-01")
	if _, err := NewEvidenceRevision(1, []WorkerSkillEvidence{original, forkA, forkB}); !errors.Is(err, ErrPinnedEvidence) {
		t.Fatalf("forked lineage error = %v", err)
	}
	otherWorker := skillRefForTest(KindWorker, "00000000-0000-0000-0000-000000000084")
	crossWorker := makeSuccessor("00000000-0000-0000-0000-000000000085", original.EvidenceID, otherWorker, skillRef, "2026-02-01")
	if _, err := NewEvidenceRevision(1, []WorkerSkillEvidence{original, crossWorker}); !errors.Is(err, ErrPinnedEvidence) {
		t.Fatalf("cross-worker lineage error = %v", err)
	}
	early := makeSuccessor("00000000-0000-0000-0000-000000000086", original.EvidenceID, worker, skillRef, "2025-12-01")
	if _, err := NewEvidenceRevision(1, []WorkerSkillEvidence{original, early}); !errors.Is(err, ErrPinnedEvidence) {
		t.Fatalf("backdated lineage error = %v", err)
	}
	otherSkill := skillRefForTest(KindSkill, "00000000-0000-0000-0000-000000000087")
	crossSkill := makeSuccessor("00000000-0000-0000-0000-000000000088", original.EvidenceID, worker, otherSkill, "2026-02-01")
	if _, err := NewEvidenceRevision(1, []WorkerSkillEvidence{original, crossSkill}); !errors.Is(err, ErrPinnedEvidence) {
		t.Fatalf("cross-skill lineage error = %v", err)
	}
	aID := skillRefForTest(values.Kind("skill_evidence"), "00000000-0000-0000-0000-000000000093")
	bID := skillRefForTest(values.Kind("skill_evidence"), "00000000-0000-0000-0000-000000000094")
	a := makeSuccessor(aID.Id, bID, worker, skillRef, "2026-02-01")
	b := makeSuccessor(bID.Id, aID, worker, skillRef, "2026-02-01")
	if _, err := NewEvidenceRevision(1, []WorkerSkillEvidence{a, b}); !errors.Is(err, ErrPinnedEvidence) {
		t.Fatalf("cyclic lineage error = %v", err)
	}
}

func skillRefForTest(kind values.Kind, id string) values.EntityRef { return skillRef(kind, id) }

func TestTodo_SKILL_002_ConsumerMismatchAndTemporalCorrection(t *testing.T) {
	ot, _, child := skillOntology(t)
	worker := skillRef(KindWorker, "00000000-0000-0000-0000-000000000090")
	original := skillEvidence(t, worker, child, "2027-01-01", true, EvidenceAssessment)
	future, err := NewWorkerSkillEvidence(WorkerSkillEvidence{EvidenceID: skillRef(values.Kind("skill_evidence"), "00000000-0000-0000-0000-000000000091"), Worker: worker, SkillRef: child, Level: 2, EvidenceKind: EvidenceAssessment, EvidenceRef: "future", Verified: true, Supersedes: original.EvidenceID, Effective: skillInterval(t, "2026-09-01", "2027-01-01")})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewEvidenceRevision(2, []WorkerSkillEvidence{original, future})
	if err != nil {
		t.Fatal(err)
	}
	report, err := ResolveConsumers(context.Background(), ot, nil, snapshot, ResolveRequest{Worker: worker, AsOf: skillDate(t, "2026-06-01"), SkillRefs: []values.EntityRef{child}}, skillConsumerAdapters(nil)...)
	if err != nil || report.Results[0].Resolution.Proficiencies[0].Level != 4 {
		t.Fatalf("future correction displaced current evidence: report=%+v err=%v", report, err)
	}
	bad := skillConsumerAdapters(nil)
	bad[3] = ConsumerResolverFunc{Kind: ConsumerPlanning, Fn: func(ctx context.Context, resolver Resolver, req ResolveRequest) (Resolution, string, error) {
		resolution, err := resolver.Resolve(ctx, req)
		resolution.CanonicalDigest = "different"
		return resolution, "planning mismatch", err
	}}
	if _, err := ResolveConsumers(context.Background(), ot, nil, snapshot, ResolveRequest{Worker: worker, AsOf: skillDate(t, "2026-06-01"), SkillRefs: []values.EntityRef{child}}, bad...); !errors.Is(err, ErrConsumerParity) {
		t.Fatalf("consumer mismatch error = %v", err)
	}
	if _, err := ResolveConsumers(context.Background(), ot, nil, snapshot, ResolveRequest{Worker: worker, AsOf: skillDate(t, "2026-06-01"), SkillRefs: []values.EntityRef{child}}); !errors.Is(err, ErrConformance) {
		t.Fatalf("missing adapters error = %v", err)
	}
	duplicate := skillConsumerAdapters(nil)
	duplicate[3] = ConsumerResolverFunc{Kind: ConsumerLearning, Fn: duplicate[3].(ConsumerResolverFunc).Fn}
	if _, err := ResolveConsumers(context.Background(), ot, nil, snapshot, ResolveRequest{Worker: worker, AsOf: skillDate(t, "2026-06-01"), SkillRefs: []values.EntityRef{child}}, duplicate...); !errors.Is(err, ErrConformance) {
		t.Fatalf("duplicate consumer error = %v", err)
	}
	invalid := skillConsumerAdapters(nil)
	invalid[3] = ConsumerResolverFunc{Kind: Consumer("UNKNOWN"), Fn: invalid[3].(ConsumerResolverFunc).Fn}
	if _, err := ResolveConsumers(context.Background(), ot, nil, snapshot, ResolveRequest{Worker: worker, AsOf: skillDate(t, "2026-06-01"), SkillRefs: []values.EntityRef{child}}, invalid...); !errors.Is(err, ErrConformance) {
		t.Fatalf("invalid consumer error = %v", err)
	}
}

func TestTodo_SKILL_002_SecurityRejectsCoordinatedWorkerAndAsOfForgery(t *testing.T) {
	ot, _, child := skillOntology(t)
	worker := skillRef(KindWorker, "00000000-0000-0000-0000-0000000000a0")
	otherWorker := skillRef(KindWorker, "00000000-0000-0000-0000-0000000000a1")
	evidence := skillEvidence(t, worker, child, "2027-01-01", true, EvidenceCredential)
	snapshot, err := NewEvidenceRevision(1, []WorkerSkillEvidence{evidence})
	if err != nil {
		t.Fatal(err)
	}
	wrongAsOf := skillDate(t, "2026-07-01")
	forged := func(kind Consumer) ConsumerResolver {
		return ConsumerResolverFunc{Kind: kind, Fn: func(context.Context, Resolver, ResolveRequest) (Resolution, string, error) {
			resolution := Resolution{Worker: otherWorker, AsOf: wrongAsOf, OntologyDigest: ot.CanonicalDigest, Proficiencies: []ProficiencyResult{{SkillRef: child, Status: StatusUnknown}}}
			resolution.CanonicalDigest = resolution.computedDigest()
			return resolution, "purpose-safe forged explanation", nil
		}}
	}
	adapters := []ConsumerResolver{forged(ConsumerQualification), forged(ConsumerRecruiting), forged(ConsumerLearning), forged(ConsumerPlanning)}
	if _, err := ResolveConsumers(context.Background(), ot, nil, snapshot, ResolveRequest{Worker: worker, AsOf: skillDate(t, "2026-06-01"), SkillRefs: []values.EntityRef{child}}, adapters...); !errors.Is(err, ErrConsumerParity) {
		t.Fatalf("coordinated worker/as-of forgery error = %v", err)
	}
}

func TestTodo_SKILL_002_MutationConsumerInputsDoNotAlias(t *testing.T) {
	ot, parent, child := skillOntology(t)
	worker := skillRef(KindWorker, "00000000-0000-0000-0000-0000000000a2")
	evidence := skillEvidence(t, worker, child, "2027-01-01", true, EvidenceCredential)
	snapshot, err := NewEvidenceRevision(1, []WorkerSkillEvidence{evidence})
	if err != nil {
		t.Fatal(err)
	}
	adapters := skillConsumerAdapters(nil)
	adapters[0] = ConsumerResolverFunc{Kind: ConsumerQualification, Fn: func(ctx context.Context, resolver Resolver, req ResolveRequest) (Resolution, string, error) {
		resolution, err := resolver.Resolve(ctx, req)
		req.SkillRefs[0] = parent
		resolver.Ontology.Skills[0].Aliases[0] = "mutated"
		reader := resolver.Reader.(FakeSkillEvidenceReader)
		reader.Evidence[0].EvidenceRef = "mutated"
		return resolution, "qualification purpose-safe explanation", err
	}}
	report, err := ResolveConsumers(context.Background(), ot, nil, snapshot, ResolveRequest{Worker: worker, AsOf: skillDate(t, "2026-06-01"), SkillRefs: []values.EntityRef{child}}, adapters...)
	if err != nil {
		t.Fatal(err)
	}
	if report.Results[3].Resolution.Proficiencies[0].SkillRef != child || report.Results[3].Resolution.Proficiencies[0].Level != 4 {
		t.Fatalf("later consumer observed aliased mutation: %+v", report.Results[3].Resolution)
	}
	report.EvidenceRevision.Evidence[0].EvidenceRef = "report-mutated"
	if snapshot.Evidence[0].EvidenceRef != evidence.EvidenceRef || ot.Skills[0].Aliases[0] == "mutated" {
		t.Fatal("report or consumer mutation escaped the conformance boundary")
	}
}

func TestTodo_SKILL_002_SecurityCorrectionAuthenticityAndScope(t *testing.T) {
	_, _, child := skillOntology(t)
	worker := skillRef(KindWorker, "00000000-0000-0000-0000-000000000095")
	original := skillEvidence(t, worker, child, "2027-01-01", true, EvidenceAssessment)
	successor, err := NewWorkerSkillEvidence(WorkerSkillEvidence{EvidenceID: skillRef(values.Kind("skill_evidence"), "00000000-0000-0000-0000-000000000096"), Worker: worker, SkillRef: child, Level: 2, EvidenceKind: EvidenceAssessment, EvidenceRef: "corrected", Verified: true, Supersedes: original.EvidenceID, Effective: skillInterval(t, "2026-02-01", "2027-01-01")})
	if err != nil {
		t.Fatal(err)
	}
	forged := original
	forged.CanonicalDigest = "forged"
	if _, err := CorrectEvidence(forged, successor, "reason", []Consumer{ConsumerQualification}); !errors.Is(err, ErrCorrection) {
		t.Fatalf("forged original error = %v", err)
	}
	wrongLink := successor
	wrongLink.Supersedes = successor.EvidenceID
	wrongLink.CanonicalDigest = ""
	if _, err := CorrectEvidence(original, wrongLink, "reason", []Consumer{ConsumerQualification}); !errors.Is(err, ErrCorrection) {
		t.Fatalf("wrong link error = %v", err)
	}
	if _, err := CorrectEvidence(original, successor, "reason", []Consumer{ConsumerQualification, ConsumerQualification}); !errors.Is(err, ErrCorrection) {
		t.Fatalf("duplicate scope error = %v", err)
	}
	if _, err := CorrectEvidence(original, successor, "reason", []Consumer{Consumer("UNKNOWN")}); !errors.Is(err, ErrCorrection) {
		t.Fatalf("unknown scope error = %v", err)
	}
	otherWorker := skillRef(KindWorker, "00000000-0000-0000-0000-000000000097")
	wrongWorker := successor
	wrongWorker.Worker = otherWorker
	wrongWorker.CanonicalDigest = ""
	wrongWorker, err = NewWorkerSkillEvidence(wrongWorker)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CorrectEvidence(original, wrongWorker, "reason", []Consumer{ConsumerQualification}); !errors.Is(err, ErrCorrection) {
		t.Fatalf("cross-worker correction error = %v", err)
	}
	backdated := successor
	backdated.Effective = skillInterval(t, "2025-01-01", "2027-01-01")
	backdated.CanonicalDigest = ""
	backdated, err = NewWorkerSkillEvidence(backdated)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CorrectEvidence(original, backdated, "reason", []Consumer{ConsumerQualification}); !errors.Is(err, ErrCorrection) {
		t.Fatalf("backdated correction error = %v", err)
	}
}

func TestTodo_SKILL_002_FaultRejectsInvalidSnapshotAndConsumer(t *testing.T) {
	ot, _, child := skillOntology(t)
	worker := skillRef(KindWorker, "00000000-0000-0000-0000-000000000098")
	evidence := skillEvidence(t, worker, child, "2027-01-01", true, EvidenceCredential)
	if _, err := NewEvidenceRevision(0, []WorkerSkillEvidence{evidence}); !errors.Is(err, ErrPinnedEvidence) {
		t.Fatalf("zero sequence error = %v", err)
	}
	if _, err := NewEvidenceRevision(1, nil); !errors.Is(err, ErrPinnedEvidence) {
		t.Fatalf("empty evidence error = %v", err)
	}
	if _, err := NewEvidenceRevision(1, []WorkerSkillEvidence{evidence, evidence}); !errors.Is(err, ErrPinnedEvidence) {
		t.Fatalf("duplicate evidence error = %v", err)
	}
	snapshot, err := NewEvidenceRevision(1, []WorkerSkillEvidence{evidence})
	if err != nil {
		t.Fatal(err)
	}
	broken := skillConsumerAdapters(nil)
	broken[0] = ConsumerResolverFunc{Kind: ConsumerQualification, Fn: func(context.Context, Resolver, ResolveRequest) (Resolution, string, error) {
		return Resolution{}, "", errors.New("consumer unavailable")
	}}
	if _, err := ResolveConsumers(context.Background(), ot, nil, snapshot, ResolveRequest{Worker: worker, AsOf: skillDate(t, "2026-06-01"), SkillRefs: []values.EntityRef{child}}, broken...); !errors.Is(err, ErrConformance) {
		t.Fatalf("consumer failure error = %v", err)
	}
	broken[0] = ConsumerResolverFunc{Kind: ConsumerQualification, Fn: func(ctx context.Context, resolver Resolver, req ResolveRequest) (Resolution, string, error) {
		resolution, err := resolver.Resolve(ctx, req)
		return resolution, "", err
	}}
	if _, err := ResolveConsumers(context.Background(), ot, nil, snapshot, ResolveRequest{Worker: worker, AsOf: skillDate(t, "2026-06-01"), SkillRefs: []values.EntityRef{child}}, broken...); !errors.Is(err, ErrConformance) {
		t.Fatalf("empty explanation error = %v", err)
	}
	broken[0] = ConsumerResolverFunc{Kind: ConsumerQualification, Fn: func(ctx context.Context, resolver Resolver, req ResolveRequest) (Resolution, string, error) {
		resolution, err := resolver.Resolve(ctx, req)
		resolution.CanonicalDigest = ""
		return resolution, "qualification purpose-safe explanation", err
	}}
	if _, err := ResolveConsumers(context.Background(), ot, nil, snapshot, ResolveRequest{Worker: worker, AsOf: skillDate(t, "2026-06-01"), SkillRefs: []values.EntityRef{child}}, broken...); !errors.Is(err, ErrConsumerParity) {
		t.Fatalf("missing resolution digest error = %v", err)
	}
	if _, _, err := (ConsumerResolverFunc{Kind: ConsumerQualification}).Resolve(context.Background(), Resolver{}, ResolveRequest{}); !errors.Is(err, ErrConformance) {
		t.Fatalf("nil consumer function error = %v", err)
	}
}

func TestTodo_SKILL_002_GoldenPreservesLegacyEvidenceDigest(t *testing.T) {
	_, _, child := skillOntology(t)
	worker := skillRef(KindWorker, "00000000-0000-0000-0000-000000000092")
	evidence := skillEvidence(t, worker, child, "2027-01-01", true, EvidenceCredential)
	w := canonicalbytes.New("hcmnext.domains.skill.WorkerSkillEvidence", schemaVersion).
		Value("evidence_id", evidence.EvidenceID).Value("worker", evidence.Worker).Value("skill_ref", evidence.SkillRef).
		Int("level", int64(evidence.Level)).String("proficiency", string(evidence.Proficiency)).String("evidence_kind", string(evidence.EvidenceKind)).
		String("evidence_ref", evidence.EvidenceRef).Bool("verified", evidence.Verified).Bool("disputed", evidence.Disputed).Value("effective", evidence.Effective)
	raw, err := w.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := evidence.CanonicalDigest, canonicalbytes.Digest(raw); got != want {
		t.Fatalf("legacy digest changed: got %s want %s", got, want)
	}
}

func TestTodo_SKILL_002_GoldenResolutionAuditSurface(t *testing.T) {
	ot, _, child := skillOntology(t)
	worker := skillRef(KindWorker, "00000000-0000-0000-0000-000000000099")
	evidence := skillEvidence(t, worker, child, "2027-01-01", true, EvidenceCredential)
	request := ResolveRequest{Worker: worker, AsOf: skillDate(t, "2026-06-01"), SkillRefs: []values.EntityRef{child}, Ontology: ot}
	resolution, err := Resolve(context.Background(), FakeSkillEvidenceReader{Evidence: []WorkerSkillEvidence{evidence}}, request)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Canonical() == nil {
		t.Fatal("resolution has no canonical audit bytes")
	}
	digest, err := resolution.Digest()
	if err != nil || digest != resolution.CanonicalDigest {
		t.Fatalf("resolution digest = %q, %v", digest, err)
	}
	explanation := resolution.Explain()
	if !strings.Contains(explanation, resolution.CanonicalDigest) || strings.Contains(explanation, evidence.EvidenceRef) {
		t.Fatalf("unsafe resolution explanation = %q", explanation)
	}
}
