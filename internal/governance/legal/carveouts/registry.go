// Package carveouts contains typed, versioned legal reference registries for
// state pay-transparency exceptions, UI separation filing formats, and local
// wage/leave overlays. The registries are reference data: they explain which
// researched parameter was selected, but they never transmit a filing or make
// a legal conclusion.
package carveouts

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const schemaVersion = 1

// Version reports the carveout registry schema version.
func Version() int { return schemaVersion }

// Typed validation failures are matchable with ErrValidation and always name
// the exact schema field that refused the value.
var (
	ErrValidation       = errors.New("legal carveouts: PACK_VALIDATION_FAILED")
	ErrDuplicateState   = errors.New("legal carveouts: duplicate state")
	ErrUnknownState     = errors.New("legal carveouts: unknown state")
	ErrRegistryCoverage = errors.New("legal carveouts: registry must contain one row per state")
)

// ValidationError identifies a refused field instead of returning an
// unstructured parser error.
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%v: field %s: %s", ErrValidation, e.Field, e.Reason)
}

func (e *ValidationError) Unwrap() error { return ErrValidation }

func invalid(field, reason string) error { return &ValidationError{Field: field, Reason: reason} }

// ReviewFlag says whether the row's research interpretation has been reviewed.
type ReviewFlag string

const (
	ReviewUnreviewed ReviewFlag = "UNREVIEWED"
	ReviewReviewed   ReviewFlag = "REVIEWED"
)

func (r ReviewFlag) valid() bool { return r == ReviewUnreviewed || r == ReviewReviewed }

// Citation points to the exact state research file and statute or research
// finding used for a row. Note is intentionally absent: the statute citation
// and review flag are the stable reference data in this registry.
type Citation struct {
	SourceFile string
	Section    string
	Review     ReviewFlag
}

func (c Citation) validate(field string) error {
	if strings.TrimSpace(c.SourceFile) == "" {
		return invalid(field+".source_file", "is required")
	}
	if !strings.HasPrefix(c.SourceFile, "planning/research/state-employment-law/") || !strings.HasSuffix(c.SourceFile, ".md") {
		return invalid(field+".source_file", "must name a state research markdown file")
	}
	if strings.TrimSpace(c.Section) == "" {
		return invalid(field+".section", "statute citation or explicit research finding is required")
	}
	if !c.Review.valid() {
		return invalid(field+".review", "must be REVIEWED or UNREVIEWED")
	}
	return nil
}

// ExceptionAppliesTo identifies the duty to which an exception attaches.
type ExceptionAppliesTo string

const (
	SalaryHistoryBan ExceptionAppliesTo = "SALARY_HISTORY_BAN"
	PostingMandate   ExceptionAppliesTo = "POSTING_MANDATE"
)

// ExceptionKind identifies a lawful carve-out before the duty trigger fires.
type ExceptionKind string

const (
	VoluntaryDisclosure        ExceptionKind = "VOLUNTARY_DISCLOSURE"
	SameEmployerVerification   ExceptionKind = "SAME_EMPLOYER_VERIFICATION"
	LateralTransferNoPayChange ExceptionKind = "LATERAL_TRANSFER_NO_PAY_CHANGE"
	EmployerSizeBelowThreshold ExceptionKind = "EMPLOYER_SIZE_BELOW_THRESHOLD"
)

// PayTransparencyException is a typed exception to a salary-history ban or
// posting mandate. EmployerSizeThreshold is used only by the size exception.
type PayTransparencyException struct {
	AppliesTo             ExceptionAppliesTo
	ExceptionKind         ExceptionKind
	EmployerSizeThreshold int
	Citation              Citation
}

// PayTransparencyDuty is the state-scoped duty to which exceptions attach.
// The exception list is evaluated before a caller applies the duty trigger.
type PayTransparencyDuty struct {
	ID                       string
	SalaryHistoryBan         bool
	PostingEmployerSizeFloor int
	Exceptions               []PayTransparencyException
	Citation                 Citation
}

func (d PayTransparencyDuty) validate(field string) error {
	if d.ID == "" {
		return invalid(field+".id", "is required")
	}
	if d.PostingEmployerSizeFloor < 0 {
		return invalid(field+".posting_employer_size_floor", "must not be negative")
	}
	if err := d.Citation.validate(field + ".citation"); err != nil {
		return err
	}
	for i, e := range d.Exceptions {
		if err := e.validate(fmt.Sprintf("%s.exceptions[%d]", field, i)); err != nil {
			return err
		}
	}
	return nil
}

func (e PayTransparencyException) validate(field string) error {
	if e.AppliesTo != SalaryHistoryBan && e.AppliesTo != PostingMandate {
		return invalid(field+".applies_to", "must be SALARY_HISTORY_BAN or POSTING_MANDATE")
	}
	switch e.ExceptionKind {
	case VoluntaryDisclosure, SameEmployerVerification, LateralTransferNoPayChange:
	case EmployerSizeBelowThreshold:
		if e.EmployerSizeThreshold <= 0 {
			return invalid(field+".employer_size_threshold", "must be positive for EMPLOYER_SIZE_BELOW_THRESHOLD")
		}
	default:
		return invalid(field+".exception_kind", "is not a supported pay-transparency exception")
	}
	if e.ExceptionKind != EmployerSizeBelowThreshold && e.EmployerSizeThreshold != 0 {
		return invalid(field+".employer_size_threshold", "must be zero unless the exception is size-based")
	}
	return e.Citation.validate(field + ".citation")
}

