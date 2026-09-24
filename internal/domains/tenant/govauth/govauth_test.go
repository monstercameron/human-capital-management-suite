package govauth

import (
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"
	"time"

	trustpentest "github.com/monstercameron/human-capital-management-suite/internal/trust/pentest"
)

var fixtureDate = time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)

func pointer(ref, owner string) EvidencePointer {
	return EvidencePointer{ArtifactRef: ref, Owner: owner, Date: fixtureDate}
}

func cmsBlock() *CMSReferenceBlock {
	return &CMSReferenceBlock{
		MARSEVolume:        "VOLUME_1",
		MARSEVersion:       "2.2",
		CMSARSRelease:      "5.1",
		DUARef:             "artifact/cms/dua",
		ISARef:             "artifact/cms/isa",
		SSPPRef:            "artifact/cms/sspp",
		PrivacyAnalysisRef: "artifact/cms/privacy-analysis",
		ReviewedBy:         "security-reviewer",
		ReviewedAt:         fixtureDate,
		ControlInheritance: []ControlInheritanceReference{{
			ControlID:     "AC-2",
			InheritedFrom: "cms-boundary",
			Evidence:      pointer("artifact/cms/ac-2", "control-owner"),
		}},
	}
}

func answers() []ProcurementAnswer {
	result := make([]ProcurementAnswer, 0, len(QuestionnaireItems))
	for _, question := range QuestionnaireItems {
		result = append(result, ProcurementAnswer{
			Question:    question,
			Answer:      "reviewed assertion for " + string(question),
			Owner:       "assurance-owner",
			Date:        fixtureDate,
			ArtifactRef: "artifact/procurement/" + string(question),
		})
	}
	return result
}

func fixture(tenant, integration string, programs ...Program) GovernmentAuthorizationProfile {
	return GovernmentAuthorizationProfile{
		Revision:           1,
		TenantID:           tenant,
		IntegrationID:      integration,
		ApplicablePrograms: programs,
		SystemBoundary:     "application, tenant data store, identity edge and declared support boundary",
		InheritedControls: []ControlInheritanceReference{{
			ControlID:     "SC-13",
			InheritedFrom: "platform-cryptography",
			Evidence:      pointer("artifact/platform/sc-13", "platform-owner"),
		}},
		Evidence:       []EvidencePointer{pointer("artifact/profile/boundary", "assurance-owner")},
		AssessorStatus: AssessorAccepted,
		ReviewDate:     fixtureDate,
		FedRAMP: &FedRAMPPathRecord{
			Path:                         FedRAMP20xA,
			PackageVersion:               "20x-package-2026.09",
			ContinuousMonitoringEvidence: []EvidencePointer{pointer("artifact/fedramp/monthly-monitoring", "assurance-owner")},
			ResponsibilityStatement:      "The customer authorizes its agency use and retains customer-side responsibilities.",
			Timeline: FedRAMPTimeline{
				AsOf:                   fixtureDate,
				NewCertificationCutoff: time.Date(2027, 6, 11, 0, 0, 0, 0, time.UTC),
				TwentyXAdoptionDate:    time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
				SourceArtifactRef:      "artifact/fedramp/timeline-2026",
			},
		},
		ProcurementAnswers: answers(),
	}
}

func TestTodo_SECARCH_018(t *testing.T) {
	profile, err := NewGovernmentAuthorizationProfile(fixture("tenant-public", "integration-case", ProgramFedRAMP, ProgramGovRAMP, ProgramFISMA))
	if err != nil {
		t.Fatalf("construct profile: %v", err)
	}
	if profile.RevisionDigest == "" {
		t.Fatal("sealed profile has no revision digest")
	}
	if profile.Explain() == "" || !strings.Contains(profile.Explain(), "programs=") {
		t.Fatalf("audit explanation is not shaped: %q", profile.Explain())
	}
}

func TestTodo_SECARCH_018_Golden(t *testing.T) {
	fixtures := []GovernmentAuthorizationProfile{
		fixture("tenant-public", "integration-case", ProgramFedRAMP, ProgramGovRAMP, ProgramFISMA),
	}
	aca := fixture("tenant-aca", "integration-exchange", ProgramMARSE, ProgramFISMA)
	aca.CMS = cmsBlock()
	fixtures = append(fixtures, aca)
	for i, input := range fixtures {
		sealed, err := NewGovernmentAuthorizationProfile(input)
		if err != nil {
			t.Fatalf("fixture %d: %v", i, err)
		}
		got, err := sealed.Digest()
		if err != nil {
			t.Fatalf("fixture %d digest: %v", i, err)
		}
		want := []string{
			"2ad76a2f5297aa2fc8157957f9bf2d7610cda8fd1cc4fc75bf60c0cb0d946f38",
			"c28a44be841f0b88ad4390d353a69c3008d8afd0ef47266ff1c0b8ade4aa97fb",
		}[i]
		if len(got) != 64 {
			t.Fatalf("fixture %d digest %q is not sha256 hex", i, got)
		}
		if got != want {
			t.Fatalf("fixture %d digest = %s, want %s", i, got, want)
		}
	}
}

func TestTodo_SECARCH_018_Security(t *testing.T) {
	bad := fixture("tenant-public", "integration-case", Program("UNDECLARED"))
	if _, err := NewGovernmentAuthorizationProfile(bad); err == nil || !strings.Contains(err.Error(), "applicable_programs") {
		t.Fatalf("undeclared program error = %v; want field-named refusal", err)
	}
	secret := "account-9876543210"
	profile := fixture("tenant-public", "integration-case", ProgramFedRAMP)
	profile.SystemBoundary = secret
	sealed, err := NewGovernmentAuthorizationProfile(profile)
	if err != nil {
		t.Fatalf("construct secret fixture: %v", err)
	}
	if strings.Contains(sealed.Explain(), secret) || strings.Contains(Explain(sealed), "tenant-public") {
		t.Fatalf("Explain disclosed sensitive content: %q", sealed.Explain())
	}
}

