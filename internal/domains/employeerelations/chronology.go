// ER-002: prove appeal, grievance, settlement, retaliation and correction
// chronology.
//
// Successor proceedings supersede without overwriting: an appeal names the
// finding or discipline it contests, a settlement preserves its allegation
// and grievance by reference, and a correction appends alongside the
// discipline it corrects. The CaseChronology only grows — entries are never
// removed — so the original finding, allegation and discipline stay
// provable. Deadlines, holds and representation stay explicit pins, and the
// evidence package returns the exact redacted history with no causal
// overclaim. Retaliation signals are visible only to investigating roles,
// never to the subject or their manager.
package employeerelations

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

var (
	ErrChronologyGap        = errors.New("employeerelations: chronology gap")
	ErrDeadlineRequired     = errors.New("employeerelations: appeal deadline is required")
	ErrHoldsRequired        = errors.New("employeerelations: settlement requires explicit hold review")
	ErrRetaliationProtected = errors.New("employeerelations: retaliation signal is protected")
)

// AppealStatus is the closed appeal lifecycle.
type AppealStatus string

// Appeal states.
const (
	AppealOpen       AppealStatus = "OPEN"
	AppealUpheld     AppealStatus = "UPHELD"
	AppealOverturned AppealStatus = "OVERTURNED"
	AppealRemanded   AppealStatus = "REMANDED"
)

func (s AppealStatus) Valid() bool {
	switch s {
	case AppealOpen, AppealUpheld, AppealOverturned, AppealRemanded:
		return true
	}
	return false
}

// AppealRevision is a successor proceeding contesting one finding or
// discipline. It carries the contested digest, never a copy of the
// contested record: the original stays authoritative.
type AppealRevision struct {
	ID, CaseRef, CompartmentRef             string
	Revision, ParentRevision                uint64
	ParentDigest                            string
	Participants                            []Participant
	ContestedDigest, ContestedKind          string
	Grounds, DeadlineRef, RepresentationRef string
	HoldRefs                                []string
	Status                                  AppealStatus
	CanonicalDigest                         string
}

// Appeal is the concise vocabulary spelling for AppealRevision.
type Appeal = AppealRevision

func (r AppealRevision) compartment() Compartment {
	return Compartment{r.CaseRef, r.CompartmentRef, r.Participants}
}

func (r AppealRevision) Validate() error {
	if err := validateLineage("appeal", r.ID, r.Revision, r.ParentRevision, r.ParentDigest, r.compartment(), r.CanonicalDigest); err != nil {
		return err
	}
	if strings.TrimSpace(r.ContestedDigest) == "" {
		return invalid("contested_digest", "appeal must name the contested digest")
	}
	if r.ContestedKind != "finding" && r.ContestedKind != "discipline" {
		return invalid("contested_kind", "appeal contests a finding or a discipline")
	}
	if strings.TrimSpace(r.Grounds) == "" {
		return invalid("grounds", "appeal grounds are required")
	}
	if strings.TrimSpace(r.DeadlineRef) == "" {
		return refused("ER_APPEAL_DEADLINE", "deadline_ref", "appeal requires the grievance deadline reference", ErrDeadlineRequired)
	}
	if strings.TrimSpace(r.RepresentationRef) == "" {
		return invalid("representation_ref", "appeal requires representation review")
	}
	if !r.Status.Valid() {
		return invalid("status", "appeal status is not declared")
	}
	return validateDigest(r.CanonicalDigest, r.computedDigest())
}

func (r AppealRevision) body() []byte {
	holds := append([]string(nil), r.HoldRefs...)
	sort.Strings(holds)
	w := canonicalbytes.New("hcmnext.domains.employeerelations.AppealRevision", schemaVersion).String("id", r.ID).String("case_ref", r.CaseRef).String("compartment_ref", r.CompartmentRef).Int("revision", int64(r.Revision)).Int("parent_revision", int64(r.ParentRevision)).String("parent_digest", r.ParentDigest).Value("compartment", r.compartment()).String("contested_digest", r.ContestedDigest).String("contested_kind", r.ContestedKind).String("grounds", r.Grounds).String("deadline_ref", r.DeadlineRef).String("representation_ref", r.RepresentationRef).SortedStrings("hold_refs", holds).String("status", string(r.Status))
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (r AppealRevision) computedDigest() string { return canonicalbytes.Digest(r.body()) }

// Canonical returns the canonical encoding, or nil when invalid.
func (r AppealRevision) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}