// StateCarveout is one state's pay-transparency policy and its exceptions.
type StateCarveout struct {
	State                    string
	SalaryHistoryBan         bool
	PostingEmployerSizeFloor int
	Exceptions               []PayTransparencyException
	Citation                 Citation
	Duty                     PayTransparencyDuty
}

func (s StateCarveout) duty() PayTransparencyDuty {
	if s.Duty.ID != "" {
		return s.Duty
	}
	return PayTransparencyDuty{ID: "pay-transparency-" + s.State, SalaryHistoryBan: s.SalaryHistoryBan, PostingEmployerSizeFloor: s.PostingEmployerSizeFloor, Exceptions: s.Exceptions, Citation: s.Citation}
}

func (s StateCarveout) validate(i int) error {
	prefix := fmt.Sprintf("states[%d]", i)
	if !validState(s.State) {
		return invalid(prefix+".state", "must be a US state code")
	}
	if err := s.duty().validate(prefix + ".duty"); err != nil {
		return err
	}
	return nil
}

// Allows reports whether a pre-trigger exception is available for the duty.
// Size-based exceptions apply when employerSize is below their threshold.
func (s StateCarveout) Allows(appliesTo ExceptionAppliesTo, kind ExceptionKind, employerSize int) bool {
	for _, e := range s.duty().Exceptions {
		if e.AppliesTo != appliesTo || e.ExceptionKind != kind {
			continue
		}
		if kind == EmployerSizeBelowThreshold {
			return employerSize >= 0 && employerSize < e.EmployerSizeThreshold
		}
		return true
	}
	return false
}

// PostingRequired reports whether the state's posting mandate applies to the
// supplied employer size. A zero floor means that this registry row has no
// posting mandate; an all-employer mandate is represented with floor 1.
func (s StateCarveout) PostingRequired(employerSize int) bool {
	floor := s.duty().PostingEmployerSizeFloor
	return floor > 0 && employerSize >= floor
}

// CarveoutRegistry is an immutable, effective-dated set of state carveouts.
type CarveoutRegistry struct {
	SchemaVersion int
	Version       int
	KnownAt       values.KnownAt
	Window        legal.EffectiveWindow
	States        []StateCarveout
	digest        string
}

// Digest returns the canonical sha256 digest of the validated registry.
func (r CarveoutRegistry) Digest() string { return r.digest }

// State returns one state row.
func (r CarveoutRegistry) State(code string) (StateCarveout, bool) {
	for _, s := range r.States {
		if s.State == code {
			return s, true
		}
	}
	return StateCarveout{}, false
}

func (r CarveoutRegistry) canonical() (*canonicalbytes.Writer, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	w := canonicalbytes.New("hcmnext.legal.carveouts", schemaVersion).
		Int("version", int64(r.Version)).
		String("known_at", r.KnownAt.String()).
		String("effective_start", r.Window.Start.String()).
		String("effective_end", r.Window.End.String()).
		Bool("effective_has_end", r.Window.HasEnd)
	rows := append([]StateCarveout(nil), r.States...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].State < rows[j].State })
	for _, s := range rows {
		duty := s.duty()
		n := canonicalbytes.New("hcmnext.legal.carveouts.state", schemaVersion).
			String("state", s.State).String("duty_id", duty.ID).Bool("salary_history_ban", duty.SalaryHistoryBan).
			Int("posting_floor", int64(duty.PostingEmployerSizeFloor)).
			String("source_file", duty.Citation.SourceFile).String("section", duty.Citation.Section).
			String("review", string(duty.Citation.Review))
		ex := append([]PayTransparencyException(nil), duty.Exceptions...)
		sort.Slice(ex, func(i, j int) bool {
			if ex[i].AppliesTo != ex[j].AppliesTo {
				return ex[i].AppliesTo < ex[j].AppliesTo
			}
			return ex[i].ExceptionKind < ex[j].ExceptionKind
		})
		for _, e := range ex {
			n.String("exception.applies_to", string(e.AppliesTo)).String("exception.kind", string(e.ExceptionKind)).
				Int("exception.threshold", int64(e.EmployerSizeThreshold)).String("exception.source_file", e.Citation.SourceFile).
				String("exception.section", e.Citation.Section).String("exception.review", string(e.Citation.Review))
		}
		if err := w.Nested("state", n).Err(); err != nil {
			return nil, err
		}
	}
	return w, nil
}

// CanonicalBytes returns the canonical registry bytes.
func (r CarveoutRegistry) CanonicalBytes() ([]byte, error) {
	w, err := r.canonical()
	if err != nil {
		return nil, err
	}
	return w.Bytes()
}

func (r CarveoutRegistry) validateMetadata() error {
	if r.SchemaVersion != schemaVersion {
		return invalid("schema_version", fmt.Sprintf("must be %d", schemaVersion))
	}
	if r.Version <= 0 {
		return invalid("version", "must be positive")
	}
	if err := r.Window.Validate(); err != nil {
		return invalid("effective", err.Error())
	}
	if err := r.KnownAt.Instant().Validate(); err != nil {
		return invalid("known_at", err.Error())
	}
	return nil
}

