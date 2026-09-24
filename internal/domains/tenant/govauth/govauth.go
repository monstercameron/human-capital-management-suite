// Package govauth owns the tenant-scoped government authorization and
// procurement-evidence contract. It is a kernel-pure, append-only value
// model: all dates and refresh inputs are supplied by the caller, and no
// database, clock, network, or provider is consulted.
package govauth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	trustpentest "github.com/monstercameron/human-capital-management-suite/internal/trust/pentest"
)

const schemaVersion = 1

// Version returns the schema revision of this package's public contract.
func Version() int { return schemaVersion }

var (
	ErrInvalidProfile     = errors.New("govauth: invalid government authorization profile")
	ErrInvalidEvidence    = errors.New("govauth: invalid evidence pointer")
	ErrInvalidFedRAMP     = errors.New("govauth: invalid FedRAMP record")
	ErrInvalidCMS         = errors.New("govauth: invalid CMS record")
	ErrUnansweredQuestion = errors.New("govauth: unanswered procurement question")
	ErrImmutableRevision  = errors.New("govauth: immutable revision digest mismatch")
	ErrTimelineRefusal    = errors.New("govauth: FedRAMP timeline refusal")
	ErrStaleAssurance     = errors.New("govauth: missing or stale assurance evidence")
)

// Program is the closed set of government authorization or regulated-data
// programs that may be made applicable to one tenant integration.
type Program string

const (
	ProgramFedRAMP Program = "FedRAMP"
	ProgramGovRAMP Program = "GovRAMP"
	ProgramTXRAMP  Program = "TX-RAMP"
	ProgramFISMA   Program = "FISMA"
	ProgramCJIS    Program = "CJIS"
	ProgramFTI     Program = "FTI"
	ProgramCUI     Program = "CUI"
	ProgramMARSE   Program = "MARS-E"

	FedRAMP       = ProgramFedRAMP
	GovRAMP       = ProgramGovRAMP
	TXRAMP        = ProgramTXRAMP
	FISMA         = ProgramFISMA
	CJIS          = ProgramCJIS
	FTI           = ProgramFTI
	CUI           = ProgramCUI
	MARSE         = ProgramMARSE
	MARS_E        = ProgramMARSE
	ProgramMARS_E = ProgramMARSE
)

func (p Program) Valid() bool {
	switch p {
	case ProgramFedRAMP, ProgramGovRAMP, ProgramTXRAMP, ProgramFISMA,
		ProgramCJIS, ProgramFTI, ProgramCUI, ProgramMARSE:
		return true
	default:
		return false
	}
}

// AssessorStatus is the closed lifecycle for an authorization assessment.
type AssessorStatus string

const (
	AssessorPending               AssessorStatus = "PENDING"
	AssessorInProgress            AssessorStatus = "IN_PROGRESS"
	AssessorAccepted              AssessorStatus = "ACCEPTED"
	AssessorConditionallyAccepted AssessorStatus = "CONDITIONALLY_ACCEPTED"
	AssessorExpired               AssessorStatus = "EXPIRED"
	AssessorNotApplicable         AssessorStatus = "NOT_APPLICABLE"

	StatusPending       = AssessorPending
	StatusInProgress    = AssessorInProgress
	StatusAccepted      = AssessorAccepted
	StatusExpired       = AssessorExpired
	StatusNotApplicable = AssessorNotApplicable
)

func (s AssessorStatus) Valid() bool {
	switch s {
	case AssessorPending, AssessorInProgress, AssessorAccepted,
		AssessorConditionallyAccepted, AssessorExpired, AssessorNotApplicable:
		return true
	default:
		return false
	}
}

// EvidencePointer identifies reviewed evidence without carrying its contents.
// ArtifactRef is intentionally a reference, not a secret or account number.
type EvidencePointer struct {
	ArtifactRef string    `json:"artifact_ref"`
	Ref         string    `json:"ref,omitempty"`
	Owner       string    `json:"owner"`
	Date        time.Time `json:"date"`
	Digest      string    `json:"digest,omitempty"`
}

// Reference returns the one populated artifact reference spelling.
func (e EvidencePointer) Reference() string {
	if strings.TrimSpace(e.ArtifactRef) != "" {
		return strings.TrimSpace(e.ArtifactRef)
	}
	return strings.TrimSpace(e.Ref)
}

func (e EvidencePointer) Validate() error {
	if strings.TrimSpace(e.ArtifactRef) != "" && strings.TrimSpace(e.Ref) != "" {
		return fieldError(ErrInvalidEvidence, "evidence.artifact_ref", "artifact_ref and ref are aliases; only one may be set")
	}
	if e.Reference() == "" {
		return fieldError(ErrInvalidEvidence, "evidence.artifact_ref", "is required")
	}
	if strings.TrimSpace(e.Owner) == "" {
		return fieldError(ErrInvalidEvidence, "evidence.owner", "is required")
	}
	if e.Date.IsZero() {
		return fieldError(ErrInvalidEvidence, "evidence.date", "is required")
	}
	if e.Date.Location() != time.UTC {
		return fieldError(ErrInvalidEvidence, "evidence.date", "must be UTC")
	}
	if e.Digest != "" && !isHexDigest(e.Digest) {
		return fieldError(ErrInvalidEvidence, "evidence.digest", "must be a 64-character hexadecimal digest")
	}
	return nil
}

// ControlInheritanceReference names a control implemented by an inherited
// service or customer and the evidence that supports that inheritance.
type ControlInheritanceReference struct {
	ControlID     string          `json:"control_id"`
	InheritedFrom string          `json:"inherited_from"`
	Evidence      EvidencePointer `json:"evidence"`
}