// Digest returns the canonical digest of a valid appeal.
func (r AppealRevision) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}

// Explain renders the appeal without identities.
func (r AppealRevision) Explain() string {
	return fmt.Sprintf("appeal %s in case %s, compartment %s, revision %d, contests %s, status %s, deadline pinned, digest %s", r.ID, r.CaseRef, r.CompartmentRef, r.Revision, r.ContestedKind, r.Status, r.CanonicalDigest)
}

// NewAppealRevision validates and seals one appeal revision.
func NewAppealRevision(r AppealRevision) (AppealRevision, error) {
	r.Participants = cloneParticipants(r.Participants)
	r.HoldRefs = append([]string(nil), r.HoldRefs...)
	r.CanonicalDigest = ""
	if err := r.Validate(); err != nil {
		return AppealRevision{}, err
	}
	r.CanonicalDigest = r.computedDigest()
	return r, nil
}

// NewAppeal is the vocabulary-oriented alias for NewAppealRevision.
func NewAppeal(r AppealRevision) (AppealRevision, error) { return NewAppealRevision(r) }

// SettlementStatus is the closed settlement lifecycle.
type SettlementStatus string

// Settlement states.
const (
	SettlementProposed SettlementStatus = "PROPOSED"
	SettlementApproved SettlementStatus = "APPROVED"
	SettlementExecuted SettlementStatus = "EXECUTED"
)

func (s SettlementStatus) Valid() bool {
	switch s {
	case SettlementProposed, SettlementApproved, SettlementExecuted:
		return true
	}
	return false
}

// SettlementRevision resolves an allegation and grievance by reference. Both
// digests stay pinned on the settlement and in the chronology: a settlement
// ends the dispute, it never erases what happened.
type SettlementRevision struct {
	ID, CaseRef, CompartmentRef       string
	Revision, ParentRevision          uint64
	ParentDigest                      string
	Participants                      []Participant
	AllegationDigest, GrievanceDigest string
	TermsRef, AuthorityRef            string
	HoldRefs                          []string
	HoldsChecked                      bool
	RetaliationSafeguard              bool
	Status                            SettlementStatus
	CanonicalDigest                   string
}

// Settlement is the concise vocabulary spelling for SettlementRevision.
type Settlement = SettlementRevision

func (r SettlementRevision) compartment() Compartment {
	return Compartment{r.CaseRef, r.CompartmentRef, r.Participants}
}

func (r SettlementRevision) Validate() error {
	if err := validateLineage("settlement", r.ID, r.Revision, r.ParentRevision, r.ParentDigest, r.compartment(), r.CanonicalDigest); err != nil {
		return err
	}
	if strings.TrimSpace(r.AllegationDigest) == "" || strings.TrimSpace(r.GrievanceDigest) == "" {
		return invalid("allegation_digest", "settlement preserves its allegation and grievance by reference")
	}
	for field, value := range map[string]string{"terms_ref": r.TermsRef, "authority_ref": r.AuthorityRef} {
		if strings.TrimSpace(value) == "" {
			return invalid(field, "value is required")
		}
	}
	if !r.HoldsChecked {
		return refused("ER_SETTLEMENT_HOLDS", "holds_checked", "settlement requires explicit hold review", ErrHoldsRequired)
	}
	if !r.RetaliationSafeguard {
		return refused("ER_SETTLEMENT_SAFEGUARD", "retaliation_safeguard", "settlement must preserve the retaliation safeguard", ErrRetaliationProtected)
	}
	if !r.Status.Valid() {
		return invalid("status", "settlement status is not declared")
	}
	return validateDigest(r.CanonicalDigest, r.computedDigest())
}