// Validate checks metadata, complete state coverage, duplicate states and all
// typed row fields. A registry never silently fills an absent state.
func (r CarveoutRegistry) Validate() error {
	if err := r.validateMetadata(); err != nil {
		return err
	}
	if len(r.States) != len(stateCodes) {
		return fmt.Errorf("%w: got %d rows, want %d", ErrRegistryCoverage, len(r.States), len(stateCodes))
	}
	seen := map[string]bool{}
	for i, s := range r.States {
		if !validState(s.State) {
			return fmt.Errorf("%w: %s", ErrUnknownState, s.State)
		}
		if seen[s.State] {
			return fmt.Errorf("%w: %s", ErrDuplicateState, s.State)
		}
		seen[s.State] = true
		if err := s.validate(i); err != nil {
			return err
		}
	}
	for _, code := range stateCodes {
		if !seen[code] {
			return fmt.Errorf("%w: missing %s", ErrRegistryCoverage, code)
		}
	}
	return nil
}

// SeparationFilingFormat is the typed state UI separation-filing answer. A
// NONE_IDENTIFIED row is a resolved research finding, not an unknown value.
type SeparationFilingFormat struct {
	State              string
	FormName           string
	RecipientAuthority string
	DeadlineDays       int
	DeadlineBasis      string
	ContentFields      []string
	Citation           Citation
}

func (f SeparationFilingFormat) validate(i int) error {
	p := fmt.Sprintf("states[%d]", i)
	if !validState(f.State) {
		return invalid(p+".state", "must be a US state code")
	}
	if strings.TrimSpace(f.FormName) == "" {
		return invalid(p+".form_name", "is required; use NONE_IDENTIFIED when research names no form")
	}
	if strings.TrimSpace(f.RecipientAuthority) == "" {
		return invalid(p+".recipient_authority", "is required")
	}
	if f.DeadlineDays < 0 {
		return invalid(p+".deadline_days", "must not be negative")
	}
	if strings.TrimSpace(f.DeadlineBasis) == "" {
		return invalid(p+".deadline_basis", "is required")
	}
	if err := f.Citation.validate(p + ".citation"); err != nil {
		return err
	}
	return nil
}

// SeparationFilingRegistry is an effective-dated row for every US state.
type SeparationFilingRegistry struct {
	SchemaVersion int
	Version       int
	KnownAt       values.KnownAt
	Window        legal.EffectiveWindow
	States        []SeparationFilingFormat
	digest        string
}

func (r SeparationFilingRegistry) Digest() string { return r.digest }

// State returns a state UI format row.
func (r SeparationFilingRegistry) State(code string) (SeparationFilingFormat, bool) {
	for _, s := range r.States {
		if s.State == code {
			return s, true
		}
	}
	return SeparationFilingFormat{}, false
}

func (r SeparationFilingRegistry) Validate() error {
	if r.SchemaVersion != schemaVersion {
		return invalid("schema_version", fmt.Sprintf("must be %d", schemaVersion))
	}
	if r.Version <= 0 {
		return invalid("version", "must be positive")
	}
	if err := r.Window.Validate(); err != nil {
		return invalid("effective", err.Error())
	}
	if err := r.KnownAt.Instant().Validate(); err != nil {
		return invalid("known_at", err.Error())
	}
	if len(r.States) != len(stateCodes) {
		return fmt.Errorf("%w: got %d rows, want %d", ErrRegistryCoverage, len(r.States), len(stateCodes))
	}
	seen := map[string]bool{}
	for i, s := range r.States {
		if !validState(s.State) {
			return fmt.Errorf("%w: %s", ErrUnknownState, s.State)
		}
		if seen[s.State] {
			return fmt.Errorf("%w: %s", ErrDuplicateState, s.State)
		}
		seen[s.State] = true
		if err := s.validate(i); err != nil {
			return err
		}
	}
	for _, code := range stateCodes {
		if !seen[code] {
			return fmt.Errorf("%w: missing %s", ErrRegistryCoverage, code)
		}
	}
	return nil
}

func (r SeparationFilingRegistry) canonical() (*canonicalbytes.Writer, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	w := canonicalbytes.New("hcmnext.legal.separation-filing-formats", schemaVersion).
		Int("version", int64(r.Version)).String("known_at", r.KnownAt.String()).
		String("effective_start", r.Window.Start.String()).String("effective_end", r.Window.End.String()).Bool("effective_has_end", r.Window.HasEnd)
	rows := append([]SeparationFilingFormat(nil), r.States...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].State < rows[j].State })
	for _, s := range rows {
		n := canonicalbytes.New("hcmnext.legal.separation-filing-format", schemaVersion).
			String("state", s.State).String("form_name", s.FormName).String("recipient_authority", s.RecipientAuthority).
			Int("deadline_days", int64(s.DeadlineDays)).String("deadline_basis", s.DeadlineBasis).
			SortedStrings("content_fields", s.ContentFields).String("source_file", s.Citation.SourceFile).
			String("section", s.Citation.Section).String("review", string(s.Citation.Review))
		if err := w.Nested("state", n).Err(); err != nil {
			return nil, err
		}
	}
	return w, nil
}

func (r SeparationFilingRegistry) CanonicalBytes() ([]byte, error) {
	w, err := r.canonical()
	if err != nil {
		return nil, err
	}
	return w.Bytes()
}

// OverlayPrecedence declares the order used when a locality is layered over
// the state floor.
type OverlayPrecedence string

const StateThenLocality OverlayPrecedence = "STATE_FLOOR_THEN_LOCALITY_OVERRIDE"

// StateLocalityRule records state citation and preemption for local overlays.
type StateLocalityRule struct {
	State           string
	PreemptedKinds  []string
	NoLocalityFound bool
	Citation        Citation
}