func (r ControlInheritanceReference) Validate() error {
	if strings.TrimSpace(r.ControlID) == "" {
		return fieldError(ErrInvalidProfile, "inherited_controls.control_id", "is required")
	}
	if strings.TrimSpace(r.InheritedFrom) == "" {
		return fieldError(ErrInvalidProfile, "inherited_controls.inherited_from", "is required")
	}
	if err := r.Evidence.Validate(); err != nil {
		return err
	}
	return nil
}

// FedRAMPPath is the permitted authorization path. REV5 is retained for an
// existing authorization; new certification after the declared cutoff must
// use one of the TWENTYX classes.
type FedRAMPPath string

const (
	FedRAMPRev5 FedRAMPPath = "REV5"
	FedRAMP20xA FedRAMPPath = "TWENTYX_A"
	FedRAMP20xB FedRAMPPath = "TWENTYX_B"
	FedRAMP20xC FedRAMPPath = "TWENTYX_C"
	REV5                    = FedRAMPRev5
	TWENTYX_A               = FedRAMP20xA
	TWENTYX_B               = FedRAMP20xB
	TWENTYX_C               = FedRAMP20xC
)

func (p FedRAMPPath) Valid() bool {
	return p == FedRAMPRev5 || p == FedRAMP20xA || p == FedRAMP20xB || p == FedRAMP20xC
}

// FedRAMPTimeline is a caller-declared published timeline fixture. Supplying
// it to RefreshFedRAMP makes transition decisions reproducible in tests and
// release generation rather than dependent on the wall clock.
type FedRAMPTimeline struct {
	AsOf                   time.Time `json:"as_of"`
	NewCertificationCutoff time.Time `json:"new_certification_cutoff"`
	TwentyXAdoptionDate    time.Time `json:"twenty_x_adoption_date"`
	SourceArtifactRef      string    `json:"source_artifact_ref"`
}

// TimelineFixture is a compatibility name for a declared FedRAMP timeline.
type TimelineFixture = FedRAMPTimeline

func (t FedRAMPTimeline) Validate() error {
	for field, value := range map[string]time.Time{
		"timeline.as_of": t.AsOf, "timeline.new_certification_cutoff": t.NewCertificationCutoff,
		"timeline.twenty_x_adoption_date": t.TwentyXAdoptionDate,
	} {
		if value.IsZero() {
			return fieldError(ErrTimelineRefusal, field, "is required")
		}
		if value.Location() != time.UTC {
			return fieldError(ErrTimelineRefusal, field, "must be UTC")
		}
	}
	if strings.TrimSpace(t.SourceArtifactRef) == "" {
		return fieldError(ErrTimelineRefusal, "timeline.source_artifact_ref", "is required")
	}
	if t.NewCertificationCutoff.Before(t.TwentyXAdoptionDate) {
		return fieldError(ErrTimelineRefusal, "timeline", "new certification cutoff cannot precede 20x adoption date")
	}
	return nil
}

// FedRAMPPathRecord is the FedRAMP portion of a government profile.
type FedRAMPPathRecord struct {
	Path                         FedRAMPPath       `json:"path"`
	PackageVersion               string            `json:"package_version"`
	ContinuousMonitoringEvidence []EvidencePointer `json:"continuous_monitoring_evidence"`
	ResponsibilityStatement      string            `json:"responsibility_statement"`
	TransitionStatus             string            `json:"transition_status,omitempty"`
	Timeline                     FedRAMPTimeline   `json:"timeline"`
}

// FedRAMPRecord is a shorter name for FedRAMPPathRecord.
type FedRAMPRecord = FedRAMPPathRecord

func (r FedRAMPPathRecord) Validate() error {
	if !r.Path.Valid() {
		return fieldError(ErrInvalidFedRAMP, "fedramp.path", "must be REV5 or TWENTYX_A, TWENTYX_B, TWENTYX_C")
	}
	if strings.TrimSpace(r.PackageVersion) == "" {
		return fieldError(ErrInvalidFedRAMP, "fedramp.package_version", "is required")
	}
	if len(r.ContinuousMonitoringEvidence) == 0 {
		return fieldError(ErrInvalidFedRAMP, "fedramp.continuous_monitoring_evidence", "is required")
	}
	for _, e := range r.ContinuousMonitoringEvidence {
		if err := e.Validate(); err != nil {
			return err
		}
	}
	if strings.TrimSpace(r.ResponsibilityStatement) == "" {
		return fieldError(ErrInvalidFedRAMP, "fedramp.responsibility_statement", "is required")
	}
	if err := r.Timeline.Validate(); err != nil {
		return err
	}
	if r.Path == FedRAMPRev5 && !r.Timeline.AsOf.Before(r.Timeline.NewCertificationCutoff) {
		return fieldError(ErrTimelineRefusal, "fedramp.path", "REV5 is not a valid new-certification path after the declared cutoff")
	}
	return nil
}

// CMSReferenceBlock pins the distinct CMS/MARS-E releases and agreements for
// an ACA or exchange integration. No release is inferred from another field.
type CMSReferenceBlock struct {
	MARSEVolume        string                        `json:"mars_e_volume"`
	MARSEVersion       string                        `json:"mars_e_version"`
	CMSARSRelease      string                        `json:"cms_ars_release"`
	DUARef             string                        `json:"dua_ref"`
	ISARef             string                        `json:"isa_ref"`
	SSPPRef            string                        `json:"sspp_ref"`
	PrivacyAnalysisRef string                        `json:"privacy_analysis_ref"`
	ControlInheritance []ControlInheritanceReference `json:"control_inheritance"`
	ReviewedBy         string                        `json:"reviewed_by"`
	ReviewedAt         time.Time                     `json:"reviewed_at"`
}

// MARSEResources is a compatibility alias for CMSReferenceBlock.
type MARSEResources = CMSReferenceBlock