func (r SettlementRevision) body() []byte {
	holds := append([]string(nil), r.HoldRefs...)
	sort.Strings(holds)
	w := canonicalbytes.New("hcmnext.domains.employeerelations.SettlementRevision", schemaVersion).String("id", r.ID).String("case_ref", r.CaseRef).String("compartment_ref", r.CompartmentRef).Int("revision", int64(r.Revision)).Int("parent_revision", int64(r.ParentRevision)).String("parent_digest", r.ParentDigest).Value("compartment", r.compartment()).String("allegation_digest", r.AllegationDigest).String("grievance_digest", r.GrievanceDigest).String("terms_ref", r.TermsRef).String("authority_ref", r.AuthorityRef).SortedStrings("hold_refs", holds).Bool("holds_checked", r.HoldsChecked).Bool("retaliation_safeguard", r.RetaliationSafeguard).String("status", string(r.Status))
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (r SettlementRevision) computedDigest() string { return canonicalbytes.Digest(r.body()) }

// Canonical returns the canonical encoding, or nil when invalid.
func (r SettlementRevision) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}

// Digest returns the canonical digest of a valid settlement.
func (r SettlementRevision) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}

// Explain renders the settlement without identities.
func (r SettlementRevision) Explain() string {
	return fmt.Sprintf("settlement %s in case %s, compartment %s, revision %d, status %s, holds reviewed, safeguard held, digest %s", r.ID, r.CaseRef, r.CompartmentRef, r.Revision, r.Status, r.CanonicalDigest)
}

// NewSettlementRevision validates and seals one settlement revision.
func NewSettlementRevision(r SettlementRevision) (SettlementRevision, error) {
	r.Participants = cloneParticipants(r.Participants)
	r.HoldRefs = append([]string(nil), r.HoldRefs...)
	r.CanonicalDigest = ""
	if err := r.Validate(); err != nil {
		return SettlementRevision{}, err
	}
	r.CanonicalDigest = r.computedDigest()
	return r, nil
}

// NewSettlement is the vocabulary-oriented alias for NewSettlementRevision.
func NewSettlement(r SettlementRevision) (SettlementRevision, error) {
	return NewSettlementRevision(r)
}

// CorrectionStatus is the closed correction lifecycle.
type CorrectionStatus string

// Correction states.
const (
	CorrectionIssued CorrectionStatus = "ISSUED"
)

func (s CorrectionStatus) Valid() bool { return s == CorrectionIssued }

// CorrectionRevision appends a correction alongside the record it corrects.
// The corrected record stays in the chronology with its original digest:
// a correction adds dated truth, it never removes prior truth.
type CorrectionRevision struct {
	ID, CaseRef, CompartmentRef      string
	Revision, ParentRevision         uint64
	ParentDigest                     string
	Participants                     []Participant
	CorrectsDigest, CorrectsKind     string
	Correction, Reason, AuthorityRef string
	Status                           CorrectionStatus
	CanonicalDigest                  string
}

// Correction is the concise vocabulary spelling for CorrectionRevision.
type Correction = CorrectionRevision

func (r CorrectionRevision) compartment() Compartment {
	return Compartment{r.CaseRef, r.CompartmentRef, r.Participants}
}

func (r CorrectionRevision) Validate() error {
	if err := validateLineage("correction", r.ID, r.Revision, r.ParentRevision, r.ParentDigest, r.compartment(), r.CanonicalDigest); err != nil {
		return err
	}
	if strings.TrimSpace(r.CorrectsDigest) == "" {
		return invalid("corrects_digest", "correction must name the corrected digest")
	}
	switch r.CorrectsKind {
	case "allegation", "investigation", "interview", "finding", "discipline", "grievance":
	default:
		return invalid("corrects_kind", "correction names a known record kind")
	}
	for field, value := range map[string]string{"correction": r.Correction, "reason": r.Reason, "authority_ref": r.AuthorityRef} {
		if strings.TrimSpace(value) == "" {
			return invalid(field, "value is required")
		}
	}
	if !r.Status.Valid() {
		return invalid("status", "correction status is not declared")
	}
	return validateDigest(r.CanonicalDigest, r.computedDigest())
}