// Preempts reports whether the state row explicitly preempts a locality rule
// of the supplied kind.
func (s StateLocalityRule) Preempts(kind string) bool {
	for _, k := range s.PreemptedKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// LocalityOverlay is a city or county override. A preempted row remains
// registered with Preempted=true, so it cannot be mistaken for missing data.
type LocalityOverlay struct {
	State          string
	Locality       string
	Level          string
	Kinds          []string
	MinimumWage    values.Money
	HasMinimumWage bool
	PaidLeaveHours int
	Preempted      bool
	Precedence     OverlayPrecedence
	Citation       Citation
}

func (o LocalityOverlay) validate(i int) error {
	p := fmt.Sprintf("overlays[%d]", i)
	if !validState(o.State) {
		return invalid(p+".state", "must be a US state code")
	}
	if strings.TrimSpace(o.Locality) == "" {
		return invalid(p+".locality", "is required")
	}
	if o.Level != "CITY" && o.Level != "COUNTY" && o.Level != "METRO" {
		return invalid(p+".level", "must be CITY, COUNTY, or METRO")
	}
	if len(o.Kinds) == 0 {
		return invalid(p+".kinds", "must name at least one regulated kind")
	}
	if o.PaidLeaveHours < 0 {
		return invalid(p+".paid_leave_hours", "must not be negative")
	}
	if o.HasMinimumWage && !containsString(o.Kinds, "WAGE_FLOOR") {
		return invalid(p+".kinds", "must include WAGE_FLOOR when minimum_wage is present")
	}
	if o.PaidLeaveHours > 0 && !containsString(o.Kinds, "LEAVE_INTERACTION") {
		return invalid(p+".kinds", "must include LEAVE_INTERACTION when paid_leave_hours is present")
	}
	if o.HasMinimumWage {
		if err := o.MinimumWage.Validate(); err != nil {
			return invalid(p+".minimum_wage", err.Error())
		}
	}
	if o.Precedence != StateThenLocality {
		return invalid(p+".precedence", "must declare STATE_FLOOR_THEN_LOCALITY_OVERRIDE")
	}
	return o.Citation.validate(p + ".citation")
}

// LocalityOverlayRegistry contains one state row plus zero or more explicit
// city/county rows.
type LocalityOverlayRegistry struct {
	SchemaVersion int
	Version       int
	KnownAt       values.KnownAt
	Window        legal.EffectiveWindow
	States        []StateLocalityRule
	Overlays      []LocalityOverlay
	digest        string
}

func (r LocalityOverlayRegistry) Digest() string { return r.digest }

func (r LocalityOverlayRegistry) Validate() error {
	if r.SchemaVersion != schemaVersion {
		return invalid("schema_version", fmt.Sprintf("must be %d", schemaVersion))
	}
	if r.Version <= 0 {
		return invalid("version", "must be positive")
	}
	if err := r.Window.Validate(); err != nil {
		return invalid("effective", err.Error())
	}
	if err := r.KnownAt.Instant().Validate(); err != nil {
		return invalid("known_at", err.Error())
	}
	if len(r.States) != len(stateCodes) {
		return fmt.Errorf("%w: got %d rows, want %d", ErrRegistryCoverage, len(r.States), len(stateCodes))
	}
	seen := map[string]bool{}
	for i, s := range r.States {
		p := fmt.Sprintf("states[%d]", i)
		if !validState(s.State) {
			return invalid(p+".state", "must be a US state code")
		}
		if seen[s.State] {
			return fmt.Errorf("%w: %s", ErrDuplicateState, s.State)
		}
		seen[s.State] = true
		if err := s.Citation.validate(p + ".citation"); err != nil {
			return err
		}
		for j, kind := range s.PreemptedKinds {
			if strings.TrimSpace(kind) == "" {
				return invalid(fmt.Sprintf("%s.preempted_kinds[%d]", p, j), "must not be empty")
			}
		}
	}
	for _, code := range stateCodes {
		if !seen[code] {
			return fmt.Errorf("%w: missing %s", ErrRegistryCoverage, code)
		}
	}
	seenOverlay := map[string]bool{}
	for i, o := range r.Overlays {
		if err := o.validate(i); err != nil {
			return err
		}
		key := o.State + "/" + o.Locality
		if seenOverlay[key] {
			return invalid(fmt.Sprintf("overlays[%d]", i), "duplicate locality row")
		}
		seenOverlay[key] = true
	}
	return nil
}

// State returns one state's locality coverage row.
func (r LocalityOverlayRegistry) State(code string) (StateLocalityRule, bool) {
	for _, s := range r.States {
		if s.State == code {
			return s, true
		}
	}
	return StateLocalityRule{}, false
}

// Overlay returns one registered city/county/metro row.
func (r LocalityOverlayRegistry) Overlay(state, locality string) (LocalityOverlay, bool) {
	for _, o := range r.Overlays {
		if o.State == state && o.Locality == locality {
			return o, true
		}
	}
	return LocalityOverlay{}, false
}

// MinimumWage applies the declared state-floor-then-locality precedence. A
// preempted locality is deliberately ignored only for the preempted kind.
func (r LocalityOverlayRegistry) MinimumWage(state string, locality string, stateFloor values.Money) (values.Money, error) {
	if err := r.Validate(); err != nil {
		return values.Money{}, err
	}
	if err := stateFloor.Validate(); err != nil {
		return values.Money{}, invalid("state_floor", err.Error())
	}
	if stateRow, ok := r.State(state); ok && stateRow.Preempts("WAGE_FLOOR") {
		return stateFloor, nil
	}
	for _, o := range r.Overlays {
		if o.State != state || o.Locality != locality || o.Preempted || !o.HasMinimumWage {
			continue
		}
		if err := o.MinimumWage.Validate(); err != nil {
			return values.Money{}, err
		}
		if o.MinimumWage.Currency() != stateFloor.Currency() {
			return values.Money{}, invalid("overlays.minimum_wage.currency", "must match state_floor currency")
		}
		return o.MinimumWage, nil
	}
	return stateFloor, nil
}

// PaidLeaveHours returns the locality's registered annual overlay hours, or
// zero when no locality overlay is registered for the requested place.
func (r LocalityOverlayRegistry) PaidLeaveHours(state, locality string) (int, bool, error) {
	if err := r.Validate(); err != nil {
		return 0, false, err
	}
	if stateRow, ok := r.State(state); ok && stateRow.Preempts("LEAVE_INTERACTION") {
		return 0, false, nil
	}
	for _, o := range r.Overlays {
		if o.State == state && o.Locality == locality && !o.Preempted {
			return o.PaidLeaveHours, true, nil
		}
	}
	return 0, false, nil
}

// Bundle is the checked-in, three-registry parameter set.
type Bundle struct {
	Carveouts        CarveoutRegistry
	SeparationFiling SeparationFilingRegistry
	LocalityOverlays LocalityOverlayRegistry
}

// Validate validates every registry and checks that all three share their
// version, effective window and knowledge time.
func (b Bundle) Validate() error {
	if err := b.Carveouts.Validate(); err != nil {
		return err
	}
	if err := b.SeparationFiling.Validate(); err != nil {
		return err
	}
	if err := b.LocalityOverlays.Validate(); err != nil {
		return err
	}
	if b.Carveouts.Version != b.SeparationFiling.Version || b.Carveouts.Version != b.LocalityOverlays.Version {
		return invalid("version", "all registries must share version")
	}
	if b.Carveouts.KnownAt.String() != b.SeparationFiling.KnownAt.String() || b.Carveouts.KnownAt.String() != b.LocalityOverlays.KnownAt.String() {
		return invalid("known_at", "all registries must share known_at")
	}
	return nil
}

// Explain returns a deterministic audit summary without claiming legal
// advice or performing any filing.
func (b Bundle) Explain() string {
	return fmt.Sprintf("legal carveouts schema=%d version=%d states=%d separation_rows=%d locality_overlays=%d digests=%s,%s,%s known_at=%s; reference-only no transmission",
		schemaVersion, b.Carveouts.Version, len(b.Carveouts.States), len(b.SeparationFiling.States), len(b.LocalityOverlays.Overlays),
		b.Carveouts.Digest(), b.SeparationFiling.Digest(), b.LocalityOverlays.Digest(), b.Carveouts.KnownAt.String())
}

// Explain describes the package contract.
func Explain() string {
	return "legal carveouts v1: effective-dated, known-at, digested state registries with explicit review flags; reference-only"
}

//go:embed testdata/carveouts.yaml
var embeddedFixture embed.FS

// LoadEmbedded loads the checked-in YAML fixture.
func LoadEmbedded() (Bundle, error) {
	b, err := embeddedFixture.ReadFile("testdata/carveouts.yaml")
	if err != nil {
		return Bundle{}, err
	}
	return Load(b)
}

// LoadFile loads a YAML fixture from an explicitly supplied path.
func LoadFile(path string) (Bundle, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Bundle{}, err
	}
	return Load(b)
}