func (c CMSReferenceBlock) Validate() error {
	for field, value := range map[string]string{
		"cms.mars_e_volume": c.MARSEVolume, "cms.mars_e_version": c.MARSEVersion,
		"cms.cms_ars_release": c.CMSARSRelease, "cms.dua_ref": c.DUARef,
		"cms.isa_ref": c.ISARef, "cms.sspp_ref": c.SSPPRef,
		"cms.privacy_analysis_ref": c.PrivacyAnalysisRef, "cms.reviewed_by": c.ReviewedBy,
	} {
		if strings.TrimSpace(value) == "" {
			return fieldError(ErrInvalidCMS, field, "is required")
		}
	}
	if c.ReviewedAt.IsZero() {
		return fieldError(ErrInvalidCMS, "cms.reviewed_at", "is required")
	}
	if c.ReviewedAt.Location() != time.UTC {
		return fieldError(ErrInvalidCMS, "cms.reviewed_at", "must be UTC")
	}
	if len(c.ControlInheritance) == 0 {
		return fieldError(ErrInvalidCMS, "cms.control_inheritance", "reviewed mapping is required")
	}
	for _, mapping := range c.ControlInheritance {
		if err := mapping.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// ProcurementQuestion is the closed standard questionnaire vocabulary.
type ProcurementQuestion string

const (
	QuestionDataLocation            ProcurementQuestion = "data_location"
	QuestionSubprocessors           ProcurementQuestion = "subprocessors"
	QuestionBoundary                ProcurementQuestion = "boundary"
	QuestionIdentity                ProcurementQuestion = "identity"
	QuestionEncryption              ProcurementQuestion = "encryption"
	QuestionTenantIsolation         ProcurementQuestion = "tenant_isolation"
	QuestionIncidentNotice          ProcurementQuestion = "incident_notice"
	QuestionRTO                     ProcurementQuestion = "rto"
	QuestionRPO                     ProcurementQuestion = "rpo"
	QuestionVulnerabilityManagement ProcurementQuestion = "vulnerability_management"
	QuestionAccessibility           ProcurementQuestion = "accessibility"
	QuestionRetention               ProcurementQuestion = "retention"
	QuestionAuditReports            ProcurementQuestion = "audit_reports"
)

// QuestionnaireItems is the deterministic required order for generated packs.
var QuestionnaireItems = []ProcurementQuestion{
	QuestionDataLocation, QuestionSubprocessors, QuestionBoundary, QuestionIdentity,
	QuestionEncryption, QuestionTenantIsolation, QuestionIncidentNotice, QuestionRTO,
	QuestionRPO, QuestionVulnerabilityManagement, QuestionAccessibility, QuestionRetention,
	QuestionAuditReports,
}

// ProcurementAnswer is one traceable answer. Answer text is a controlled
// assertion supplied by the profile owner; secrets and personal identifiers
// are not accepted as part of the audit explanation.
type ProcurementAnswer struct {
	Question    ProcurementQuestion `json:"question"`
	Answer      string              `json:"answer"`
	Owner       string              `json:"owner"`
	Date        time.Time           `json:"date"`
	ArtifactRef string              `json:"artifact_ref"`
}

// QuestionnaireAnswer is a compatibility name for ProcurementAnswer.
type QuestionnaireAnswer = ProcurementAnswer

func (a ProcurementAnswer) Validate() error {
	if !questionValid(a.Question) {
		return fieldError(ErrUnansweredQuestion, "procurement.question", "is not in the closed questionnaire set")
	}
	if strings.TrimSpace(a.Answer) == "" {
		return fieldError(ErrUnansweredQuestion, "procurement."+string(a.Question)+".answer", "is unanswered")
	}
	if strings.TrimSpace(a.Owner) == "" {
		return fieldError(ErrUnansweredQuestion, "procurement."+string(a.Question)+".owner", "is required")
	}
	if a.Date.IsZero() {
		return fieldError(ErrUnansweredQuestion, "procurement."+string(a.Question)+".date", "is required")
	}
	if a.Date.Location() != time.UTC {
		return fieldError(ErrUnansweredQuestion, "procurement."+string(a.Question)+".date", "must be UTC")
	}
	if strings.TrimSpace(a.ArtifactRef) == "" {
		return fieldError(ErrUnansweredQuestion, "procurement."+string(a.Question)+".artifact_ref", "is required")
	}
	return nil
}

// GovernmentAuthorizationProfile is one immutable, versioned authorization
// revision for one tenant and one integration. RevisionDigest is the digest
// of all semantic fields and is excluded from its own canonical input.
type GovernmentAuthorizationProfile struct {
	SchemaVersion              int                           `json:"schema_version"`
	Revision                   uint64                        `json:"revision"`
	Version                    uint64                        `json:"version,omitempty"`
	TenantID                   string                        `json:"tenant_id"`
	TenantRef                  string                        `json:"tenant_ref,omitempty"`
	IntegrationID              string                        `json:"integration_id"`
	IntegrationRef             string                        `json:"integration_ref,omitempty"`
	ApplicablePrograms         []Program                     `json:"applicable_programs"`
	Programs                   []Program                     `json:"programs,omitempty"`
	SystemBoundary             string                        `json:"system_boundary"`
	InheritedControls          []ControlInheritanceReference `json:"inherited_controls"`
	InheritedControlReferences []ControlInheritanceReference `json:"inherited_control_references,omitempty"`
	Evidence                   []EvidencePointer             `json:"evidence"`
	EvidenceCatalog            []EvidencePointer             `json:"evidence_catalog,omitempty"`
	AssessorStatus             AssessorStatus                `json:"assessor_status"`
	ReviewDate                 time.Time                     `json:"review_date"`
	FedRAMP                    *FedRAMPPathRecord            `json:"fedramp,omitempty"`
	FedRAMPPath                *FedRAMPPathRecord            `json:"fedramp_path,omitempty"`
	CMS                        *CMSReferenceBlock            `json:"cms,omitempty"`
	MARSEResources             *CMSReferenceBlock            `json:"mars_e,omitempty"`
	ProcurementAnswers         []ProcurementAnswer           `json:"procurement_answers"`
	RevisionDigest             string                        `json:"revision_digest,omitempty"`
}

// NewGovernmentAuthorizationProfile validates and seals a profile revision.
func NewGovernmentAuthorizationProfile(profile GovernmentAuthorizationProfile) (GovernmentAuthorizationProfile, error) {
	sealed := cloneProfile(profile)
	if sealed.SchemaVersion == 0 {
		sealed.SchemaVersion = schemaVersion
	}
	if err := sealed.Validate(); err != nil {
		return GovernmentAuthorizationProfile{}, err
	}
	digest, err := sealed.contentDigest()
	if err != nil {
		return GovernmentAuthorizationProfile{}, err
	}
	sealed.RevisionDigest = digest
	return sealed, nil
}

// NewProfile is the concise constructor name.
func NewProfile(profile GovernmentAuthorizationProfile) (GovernmentAuthorizationProfile, error) {
	return NewGovernmentAuthorizationProfile(profile)
}

func (p GovernmentAuthorizationProfile) revisionNumber() uint64 {
	if p.Revision != 0 {
		return p.Revision
	}
	return p.Version
}

func (p GovernmentAuthorizationProfile) tenantReference() string {
	if strings.TrimSpace(p.TenantID) != "" {
		return strings.TrimSpace(p.TenantID)
	}
	return strings.TrimSpace(p.TenantRef)
}

func (p GovernmentAuthorizationProfile) programs() []Program {
	if len(p.ApplicablePrograms) != 0 {
		return p.ApplicablePrograms
	}
	return p.Programs
}

func (p GovernmentAuthorizationProfile) inheritedControls() []ControlInheritanceReference {
	if len(p.InheritedControls) != 0 {
		return p.InheritedControls
	}
	return p.InheritedControlReferences
}

func (p GovernmentAuthorizationProfile) evidencePointers() []EvidencePointer {
	if len(p.Evidence) != 0 {
		return p.Evidence
	}
	return p.EvidenceCatalog
}

func (p GovernmentAuthorizationProfile) integrationReference() string {
	if strings.TrimSpace(p.IntegrationID) != "" {
		return strings.TrimSpace(p.IntegrationID)
	}
	return strings.TrimSpace(p.IntegrationRef)
}

func (p GovernmentAuthorizationProfile) fedrampRecord() *FedRAMPPathRecord {
	if p.FedRAMP != nil {
		return p.FedRAMP
	}
	return p.FedRAMPPath
}

func (p GovernmentAuthorizationProfile) cmsRecord() *CMSReferenceBlock {
	if p.CMS != nil {
		return p.CMS
	}
	return p.MARSEResources
}

// Validate checks every mandatory field and all closed vocabularies. Errors
// name the exact field that refused the revision.
func (p GovernmentAuthorizationProfile) Validate() error {
	if p.SchemaVersion != 0 && p.SchemaVersion != schemaVersion {
		return fieldError(ErrInvalidProfile, "schema_version", "is unsupported")
	}
	if p.revisionNumber() == 0 {
		return fieldError(ErrInvalidProfile, "revision", "must be positive")
	}
	if p.Revision != 0 && p.Version != 0 && p.Revision != p.Version {
		return fieldError(ErrInvalidProfile, "revision", "revision and version disagree")
	}
	if p.tenantReference() == "" {
		return fieldError(ErrInvalidProfile, "tenant_id", "is required")
	}
	if p.integrationReference() == "" {
		return fieldError(ErrInvalidProfile, "integration_id", "is required")
	}
	if strings.TrimSpace(p.SystemBoundary) == "" {
		return fieldError(ErrInvalidProfile, "system_boundary", "is required")
	}
	if !p.AssessorStatus.Valid() {
		return fieldError(ErrInvalidProfile, "assessor_status", "is not declared")
	}
	if p.ReviewDate.IsZero() {
		return fieldError(ErrInvalidProfile, "review_date", "is required")
	}
	if p.ReviewDate.Location() != time.UTC {
		return fieldError(ErrInvalidProfile, "review_date", "must be UTC")
	}
	if len(p.programs()) == 0 {
		return fieldError(ErrInvalidProfile, "applicable_programs", "must contain at least one declared program")
	}
	seenPrograms := make(map[Program]bool, len(p.programs()))
	for _, program := range p.programs() {
		if !program.Valid() {
			return fieldError(ErrInvalidProfile, "applicable_programs", "contains an undeclared program")
		}
		if seenPrograms[program] {
			return fieldError(ErrInvalidProfile, "applicable_programs", "contains a duplicate program")
		}
		seenPrograms[program] = true
	}
	for _, control := range p.inheritedControls() {
		if err := control.Validate(); err != nil {
			return err
		}
	}
	for _, evidence := range p.evidencePointers() {
		if err := evidence.Validate(); err != nil {
			return err
		}
	}
	if fedramp := p.fedrampRecord(); fedramp != nil {
		if err := fedramp.Validate(); err != nil {
			return err
		}
	}
	if cms := p.cmsRecord(); cms != nil {
		if err := cms.Validate(); err != nil {
			return err
		}
	}
	if p.hasProgram(ProgramMARSE) && p.cmsRecord() == nil {
		return fieldError(ErrInvalidCMS, "cms", "is required when MARS-E is applicable")
	}
	seenQuestions := make(map[ProcurementQuestion]bool, len(p.ProcurementAnswers))
	for _, answer := range p.ProcurementAnswers {
		if seenQuestions[answer.Question] {
			return fieldError(ErrUnansweredQuestion, "procurement."+string(answer.Question), "is duplicated")
		}
		seenQuestions[answer.Question] = true
		if err := answer.Validate(); err != nil {
			return err
		}
	}
	if p.RevisionDigest != "" && !isHexDigest(p.RevisionDigest) {
		return fieldError(ErrImmutableRevision, "revision_digest", "must be a 64-character hexadecimal digest")
	}
	return nil
}

func (p GovernmentAuthorizationProfile) hasProgram(program Program) bool {
	for _, candidate := range p.programs() {
		if candidate == program {
			return true
		}
	}
	return false
}

// Digest returns the SHA-256 digest of the canonical profile revision.
func (p GovernmentAuthorizationProfile) Digest() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	digest, err := p.contentDigest()
	if err != nil {
		return "", err
	}
	if p.RevisionDigest != "" && !strings.EqualFold(p.RevisionDigest, digest) {
		return "", fmt.Errorf("%w: revision_digest", ErrImmutableRevision)
	}
	return digest, nil
}

func (p GovernmentAuthorizationProfile) contentDigest() (string, error) {
	canonical := canonicalProfile(p)
	b, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("govauth: canonical profile: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// RefreshFedRAMP returns a new profile revision after evaluating the supplied
// timeline fixture. The receiver and its slices are never mutated.
func (p GovernmentAuthorizationProfile) RefreshFedRAMP(timeline FedRAMPTimeline) (GovernmentAuthorizationProfile, error) {
	if err := p.Validate(); err != nil {
		return GovernmentAuthorizationProfile{}, err
	}
	if err := timeline.Validate(); err != nil {
		return GovernmentAuthorizationProfile{}, err
	}
	record := p.fedrampRecord()
	if record == nil {
		return GovernmentAuthorizationProfile{}, fieldError(ErrTimelineRefusal, "fedramp", "is required for a FedRAMP refresh")
	}
	revised := cloneProfile(p)
	refreshed := *record
	refreshed.Timeline = timeline
	revised.FedRAMP = &refreshed
	revised.FedRAMPPath = nil
	revised.Revision = p.revisionNumber() + 1
	revised.Version = 0
	revised.RevisionDigest = ""
	return NewGovernmentAuthorizationProfile(revised)
}

// Refresh is a concise alias for RefreshFedRAMP.
func (p GovernmentAuthorizationProfile) Refresh(timeline FedRAMPTimeline) (GovernmentAuthorizationProfile, error) {
	return p.RefreshFedRAMP(timeline)
}

// ProcurementPack is the generated, immutable questionnaire artifact.
type ProcurementPack struct {
	SchemaVersion   int                             `json:"schema_version"`
	ProfileDigest   string                          `json:"profile_digest"`
	ProfileRevision uint64                          `json:"profile_revision"`
	GeneratedAt     time.Time                       `json:"generated_at"`
	Answers         []ProcurementAnswer             `json:"answers"`
	Assurance       *ProcurementAssuranceReferences `json:"assurance,omitempty"`
	PackDigest      string                          `json:"pack_digest,omitempty"`
}

// PenetrationTestProcurementEvidence is the transport-neutral projection of a
// verified independent-test answer. The source service must verify its
// signed answer before adapting it to this contract.
type PenetrationTestProcurementEvidence struct {
	Answer          string `json:"answer"`
	AsOf            string `json:"as_of"`
	Status          string `json:"status"`
	EvidenceDigest  string `json:"evidence_digest"`
	AnswerDigest    string `json:"answer_digest"`
	ArtifactRef     string `json:"artifact_ref"`
	SignerPublicKey string `json:"signer_public_key"`
	Signature       string `json:"signature"`
}

// VPATReportReference is the version and digest pin produced by the VPAT
// service, paired with the latest UX-003 run known to the pack generator.
type VPATReportReference struct {
	Version     string    `json:"version"`
	Digest      string    `json:"sha256"`
	SourceRunID string    `json:"source_run_id"`
	SourceRunAt time.Time `json:"source_run_at"`
	LatestRunAt time.Time `json:"latest_ux003_run_at"`
}

// ProcurementAssuranceInputs are unverified data transferred from assurance
// services. Do not pass these values directly to pack generation; first build
// a VerifiedProcurementAssurance against a deployment-owned trust set.
type ProcurementAssuranceInputs struct {
	AsOf            time.Time                          `json:"as_of"`
	PenetrationTest PenetrationTestProcurementEvidence `json:"penetration_test"`
	Accessibility   VPATReportReference                `json:"accessibility"`
}

// VerifiedProcurementAssurance is a sealed value. Its fields are private so a
// caller cannot construct assurance references from raw answer text/digests.
type VerifiedProcurementAssurance struct {
	inputs   ProcurementAssuranceInputs
	digest   string
	verified bool
}

// VerifyProcurementAssurance authenticates the answer using an out-of-band
// trusted-key allowlist and validates its dated evidence references. The
// caller must source trustedKeys from deployment governance, not request data.
func VerifyProcurementAssurance(inputs ProcurementAssuranceInputs, trustedKeys [][]byte) (VerifiedProcurementAssurance, error) {
	if inputs.PenetrationTest.Status == "no_completed_engagement" && strings.TrimSpace(inputs.PenetrationTest.ArtifactRef) == "" && isHexDigest(inputs.PenetrationTest.AnswerDigest) {
		inputs.PenetrationTest.ArtifactRef = "pentest:no-engagement:sha256:" + strings.ToLower(inputs.PenetrationTest.AnswerDigest)
	}
	if err := validateProcurementAssuranceInputs(inputs, trustedKeys); err != nil {
		return VerifiedProcurementAssurance{}, err
	}
	digest, err := assuranceInputsDigest(inputs)
	if err != nil {
		return VerifiedProcurementAssurance{}, err
	}
	return VerifiedProcurementAssurance{inputs: inputs, digest: digest, verified: true}, nil
}

// ProcurementAssuranceReferences are embedded in a generated pack so its
// assurance answers remain traceable to versioned source evidence.
type ProcurementAssuranceReferences struct {
	PenetrationTest PenetrationTestProcurementEvidence `json:"penetration_test"`
	Accessibility   VPATReportReference                `json:"accessibility"`
}

// GenerateProcurementPackWithAssurance requires a current dated pentest
// answer and a digest-pinned VPAT report no older than the latest UX-003 run.
func GenerateProcurementPackWithAssurance(profile GovernmentAuthorizationProfile, assurance VerifiedProcurementAssurance) (ProcurementPack, error) {
	if err := profile.Validate(); err != nil {
		return ProcurementPack{}, err
	}
	inputs := assurance.inputs
	digest, digestErr := assuranceInputsDigest(inputs)
	if !assurance.verified || digestErr != nil || digest != assurance.digest {
		return ProcurementPack{}, fieldError(ErrStaleAssurance, "assurance", "verified assurance receipt is required")
	}
	if inputs.AsOf.IsZero() || inputs.AsOf.Location() != time.UTC || !sameUTCDay(inputs.AsOf, profile.ReviewDate) {
		return ProcurementPack{}, fieldError(ErrStaleAssurance, "assurance.as_of", "must match the profile review date in UTC")
	}
	p := inputs.PenetrationTest
	parsedAsOf, err := time.Parse("2006-01-02", p.AsOf)
	noEngagement := p.Status == "no_completed_engagement"
	if err != nil || !sameUTCDay(parsedAsOf, inputs.AsOf) || strings.TrimSpace(p.Answer) == "" || !validPentestStatus(p.Status) || (!noEngagement && !isHexDigest(p.EvidenceDigest)) || !isHexDigest(p.AnswerDigest) {
		return ProcurementPack{}, fieldError(ErrStaleAssurance, "assurance.penetration_test", "requires a current dated answer and pinned source digests")
	}
	if strings.TrimSpace(p.ArtifactRef) == "" && noEngagement {
		p.ArtifactRef = "pentest:no-engagement:sha256:" + strings.ToLower(p.AnswerDigest)
	}
	if strings.TrimSpace(p.ArtifactRef) == "" {
		return ProcurementPack{}, fieldError(ErrStaleAssurance, "assurance.penetration_test.artifact_ref", "is required")
	}
	v := inputs.Accessibility
	if strings.TrimSpace(v.Version) == "" || !isHexDigest(v.Digest) || strings.TrimSpace(v.SourceRunID) == "" || v.SourceRunAt.IsZero() || v.SourceRunAt.Location() != time.UTC || v.LatestRunAt.IsZero() || v.LatestRunAt.Location() != time.UTC || v.SourceRunAt.Before(v.LatestRunAt) || v.SourceRunAt.After(inputs.AsOf) {
		return ProcurementPack{}, fieldError(ErrStaleAssurance, "assurance.accessibility", "requires a current versioned digest-pinned VPAT reference")
	}
	pack, err := GenerateProcurementPack(profile)
	if err != nil {
		return ProcurementPack{}, err
	}
	for i := range pack.Answers {
		switch pack.Answers[i].Question {
		case QuestionVulnerabilityManagement:
			pack.Answers[i] = ProcurementAnswer{Question: QuestionVulnerabilityManagement, Answer: p.Answer, Owner: pack.Answers[i].Owner, Date: inputs.AsOf, ArtifactRef: p.ArtifactRef}
		case QuestionAccessibility:
			pack.Answers[i] = ProcurementAnswer{Question: QuestionAccessibility, Answer: "Interim accessibility evidence report " + v.Version + " SHA-256 " + strings.ToLower(v.Digest), Owner: pack.Answers[i].Owner, Date: inputs.AsOf, ArtifactRef: "vpat:" + v.Version + ":sha256:" + strings.ToLower(v.Digest)}
		}
	}
	pack.GeneratedAt = inputs.AsOf
	pack.Assurance = &ProcurementAssuranceReferences{PenetrationTest: p, Accessibility: v}
	digest, err = pack.contentDigest()
	if err != nil {
		return ProcurementPack{}, err
	}
	pack.PackDigest = digest
	return pack, nil
}

func validateProcurementAssuranceInputs(inputs ProcurementAssuranceInputs, trustedKeys [][]byte) error {
	if inputs.AsOf.IsZero() || inputs.AsOf.Location() != time.UTC {
		return fieldError(ErrStaleAssurance, "assurance.as_of", "must be a UTC date")
	}
	p := inputs.PenetrationTest
	parsedAsOf, err := time.Parse("2006-01-02", p.AsOf)
	noEngagement := p.Status == "no_completed_engagement"
	if err != nil || !sameUTCDay(parsedAsOf, inputs.AsOf) || strings.TrimSpace(p.Answer) == "" || !validPentestStatus(p.Status) || (!noEngagement && !isHexDigest(p.EvidenceDigest)) || !isHexDigest(p.AnswerDigest) || strings.TrimSpace(p.ArtifactRef) == "" {
		return fieldError(ErrStaleAssurance, "assurance.penetration_test", "requires a current dated answer and pinned source digests")
	}
	if err := trustpentest.VerifyTrustedSignature([]byte(p.AnswerDigest), p.SignerPublicKey, p.Signature, trustedKeys); err != nil {
		return fieldError(ErrStaleAssurance, "assurance.penetration_test.signature", "is not signed by a trusted assurance key")
	}
	v := inputs.Accessibility
	if strings.TrimSpace(v.Version) == "" || !isHexDigest(v.Digest) || strings.TrimSpace(v.SourceRunID) == "" || v.SourceRunAt.IsZero() || v.SourceRunAt.Location() != time.UTC || v.LatestRunAt.IsZero() || v.LatestRunAt.Location() != time.UTC || v.SourceRunAt.Before(v.LatestRunAt) || v.SourceRunAt.After(inputs.AsOf) {
		return fieldError(ErrStaleAssurance, "assurance.accessibility", "requires a current versioned digest-pinned VPAT reference")
	}
	return nil
}

func assuranceInputsDigest(inputs ProcurementAssuranceInputs) (string, error) {
	b, err := json.Marshal(inputs)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func sameUTCDay(a, b time.Time) bool {
	return !a.IsZero() && !b.IsZero() && a.UTC().Format("2006-01-02") == b.UTC().Format("2006-01-02")
}

func validPentestStatus(status string) bool {
	switch status {
	case "current", "overdue_high_or_critical_findings", "overdue_for_next_test", "no_completed_engagement":
		return true
	default:
		return false
	}
}

// GenerateProcurementPack answers all standard questions from the profile's
// existing evidence assertions. Missing answers refuse the named question.
func GenerateProcurementPack(profile GovernmentAuthorizationProfile) (ProcurementPack, error) {
	if err := profile.Validate(); err != nil {
		return ProcurementPack{}, err
	}
	profileDigest, err := profile.Digest()
	if err != nil {
		return ProcurementPack{}, err
	}
	answers := make(map[ProcurementQuestion]ProcurementAnswer, len(profile.ProcurementAnswers))
	for _, answer := range profile.ProcurementAnswers {
		answers[answer.Question] = answer
	}
	ordered := make([]ProcurementAnswer, 0, len(QuestionnaireItems))
	for _, question := range QuestionnaireItems {
		answer, ok := answers[question]
		if !ok {
			return ProcurementPack{}, fieldError(ErrUnansweredQuestion, "procurement."+string(question), "is unanswered")
		}
		ordered = append(ordered, answer)
	}
	pack := ProcurementPack{
		SchemaVersion:   schemaVersion,
		ProfileDigest:   profileDigest,
		ProfileRevision: profile.revisionNumber(),
		GeneratedAt:     profile.ReviewDate,
		Answers:         ordered,
	}
	digest, err := pack.contentDigest()
	if err != nil {
		return ProcurementPack{}, err
	}
	pack.PackDigest = digest
	return pack, nil
}

// GenerateProcurementEvidencePack is a descriptive alias for the generator.
func GenerateProcurementEvidencePack(profile GovernmentAuthorizationProfile) (ProcurementPack, error) {
	return GenerateProcurementPack(profile)
}

func (p ProcurementPack) contentDigest() (string, error) {
	b, err := json.Marshal(struct {
		SchemaVersion   int                             `json:"schema_version"`
		ProfileDigest   string                          `json:"profile_digest"`
		ProfileRevision uint64                          `json:"profile_revision"`
		GeneratedAt     time.Time                       `json:"generated_at"`
		Answers         []ProcurementAnswer             `json:"answers"`
		Assurance       *ProcurementAssuranceReferences `json:"assurance,omitempty"`
	}{p.SchemaVersion, p.ProfileDigest, p.ProfileRevision, p.GeneratedAt.UTC(), p.Answers, p.Assurance})
	if err != nil {
		return "", fmt.Errorf("govauth: canonical procurement pack: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// Digest returns the generated pack's digest and detects post-generation
// mutation of the exported value.
func (p ProcurementPack) Digest() (string, error) {
	if p.SchemaVersion != schemaVersion {
		return "", fieldError(ErrInvalidProfile, "pack.schema_version", "is unsupported")
	}
	if len(p.Answers) != len(QuestionnaireItems) {
		return "", fieldError(ErrUnansweredQuestion, "pack.answers", "does not cover every standard questionnaire item")
	}
	for i, question := range QuestionnaireItems {
		if p.Answers[i].Question != question {
			return "", fieldError(ErrUnansweredQuestion, "pack.answers", "is not in standard order")
		}
		if err := p.Answers[i].Validate(); err != nil {
			return "", err
		}
	}
	if p.Assurance != nil {
		refs := p.Assurance
		pt, vpat := refs.PenetrationTest, refs.Accessibility
		parsed, err := time.Parse("2006-01-02", pt.AsOf)
		noEngagement := pt.Status == "no_completed_engagement"
		if err != nil || !sameUTCDay(parsed, p.GeneratedAt) || strings.TrimSpace(pt.Answer) == "" || !validPentestStatus(pt.Status) || !isHexDigest(pt.AnswerDigest) || (!noEngagement && !isHexDigest(pt.EvidenceDigest)) || strings.TrimSpace(pt.ArtifactRef) == "" {
			return "", fieldError(ErrStaleAssurance, "pack.assurance.penetration_test", "is malformed or stale")
		}
		if vpat.Version == "" || !isHexDigest(vpat.Digest) || vpat.SourceRunID == "" || vpat.SourceRunAt.IsZero() || vpat.SourceRunAt.Location() != time.UTC || vpat.LatestRunAt.IsZero() || vpat.LatestRunAt.Location() != time.UTC || vpat.SourceRunAt.Before(vpat.LatestRunAt) || vpat.SourceRunAt.After(p.GeneratedAt) {
			return "", fieldError(ErrStaleAssurance, "pack.assurance.accessibility", "is malformed or stale")
		}
		foundPT, foundVPAT := false, false
		for _, answer := range p.Answers {
			if answer.Question == QuestionVulnerabilityManagement {
				foundPT = answer.Answer == pt.Answer && answer.ArtifactRef == pt.ArtifactRef && sameUTCDay(answer.Date, parsed)
			}
			if answer.Question == QuestionAccessibility {
				foundVPAT = answer.ArtifactRef == "vpat:"+vpat.Version+":sha256:"+strings.ToLower(vpat.Digest)
			}
		}
		if !foundPT || !foundVPAT {
			return "", fieldError(ErrStaleAssurance, "pack.assurance", "does not match questionnaire answers")
		}
	}
	digest, err := p.contentDigest()
	if err != nil {
		return "", err
	}
	if p.PackDigest != "" && !strings.EqualFold(p.PackDigest, digest) {
		return "", fmt.Errorf("%w: pack_digest", ErrImmutableRevision)
	}
	return digest, nil
}

// Explain returns an audit-safe summary. It intentionally omits tenant and
// integration identifiers, artifact references, owner identities, answer
// text, and all other potentially sensitive identifiers.
func (p GovernmentAuthorizationProfile) Explain() string {
	programs := append([]Program(nil), p.programs()...)
	sort.Slice(programs, func(i, j int) bool { return programs[i] < programs[j] })
	fedramp := "none"
	if record := p.fedrampRecord(); record != nil {
		fedramp = string(record.Path)
	}
	return fmt.Sprintf("government authorization profile revision %d; programs=%s; assessor=%s; evidence=%d; inherited_controls=%d; fedramp=%s; cms=%t; procurement_answers=%d",
		p.revisionNumber(), strings.Join(programStrings(programs), ","), p.AssessorStatus,
		len(p.evidencePointers()), len(p.inheritedControls()), fedramp, p.cmsRecord() != nil, len(p.ProcurementAnswers))
}

// Explain is the package-level audit-safe explanation helper.
func Explain(profile GovernmentAuthorizationProfile) string { return profile.Explain() }

// ExplainPack summarizes a procurement pack without exposing answer content
// or artifact identifiers.
func ExplainPack(pack ProcurementPack) string {
	return fmt.Sprintf("procurement evidence pack revision %d; answers=%d", pack.ProfileRevision, len(pack.Answers))
}

func questionValid(question ProcurementQuestion) bool {
	for _, item := range QuestionnaireItems {
		if question == item {
			return true
		}
	}
	return false
}

func programStrings(programs []Program) []string {
	result := make([]string, len(programs))
	for i, program := range programs {
		result[i] = string(program)
	}
	return result
}

func isHexDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func fieldError(kind error, field, reason string) error {
	return fmt.Errorf("%w: %s %s", kind, field, reason)
}

func cloneProfile(profile GovernmentAuthorizationProfile) GovernmentAuthorizationProfile {
	clone := profile
	clone.ApplicablePrograms = append([]Program(nil), profile.ApplicablePrograms...)
	clone.Programs = append([]Program(nil), profile.Programs...)
	clone.InheritedControls = append([]ControlInheritanceReference(nil), profile.InheritedControls...)
	clone.InheritedControlReferences = append([]ControlInheritanceReference(nil), profile.InheritedControlReferences...)
	clone.Evidence = append([]EvidencePointer(nil), profile.Evidence...)
	clone.EvidenceCatalog = append([]EvidencePointer(nil), profile.EvidenceCatalog...)
	clone.ProcurementAnswers = append([]ProcurementAnswer(nil), profile.ProcurementAnswers...)
	if profile.FedRAMP != nil {
		record := *profile.FedRAMP
		record.ContinuousMonitoringEvidence = append([]EvidencePointer(nil), profile.FedRAMP.ContinuousMonitoringEvidence...)
		clone.FedRAMP = &record
	}
	if profile.FedRAMPPath != nil {
		record := *profile.FedRAMPPath
		record.ContinuousMonitoringEvidence = append([]EvidencePointer(nil), profile.FedRAMPPath.ContinuousMonitoringEvidence...)
		clone.FedRAMPPath = &record
	}
	if profile.CMS != nil {
		record := *profile.CMS
		record.ControlInheritance = append([]ControlInheritanceReference(nil), profile.CMS.ControlInheritance...)
		clone.CMS = &record
	}
	if profile.MARSEResources != nil {
		record := *profile.MARSEResources
		record.ControlInheritance = append([]ControlInheritanceReference(nil), profile.MARSEResources.ControlInheritance...)
		clone.MARSEResources = &record
	}
	return clone
}

type canonicalProfilePayload struct {
	SchemaVersion      int                           `json:"schema_version"`
	Revision           uint64                        `json:"revision"`
	TenantID           string                        `json:"tenant_id"`
	IntegrationID      string                        `json:"integration_id"`
	ApplicablePrograms []Program                     `json:"applicable_programs"`
	SystemBoundary     string                        `json:"system_boundary"`
	InheritedControls  []ControlInheritanceReference `json:"inherited_controls"`
	Evidence           []EvidencePointer             `json:"evidence"`
	AssessorStatus     AssessorStatus                `json:"assessor_status"`
	ReviewDate         time.Time                     `json:"review_date"`
	FedRAMP            *FedRAMPPathRecord            `json:"fedramp,omitempty"`
	CMS                *CMSReferenceBlock            `json:"cms,omitempty"`
	ProcurementAnswers []ProcurementAnswer           `json:"procurement_answers"`
}

func canonicalProfile(profile GovernmentAuthorizationProfile) canonicalProfilePayload {
	programs := append([]Program(nil), profile.programs()...)
	sort.Slice(programs, func(i, j int) bool { return programs[i] < programs[j] })
	controls := append([]ControlInheritanceReference(nil), profile.inheritedControls()...)
	sort.SliceStable(controls, func(i, j int) bool { return controls[i].ControlID < controls[j].ControlID })
	evidence := append([]EvidencePointer(nil), profile.evidencePointers()...)
	sort.SliceStable(evidence, func(i, j int) bool { return evidence[i].Reference() < evidence[j].Reference() })
	answers := append([]ProcurementAnswer(nil), profile.ProcurementAnswers...)
	sort.SliceStable(answers, func(i, j int) bool { return answers[i].Question < answers[j].Question })
	return canonicalProfilePayload{
		SchemaVersion: schemaVersion, Revision: profile.revisionNumber(),
		TenantID: profile.tenantReference(), IntegrationID: profile.integrationReference(),
		ApplicablePrograms: programs, SystemBoundary: profile.SystemBoundary,
		InheritedControls: controls, Evidence: evidence, AssessorStatus: profile.AssessorStatus,
		ReviewDate: profile.ReviewDate.UTC(), FedRAMP: profile.fedrampRecord(), CMS: profile.cmsRecord(),
		ProcurementAnswers: answers,
	}
}