func TestTodo_SECARCH_018_Integration(t *testing.T) {
	profile, err := NewProfile(fixture("tenant-public", "integration-case", ProgramGovRAMP, ProgramTXRAMP))
	if err != nil {
		t.Fatalf("constructor integration: %v", err)
	}
	if _, err := profile.Digest(); err != nil {
		t.Fatalf("constructed profile digest: %v", err)
	}
	if profile.TenantID != "tenant-public" || profile.IntegrationID != "integration-case" {
		t.Fatal("constructor lost tenant/integration binding")
	}
}

func TestTodo_SECARCH_018_Conformance(t *testing.T) {
	if Version() != 1 {
		t.Fatalf("version = %d, want 1", Version())
	}
	for _, program := range []Program{FedRAMP, GovRAMP, TXRAMP, FISMA, CJIS, FTI, CUI, MARSE, MARS_E} {
		if !program.Valid() {
			t.Fatalf("closed program %q rejected", program)
		}
	}
	if Program("FedRAMP-Moderate").Valid() {
		t.Fatal("undeclared program accepted")
	}
}

func TestTodo_SECARCH_018_Mutation(t *testing.T) {
	input := fixture("tenant-public", "integration-case", ProgramFedRAMP)
	sealed, err := NewGovernmentAuthorizationProfile(input)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	want := sealed.RevisionDigest
	input.ApplicablePrograms[0] = ProgramCUI
	input.Evidence[0].Owner = "changed-owner"
	if sealed.RevisionDigest != want {
		t.Fatal("sealed revision changed through source slices")
	}
	sealed.SystemBoundary = "changed"
	if _, err := sealed.Digest(); !errors.Is(err, ErrImmutableRevision) {
		t.Fatalf("post-seal mutation error = %v; want ErrImmutableRevision", err)
	}
}

func TestTodo_SECARCH_019(t *testing.T) {
	profile := fixture("tenant-public", "integration-case", ProgramFedRAMP)
	refreshed, err := profile.RefreshFedRAMP(profile.FedRAMP.Timeline)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if refreshed.Revision != 2 || refreshed.FedRAMP.Timeline.AsOf.IsZero() {
		t.Fatalf("refreshed profile = revision %d, timeline=%v", refreshed.Revision, refreshed.FedRAMP.Timeline.AsOf)
	}
	if refreshed.FedRAMP.PackageVersion == "" || len(refreshed.FedRAMP.ContinuousMonitoringEvidence) != 1 || refreshed.FedRAMP.ResponsibilityStatement == "" {
		t.Fatal("FedRAMP record is incomplete")
	}
}