type fixture struct {
	SchemaVersion int              `json:"schema_version"`
	Version       int              `json:"version"`
	KnownAt       string           `json:"known_at"`
	Effective     fixtureEffective `json:"effective"`
	States        []fixtureState   `json:"states"`
	Overlays      []fixtureOverlay `json:"locality_overlays"`
}
type fixtureEffective struct {
	Start string `json:"start"`
	End   string `json:"end"`
}
type fixtureState struct {
	Code             string             `json:"code"`
	Citation         string             `json:"citation"`
	Review           ReviewFlag         `json:"review"`
	SalaryHistoryBan bool               `json:"salary_history_ban"`
	PostingFloor     int                `json:"posting_employer_size_floor"`
	Exceptions       []fixtureException `json:"exceptions"`
	Separation       fixtureSeparation  `json:"separation"`
	PreemptedKinds   []string           `json:"preempted_kinds"`
	NoLocalityFound  bool               `json:"no_locality_found"`
}
type fixtureException struct {
	AppliesTo ExceptionAppliesTo `json:"applies_to"`
	Kind      ExceptionKind      `json:"exception_kind"`
	Threshold int                `json:"employer_size_threshold"`
	Citation  string             `json:"citation"`
}
type fixtureSeparation struct {
	Form         string     `json:"form_name"`
	Authority    string     `json:"recipient_authority"`
	DeadlineDays int        `json:"deadline_days"`
	Basis        string     `json:"deadline_basis"`
	Content      []string   `json:"content_fields"`
	Review       ReviewFlag `json:"review"`
}
type fixtureOverlay struct {
	State          string   `json:"state"`
	Locality       string   `json:"locality"`
	Level          string   `json:"level"`
	Kinds          []string `json:"kinds"`
	MinimumWage    string   `json:"minimum_wage"`
	PaidLeaveHours int      `json:"paid_leave_hours"`
	Preempted      bool     `json:"preempted"`
	Citation       string   `json:"citation"`
}