func (r CorrectionRevision) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.employeerelations.CorrectionRevision", schemaVersion).String("id", r.ID).String("case_ref", r.CaseRef).String("compartment_ref", r.CompartmentRef).Int("revision", int64(r.Revision)).Int("parent_revision", int64(r.ParentRevision)).String("parent_digest", r.ParentDigest).Value("compartment", r.compartment()).String("corrects_digest", r.CorrectsDigest).String("corrects_kind", r.CorrectsKind).String("correction", r.Correction).String("reason", r.Reason).String("authority_ref", r.AuthorityRef).String("status", string(r.Status))
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (r CorrectionRevision) computedDigest() string { return canonicalbytes.Digest(r.body()) }

// Canonical returns the canonical encoding, or nil when invalid.
func (r CorrectionRevision) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}

// Digest returns the canonical digest of a valid correction.
func (r CorrectionRevision) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}

// Explain renders the correction without identities.
func (r CorrectionRevision) Explain() string {
	return fmt.Sprintf("correction %s in case %s, compartment %s, revision %d, corrects %s, status %s, digest %s", r.ID, r.CaseRef, r.CompartmentRef, r.Revision, r.CorrectsKind, r.Status, r.CanonicalDigest)
}

// NewCorrectionRevision validates and seals one correction revision.
func NewCorrectionRevision(r CorrectionRevision) (CorrectionRevision, error) {
	r.Participants = cloneParticipants(r.Participants)
	r.CanonicalDigest = ""
	if err := r.Validate(); err != nil {
		return CorrectionRevision{}, err
	}
	r.CanonicalDigest = r.computedDigest()
	return r, nil
}

// NewCorrection is the vocabulary-oriented alias for NewCorrectionRevision.
func NewCorrection(r CorrectionRevision) (CorrectionRevision, error) {
	return NewCorrectionRevision(r)
}

// RetaliationSignal is a protected report that a subject may face
// retaliation. Only investigating roles may view it: the subject and their
// manager are always refused, whatever role they claim.
type RetaliationSignal struct {
	ID, CaseRef, CompartmentRef         string
	ReporterRef, SubjectRef, ManagerRef string
	DetailRef                           string
}

// NewRetaliationSignal validates one retaliation signal.
func NewRetaliationSignal(s RetaliationSignal) (RetaliationSignal, error) {
	if strings.TrimSpace(s.ID) == "" || strings.TrimSpace(s.CaseRef) == "" || strings.TrimSpace(s.CompartmentRef) == "" {
		return RetaliationSignal{}, invalid("id", "signal identity is required")
	}
	for field, value := range map[string]string{"reporter_ref": s.ReporterRef, "subject_ref": s.SubjectRef, "manager_ref": s.ManagerRef, "detail_ref": s.DetailRef} {
		if strings.TrimSpace(value) == "" {
			return RetaliationSignal{}, invalid(field, "value is required")
		}
	}
	return s, nil
}

// AuthorizeViewer reports whether principal acting as role may view the
// signal. Membership is checked against the case participants: strangers
// are refused even under an investigating role.
func (s RetaliationSignal) AuthorizeViewer(principal string, role ParticipantRole, participants []Participant) error {
	member := false
	for _, p := range participants {
		if p.Ref == principal {
			member = true
		}
	}
	if !member {
		return refused("ER_RETALIATION_VIEWER", "principal", "viewer is not a case participant", ErrRetaliationProtected)
	}
	if principal == s.SubjectRef || principal == s.ManagerRef {
		return refused("ER_RETALIATION_VIEWER", "principal", "subject and manager never view retaliation signals", ErrRetaliationProtected)
	}
	switch role {
	case RoleInvestigator, RoleDecisionMaker, RoleReviewer:
		return nil
	}
	return refused("ER_RETALIATION_VIEWER", "role", "retaliation signals are visible to investigating roles only", ErrRetaliationProtected)
}

// Explain renders the signal without any identity.
func (s RetaliationSignal) Explain() string {
	return fmt.Sprintf("retaliation signal %s in case %s, compartment %s, identities protected", s.ID, s.CaseRef, s.CompartmentRef)
}

// ChronologyEntry is one ordered, content-free history pin: kind, id,
// digest and the digest it supersedes, if any.
type ChronologyEntry struct {
	Kind, ID, Digest, Supersedes string
}