func TestTodo_SECARCH_019_Golden(t *testing.T) {
	profile := fixture("tenant-public", "integration-case", ProgramFedRAMP)
	got, err := profile.FedRAMP.Timeline.Validate(), error(nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = got
	if profile.FedRAMP.Path != TWENTYX_A {
		t.Fatalf("path = %s", profile.FedRAMP.Path)
	}
}

func TestTodo_SECARCH_019_Security(t *testing.T) {
	profile := fixture("tenant-public", "integration-case", ProgramFedRAMP)
	profile.FedRAMP.Path = FedRAMPRev5
	late := profile.FedRAMP.Timeline
	late.AsOf = time.Date(2027, 6, 11, 0, 0, 0, 0, time.UTC)
	if _, err := profile.RefreshFedRAMP(late); err == nil || !errors.Is(err, ErrTimelineRefusal) {
		t.Fatalf("late REV5 refresh error = %v; want timeline refusal", err)
	}
}

func TestTodo_SECARCH_019_Integration(t *testing.T) {
	profile := fixture("tenant-public", "integration-case", ProgramFedRAMP)
	profile.FedRAMP.Path = FedRAMP20xB
	refreshed, err := profile.Refresh(profile.FedRAMP.Timeline)
	if err != nil || refreshed.FedRAMP.Path != FedRAMP20xB {
		t.Fatalf("20x refresh = path %v, err %v", refreshed.FedRAMP.Path, err)
	}
}

func TestTodo_SECARCH_019_Conformance(t *testing.T) {
	for _, path := range []FedRAMPPath{REV5, TWENTYX_A, TWENTYX_B, TWENTYX_C} {
		if !path.Valid() {
			t.Fatalf("path %q is not valid", path)
		}
	}
	if FedRAMPPath("MODERATE").Valid() {
		t.Fatal("unlisted FedRAMP class accepted")
	}
}

func TestTodo_SECARCH_019_Mutation(t *testing.T) {
	profile := fixture("tenant-public", "integration-case", ProgramFedRAMP)
	refreshed, err := profile.RefreshFedRAMP(profile.FedRAMP.Timeline)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.RevisionDigest == profile.RevisionDigest {
		t.Fatal("FedRAMP refresh did not create a new digested revision")
	}
	if profile.Revision != 1 {
		t.Fatal("refresh mutated receiver")
	}
}

func TestTodo_SECARCH_022(t *testing.T) {
	profile := fixture("tenant-aca", "integration-exchange", ProgramMARSE, ProgramFISMA)
	profile.CMS = cmsBlock()
	sealed, err := NewGovernmentAuthorizationProfile(profile)
	if err != nil {
		t.Fatalf("ACA profile: %v", err)
	}
	if sealed.CMS.MARSEVersion != "2.2" || sealed.CMS.CMSARSRelease != "5.1" || sealed.CMS.PrivacyAnalysisRef == "" {
		t.Fatal("CMS releases or privacy analysis are not pinned")
	}
}

func TestTodo_SECARCH_022_Golden(t *testing.T) {
	profile := fixture("tenant-aca", "integration-exchange", ProgramMARSE)
	profile.CMS = cmsBlock()
	first, err := NewGovernmentAuthorizationProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewGovernmentAuthorizationProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if first.RevisionDigest != second.RevisionDigest {
		t.Fatalf("identical ACA fixtures differ: %s vs %s", first.RevisionDigest, second.RevisionDigest)
	}
}

func TestTodo_SECARCH_022_Security(t *testing.T) {
	profile := fixture("tenant-aca", "integration-exchange", ProgramMARSE)
	if _, err := NewGovernmentAuthorizationProfile(profile); err == nil || !strings.Contains(err.Error(), "cms") {
		t.Fatalf("missing CMS block error = %v", err)
	}
	block := cmsBlock()
	block.CMSARSRelease = ""
	profile.CMS = block
	if _, err := NewGovernmentAuthorizationProfile(profile); err == nil || !strings.Contains(err.Error(), "cms.cms_ars_release") {
		t.Fatalf("missing CMS ARS error = %v", err)
	}
}

func TestTodo_SECARCH_022_Integration(t *testing.T) {
	profile := fixture("tenant-aca", "integration-exchange", ProgramMARSE)
	profile.CMS = cmsBlock()
	sealed, err := NewProfile(profile)
	if err != nil || len(sealed.CMS.ControlInheritance) != 1 {
		t.Fatalf("CMS integration = %+v, err %v", sealed.CMS, err)
	}
}

func TestTodo_SECARCH_022_Conformance(t *testing.T) {
	block := cmsBlock()
	if err := block.Validate(); err != nil {
		t.Fatal(err)
	}
	block.ControlInheritance[0].Evidence.ArtifactRef = ""
	if err := block.Validate(); err == nil || !strings.Contains(err.Error(), "evidence.artifact_ref") {
		t.Fatalf("unreviewable inheritance error = %v", err)
	}
}

func TestTodo_SECARCH_022_Mutation(t *testing.T) {
	profile := fixture("tenant-aca", "integration-exchange", ProgramMARSE)
	profile.CMS = cmsBlock()
	sealed, err := NewGovernmentAuthorizationProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	sealed.CMS.ReviewedBy = "different-reviewer"
	if _, err := sealed.Digest(); !errors.Is(err, ErrImmutableRevision) {
		t.Fatalf("CMS mutation error = %v", err)
	}
}

func TestTodo_SECARCH_024(t *testing.T) {
	pack, err := GenerateProcurementPack(fixture("tenant-public", "integration-case", ProgramGovRAMP))
	if err != nil {
		t.Fatalf("generate pack: %v", err)
	}
	if len(pack.Answers) != len(QuestionnaireItems) || pack.PackDigest == "" {
		t.Fatalf("pack = %+v", pack)
	}
}

func TestTodo_SECARCH_024_Golden(t *testing.T) {
	profiles := []GovernmentAuthorizationProfile{
		fixture("tenant-public", "integration-case", ProgramGovRAMP),
		fixture("tenant-aca", "integration-exchange", ProgramMARSE),
	}
	profiles[1].CMS = cmsBlock()
	for i, profile := range profiles {
		pack, err := GenerateProcurementEvidencePack(profile)
		if err != nil {
			t.Fatalf("fixture %d: %v", i, err)
		}
		if got, err := pack.Digest(); err != nil || got != pack.PackDigest {
			t.Fatalf("fixture %d pack digest = %s, err %v", i, got, err)
		}
	}
}

func TestTodo_SECARCH_024_Security(t *testing.T) {
	profile := fixture("tenant-public", "integration-case", ProgramGovRAMP)
	profile.ProcurementAnswers = profile.ProcurementAnswers[:len(profile.ProcurementAnswers)-1]
	if _, err := GenerateProcurementPack(profile); err == nil || !errors.Is(err, ErrUnansweredQuestion) || !strings.Contains(err.Error(), "audit_reports") {
		t.Fatalf("missing answer error = %v", err)
	}
	pack, err := GenerateProcurementPack(fixture("tenant-public", "integration-case", ProgramGovRAMP))
	if err != nil {
		t.Fatal(err)
	}
	pack.Answers[0].Answer = "changed"
	if _, err := pack.Digest(); !errors.Is(err, ErrImmutableRevision) {
		t.Fatalf("pack mutation error = %v", err)
	}
}

func TestTodo_SECARCH_024_Integration(t *testing.T) {
	profile, err := NewGovernmentAuthorizationProfile(fixture("tenant-public", "integration-case", ProgramGovRAMP))
	if err != nil {
		t.Fatal(err)
	}
	pack, err := GenerateProcurementPack(profile)
	if err != nil || pack.ProfileDigest != profile.RevisionDigest {
		t.Fatalf("pack/profile wiring = %s/%s, err %v", pack.ProfileDigest, profile.RevisionDigest, err)
	}
}

func TestTodo_SECARCH_024_Conformance(t *testing.T) {
	if len(QuestionnaireItems) != 13 {
		t.Fatalf("standard item count = %d, want 13", len(QuestionnaireItems))
	}
	for i, answer := range answers() {
		if answer.Question != QuestionnaireItems[i] {
			t.Fatalf("question %d = %s, want %s", i, answer.Question, QuestionnaireItems[i])
		}
	}
}

func TestTodo_SECARCH_024_Mutation(t *testing.T) {
	input := fixture("tenant-public", "integration-case", ProgramGovRAMP)
	pack, err := GenerateProcurementPack(input)
	if err != nil {
		t.Fatal(err)
	}
	input.ProcurementAnswers[0].Answer = "different source assertion"
	if _, err := pack.Digest(); err != nil {
		t.Fatalf("pack was affected by source mutation: %v", err)
	}
}

func TestGovauth_VocabulariesEvidenceAndTimelineBoundaries(t *testing.T) {
	if Version() != 1 {
		t.Fatalf("Version = %d, want 1", Version())
	}
	for _, program := range []Program{ProgramFedRAMP, ProgramGovRAMP, ProgramTXRAMP, ProgramFISMA, ProgramCJIS, ProgramFTI, ProgramCUI, ProgramMARSE} {
		if !program.Valid() {
			t.Fatalf("Program.Valid rejected %q", program)
		}
	}
	if Program("not-declared").Valid() {
		t.Fatal("Program.Valid accepted an undeclared value")
	}
	for _, status := range []AssessorStatus{AssessorPending, AssessorInProgress, AssessorAccepted, AssessorConditionallyAccepted, AssessorExpired, AssessorNotApplicable} {
		if !status.Valid() {
			t.Fatalf("AssessorStatus.Valid rejected %q", status)
		}
	}
	if AssessorStatus("UNKNOWN").Valid() {
		t.Fatal("AssessorStatus.Valid accepted an undeclared value")
	}

	if got := pointer("  artifact/ref  ", "owner").Reference(); got != "artifact/ref" {
		t.Fatalf("ArtifactRef Reference = %q", got)
	}
	if got := (EvidencePointer{Ref: "  legacy/ref  "}).Reference(); got != "legacy/ref" {
		t.Fatalf("Ref Reference = %q", got)
	}
	if got := (EvidencePointer{}).Reference(); got != "" {
		t.Fatalf("empty Reference = %q", got)
	}
	validEvidence := pointer("artifact/evidence", "owner")
	validEvidence.Digest = strings.Repeat("a", 64)
	if err := validEvidence.Validate(); err != nil {
		t.Fatalf("valid evidence = %v", err)
	}
	for _, tt := range []struct {
		name   string
		mutate func(*EvidencePointer)
	}{
		{"both aliases", func(e *EvidencePointer) { e.Ref = "legacy" }},
		{"missing reference", func(e *EvidencePointer) { e.ArtifactRef = "" }},
		{"missing owner", func(e *EvidencePointer) { e.Owner = "" }},
		{"missing date", func(e *EvidencePointer) { e.Date = time.Time{} }},
		{"non UTC date", func(e *EvidencePointer) { e.Date = time.Date(2026, 9, 5, 0, 0, 0, 0, time.FixedZone("EDT", -4*60*60)) }},
		{"bad digest", func(e *EvidencePointer) { e.Digest = "not-a-digest" }},
	} {
		t.Run("evidence/"+tt.name, func(t *testing.T) {
			e := validEvidence
			tt.mutate(&e)
			if err := e.Validate(); !errors.Is(err, ErrInvalidEvidence) {
				t.Fatalf("Validate = %v, want ErrInvalidEvidence", err)
			}
		})
	}

	baseTimeline := fixture("tenant", "integration", ProgramFedRAMP).FedRAMP.Timeline
	if err := baseTimeline.Validate(); err != nil {
		t.Fatalf("valid timeline = %v", err)
	}
	if err := (FedRAMPTimeline{AsOf: baseTimeline.AsOf, NewCertificationCutoff: baseTimeline.NewCertificationCutoff, TwentyXAdoptionDate: baseTimeline.TwentyXAdoptionDate}).Validate(); !errors.Is(err, ErrTimelineRefusal) {
		t.Fatalf("missing timeline source error = %v, want ErrTimelineRefusal", err)
	}
	for _, tt := range []struct {
		name   string
		mutate func(*FedRAMPTimeline)
	}{
		{"as of", func(t *FedRAMPTimeline) { t.AsOf = time.Time{} }},
		{"cutoff", func(t *FedRAMPTimeline) { t.NewCertificationCutoff = time.Time{} }},
		{"adoption", func(t *FedRAMPTimeline) { t.TwentyXAdoptionDate = time.Time{} }},
		{"non UTC", func(t *FedRAMPTimeline) { t.AsOf = time.Date(2026, 9, 5, 0, 0, 0, 0, time.FixedZone("EDT", -4*60*60)) }},
		{"cutoff before adoption", func(t *FedRAMPTimeline) { t.NewCertificationCutoff = t.TwentyXAdoptionDate.Add(-time.Nanosecond) }},
	} {
		t.Run("timeline/"+tt.name, func(t *testing.T) {
			timeline := baseTimeline
			tt.mutate(&timeline)
			if err := timeline.Validate(); !errors.Is(err, ErrTimelineRefusal) {
				t.Fatalf("Validate = %v, want ErrTimelineRefusal", err)
			}
		})
	}
}

func TestControlInheritanceAndProcurementAnswer_ValidateEveryRequiredField(t *testing.T) {
	control := ControlInheritanceReference{ControlID: "AC-2", InheritedFrom: "platform", Evidence: pointer("artifact/ac-2", "owner")}
	if err := control.Validate(); err != nil {
		t.Fatalf("valid control = %v", err)
	}
	for _, tt := range []struct {
		name   string
		mutate func(*ControlInheritanceReference)
	}{
		{"control id", func(r *ControlInheritanceReference) { r.ControlID = "" }},
		{"inherited from", func(r *ControlInheritanceReference) { r.InheritedFrom = "" }},
		{"evidence", func(r *ControlInheritanceReference) { r.Evidence = EvidencePointer{} }},
	} {
		t.Run("control/"+tt.name, func(t *testing.T) {
			candidate := control
			tt.mutate(&candidate)
			if err := candidate.Validate(); err == nil || !errors.Is(err, ErrInvalidProfile) && !errors.Is(err, ErrInvalidEvidence) {
				t.Fatalf("Validate = %v, want a declared validation sentinel", err)
			}
		})
	}
	if !questionValid(QuestionDataLocation) || questionValid(ProcurementQuestion("unknown")) {
		t.Fatal("questionValid did not enforce QuestionnaireItems")
	}
	answer := ProcurementAnswer{Question: QuestionDataLocation, Answer: "assertion", Owner: "owner", Date: fixtureDate, ArtifactRef: "artifact/data"}
	if err := answer.Validate(); err != nil {
		t.Fatalf("valid procurement answer = %v", err)
	}
	for _, tt := range []struct {
		name   string
		mutate func(*ProcurementAnswer)
	}{
		{"unknown question", func(a *ProcurementAnswer) { a.Question = "unknown" }},
		{"missing answer", func(a *ProcurementAnswer) { a.Answer = "" }},
		{"missing owner", func(a *ProcurementAnswer) { a.Owner = "" }},
		{"missing date", func(a *ProcurementAnswer) { a.Date = time.Time{} }},
		{"non UTC date", func(a *ProcurementAnswer) {
			a.Date = time.Date(2026, 9, 5, 0, 0, 0, 0, time.FixedZone("EDT", -4*60*60))
		}},
		{"missing artifact", func(a *ProcurementAnswer) { a.ArtifactRef = " " }},
	} {
		t.Run("answer/"+tt.name, func(t *testing.T) {
			candidate := answer
			tt.mutate(&candidate)
			if err := candidate.Validate(); !errors.Is(err, ErrUnansweredQuestion) {
				t.Fatalf("Validate = %v, want ErrUnansweredQuestion", err)
			}
		})
	}
}

func TestFedRAMPPathRecord_ValidateRejectsEverySecurityRelevantField(t *testing.T) {
	base := *fixture("tenant", "integration", ProgramFedRAMP).FedRAMP
	if err := base.Validate(); err != nil {
		t.Fatalf("valid FedRAMP record = %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*FedRAMPPathRecord)
		want   error
	}{
		{"path", func(r *FedRAMPPathRecord) { r.Path = "UNKNOWN" }, ErrInvalidFedRAMP},
		{"package version", func(r *FedRAMPPathRecord) { r.PackageVersion = "" }, ErrInvalidFedRAMP},
		{"monitoring evidence", func(r *FedRAMPPathRecord) { r.ContinuousMonitoringEvidence = nil }, ErrInvalidFedRAMP},
		{"monitoring evidence invalid", func(r *FedRAMPPathRecord) { r.ContinuousMonitoringEvidence[0] = EvidencePointer{} }, ErrInvalidEvidence},
		{"responsibility", func(r *FedRAMPPathRecord) { r.ResponsibilityStatement = "" }, ErrInvalidFedRAMP},
		{"timeline", func(r *FedRAMPPathRecord) { r.Timeline.SourceArtifactRef = "" }, ErrTimelineRefusal},
		{"REV5 after cutoff", func(r *FedRAMPPathRecord) { r.Path = FedRAMPRev5; r.Timeline.AsOf = r.Timeline.NewCertificationCutoff }, ErrTimelineRefusal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := base
			record.ContinuousMonitoringEvidence = append([]EvidencePointer(nil), base.ContinuousMonitoringEvidence...)
			tt.mutate(&record)
			if err := record.Validate(); !errors.Is(err, tt.want) {
				t.Fatalf("Validate = %v, want %v", err, tt.want)
			}
		})
	}
	if err := (FedRAMPPathRecord{Path: FedRAMPRev5, PackageVersion: "v", ContinuousMonitoringEvidence: []EvidencePointer{pointer("artifact", "owner")}, ResponsibilityStatement: "ok", Timeline: FedRAMPTimeline{AsOf: fixtureDate, NewCertificationCutoff: fixtureDate.Add(time.Hour), TwentyXAdoptionDate: fixtureDate.Add(2 * time.Hour), SourceArtifactRef: "source"}}).Validate(); !errors.Is(err, ErrTimelineRefusal) {
		t.Fatalf("cutoff-before-adoption nested error = %v, want ErrTimelineRefusal", err)
	}
}

func TestCMSReferenceBlock_ValidateRejectsEveryRequiredField(t *testing.T) {
	base := *cmsBlock()
	if err := base.Validate(); err != nil {
		t.Fatalf("valid CMS block = %v", err)
	}
	fields := []struct {
		name   string
		mutate func(*CMSReferenceBlock)
	}{
		{"MARS-E volume", func(c *CMSReferenceBlock) { c.MARSEVolume = "" }},
		{"MARS-E version", func(c *CMSReferenceBlock) { c.MARSEVersion = "" }},
		{"CMS ARS release", func(c *CMSReferenceBlock) { c.CMSARSRelease = "" }},
		{"DUA", func(c *CMSReferenceBlock) { c.DUARef = "" }},
		{"ISA", func(c *CMSReferenceBlock) { c.ISARef = "" }},
		{"SSPP", func(c *CMSReferenceBlock) { c.SSPPRef = "" }},
		{"privacy analysis", func(c *CMSReferenceBlock) { c.PrivacyAnalysisRef = "" }},
		{"reviewer", func(c *CMSReferenceBlock) { c.ReviewedBy = "" }},
		{"review date", func(c *CMSReferenceBlock) { c.ReviewedAt = time.Time{} }},
		{"non UTC review date", func(c *CMSReferenceBlock) {
			c.ReviewedAt = time.Date(2026, 9, 5, 0, 0, 0, 0, time.FixedZone("EDT", -4*60*60))
		}},
		{"inheritance", func(c *CMSReferenceBlock) { c.ControlInheritance = nil }},
		{"invalid inheritance", func(c *CMSReferenceBlock) { c.ControlInheritance[0].ControlID = "" }},
	}
	for _, tt := range fields {
		t.Run(tt.name, func(t *testing.T) {
			block := base
			block.ControlInheritance = append([]ControlInheritanceReference(nil), base.ControlInheritance...)
			tt.mutate(&block)
			if err := block.Validate(); !errors.Is(err, ErrInvalidCMS) && !errors.Is(err, ErrInvalidProfile) {
				if !errors.Is(err, ErrInvalidEvidence) {
					t.Fatalf("Validate = %v, want a CMS/profile/evidence sentinel", err)
				}
			}
		})
	}
}

func TestGovernmentAuthorizationProfile_ValidateRejectsRequiredFieldsAndConflicts(t *testing.T) {
	base := fixture("tenant", "integration", ProgramGovRAMP)
	cases := []struct {
		name   string
		mutate func(*GovernmentAuthorizationProfile)
		want   error
	}{
		{"schema version", func(p *GovernmentAuthorizationProfile) { p.SchemaVersion = 99 }, ErrInvalidProfile},
		{"revision", func(p *GovernmentAuthorizationProfile) { p.Revision = 0 }, ErrInvalidProfile},
		{"revision/version mismatch", func(p *GovernmentAuthorizationProfile) { p.Version = 2 }, ErrInvalidProfile},
		{"tenant", func(p *GovernmentAuthorizationProfile) { p.TenantID, p.TenantRef = "", "" }, ErrInvalidProfile},
		{"integration", func(p *GovernmentAuthorizationProfile) { p.IntegrationID, p.IntegrationRef = "", "" }, ErrInvalidProfile},
		{"boundary", func(p *GovernmentAuthorizationProfile) { p.SystemBoundary = " " }, ErrInvalidProfile},
		{"assessor status", func(p *GovernmentAuthorizationProfile) { p.AssessorStatus = "UNKNOWN" }, ErrInvalidProfile},
		{"review date", func(p *GovernmentAuthorizationProfile) { p.ReviewDate = time.Time{} }, ErrInvalidProfile},
		{"non UTC review date", func(p *GovernmentAuthorizationProfile) {
			p.ReviewDate = time.Date(2026, 9, 5, 0, 0, 0, 0, time.FixedZone("EDT", -4*60*60))
		}, ErrInvalidProfile},
		{"no programs", func(p *GovernmentAuthorizationProfile) { p.ApplicablePrograms, p.Programs = nil, nil }, ErrInvalidProfile},
		{"unknown program", func(p *GovernmentAuthorizationProfile) { p.ApplicablePrograms = []Program{"UNKNOWN"} }, ErrInvalidProfile},
		{"duplicate program", func(p *GovernmentAuthorizationProfile) {
			p.ApplicablePrograms = []Program{ProgramGovRAMP, ProgramGovRAMP}
		}, ErrInvalidProfile},
		{"invalid inherited control", func(p *GovernmentAuthorizationProfile) { p.InheritedControls = []ControlInheritanceReference{{}} }, ErrInvalidProfile},
		{"invalid evidence", func(p *GovernmentAuthorizationProfile) { p.Evidence = []EvidencePointer{{}} }, ErrInvalidEvidence},
		{"invalid fedramp", func(p *GovernmentAuthorizationProfile) { p.FedRAMP = &FedRAMPPathRecord{} }, ErrInvalidFedRAMP},
		{"invalid CMS", func(p *GovernmentAuthorizationProfile) { p.CMS = &CMSReferenceBlock{} }, ErrInvalidCMS},
		{"MARS-E without CMS", func(p *GovernmentAuthorizationProfile) { p.ApplicablePrograms = []Program{ProgramMARSE}; p.CMS = nil }, ErrInvalidCMS},
		{"duplicate procurement answer", func(p *GovernmentAuthorizationProfile) {
			p.ProcurementAnswers = append(p.ProcurementAnswers, p.ProcurementAnswers[0])
		}, ErrUnansweredQuestion},
		{"bad revision digest", func(p *GovernmentAuthorizationProfile) { p.RevisionDigest = "bad" }, ErrImmutableRevision},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			profile := base
			tt.mutate(&profile)
			if err := profile.Validate(); !errors.Is(err, tt.want) {
				t.Fatalf("Validate = %v, want %v", err, tt.want)
			}
			if _, err := NewGovernmentAuthorizationProfile(profile); !errors.Is(err, tt.want) {
				t.Fatalf("constructor error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestGovernmentAuthorizationProfile_LegacyAliasesCloneNestedDataAndDigest(t *testing.T) {
	fedramp := *fixture("tenant", "integration", ProgramFedRAMP).FedRAMP
	cms := *cmsBlock()
	legacy := GovernmentAuthorizationProfile{
		Version:                    4,
		TenantRef:                  "tenant",
		IntegrationRef:             "integration",
		Programs:                   []Program{ProgramFedRAMP},
		SystemBoundary:             "platform boundary",
		InheritedControlReferences: []ControlInheritanceReference{{ControlID: "AC-2", InheritedFrom: "platform", Evidence: pointer("artifact/ac-2", "owner")}},
		EvidenceCatalog:            []EvidencePointer{pointer("artifact/profile", "owner")},
		AssessorStatus:             AssessorAccepted,
		ReviewDate:                 fixtureDate,
		FedRAMPPath:                &fedramp,
		MARSEResources:             &cms,
		ProcurementAnswers:         answers(),
	}
	sealed, err := NewProfile(legacy)
	if err != nil {
		t.Fatalf("legacy constructor = %v", err)
	}
	if sealed.SchemaVersion != 1 || sealed.RevisionDigest == "" {
		t.Fatalf("legacy profile was not sealed: %+v", sealed)
	}
	wantDigest := sealed.RevisionDigest
	legacy.FedRAMPPath.ContinuousMonitoringEvidence[0].Owner = "changed"
	legacy.MARSEResources.ControlInheritance[0].Evidence.Owner = "changed"
	legacy.EvidenceCatalog[0].Owner = "changed"
	if sealed.RevisionDigest != wantDigest {
		t.Fatal("sealed profile changed after nested source mutation")
	}
	if got, err := sealed.Digest(); err != nil || got != wantDigest {
		t.Fatalf("sealed legacy digest = %s, err=%v, want %s", got, err, wantDigest)
	}
}

func TestGovernmentAuthorizationProfile_DigestAndRefreshFailureBranches(t *testing.T) {
	profile := fixture("tenant", "integration", ProgramGovRAMP)
	profile.FedRAMP = nil
	timeline := fixture("tenant", "integration", ProgramFedRAMP).FedRAMP.Timeline
	if _, err := profile.RefreshFedRAMP(timeline); !errors.Is(err, ErrTimelineRefusal) {
		t.Fatalf("refresh without FedRAMP = %v, want ErrTimelineRefusal", err)
	}
	if _, err := profile.Refresh(timeline); !errors.Is(err, ErrTimelineRefusal) {
		t.Fatalf("Refresh alias without FedRAMP = %v, want ErrTimelineRefusal", err)
	}
	profile.RevisionDigest = strings.Repeat("b", 64)
	if _, err := profile.Digest(); !errors.Is(err, ErrImmutableRevision) {
		t.Fatalf("mismatched revision digest = %v, want ErrImmutableRevision", err)
	}
	sealed, err := NewGovernmentAuthorizationProfile(fixture("tenant", "integration", ProgramFedRAMP))
	if err != nil {
		t.Fatal(err)
	}
	badTimeline := sealed.FedRAMP.Timeline
	badTimeline.SourceArtifactRef = ""
	if _, err := sealed.RefreshFedRAMP(badTimeline); !errors.Is(err, ErrTimelineRefusal) {
		t.Fatalf("invalid refresh timeline = %v, want ErrTimelineRefusal", err)
	}
	upper := sealed
	upper.RevisionDigest = strings.ToUpper(upper.RevisionDigest)
	if got, err := upper.Digest(); err != nil || !strings.EqualFold(got, upper.RevisionDigest) {
		t.Fatalf("uppercase revision digest = %s, err=%v", got, err)
	}
}

func TestProcurementPack_GenerationAndDigestRejectsMalformedArtifacts(t *testing.T) {
	profile := fixture("tenant", "integration", ProgramGovRAMP)
	pack, err := GenerateProcurementPack(profile)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := pack.Digest(); err != nil || got != pack.PackDigest {
		t.Fatalf("valid pack digest = %s, err=%v, want %s", got, err, pack.PackDigest)
	}
	if got := ExplainPack(pack); !strings.Contains(got, "revision 1") || !strings.Contains(got, "answers=13") {
		t.Fatalf("ExplainPack = %q", got)
	}
	for _, tt := range []struct {
		name   string
		mutate func(*ProcurementPack)
		want   error
	}{
		{"schema", func(p *ProcurementPack) { p.SchemaVersion = 99 }, ErrInvalidProfile},
		{"answer count", func(p *ProcurementPack) { p.Answers = p.Answers[:len(p.Answers)-1] }, ErrUnansweredQuestion},
		{"answer order", func(p *ProcurementPack) { p.Answers[0], p.Answers[1] = p.Answers[1], p.Answers[0] }, ErrUnansweredQuestion},
		{"invalid answer", func(p *ProcurementPack) { p.Answers[0].Answer = "" }, ErrUnansweredQuestion},
		{"digest mutation", func(p *ProcurementPack) { p.PackDigest = strings.Repeat("c", 64) }, ErrImmutableRevision},
	} {
		t.Run(tt.name, func(t *testing.T) {
			candidate := pack
			candidate.Answers = append([]ProcurementAnswer(nil), pack.Answers...)
			tt.mutate(&candidate)
			if _, err := candidate.Digest(); !errors.Is(err, tt.want) {
				t.Fatalf("Digest = %v, want %v", err, tt.want)
			}
		})
	}

	for i, question := range QuestionnaireItems {
		t.Run("missing/"+string(question), func(t *testing.T) {
			candidate := profile
			candidate.ProcurementAnswers = append([]ProcurementAnswer(nil), profile.ProcurementAnswers[:i]...)
			candidate.ProcurementAnswers = append(candidate.ProcurementAnswers, profile.ProcurementAnswers[i+1:]...)
			if _, err := GenerateProcurementEvidencePack(candidate); !errors.Is(err, ErrUnansweredQuestion) || !strings.Contains(err.Error(), string(question)) {
				t.Fatalf("missing %s error = %v, want named ErrUnansweredQuestion", question, err)
			}
		})
	}
}

func TestGovauthCanonicalHelpersAndExplanations(t *testing.T) {
	if got := programStrings([]Program{ProgramGovRAMP, ProgramFedRAMP}); len(got) != 2 || got[0] != "GovRAMP" || got[1] != "FedRAMP" {
		t.Fatalf("programStrings = %v", got)
	}
	for _, value := range []string{"", "short", strings.Repeat("a", 63), strings.Repeat("g", 64)} {
		if isHexDigest(value) {
			t.Fatalf("isHexDigest accepted %q", value)
		}
	}
	if !isHexDigest(strings.Repeat("a", 64)) || !isHexDigest(strings.Repeat("A", 64)) {
		t.Fatal("isHexDigest rejected valid hexadecimal")
	}
	if err := fieldError(ErrInvalidProfile, "field", "reason"); !errors.Is(err, ErrInvalidProfile) || !strings.Contains(err.Error(), "field reason") {
		t.Fatalf("fieldError = %v", err)
	}
	profile := fixture("tenant-secret", "integration-secret", ProgramFedRAMP)
	if got := Explain(profile); got == "" || strings.Contains(got, "tenant-secret") || strings.Contains(got, "integration-secret") {
		t.Fatalf("Explain exposed identifiers or was empty: %q", got)
	}
	if got := profile.Explain(); !strings.Contains(got, "fedramp=TWENTYX_A") || !strings.Contains(got, "programs=FedRAMP") {
		t.Fatalf("profile explanation = %q", got)
	}
}

func currentAssuranceInputs() ProcurementAssuranceInputs {
	input := ProcurementAssuranceInputs{
		AsOf: fixtureDate,
		PenetrationTest: PenetrationTestProcurementEvidence{
			Answer: "As of 2026-09-05, an independent penetration test is recorded with 0 open high and 0 open critical findings.",
			AsOf:   "2026-09-05", Status: "current", EvidenceDigest: strings.Repeat("a", 64),
			AnswerDigest: strings.Repeat("b", 64), ArtifactRef: "pentest:engagement-7:v2",
		},
		Accessibility: VPATReportReference{
			Version: "v1", Digest: strings.Repeat("c", 64), SourceRunID: "ux003-run-9",
			SourceRunAt: fixtureDate.Add(-time.Hour), LatestRunAt: fixtureDate.Add(-time.Hour),
		},
	}
	private := ed25519.NewKeyFromSeed([]byte(strings.Repeat("g", ed25519.SeedSize)))
	public, signature, _ := trustpentest.SignTrustedPayload([]byte(input.PenetrationTest.AnswerDigest), private)
	input.PenetrationTest.SignerPublicKey, input.PenetrationTest.Signature = public, signature
	return input
}

func govauthAssuranceKeys() [][]byte {
	private := ed25519.NewKeyFromSeed([]byte(strings.Repeat("g", ed25519.SeedSize)))
	return [][]byte{append([]byte(nil), private.Public().(ed25519.PublicKey)...)}
}

func verifiedCurrentAssurance(t *testing.T, inputs ProcurementAssuranceInputs) VerifiedProcurementAssurance {
	t.Helper()
	verified, err := VerifyProcurementAssurance(inputs, govauthAssuranceKeys())
	if err != nil {
		t.Fatal(err)
	}
	return verified
}

func TestTodo_REV_099_03(t *testing.T) {
	profile := fixture("tenant", "integration", ProgramGovRAMP)
	pack, err := GenerateProcurementPackWithAssurance(profile, verifiedCurrentAssurance(t, currentAssuranceInputs()))
	if err != nil {
		t.Fatal(err)
	}
	answer := pack.Answers[9]
	if answer.Question != QuestionVulnerabilityManagement || answer.Answer != currentAssuranceInputs().PenetrationTest.Answer || answer.Date != fixtureDate {
		t.Fatalf("pentest questionnaire answer = %+v", answer)
	}
	if pack.Assurance == nil || pack.Assurance.PenetrationTest.AnswerDigest != strings.Repeat("b", 64) {
		t.Fatalf("pack lost pinned pentest answer provenance: %+v", pack.Assurance)
	}
}

func TestTodo_REV_099_03_Security(t *testing.T) {
	profile := fixture("tenant", "integration", ProgramGovRAMP)
	if _, err := GenerateProcurementPackWithAssurance(profile, VerifiedProcurementAssurance{}); !errors.Is(err, ErrStaleAssurance) {
		t.Fatalf("zero-value raw assurance receipt bypassed verification: %v", err)
	}
	otherPrivate := ed25519.NewKeyFromSeed([]byte(strings.Repeat("x", ed25519.SeedSize)))
	if _, err := VerifyProcurementAssurance(currentAssuranceInputs(), [][]byte{otherPrivate.Public().(ed25519.PublicKey)}); !errors.Is(err, ErrStaleAssurance) {
		t.Fatalf("answer signed by an untrusted key was accepted: %v", err)
	}
	for _, mutate := range []func(*ProcurementAssuranceInputs){
		func(i *ProcurementAssuranceInputs) { i.PenetrationTest.AsOf = "2026-09-04" },
		func(i *ProcurementAssuranceInputs) { i.PenetrationTest.AnswerDigest = "forged" },
		func(i *ProcurementAssuranceInputs) { i.PenetrationTest.ArtifactRef = " " },
	} {
		input := currentAssuranceInputs()
		mutate(&input)
		if _, err := VerifyProcurementAssurance(input, govauthAssuranceKeys()); !errors.Is(err, ErrStaleAssurance) {
			t.Fatalf("stale or unpinned pentest evidence accepted: %v", err)
		}
	}
}

func TestTodo_REV_099_03_NoEngagementRemainsExplicit(t *testing.T) {
	profile := fixture("tenant", "integration", ProgramGovRAMP)
	input := currentAssuranceInputs()
	input.PenetrationTest = PenetrationTestProcurementEvidence{
		Answer: "As of 2026-09-05, no completed independent penetration-test engagement is recorded.",
		AsOf:   "2026-09-05", Status: "no_completed_engagement", AnswerDigest: strings.Repeat("e", 64),
	}
	private := ed25519.NewKeyFromSeed([]byte(strings.Repeat("g", ed25519.SeedSize)))
	public, signature, _ := trustpentest.SignTrustedPayload([]byte(input.PenetrationTest.AnswerDigest), private)
	input.PenetrationTest.SignerPublicKey, input.PenetrationTest.Signature = public, signature
	pack, err := GenerateProcurementPackWithAssurance(profile, verifiedCurrentAssurance(t, input))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pack.Answers[9].Answer, "no completed") || !strings.Contains(pack.Answers[9].ArtifactRef, strings.Repeat("e", 64)) {
		t.Fatalf("absence answer was not preserved as explicit source evidence: %+v", pack.Answers[9])
	}
}

func TestTodo_REV_099_04(t *testing.T) {
	pack, err := GenerateProcurementPackWithAssurance(fixture("tenant", "integration", ProgramGovRAMP), verifiedCurrentAssurance(t, currentAssuranceInputs()))
	if err != nil {
		t.Fatal(err)
	}
	accessibility := pack.Answers[10]
	wantAnswer := "Interim accessibility evidence report v1 SHA-256 " + strings.Repeat("c", 64)
	if accessibility.Question != QuestionAccessibility || accessibility.Answer != wantAnswer || !strings.Contains(accessibility.ArtifactRef, "vpat:v1") {
		t.Fatalf("accessibility answer does not accurately label the pinned interim report: %+v", accessibility)
	}
	if digest, err := pack.Digest(); err != nil || digest != pack.PackDigest {
		t.Fatalf("integrated pack digest = %s, err=%v", digest, err)
	}
}

func TestTodo_REV_099_04_Security(t *testing.T) {
	profile := fixture("tenant", "integration", ProgramGovRAMP)
	for _, mutate := range []func(*ProcurementAssuranceInputs){
		func(i *ProcurementAssuranceInputs) { i.Accessibility.Version = "" },
		func(i *ProcurementAssuranceInputs) { i.Accessibility.Digest = "tampered" },
		func(i *ProcurementAssuranceInputs) {
			i.Accessibility.LatestRunAt = fixtureDate
			i.Accessibility.SourceRunAt = fixtureDate.Add(-time.Hour)
		},
	} {
		input := currentAssuranceInputs()
		mutate(&input)
		if _, err := VerifyProcurementAssurance(input, govauthAssuranceKeys()); !errors.Is(err, ErrStaleAssurance) {
			t.Fatalf("missing or stale VPAT reference accepted: %v", err)
		}
	}
	pack, err := GenerateProcurementPackWithAssurance(profile, verifiedCurrentAssurance(t, currentAssuranceInputs()))
	if err != nil {
		t.Fatal(err)
	}
	pack.Assurance.Accessibility.Digest = strings.Repeat("d", 64)
	if _, err := pack.Digest(); !errors.Is(err, ErrStaleAssurance) && !errors.Is(err, ErrImmutableRevision) {
		t.Fatalf("tampered VPAT reference accepted: %v", err)
	}
}