// Load parses, validates and seals the YAML parameter set.
func Load(data []byte) (Bundle, error) {
	f, err := parseFixtureYAML(data)
	if err != nil {
		return Bundle{}, fmt.Errorf("%w: yaml: %v", ErrValidation, err)
	}
	knownInstant := values.Instant{}
	if err := knownInstant.UnmarshalText([]byte(f.KnownAt)); err != nil {
		return Bundle{}, invalid("known_at", err.Error())
	}
	known, err := values.NewKnownAt(knownInstant)
	if err != nil {
		return Bundle{}, invalid("known_at", err.Error())
	}
	start, err := values.ParseLocalDate(f.Effective.Start)
	if err != nil {
		return Bundle{}, invalid("effective.start", err.Error())
	}
	window, err := legal.NewOpenEffectiveWindow(start)
	if err != nil {
		return Bundle{}, invalid("effective.start", err.Error())
	}
	if f.Effective.End != "" {
		end, e := values.ParseLocalDate(f.Effective.End)
		if e != nil {
			return Bundle{}, invalid("effective.end", e.Error())
		}
		window, err = legal.NewClosedEffectiveWindow(start, end)
		if err != nil {
			return Bundle{}, invalid("effective", err.Error())
		}
	}

	carve := CarveoutRegistry{SchemaVersion: f.SchemaVersion, Version: f.Version, KnownAt: known, Window: window}
	sep := SeparationFilingRegistry{SchemaVersion: f.SchemaVersion, Version: f.Version, KnownAt: known, Window: window}
	local := LocalityOverlayRegistry{SchemaVersion: f.SchemaVersion, Version: f.Version, KnownAt: known, Window: window}
	for i, raw := range f.States {
		if !validState(raw.Code) {
			return Bundle{}, invalid(fmt.Sprintf("states[%d].code", i), "must be a US state code")
		}
		file := researchFile(raw.Code)
		cit := Citation{SourceFile: file, Section: raw.Citation, Review: raw.Review}
		var ex []PayTransparencyException
		for j, e := range raw.Exceptions {
			ex = append(ex, PayTransparencyException{AppliesTo: e.AppliesTo, ExceptionKind: e.Kind, EmployerSizeThreshold: e.Threshold, Citation: Citation{SourceFile: file, Section: e.Citation, Review: raw.Review}})
			if e.Citation == "" {
				return Bundle{}, invalid(fmt.Sprintf("states[%d].exceptions[%d].citation", i, j), "is required")
			}
		}
		duty := PayTransparencyDuty{ID: "pay-transparency-" + raw.Code, SalaryHistoryBan: raw.SalaryHistoryBan, PostingEmployerSizeFloor: raw.PostingFloor, Exceptions: ex, Citation: cit}
		carve.States = append(carve.States, StateCarveout{State: raw.Code, SalaryHistoryBan: raw.SalaryHistoryBan, PostingEmployerSizeFloor: raw.PostingFloor, Exceptions: ex, Citation: cit, Duty: duty})
		form := raw.Separation.Form
		if form == "" {
			form = "NONE_IDENTIFIED"
		}
		review := raw.Separation.Review
		if review == "" {
			review = ReviewUnreviewed
		}
		sep.States = append(sep.States, SeparationFilingFormat{State: raw.Code, FormName: form, RecipientAuthority: raw.Separation.Authority, DeadlineDays: raw.Separation.DeadlineDays, DeadlineBasis: raw.Separation.Basis, ContentFields: raw.Separation.Content, Citation: Citation{SourceFile: file, Section: raw.Citation, Review: review}})
		local.States = append(local.States, StateLocalityRule{State: raw.Code, PreemptedKinds: raw.PreemptedKinds, NoLocalityFound: raw.NoLocalityFound, Citation: cit})
	}
	for i, raw := range f.Overlays {
		o := LocalityOverlay{State: raw.State, Locality: raw.Locality, Level: raw.Level, Kinds: raw.Kinds, PaidLeaveHours: raw.PaidLeaveHours, Preempted: raw.Preempted, Precedence: StateThenLocality, Citation: Citation{SourceFile: researchFile(raw.State), Section: raw.Citation, Review: ReviewReviewed}}
		if raw.MinimumWage != "" {
			m, e := parseMoney(raw.MinimumWage)
			if e != nil {
				return Bundle{}, invalid(fmt.Sprintf("locality_overlays[%d].minimum_wage", i), e.Error())
			}
			o.MinimumWage, o.HasMinimumWage = m, true
		}
		local.Overlays = append(local.Overlays, o)
	}
	b := Bundle{Carveouts: carve, SeparationFiling: sep, LocalityOverlays: local}
	if err := b.Validate(); err != nil {
		return Bundle{}, err
	}
	for _, r := range []*CarveoutRegistry{&b.Carveouts} {
		raw, e := r.CanonicalBytes()
		if e != nil {
			return Bundle{}, e
		}
		r.digest = canonicalbytes.Digest(raw)
	}
	if raw, e := b.SeparationFiling.CanonicalBytes(); e != nil {
		return Bundle{}, e
	} else {
		b.SeparationFiling.digest = canonicalbytes.Digest(raw)
	}
	if raw, e := localityCanonicalBytes(b.LocalityOverlays); e != nil {
		return Bundle{}, e
	} else {
		b.LocalityOverlays.digest = canonicalbytes.Digest(raw)
	}
	return b, nil
}

