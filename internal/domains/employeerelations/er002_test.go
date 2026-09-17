package employeerelations

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func er002Chain(t *testing.T) (AllegationRevision, InvestigationRevision, FindingRevision, DisciplineRevision, GrievanceRevision) {
	t.Helper()
	allegation, err := NewAllegationRevision(AllegationRevision{ID: "allegation-1", CaseRef: "case-1", CompartmentRef: "er-confidential", Participants: erCompartment(), ReporterRef: "person-reporter", SubjectRef: "person-subject", Summary: "protected allegation summary", Status: AllegationOpen, RetaliationSafeguard: true, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	investigation, err := NewInvestigationRevision(InvestigationRevision{ID: "investigation-1", CaseRef: "case-1", CompartmentRef: "er-confidential", Participants: erCompartment(), AllegationRef: allegation.CanonicalDigest, InvestigatorRef: "person-investigator", AuthorityRef: "authority-er-1", Purpose: "establish facts", Scope: "the reported conduct", Status: InvestigationActive, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	finding, err := NewFindingRevision(FindingRevision{ID: "finding-1", CaseRef: "case-1", CompartmentRef: "er-confidential", Participants: erCompartment(), InvestigationRef: investigation.CanonicalDigest, InvestigatorRef: "person-investigator", SubjectRef: "person-subject", ReporterRef: "person-reporter", EvidenceStandard: EvidenceMoreLikelyThanNot, EvidenceRefs: []string{"evidence-1"}, Disposition: FindingSubstantiated, Rationale: "evidence supports the finding", Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	discipline, err := NewDisciplineRevision(DisciplineRevision{ID: "discipline-1", CaseRef: "case-1", CompartmentRef: "er-confidential", Participants: erCompartment(), FindingRef: finding.CanonicalDigest, SubjectRef: "person-subject", Action: "written warning", LegalReviewRef: "legal-review-1", RepresentationReviewRef: "representation-review-1", Status: DisciplineApproved, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	grievance, err := NewGrievanceRevision(GrievanceRevision{ID: "grievance-1", CaseRef: "case-1", CompartmentRef: "er-confidential", Participants: erCompartment(), DecisionRef: discipline.CanonicalDigest, GrievantRef: "person-subject", Grounds: "decision is contested", Status: GrievanceOpen, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	return allegation, investigation, finding, discipline, grievance
}

func er002Appeal(t *testing.T, finding FindingRevision) AppealRevision {
	t.Helper()
	appeal, err := NewAppealRevision(AppealRevision{ID: "appeal-1", CaseRef: "case-1", CompartmentRef: "er-confidential", Participants: erCompartment(), ContestedDigest: finding.CanonicalDigest, ContestedKind: "finding", Grounds: "new evidence bears on the finding", DeadlineRef: "deadline:appeal:2026-04", RepresentationRef: "representation-review-2", HoldRefs: []string{"hold:legal-1"}, Status: AppealOpen, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	return appeal
}

func er002Settlement(t *testing.T, allegation AllegationRevision, grievance GrievanceRevision) SettlementRevision {
	t.Helper()
	settlement, err := NewSettlementRevision(SettlementRevision{ID: "settlement-1", CaseRef: "case-1", CompartmentRef: "er-confidential", Participants: erCompartment(), AllegationDigest: allegation.CanonicalDigest, GrievanceDigest: grievance.CanonicalDigest, TermsRef: "terms:settlement-1", AuthorityRef: "authority-er-2", HoldRefs: []string{"hold:legal-1"}, HoldsChecked: true, RetaliationSafeguard: true, Status: SettlementApproved, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	return settlement
}

func er002Chronology(t *testing.T) (CaseChronology, AppealRevision, SettlementRevision, CorrectionRevision) {
	t.Helper()
	allegation, investigation, finding, discipline, grievance := er002Chain(t)
	appeal := er002Appeal(t, finding)
	settlement := er002Settlement(t, allegation, grievance)
	correction, err := NewCorrectionRevision(CorrectionRevision{ID: "correction-1", CaseRef: "case-1", CompartmentRef: "er-confidential", Participants: erCompartment(), CorrectsDigest: discipline.CanonicalDigest, CorrectsKind: "discipline", Correction: "action reduced to verbal counseling", Reason: "length of service", AuthorityRef: "authority-er-3", Status: CorrectionIssued, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	chronology, err := NewCaseChronology("case-1", "er-confidential", erCompartment())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range [][3]string{
		{"allegation", "allegation-1", allegation.CanonicalDigest},
		{"investigation", "investigation-1", investigation.CanonicalDigest},
		{"finding", "finding-1", finding.CanonicalDigest},
		{"discipline", "discipline-1", discipline.CanonicalDigest},
		{"grievance", "grievance-1", grievance.CanonicalDigest},
	} {
		if err := chronology.AppendRecord(entry[0], entry[1], entry[2]); err != nil {
			t.Fatal(err)
		}
	}
	if err := chronology.AppendAppeal(appeal); err != nil {
		t.Fatal(err)
	}
	if err := chronology.AppendSettlement(settlement); err != nil {
		t.Fatal(err)
	}
	if err := chronology.AppendCorrection(correction); err != nil {
		t.Fatal(err)
	}
	return chronology, appeal, settlement, correction
}

// TestEmployeeRelationsConformancePreservesAppealSettlementAndRetaliationSafeguards
// is the RED contract: successor proceedings keep compartments and
// chronology, and safeguards survive them.
func TestEmployeeRelationsConformancePreservesAppealSettlementAndRetaliationSafeguards(t *testing.T) {
	chronology, appeal, settlement, correction := er002Chronology(t)
	if len(chronology.Entries) != 8 {
		t.Fatalf("entries = %d, want 8", len(chronology.Entries))
	}
	// The original finding and discipline are still present: the appeal and
	// correction supersede without overwriting.
	byDigest := map[string]ChronologyEntry{}
	for _, e := range chronology.Entries {
		byDigest[e.Digest] = e
	}
	if _, ok := byDigest[appeal.ContestedDigest]; !ok {
		t.Fatal("appeal overwrote its finding")
	}
	if _, ok := byDigest[correction.CorrectsDigest]; !ok {
		t.Fatal("correction removed its discipline")
	}
	// The settlement preserves both sides by reference.
	if _, ok := byDigest[settlement.AllegationDigest]; !ok {
		t.Fatal("settlement erased its allegation")
	}
	if _, ok := byDigest[settlement.GrievanceDigest]; !ok {
		t.Fatal("settlement erased its grievance")
	}
	// Deadlines, holds and representation stay explicit.
	pkg := chronology.EvidencePackage()
	if len(pkg.Deadlines) == 0 || len(pkg.Holds) == 0 {
		t.Fatalf("package lacks deadlines/holds: %+v", pkg)
	}
	// The retaliation signal never reaches the subject's manager.
	signal, err := NewRetaliationSignal(RetaliationSignal{ID: "retaliation-1", CaseRef: "case-1", CompartmentRef: "er-confidential", ReporterRef: "person-reporter", SubjectRef: "person-subject", ManagerRef: "person-manager", DetailRef: "evidence:retaliation-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := signal.AuthorizeViewer("person-manager", RoleDecisionMaker, erCompartment()); !errors.Is(err, ErrRetaliationProtected) {
		t.Fatalf("manager view err=%v, want protected", err)
	}
	if err := signal.AuthorizeViewer("person-investigator", RoleInvestigator, erCompartment()); err != nil {
		t.Fatalf("investigator view: %v", err)
	}
}

// TestTodo_ER_002_Property proves every successor append preserves the prior
// prefix and carries its safeguards.
func TestTodo_ER_002_Property(t *testing.T) {
	allegation, _, finding, discipline, grievance := er002Chain(t)
	appeal := er002Appeal(t, finding)
	settlement := er002Settlement(t, allegation, grievance)
	chronology, err := NewCaseChronology("case-1", "er-confidential", erCompartment())
	if err != nil {
		t.Fatal(err)
	}
	prefix := 0
	appendAndCheck := func(kind, id, digest string, appendFn func() error) {
		t.Helper()
		before := append([]ChronologyEntry(nil), chronology.Entries...)
		if err := appendFn(); err != nil {
			t.Fatalf("append %s: %v", kind, err)
		}
		if len(chronology.Entries) != prefix+1 {
			t.Fatalf("append %s grew by %d", kind, len(chronology.Entries)-prefix)
		}
		for i, e := range before {
			if chronology.Entries[i] != e {
				t.Fatalf("append %s rewrote entry %d", kind, i)
			}
		}
		last := chronology.Entries[len(chronology.Entries)-1]
		if last.Kind != kind || last.ID != id || last.Digest != digest {
			t.Fatalf("append %s last = %+v", kind, last)
		}
		prefix++
	}
	appendAndCheck("allegation", "allegation-1", allegation.CanonicalDigest, func() error { return chronology.AppendRecord("allegation", "allegation-1", allegation.CanonicalDigest) })
	appendAndCheck("finding", "finding-1", finding.CanonicalDigest, func() error { return chronology.AppendRecord("finding", "finding-1", finding.CanonicalDigest) })
	appendAndCheck("discipline", "discipline-1", discipline.CanonicalDigest, func() error { return chronology.AppendRecord("discipline", "discipline-1", discipline.CanonicalDigest) })
	appendAndCheck("grievance", "grievance-1", grievance.CanonicalDigest, func() error { return chronology.AppendRecord("grievance", "grievance-1", grievance.CanonicalDigest) })
	appendAndCheck("appeal", "appeal-1", appeal.CanonicalDigest, func() error { return chronology.AppendAppeal(appeal) })
	appendAndCheck("settlement", "settlement-1", settlement.CanonicalDigest, func() error { return chronology.AppendSettlement(settlement) })
}

// TestTodo_ER_002_Golden pins the exact evidence package of the fixture
// chain: ordered kinds, supersede links, holds and deadlines.
func TestTodo_ER_002_Golden(t *testing.T) {
	chronology, appeal, settlement, correction := er002Chronology(t)
	pkg := chronology.EvidencePackage()
	wantKinds := []string{"allegation", "investigation", "finding", "discipline", "grievance", "appeal", "settlement", "correction"}
	if len(pkg.Entries) != len(wantKinds) {
		t.Fatalf("entries = %+v", pkg.Entries)
	}
	for i, kind := range wantKinds {
		if pkg.Entries[i].Kind != kind {
			t.Fatalf("entry %d = %s, want %s", i, pkg.Entries[i].Kind, kind)
		}
	}
	if pkg.Entries[5].Supersedes != appeal.ContestedDigest || pkg.Entries[7].Supersedes != correction.CorrectsDigest {
		t.Fatalf("supersede links = %+v", pkg.Entries)
	}
	if pkg.Digest == "" {
		t.Fatal("package carries no digest")
	}
	again := chronology.EvidencePackage()
	if again.Digest != pkg.Digest {
		t.Fatal("package digest unstable")
	}
	// The successor API surface is exercised: canonical bytes pin, digests
	// agree, explanations carry no identities, aliases seal identically.
	if appeal.Canonical() == nil || settlement.Canonical() == nil || correction.Canonical() == nil {
		t.Fatal("canonical bytes missing")
	}
	ad, err := appeal.Digest()
	if err != nil || ad != appeal.CanonicalDigest {
		t.Fatalf("appeal digest = %q, err=%v", ad, err)
	}
	sd, err := settlement.Digest()
	if err != nil || sd != settlement.CanonicalDigest {
		t.Fatalf("settlement digest = %q, err=%v", sd, err)
	}
	cd, err := correction.Digest()
	if err != nil || cd != correction.CanonicalDigest {
		t.Fatalf("correction digest = %q, err=%v", cd, err)
	}
	for _, text := range []string{appeal.Explain(), settlement.Explain(), correction.Explain()} {
		if text == "" {
			t.Fatal("empty explanation")
		}
		for _, leaked := range []string{"person-reporter", "person-subject", "person-manager", "person-investigator"} {
			if strings.Contains(text, leaked) {
				t.Fatalf("explanation leaks %q: %q", leaked, text)
			}
		}
	}
	appeal.CanonicalDigest = ""
	if _, err := NewAppeal(appeal); err != nil {
		t.Fatalf("NewAppeal: %v", err)
	}
	settlement.CanonicalDigest = ""
	if _, err := NewSettlement(settlement); err != nil {
		t.Fatalf("NewSettlement: %v", err)
	}
	correction.CanonicalDigest = ""
	if _, err := NewCorrection(correction); err != nil {
		t.Fatalf("NewCorrection: %v", err)
	}
}

// TestTodo_ER_002_Race proves concurrent saves and digest reads stay
// consistent on the shared store.
func TestTodo_ER_002_Race(t *testing.T) {
	_, _, _, _, grievance := er002Chain(t)
	store := NewMemoryStore()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g := grievance
			if err := store.SaveGrievance(g); err != nil {
				t.Error(err)
			}
			if _, ok := store.GetGrievance(g.ID, g.Revision); !ok {
				t.Error("grievance missing after save")
			}
			if _, err := g.Digest(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

// TestTodo_ER_002_Fault proves gaps and missing authorities refuse before
// any entry lands.
func TestTodo_ER_002_Fault(t *testing.T) {
	allegation, _, finding, _, grievance := er002Chain(t)
	appeal := er002Appeal(t, finding)
	chronology, err := NewCaseChronology("case-1", "er-confidential", erCompartment())
	if err != nil {
		t.Fatal(err)
	}
	// Appeal of a finding the chronology never saw.
	if err := chronology.AppendAppeal(appeal); !errors.Is(err, ErrChronologyGap) {
		t.Fatalf("gap appeal err=%v, want gap", err)
	}
	// Settlement that erases an unseen grievance.
	settlement := er002Settlement(t, allegation, grievance)
	if err := chronology.AppendSettlement(settlement); !errors.Is(err, ErrChronologyGap) {
		t.Fatalf("gap settlement err=%v, want gap", err)
	}
	// Correction of an unknown discipline.
	correction, err := NewCorrectionRevision(CorrectionRevision{ID: "correction-9", CaseRef: "case-1", CompartmentRef: "er-confidential", Participants: erCompartment(), CorrectsDigest: "sha256:unknown", CorrectsKind: "discipline", Correction: "change", Reason: "reason", AuthorityRef: "authority-er-3", Status: CorrectionIssued, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := chronology.AppendCorrection(correction); !errors.Is(err, ErrChronologyGap) {
		t.Fatalf("gap correction err=%v, want gap", err)
	}
	// Appeal without a deadline.
	nodeadline := appeal
	nodeadline.ID = "appeal-2"
	nodeadline.Revision = 1
	nodeadline.ParentRevision = 0
	nodeadline.ParentDigest = ""
	nodeadline.DeadlineRef = ""
	nodeadline.CanonicalDigest = ""
	if _, err := NewAppealRevision(nodeadline); !errors.Is(err, ErrDeadlineRequired) {
		t.Fatalf("deadline-free appeal err=%v, want deadline refusal", err)
	}
	if len(chronology.Entries) != 0 {
		t.Fatal("refused appends landed entries")
	}
}

// TestTodo_ER_002_Security proves retaliation and identity safeguards: the
// subject and manager stay blind, and explanations carry no identities.
func TestTodo_ER_002_Security(t *testing.T) {
	signal, err := NewRetaliationSignal(RetaliationSignal{ID: "retaliation-1", CaseRef: "case-1", CompartmentRef: "er-confidential", ReporterRef: "person-reporter", SubjectRef: "person-subject", ManagerRef: "person-manager", DetailRef: "evidence:retaliation-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := signal.AuthorizeViewer("person-subject", RoleSubject, erCompartment()); !errors.Is(err, ErrRetaliationProtected) {
		t.Fatalf("subject view err=%v, want protected", err)
	}
	if err := signal.AuthorizeViewer("person-stranger", RoleInvestigator, erCompartment()); !errors.Is(err, ErrRetaliationProtected) {
		t.Fatalf("stranger view err=%v, want protected", err)
	}
	for _, leaked := range []string{"person-reporter", "person-subject", "person-manager"} {
		if strings.Contains(signal.Explain(), leaked) {
			t.Fatalf("signal explanation leaks %q", leaked)
		}
	}
	chronology, _, _, _ := er002Chronology(t)
	for _, leaked := range []string{"person-reporter", "person-subject", "person-manager"} {
		if strings.Contains(chronology.EvidencePackage().Explain(), leaked) {
			t.Fatalf("package explanation leaks %q", leaked)
		}
	}
	pkg := chronology.EvidencePackage()
	for _, e := range pkg.Entries {
		if strings.Contains(e.ID, "person-") || strings.Contains(e.Digest, "person-") {
			t.Fatalf("package entry leaks identity: %+v", e)
		}
	}
}

// TestTodo_ER_002_Conformance proves the chronology agrees with the stored
// canonical digests: every entry resolves through the store-backed oracle.
func TestTodo_ER_002_Conformance(t *testing.T) {
	allegation, investigation, finding, discipline, grievance := er002Chain(t)
	store := NewMemoryStore()
	for _, save := range []func() error{
		func() error { return store.SaveAllegation(allegation) },
		func() error { return store.SaveInvestigation(investigation) },
		func() error { return store.SaveFinding(finding) },
		func() error { return store.SaveDiscipline(discipline) },
		func() error { return store.SaveGrievance(grievance) },
	} {
		if err := save(); err != nil {
			t.Fatal(err)
		}
	}
	chronology, appeal, settlement, correction := er002Chronology(t)
	resolve := func(kind, id string) (string, bool) {
		switch kind {
		case "allegation":
			r, ok := store.GetAllegation(id, 1)
			return r.CanonicalDigest, ok
		case "investigation":
			r, ok := store.GetInvestigation(id, 1)
			return r.CanonicalDigest, ok
		case "finding":
			r, ok := store.GetFinding(id, 1)
			return r.CanonicalDigest, ok
		case "discipline":
			r, ok := store.GetDiscipline(id, 1)
			return r.CanonicalDigest, ok
		case "grievance":
			r, ok := store.GetGrievance(id, 1)
			return r.CanonicalDigest, ok
		case "appeal":
			return appeal.CanonicalDigest, id == appeal.ID
		case "settlement":
			return settlement.CanonicalDigest, id == settlement.ID
		case "correction":
			return correction.CanonicalDigest, id == correction.ID
		}
		return "", false
	}
	if err := chronology.VerifyDigests(resolve); err != nil {
		t.Fatalf("VerifyDigests: %v", err)
	}
	broken := chronology
	broken.Entries[2].Digest = "sha256:forged"
	if err := broken.VerifyDigests(resolve); !errors.Is(err, ErrChronologyGap) {
		t.Fatalf("forged entry err=%v, want gap", err)
	}
}

// TestTodo_ER_002_Mutation proves tampered successors cannot slip through as
// clean chronology entries.
func TestTodo_ER_002_Mutation(t *testing.T) {
	_, _, finding, _, _ := er002Chain(t)
	appeal := er002Appeal(t, finding)
	// A tampered contest with a stale digest refuses revalidation: stored
	// values are always revalidated, never trusted.
	tampered := appeal
	tampered.ContestedDigest = "sha256:forged"
	if err := tampered.Validate(); !errors.Is(err, ErrInvalidRevision) {
		t.Fatalf("stale-digest appeal err=%v, want invalid", err)
	}
	// Reissued against an unseen record it shapes fine but never lands.
	tampered.CanonicalDigest = ""
	reissued, err := NewAppealRevision(tampered)
	if err != nil {
		t.Fatalf("reissue: %v", err)
	}
	chronology, err := NewCaseChronology("case-1", "er-confidential", erCompartment())
	if err != nil {
		t.Fatal(err)
	}
	if err := chronology.AppendRecord("finding", "finding-1", finding.CanonicalDigest); err != nil {
		t.Fatal(err)
	}
	if err := chronology.AppendAppeal(reissued); !errors.Is(err, ErrChronologyGap) {
		t.Fatalf("gap appeal err=%v, want gap", err)
	}
	// A cross-case appeal never lands either.
	other := appeal
	other.CaseRef = "case-2"
	other.CanonicalDigest = ""
	recased, err := NewAppealRevision(other)
	if err != nil {
		t.Fatalf("recase: %v", err)
	}
	if err := chronology.AppendAppeal(recased); !errors.Is(err, ErrChronologyGap) {
		t.Fatalf("cross-case appeal err=%v, want gap", err)
	}
	if err := chronology.AppendAppeal(appeal); err != nil {
		t.Fatalf("same-compartment appeal: %v", err)
	}
}