// CaseChronology is the append-only proceeding history of one case
// compartment. Entries only grow; deadlines and holds stay pinned.
type CaseChronology struct {
	CaseRef, CompartmentRef string
	Participants            []Participant
	Entries                 []ChronologyEntry
	HoldRefs                []string
	DeadlineRefs            []string
}

// NewCaseChronology opens the chronology for one case compartment.
func NewCaseChronology(caseRef, compartmentRef string, participants []Participant) (CaseChronology, error) {
	if strings.TrimSpace(caseRef) == "" || strings.TrimSpace(compartmentRef) == "" {
		return CaseChronology{}, invalid("case_ref", "chronology case and compartment are required")
	}
	if len(participants) == 0 {
		return CaseChronology{}, invalid("participants", "chronology requires participants")
	}
	return CaseChronology{CaseRef: caseRef, CompartmentRef: compartmentRef, Participants: cloneParticipants(participants)}, nil
}

func (c CaseChronology) hasDigest(digest string) bool {
	for _, e := range c.Entries {
		if e.Digest == digest {
			return true
		}
	}
	return false
}

func (c *CaseChronology) pinHold(refs []string) {
	seen := map[string]bool{}
	for _, h := range c.HoldRefs {
		seen[h] = true
	}
	for _, h := range refs {
		if h != "" && !seen[h] {
			seen[h] = true
			c.HoldRefs = append(c.HoldRefs, h)
		}
	}
	sort.Strings(c.HoldRefs)
}

func (c *CaseChronology) pinDeadline(ref string) {
	for _, d := range c.DeadlineRefs {
		if d == ref {
			return
		}
	}
	c.DeadlineRefs = append(c.DeadlineRefs, ref)
	sort.Strings(c.DeadlineRefs)
}

// AppendRecord pins one base proceeding (allegation, investigation,
// interview, finding, discipline, grievance) by its canonical digest.
func (c *CaseChronology) AppendRecord(kind, id, digest string) error {
	switch kind {
	case "allegation", "investigation", "interview", "finding", "discipline", "grievance":
	default:
		return refused("ER_CHRONOLOGY_KIND", "kind", "chronology records a known proceeding kind", ErrChronologyGap)
	}
	if strings.TrimSpace(id) == "" || strings.TrimSpace(digest) == "" {
		return invalid("digest", "chronology entry requires id and digest")
	}
	if c.hasDigest(digest) {
		return refused("ER_CHRONOLOGY_DUPLICATE", "digest", "chronology never replays an entry", ErrChronologyGap)
	}
	c.Entries = append(c.Entries, ChronologyEntry{Kind: kind, ID: id, Digest: digest})
	return nil
}

func (c CaseChronology) scoped(kind, id string, caseRef, compartmentRef string) error {
	if caseRef != c.CaseRef || compartmentRef != c.CompartmentRef {
		return refused("ER_CHRONOLOGY_SCOPE", kind, "successor belongs to another case compartment", ErrChronologyGap)
	}
	return nil
}

// AppendAppeal appends a validated appeal whose contested digest is already
// in the chronology. The contested entry stays: the appeal supersedes
// without overwriting.
func (c *CaseChronology) AppendAppeal(a AppealRevision) error {
	if err := a.Validate(); err != nil {
		return err
	}
	if err := c.scoped("appeal", a.ID, a.CaseRef, a.CompartmentRef); err != nil {
		return err
	}
	if !c.hasDigest(a.ContestedDigest) {
		return refused("ER_CHRONOLOGY_GAP", "contested_digest", "appeal contests an unseen record", ErrChronologyGap)
	}
	if c.hasDigest(a.CanonicalDigest) {
		return refused("ER_CHRONOLOGY_DUPLICATE", "digest", "chronology never replays an entry", ErrChronologyGap)
	}
	c.Entries = append(c.Entries, ChronologyEntry{Kind: "appeal", ID: a.ID, Digest: a.CanonicalDigest, Supersedes: a.ContestedDigest})
	c.pinHold(a.HoldRefs)
	c.pinDeadline(a.DeadlineRef)
	return nil
}