// parseFixtureYAML reads the deliberately bounded YAML dialect used by the
// checked-in reference fixture. It accepts block section headers and YAML
// flow maps/lists, rejects unknown structure, and has no implicit defaults.
// Keeping this parser local is intentional: governance packages are not
// allowed to import semantic third-party libraries merely to read reference
// data. The accepted fixture is ordinary YAML and its flow values are parsed
// without evaluation or code execution.
func parseFixtureYAML(data []byte) (fixture, error) {
	var out fixture
	section := ""
	for lineNumber, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "effective:" || line == "states:" || line == "locality_overlays:" {
			section = strings.TrimSuffix(line, ":")
			continue
		}
		if strings.HasPrefix(line, "- ") {
			value, err := parseFlowObject(strings.TrimSpace(strings.TrimPrefix(line, "- ")))
			if err != nil {
				return fixture{}, fmt.Errorf("line %d: %v", lineNumber+1, err)
			}
			if section == "states" {
				var raw fixtureState
				if err := decodeObject(value, &raw); err != nil {
					return fixture{}, fmt.Errorf("line %d: %v", lineNumber+1, err)
				}
				out.States = append(out.States, raw)
			} else if section == "locality_overlays" {
				var raw fixtureOverlay
				if err := decodeObject(value, &raw); err != nil {
					return fixture{}, fmt.Errorf("line %d: %v", lineNumber+1, err)
				}
				out.Overlays = append(out.Overlays, raw)
			} else {
				return fixture{}, fmt.Errorf("line %d: list item outside a registry section", lineNumber+1)
			}
			continue
		}
		key, rawValue, ok := splitFlowPair(line)
		if !ok {
			return fixture{}, fmt.Errorf("line %d: expected key: value", lineNumber+1)
		}
		value, err := parseFlowValue(rawValue)
		if err != nil {
			return fixture{}, fmt.Errorf("line %d: %v", lineNumber+1, err)
		}
		switch {
		case section == "" && key == "schema_version":
			out.SchemaVersion, err = asInt(value)
		case section == "" && key == "version":
			out.Version, err = asInt(value)
		case section == "" && key == "known_at":
			out.KnownAt, err = asString(value)
		case section == "effective" && key == "start":
			out.Effective.Start, err = asString(value)
		case section == "effective" && key == "end":
			out.Effective.End, err = asString(value)
		default:
			return fixture{}, fmt.Errorf("line %d: unknown or misplaced key %q", lineNumber+1, key)
		}
		if err != nil {
			return fixture{}, fmt.Errorf("line %d field %s: %v", lineNumber+1, key, err)
		}
	}
	return out, nil
}

func decodeObject(value any, out any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	return d.Decode(out)
}

type flowParser struct {
	text string
	pos  int
}

func parseFlowObject(text string) (map[string]any, error) {
	p := &flowParser{text: strings.TrimSpace(text)}
	v, err := p.value()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.text) {
		return nil, fmt.Errorf("unexpected YAML after flow object")
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("registry item must be a flow map")
	}
	return obj, nil
}

func parseFlowValue(text string) (any, error) {
	p := &flowParser{text: strings.TrimSpace(text)}
	v, err := p.value()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.text) {
		return nil, fmt.Errorf("unexpected YAML value %q", text)
	}
	return v, nil
}

func (p *flowParser) skip() {
	for p.pos < len(p.text) && (p.text[p.pos] == ' ' || p.text[p.pos] == '\t') {
		p.pos++
	}
}

func (p *flowParser) value() (any, error) {
	p.skip()
	if p.pos >= len(p.text) {
		return nil, fmt.Errorf("missing YAML value")
	}
	switch p.text[p.pos] {
	case '{':
		return p.object()
	case '[':
		return p.array()
	case '"':
		return p.quoted()
	default:
		return p.scalar()
	}
}

func (p *flowParser) object() (map[string]any, error) {
	p.pos++
	out := map[string]any{}
	p.skip()
	if p.pos < len(p.text) && p.text[p.pos] == '}' {
		p.pos++
		return out, nil
	}
	for {
		p.skip()
		key, err := p.token(':')
		if err != nil {
			return nil, err
		}
		p.skip()
		if p.pos >= len(p.text) || p.text[p.pos] != ':' {
			return nil, fmt.Errorf("missing colon after key %q", key)
		}
		p.pos++
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		out[key] = v
		p.skip()
		if p.pos >= len(p.text) {
			return nil, fmt.Errorf("unterminated YAML object")
		}
		if p.text[p.pos] == '}' {
			p.pos++
			return out, nil
		}
		if p.text[p.pos] != ',' {
			return nil, fmt.Errorf("expected comma in YAML object")
		}
		p.pos++
	}
}

func (p *flowParser) array() ([]any, error) {
	p.pos++
	var out []any
	p.skip()
	if p.pos < len(p.text) && p.text[p.pos] == ']' {
		p.pos++
		return out, nil
	}
	for {
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		p.skip()
		if p.pos >= len(p.text) {
			return nil, fmt.Errorf("unterminated YAML array")
		}
		if p.text[p.pos] == ']' {
			p.pos++
			return out, nil
		}
		if p.text[p.pos] != ',' {
			return nil, fmt.Errorf("expected comma in YAML array")
		}
		p.pos++
	}
}

func (p *flowParser) quoted() (string, error) {
	start := p.pos
	p.pos++
	for p.pos < len(p.text) {
		if p.text[p.pos] == '\\' {
			p.pos += 2
			continue
		}
		if p.text[p.pos] == '"' {
			p.pos++
			return strconv.Unquote(p.text[start:p.pos])
		}
		p.pos++
	}
	return "", fmt.Errorf("unterminated quoted YAML string")
}

func (p *flowParser) scalar() (any, error) {
	start := p.pos
	for p.pos < len(p.text) && p.text[p.pos] != ',' && p.text[p.pos] != ']' && p.text[p.pos] != '}' {
		p.pos++
	}
	text := strings.TrimSpace(p.text[start:p.pos])
	if text == "" {
		return nil, fmt.Errorf("empty YAML scalar")
	}
	if text == "true" {
		return true, nil
	}
	if text == "false" {
		return false, nil
	}
	if n, err := strconv.Atoi(text); err == nil {
		return n, nil
	}
	return text, nil
}

func (p *flowParser) token(stop byte) (string, error) {
	start := p.pos
	for p.pos < len(p.text) && p.text[p.pos] != stop && p.text[p.pos] != ',' && p.text[p.pos] != '}' {
		p.pos++
	}
	token := strings.TrimSpace(p.text[start:p.pos])
	if token == "" {
		return "", fmt.Errorf("empty YAML key")
	}
	return token, nil
}

func splitFlowPair(line string) (string, string, bool) {
	i := strings.IndexByte(line, ':')
	if i <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
}

func asString(value any) (string, error) {
	s, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("want string")
	}
	return s, nil
}

func asInt(value any) (int, error) {
	n, ok := value.(int)
	if !ok {
		return 0, fmt.Errorf("want integer")
	}
	return n, nil
}

func localityCanonicalBytes(r LocalityOverlayRegistry) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	w := canonicalbytes.New("hcmnext.legal.locality-overlays", schemaVersion).Int("version", int64(r.Version)).String("known_at", r.KnownAt.String()).String("effective_start", r.Window.Start.String()).String("effective_end", r.Window.End.String()).Bool("effective_has_end", r.Window.HasEnd)
	stateRows := append([]StateLocalityRule(nil), r.States...)
	sort.Slice(stateRows, func(i, j int) bool { return stateRows[i].State < stateRows[j].State })
	for _, s := range stateRows {
		n := canonicalbytes.New("hcmnext.legal.locality-state", schemaVersion).String("state", s.State).SortedStrings("preempted_kinds", s.PreemptedKinds).Bool("no_locality_found", s.NoLocalityFound).String("source_file", s.Citation.SourceFile).String("section", s.Citation.Section).String("review", string(s.Citation.Review))
		if err := w.Nested("state", n).Err(); err != nil {
			return nil, err
		}
	}
	rows := append([]LocalityOverlay(nil), r.Overlays...)
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].State != rows[j].State {
			return rows[i].State < rows[j].State
		}
		return rows[i].Locality < rows[j].Locality
	})
	for _, o := range rows {
		n := canonicalbytes.New("hcmnext.legal.locality-overlay", schemaVersion).String("state", o.State).String("locality", o.Locality).String("level", o.Level).SortedStrings("kinds", o.Kinds).String("minimum_wage", o.MinimumWage.String()).Bool("has_minimum_wage", o.HasMinimumWage).Int("paid_leave_hours", int64(o.PaidLeaveHours)).Bool("preempted", o.Preempted).String("precedence", string(o.Precedence)).String("source_file", o.Citation.SourceFile).String("section", o.Citation.Section).String("review", string(o.Citation.Review))
		if err := w.Nested("overlay", n).Err(); err != nil {
			return nil, err
		}
	}
	return w.Bytes()
}

func parseMoney(text string) (values.Money, error) {
	parts := strings.Fields(text)
	if len(parts) != 2 {
		return values.Money{}, fmt.Errorf("must be '<amount> <currency>'")
	}
	return values.NewMoney(parts[0], parts[1], 2, values.RoundingHalfEven)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func (r LocalityOverlayRegistry) CanonicalBytes() ([]byte, error) { return localityCanonicalBytes(r) }

func validState(code string) bool {
	for _, s := range stateCodes {
		if s == code {
			return true
		}
	}
	return false
}

func researchFile(code string) string {
	names := map[string]string{"AL": "alabama", "AK": "alaska", "AZ": "arizona", "AR": "arkansas", "CA": "california", "CO": "colorado", "CT": "connecticut", "DC": "district-of-columbia", "DE": "delaware", "FL": "florida", "GA": "georgia", "HI": "hawaii", "ID": "idaho", "IL": "illinois", "IN": "indiana", "IA": "iowa", "KS": "kansas", "KY": "kentucky", "LA": "louisiana", "ME": "maine", "MD": "maryland", "MA": "massachusetts", "MI": "michigan", "MN": "minnesota", "MS": "mississippi", "MO": "missouri", "MT": "montana", "NE": "nebraska", "NV": "nevada", "NH": "new-hampshire", "NJ": "new-jersey", "NM": "new-mexico", "NY": "new-york", "NC": "north-carolina", "ND": "north-dakota", "OH": "ohio", "OK": "oklahoma", "OR": "oregon", "PA": "pennsylvania", "RI": "rhode-island", "SC": "south-carolina", "SD": "south-dakota", "TN": "tennessee", "TX": "texas", "UT": "utah", "VT": "vermont", "VA": "virginia", "WA": "washington", "WV": "west-virginia", "WI": "wisconsin", "WY": "wyoming"}
	return "planning/research/state-employment-law/" + names[code] + ".md"
}

var stateCodes = []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DC", "DE", "FL", "GA", "HI", "ID", "IL", "IN", "IA", "KS", "KY", "LA", "ME", "MD", "MA", "MI", "MN", "MS", "MO", "MT", "NE", "NV", "NH", "NJ", "NM", "NY", "NC", "ND", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VT", "VA", "WA", "WV", "WI", "WY"}