// AppendSettlement appends a validated settlement whose allegation and
// grievance are both already in the chronology: nothing is erased.
func (c *CaseChronology) AppendSettlement(s SettlementRevision) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if err := c.scoped("settlement", s.ID, s.CaseRef, s.CompartmentRef); err != nil {
		return err
	}
	if !c.hasDigest(s.AllegationDigest) || !c.hasDigest(s.GrievanceDigest) {
		return refused("ER_CHRONOLOGY_GAP", "allegation_digest", "settlement preserves an unseen allegation or grievance", ErrChronologyGap)
	}
	if c.hasDigest(s.CanonicalDigest) {
		return refused("ER_CHRONOLOGY_DUPLICATE", "digest", "chronology never replays an entry", ErrChronologyGap)
	}
	c.Entries = append(c.Entries, ChronologyEntry{Kind: "settlement", ID: s.ID, Digest: s.CanonicalDigest})
	c.pinHold(s.HoldRefs)
	return nil
}

// AppendCorrection appends a validated correction whose target is already
// in the chronology. The corrected entry stays with its original digest.
func (c *CaseChronology) AppendCorrection(r CorrectionRevision) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if err := c.scoped("correction", r.ID, r.CaseRef, r.CompartmentRef); err != nil {
		return err
	}
	if !c.hasDigest(r.CorrectsDigest) {
		return refused("ER_CHRONOLOGY_GAP", "corrects_digest", "correction targets an unseen record", ErrChronologyGap)
	}
	if c.hasDigest(r.CanonicalDigest) {
		return refused("ER_CHRONOLOGY_DUPLICATE", "digest", "chronology never replays an entry", ErrChronologyGap)
	}
	c.Entries = append(c.Entries, ChronologyEntry{Kind: "correction", ID: r.ID, Digest: r.CanonicalDigest, Supersedes: r.CorrectsDigest})
	return nil
}

// ChronologyPackage is the exact redacted history: ordered entries, holds
// and deadlines, with no identities and no causal claims.
type ChronologyPackage struct {
	CaseRef, CompartmentRef string
	Entries                 []ChronologyEntry
	Holds                   []string
	Deadlines               []string
	Digest                  string
}

// Explain renders the package without identities.
func (p ChronologyPackage) Explain() string {
	return fmt.Sprintf("er chronology package for case %s, compartment %s, %d entries, digest %s", p.CaseRef, p.CompartmentRef, len(p.Entries), p.Digest)
}

// EvidencePackage returns the exact redacted history with its digest.
func (c CaseChronology) EvidencePackage() ChronologyPackage {
	entries := append([]ChronologyEntry(nil), c.Entries...)
	pkg := ChronologyPackage{CaseRef: c.CaseRef, CompartmentRef: c.CompartmentRef, Entries: entries, Holds: append([]string(nil), c.HoldRefs...), Deadlines: append([]string(nil), c.DeadlineRefs...)}
	h := canonicalbytes.New("hcmnext.domains.employeerelations.ChronologyPackage", schemaVersion).String("case_ref", pkg.CaseRef).String("compartment_ref", pkg.CompartmentRef).Count("entries", len(entries))
	for _, e := range entries {
		h.String("entry.kind", e.Kind).String("entry.id", e.ID).String("entry.digest", e.Digest).String("entry.supersedes", e.Supersedes)
	}
	h.SortedStrings("holds", pkg.Holds).SortedStrings("deadlines", pkg.Deadlines)
	if raw, err := h.Bytes(); err == nil {
		pkg.Digest = canonicalbytes.Digest(raw)
	}
	return pkg
}

// VerifyDigests checks every entry against a store-backed oracle: resolve
// returns the canonical digest the store holds for kind+id.
func (c CaseChronology) VerifyDigests(resolve func(kind, id string) (string, bool)) error {
	for i, e := range c.Entries {
		digest, ok := resolve(e.Kind, e.ID)
		if !ok {
			return refused("ER_CHRONOLOGY_GAP", "entries", fmt.Sprintf("entry %d (%s %s) resolves nowhere", i, e.Kind, e.ID), ErrChronologyGap)
		}
		if digest != e.Digest {
			return refused("ER_CHRONOLOGY_GAP", "entries", fmt.Sprintf("entry %d (%s %s) digest disagrees", i, e.Kind, e.ID), ErrChronologyGap)
		}
	}
	return nil
}
