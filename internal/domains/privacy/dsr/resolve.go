package dsr

import (
	"errors"
	"fmt"
	"slices"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// This file implements planning/todos.md PRIV-006: resolve every item of a
// verified, advanceable data-subject request against the subject's known
// copy inventory, honoring legal holds (RECORDS-HOLD-001) and legal
// exceptions, and returning one typed outcome per item with its authority,
// redaction reason, completeness digest and appeal route.
//
// Resolve is a pure function over the presented inputs: it performs no I/O
// and trusts no caller-supplied completeness claim further than it can
// check. A copy class outside the closed [CopyClass] vocabulary, a declared
// complete class with no presented copy, an exception without authority, an
// empty inventory, or a request that cannot advance all fail closed with
// [ErrResolutionBlocked] or [ErrResolutionRefused] -- never with a partial
// certificate that looks complete.

// ErrResolutionRefused is returned by [Resolve] when the request itself may
// not be fulfilled: it is invalid, unverified, duplicate-linked, or verified
// below its kind's assurance floor. The wrapped [AdvanceCode] names which.
var ErrResolutionRefused = errors.New("dsr: request may not be resolved")

// ErrResolutionBlocked is returned by [Resolve] when the presented inventory
// or exception set cannot prove a complete resolution: unknown copy class,
// omitted declared-complete class, authority-less exception, empty or
// inconsistent inventory, or a cross-tenant/cross-subject copy.
var ErrResolutionBlocked = errors.New("dsr: resolution blocked: completeness unprovable")

const resolutionEvidencePrefix = "ev:privacy:dsr:resolution:"

// CopyClass is the closed vocabulary of copy surfaces a DSR resolution must
// cover. It names the ten classes PRIV-006's RED clause enumerates
// (projection, search, vector, cache, telemetry, export, backup, provider,
// privileged, retained) plus AUTHORITATIVE, the canonical system-of-record
// copy -- resolving an erasure without directing the canonical record would
// certify a deletion that never happened. Anything else (a novel store, a
// mistyped token) fails closed: [Resolve] refuses it rather than silently
// dropping a copy it does not understand.
type CopyClass string

// The closed copy-class vocabulary.
const (
	CopyAuthoritative CopyClass = "AUTHORITATIVE"
	CopyProjection    CopyClass = "PROJECTION"
	CopySearch        CopyClass = "SEARCH"
	CopyVector        CopyClass = "VECTOR"
	CopyCache         CopyClass = "CACHE"
	CopyTelemetry     CopyClass = "TELEMETRY"
	CopyExport        CopyClass = "EXPORT"
	CopyBackup        CopyClass = "BACKUP"
	CopyProvider      CopyClass = "PROVIDER"
	CopyPrivileged    CopyClass = "PRIVILEGED"
	CopyRetained      CopyClass = "RETAINED"
)

// allCopyClasses is the authoritative enumeration every completeness check
// walks.
var allCopyClasses = []CopyClass{
	CopyAuthoritative,
	CopyProjection,
	CopySearch,
	CopyVector,
	CopyCache,
	CopyTelemetry,
	CopyExport,
	CopyBackup,
	CopyProvider,
	CopyPrivileged,
	CopyRetained,
}

// AllCopyClasses returns a fresh copy of the closed copy-class vocabulary.
func AllCopyClasses() []CopyClass { return slices.Clone(allCopyClasses) }

// Validate reports whether c is a declared copy class.
func (c CopyClass) Validate() error {
	if slices.Contains(allCopyClasses, c) {
		return nil
	}
	return fmt.Errorf("%w: unknown copy class %q", ErrResolutionBlocked, string(c))
}

// String returns the wire spelling of c.
func (c CopyClass) String() string { return string(c) }

// DeletionCapability mirrors RECORDS-COPY-001's deletion_capability enum so
// an erasure resolution directs each copy at what that copy can actually
// do. NONE means the copy is immutable by design (a WORM archive, a
// sealed backup); erasing it is impossible, so it resolves to RETAIN under
// a named retention authority rather than to a FULFILL that never happens.
type DeletionCapability string

// The closed deletion-capability vocabulary.
const (
	DeletionDelete    DeletionCapability = "DELETE"
	DeletionAnonymize DeletionCapability = "ANONYMIZE"
	DeletionTombstone DeletionCapability = "TOMBSTONE"
	DeletionNone      DeletionCapability = "NONE"
)

func (d DeletionCapability) valid() bool {
	switch d {
	case DeletionDelete, DeletionAnonymize, DeletionTombstone, DeletionNone:
		return true
	default:
		return false
	}
}

// ItemOutcome is the closed vocabulary of per-item resolution outcomes
// PRIV-006's GREEN clause names.
type ItemOutcome string

// The closed item-outcome vocabulary.
const (
	OutcomeFulfill   ItemOutcome = "FULFILL"
	OutcomePartial   ItemOutcome = "PARTIAL"
	OutcomeDeny      ItemOutcome = "DENY"
	OutcomeRestrict  ItemOutcome = "RESTRICT"
	OutcomeRetain    ItemOutcome = "RETAIN"
	OutcomeAnonymize ItemOutcome = "ANONYMIZE"
)

// Validate reports whether o is a declared outcome.
func (o ItemOutcome) Validate() error {
	switch o {
	case OutcomeFulfill, OutcomePartial, OutcomeDeny, OutcomeRestrict, OutcomeRetain, OutcomeAnonymize:
		return nil
	default:
		return fmt.Errorf("dsr: item outcome %q is not a declared value", string(o))
	}
}

// String returns the wire spelling of o.
func (o ItemOutcome) String() string { return string(o) }

// RedactionReason is the closed vocabulary of why an item's payload was
// reduced. NONE means the item is acted on whole.
type RedactionReason string

// The closed redaction-reason vocabulary.
const (
	RedactionNone             RedactionReason = "NONE"
	RedactionThirdParty       RedactionReason = "THIRD_PARTY"
	RedactionPrivilege        RedactionReason = "PRIVILEGE"
	RedactionStatutorySecrecy RedactionReason = "STATUTORY_SECRECY"
	RedactionSafety           RedactionReason = "SAFETY"
)

func (r RedactionReason) valid() bool {
	switch r {
	case RedactionNone, RedactionThirdParty, RedactionPrivilege, RedactionStatutorySecrecy, RedactionSafety:
		return true
	default:
		return false
	}
}

// AppealRoute is the closed vocabulary of where the subject can contest an
// item's outcome. FULFILL and ANONYMIZE carry APPEAL_NONE: there is nothing
// left to contest, and the record says so explicitly rather than leaving
// the route blank. Denials and retentions -- the outcomes that withhold
// the subject's right -- route to the supervisory authority; partial
// fulfillment and restriction route to the DPO first.
type AppealRoute string

// The closed appeal-route vocabulary.
const (
	AppealNone                 AppealRoute = "APPEAL_NONE"
	AppealDPO                  AppealRoute = "APPEAL_DPO"
	AppealSupervisoryAuthority AppealRoute = "APPEAL_SUPERVISORY_AUTHORITY"
)

// appealFor derives the appeal route from the outcome alone, so two items
// with the same outcome can never route differently.
func appealFor(o ItemOutcome) AppealRoute {
	switch o {
	case OutcomeDeny, OutcomeRetain:
		return AppealSupervisoryAuthority
	case OutcomePartial, OutcomeRestrict:
		return AppealDPO
	default:
		return AppealNone
	}
}

// ExceptionBasis is the closed vocabulary of legal grounds an exception may
// cite. Only STATUTORY_SECRECY, LITIGATION_PRIVILEGE and PUBLIC_SAFETY may
// redact access; TAX_RETENTION preserves records but never redacts them --
// a tax-retention exception claiming RedactsAccess is contradictory and
// [Resolve] refuses it outright.
type ExceptionBasis string

// The closed exception-basis vocabulary.
const (
	ExceptionTaxRetention     ExceptionBasis = "TAX_RETENTION"
	ExceptionLitigation       ExceptionBasis = "LITIGATION_PRIVILEGE"
	ExceptionStatutorySecrecy ExceptionBasis = "STATUTORY_SECRECY"
	ExceptionPublicSafety     ExceptionBasis = "PUBLIC_SAFETY"
)

func (b ExceptionBasis) valid() bool {
	switch b {
	case ExceptionTaxRetention, ExceptionLitigation, ExceptionStatutorySecrecy, ExceptionPublicSafety:
		return true
	default:
		return false
	}
}

// exceptionRedaction maps a redacting basis to its redaction reason.
func (b ExceptionBasis) exceptionRedaction() RedactionReason {
	switch b {
	case ExceptionStatutorySecrecy:
		return RedactionStatutorySecrecy
	case ExceptionLitigation:
		return RedactionPrivilege
	case ExceptionPublicSafety:
		return RedactionSafety
	default:
		return RedactionNone
	}
}

// CopyDescriptor is one known copy about the request's subject, as certified
// by the RECORDS-COPY-001 inventory. Hold fields mirror the
// RECORDS-HOLD-001 intersection: Held means an active hold grips this copy
// and HoldAuthority is the hold's reference, which becomes the RETAIN
// item's authority verbatim.
type CopyDescriptor struct {
	CopyID     string          `json:"copy_id"`
	Class      CopyClass       `json:"class"`
	Tenant     values.TenantId `json:"tenant"`
	SubjectKey string          `json:"subject_key"`
	Held       bool            `json:"held"`
	// HoldAuthority is the RECORDS-HOLD-001 hold reference. Required when
	// Held: a hold that cannot name itself cannot authorize a RETAIN.
	HoldAuthority string             `json:"hold_authority,omitempty"`
	Capability    DeletionCapability `json:"capability"`
	// RetentionAuthority names the schedule or authority that blocks
	// destruction of an immutable (NONE-capability) copy. Required when an
	// erasure meets Capability NONE: deletion stays blocked while a
	// minimum applies, and the blocking authority is named in the
	// response -- never silently retained.
	RetentionAuthority string `json:"retention_authority,omitempty"`
	// ErasureFulfilled marks a copy whose deletion an earlier resolution
	// already fulfilled and which has reappeared via restore (PRIV-004):
	// an erasure re-resolves it to FULFILL (re-delete), never to a
	// by-default RETAIN that would resurrect deleted data.
	ErasureFulfilled bool `json:"erasure_fulfilled,omitempty"`
}

// LegalException is one legal ground withholding or reducing fulfillment
// for the named copies. Authority is mandatory: PRIV-006's RED clause
// makes an exception with no authority a blocking defect, not a silent
// pass-through.
type LegalException struct {
	CopyIDs []string `json:"copy_ids"`
	// Authority is the citable ground (a statute section, a matter
	// reference, a supervisory-authority order). Empty blocks resolution.
	Authority string         `json:"authority"`
	Basis     ExceptionBasis `json:"basis"`
	// RedactsAccess additionally reduces ACCESS/PORTABILITY fulfillment
	// to PARTIAL for the named copies. It is refused with TAX_RETENTION,
	// which preserves records without redacting them.
	RedactsAccess bool `json:"redacts_access,omitempty"`
}

// ItemResolution is the decided outcome for one copy.
type ItemResolution struct {
	CopyID          string          `json:"copy_id"`
	Class           CopyClass       `json:"class"`
	Outcome         ItemOutcome     `json:"outcome"`
	Authority       string          `json:"authority"`
	RedactionReason RedactionReason `json:"redaction_reason"`
	AppealRoute     AppealRoute     `json:"appeal_route"`
	// Detail is a stable reason token disambiguating how the outcome was
	// reached (e.g. TOMBSTONE_DIRECTED vs DELETE_DIRECTED). Empty means
	// the outcome needs no further disambiguation.
	Detail string `json:"detail,omitempty"`
}

// Resolution is the complete, digest-bound certificate for one request:
// every presented copy resolved, every exception applied, nothing omitted.
// Items are stored sorted by CopyID so independently-built resolutions
// over the same inputs digest identically.
type Resolution struct {
	RequestID     string           `json:"request_id"`
	RequestDigest string           `json:"request_digest"`
	Kind          Kind             `json:"kind"`
	Tenant        values.TenantId  `json:"tenant"`
	ResolvedAt    values.Instant   `json:"resolved_at"`
	Items         []ItemResolution `json:"items"`
	Exceptions    []LegalException `json:"exceptions,omitempty"`
	// EvidenceID is derived from this certificate's own [Resolution.Digest].
	EvidenceID string `json:"evidence_id"`
}

// ResolutionSpec is everything [Resolve] needs. CompleteClasses is the set
// of copy classes the caller certifies as complete for this subject from
// the RECORDS-COPY-001 inventory: every one of them must have at least one
// presented copy, or the subject's VECTOR (or whichever) copy has been
// omitted and the resolution blocks instead of certifying a false
// completeness.
type ResolutionSpec struct {
	Request         DataSubjectRequest
	Copies          []CopyDescriptor
	CompleteClasses []CopyClass
	Exceptions      []LegalException
	At              values.Instant
}

// canonicalBytes appends r's deterministic encoding: request binding, kind,
// tenant, instant, item count, every item sorted by copy id, then every
// exception with sorted copy ids. Exceptions are part of the digest so a
// certificate cannot be separated from the grounds that shaped it.
func (r Resolution) canonicalBytes() []byte {
	sec, nsec := r.ResolvedAt.Unix()
	dst := appendFields(nil,
		"request_id", r.RequestID,
		"request_digest", r.RequestDigest,
		"kind", string(r.Kind),
		"tenant", r.Tenant.String(),
		"resolved_at_sec", itoa(sec),
		"resolved_at_nsec", itoa(int64(nsec)),
		"item_count", itoa(int64(len(r.Items))),
	)
	items := slices.Clone(r.Items)
	slices.SortFunc(items, func(a, b ItemResolution) int {
		if a.CopyID != b.CopyID {
			return compareString(a.CopyID, b.CopyID)
		}
		return 0
	})
	for _, item := range items {
		dst = appendFields(dst,
			"copy_id", item.CopyID,
			"class", string(item.Class),
			"outcome", string(item.Outcome),
			"authority", item.Authority,
			"redaction_reason", string(item.RedactionReason),
			"appeal_route", string(item.AppealRoute),
			"detail", item.Detail,
		)
	}
	exceptions := slices.Clone(r.Exceptions)
	slices.SortFunc(exceptions, func(a, b LegalException) int {
		return compareString(exceptionKey(a), exceptionKey(b))
	})
	for _, e := range exceptions {
		dst = appendFields(dst,
			"exception_copies", joinStrings(sortedStrings(e.CopyIDs), ","),
			"exception_authority", e.Authority,
			"exception_basis", string(e.Basis),
			"exception_redacts", boolString(e.RedactsAccess),
		)
	}
	return dst
}

// Digest is the canonical content digest of this exact certificate.
func (r Resolution) Digest() string { return digestHex(r.canonicalBytes()) }

// Validate reports whether r is internally consistent: every required
// field present, every token from its closed vocabulary, items unique and
// sorted by copy id, appeal routes matching their outcomes, and EvidenceID
// matching the certificate's own digest.
func (r Resolution) Validate() error {
	if r.RequestID == "" {
		return fmt.Errorf("%w: no request id", ErrResolutionBlocked)
	}
	if r.RequestDigest == "" {
		return fmt.Errorf("%w: no request digest", ErrResolutionBlocked)
	}
	if err := r.Kind.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrResolutionBlocked, err)
	}
	if err := r.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrResolutionBlocked, err)
	}
	if !r.ResolvedAt.IsSet() {
		return fmt.Errorf("%w: no resolved_at", ErrResolutionBlocked)
	}
	if len(r.Items) == 0 {
		return fmt.Errorf("%w: no resolved items", ErrResolutionBlocked)
	}
	seen := make(map[string]struct{}, len(r.Items))
	for i, item := range r.Items {
		if item.CopyID == "" {
			return fmt.Errorf("%w: item %d has no copy id", ErrResolutionBlocked, i)
		}
		if _, dup := seen[item.CopyID]; dup {
			return fmt.Errorf("%w: duplicate item for copy %q", ErrResolutionBlocked, item.CopyID)
		}
		seen[item.CopyID] = struct{}{}
		if i > 0 && compareString(r.Items[i-1].CopyID, item.CopyID) >= 0 {
			return fmt.Errorf("%w: items are not sorted by copy id", ErrResolutionBlocked)
		}
		if err := item.Class.Validate(); err != nil {
			return fmt.Errorf("%w: item %q: %v", ErrResolutionBlocked, item.CopyID, err)
		}
		if err := item.Outcome.Validate(); err != nil {
			return fmt.Errorf("%w: item %q: %v", ErrResolutionBlocked, item.CopyID, err)
		}
		if !item.RedactionReason.valid() {
			return fmt.Errorf("%w: item %q has an undeclared redaction reason %q", ErrResolutionBlocked, item.CopyID, string(item.RedactionReason))
		}
		if item.Authority == "" {
			return fmt.Errorf("%w: item %q has no authority", ErrResolutionBlocked, item.CopyID)
		}
		if item.AppealRoute != appealFor(item.Outcome) {
			return fmt.Errorf("%w: item %q appeal route %s does not match outcome %s", ErrResolutionBlocked, item.CopyID, item.AppealRoute, item.Outcome)
		}
	}
	if r.EvidenceID == "" {
		return fmt.Errorf("%w: no evidence id", ErrResolutionBlocked)
	}
	if r.EvidenceID != resolutionEvidencePrefix+r.Digest() {
		return fmt.Errorf("%w: evidence id does not match the certificate's own digest", ErrResolutionBlocked)
	}
	return nil
}

// Resolve decides every presented copy for spec.Request and returns the
// digest-bound [Resolution] certificate. It is deny- and block-by-default:
//
//   - a request that cannot advance (invalid, unverified, duplicate-linked,
//     below its assurance floor) is refused with [ErrResolutionRefused];
//   - an empty copy set, an unknown copy class, a declared-complete class
//     with no presented copy, a cross-tenant or cross-subject copy, a hold
//     that cannot name itself, or an exception without authority blocks
//     with [ErrResolutionBlocked].
//
// The per-kind rule table:
//
//	ERASURE:       held/excepted -> RETAIN (hold/exception authority);
//	              restored re-deletion -> FULFILL; otherwise by capability
//	              (DELETE/TOMBSTONE -> FULFILL, ANONYMIZE -> ANONYMIZE,
//	              NONE -> RETAIN under the named retention authority).
//	ACCESS:        access-redacting exception -> PARTIAL; otherwise FULFILL
//	              (holds never block reads).
//	PORTABILITY:   like ACCESS, except PRIVILEGED extracts -> DENY.
//	RECTIFICATION: held -> PARTIAL (correction appended, original preserved
//	              under hold); excepted -> RETAIN; immutable BACKUP/RETAINED
//	              -> PARTIAL (converges on rotation); otherwise FULFILL.
//	RESTRICTION:   excepted -> RETAIN; immutable BACKUP/RETAINED -> PARTIAL;
//	              otherwise RESTRICT (holds do not conflict with markers).
//	OBJECTION:     processing copies -> RESTRICT; core/immutable/privileged
//	              copies -> PARTIAL (objection recorded, record kept under
//	              contract); held/excepted -> RETAIN.
func Resolve(spec ResolutionSpec) (Resolution, error) {
	if !spec.At.IsSet() {
		return Resolution{}, fmt.Errorf("%w: no resolution instant", ErrResolutionBlocked)
	}
	if err := spec.Request.Validate(); err != nil {
		return Resolution{}, fmt.Errorf("%v: %w", ErrResolutionRefused, err)
	}
	if advance, code := spec.Request.CanAdvance(); !advance {
		return Resolution{}, fmt.Errorf("%w: request %q cannot advance: %s", ErrResolutionRefused, spec.Request.ID, code)
	}
	if spec.At.Before(spec.Request.ReceivedAt) {
		return Resolution{}, fmt.Errorf("%w: resolution precedes intake", ErrResolutionBlocked)
	}
	if len(spec.Copies) == 0 {
		return Resolution{}, fmt.Errorf("%w: no copies inventoried: completeness unprovable", ErrResolutionBlocked)
	}
	if err := validateExceptions(spec.Exceptions); err != nil {
		return Resolution{}, err
	}
	if err := validateCopies(spec.Request, spec.Copies); err != nil {
		return Resolution{}, err
	}
	if err := checkCompleteness(spec.CompleteClasses, spec.Copies); err != nil {
		return Resolution{}, err
	}
	if err := checkExceptionScope(spec.Exceptions, spec.Copies); err != nil {
		return Resolution{}, err
	}
	byCopy := exceptionIndex(spec.Exceptions, spec.Copies)

	items := make([]ItemResolution, 0, len(spec.Copies))
	for _, copy := range spec.Copies {
		item, err := resolveItem(spec.Request, copy, byCopy[copy.CopyID])
		if err != nil {
			return Resolution{}, err
		}
		items = append(items, item)
	}
	slices.SortFunc(items, func(a, b ItemResolution) int { return compareString(a.CopyID, b.CopyID) })

	res := Resolution{
		RequestID:     spec.Request.ID,
		RequestDigest: spec.Request.Digest(),
		Kind:          spec.Request.Kind,
		Tenant:        spec.Request.Tenant,
		ResolvedAt:    spec.At,
		Items:         items,
		Exceptions:    slices.Clone(spec.Exceptions),
	}
	res.EvidenceID = resolutionEvidencePrefix + res.Digest()
	if err := res.Validate(); err != nil {
		return Resolution{}, err
	}
	return res, nil
}

// validateExceptions checks every exception names at least one copy, cites
// an authority, declares a valid basis, never claims tax retention redacts
// access, and never shares a copy with another exception (overlapping
// grounds are ambiguous authority, so they block).
func validateExceptions(exceptions []LegalException) error {
	covered := make(map[string]string, len(exceptions))
	for i, e := range exceptions {
		if len(e.CopyIDs) == 0 {
			return fmt.Errorf("%w: exception %d names no copies", ErrResolutionBlocked, i)
		}
		if e.Authority == "" {
			return fmt.Errorf("%w: exception %d (%s) has no authority", ErrResolutionBlocked, i, e.Basis)
		}
		if !e.Basis.valid() {
			return fmt.Errorf("%w: exception %d cites an undeclared basis %q", ErrResolutionBlocked, i, string(e.Basis))
		}
		if e.Basis == ExceptionTaxRetention && e.RedactsAccess {
			return fmt.Errorf("%w: tax retention preserves records, it never redacts access", ErrResolutionBlocked)
		}
		for _, id := range e.CopyIDs {
			if id == "" {
				return fmt.Errorf("%w: exception %d names an empty copy id", ErrResolutionBlocked, i)
			}
			if first, dup := covered[id]; dup {
				return fmt.Errorf("%w: copy %q is covered by overlapping exceptions (%q and %q)", ErrResolutionBlocked, id, first, e.Authority)
			}
			covered[id] = e.Authority
		}
	}
	return nil
}

// validateCopies checks every copy is well-formed, of a known class, owned
// by the request's tenant and subject, uniquely identified, and -- when
// held or immutable -- able to name the authority that will appear on its
// RETAIN item. Every exception must also resolve to a presented copy: a
// dangling exception withholds fulfillment for data that is not even in
// scope, so it blocks.
func validateCopies(req DataSubjectRequest, copies []CopyDescriptor) error {
	seen := make(map[string]struct{}, len(copies))
	for i, c := range copies {
		if c.CopyID == "" {
			return fmt.Errorf("%w: copy %d has no id", ErrResolutionBlocked, i)
		}
		if _, dup := seen[c.CopyID]; dup {
			return fmt.Errorf("%w: duplicate copy %q", ErrResolutionBlocked, c.CopyID)
		}
		seen[c.CopyID] = struct{}{}
		if err := c.Class.Validate(); err != nil {
			return fmt.Errorf("%w: copy %q: %v", ErrResolutionBlocked, c.CopyID, err)
		}
		if c.Tenant != req.Tenant {
			return fmt.Errorf("%w: copy %q belongs to another tenant: cross-tenant resolution refused", ErrResolutionBlocked, c.CopyID)
		}
		if c.SubjectKey == "" || c.SubjectKey != req.Claims.Key() {
			return fmt.Errorf("%w: copy %q is not about this request's subject: cross-subject resolution refused", ErrResolutionBlocked, c.CopyID)
		}
		if !c.Capability.valid() {
			return fmt.Errorf("%w: copy %q declares an undeclared deletion capability %q", ErrResolutionBlocked, c.CopyID, string(c.Capability))
		}
		if c.Held && c.HoldAuthority == "" {
			return fmt.Errorf("%w: held copy %q names no hold authority", ErrResolutionBlocked, c.CopyID)
		}
		if c.ErasureFulfilled && c.Capability == DeletionNone {
			return fmt.Errorf("%w: immutable copy %q claims a prior fulfilled erasure: contradictory", ErrResolutionBlocked, c.CopyID)
		}
	}
	return nil
}

// checkCompleteness requires every declared-complete class to have at least
// one presented copy. CompleteClasses comes from the RECORDS-COPY-001
// certificate: omitting the subject's VECTOR copy while certifying VECTOR
// complete is exactly the RED failure this todo exists to prevent.
func checkCompleteness(complete []CopyClass, copies []CopyDescriptor) error {
	present := make(map[CopyClass]struct{}, len(copies))
	for _, c := range copies {
		present[c.Class] = struct{}{}
	}
	for _, class := range complete {
		if err := class.Validate(); err != nil {
			return err
		}
		if _, ok := present[class]; !ok {
			return fmt.Errorf("%w: class %s is declared complete but no copy was presented: omitted copy", ErrResolutionBlocked, class)
		}
	}
	return nil
}

// checkExceptionScope requires every exception to resolve to a presented
// copy. An exception naming a copy that was never presented dangles -- it
// withholds fulfillment for out-of-scope data -- so it blocks.
func checkExceptionScope(exceptions []LegalException, copies []CopyDescriptor) error {
	presented := make(map[string]struct{}, len(copies))
	for _, c := range copies {
		presented[c.CopyID] = struct{}{}
	}
	for _, e := range exceptions {
		for _, id := range e.CopyIDs {
			if _, ok := presented[id]; !ok {
				return fmt.Errorf("%w: exception %q covers unpresented copy %q: dangling exception", ErrResolutionBlocked, e.Authority, id)
			}
		}
	}
	return nil
}

// exceptionIndex maps each presented copy id to its covering exception.
func exceptionIndex(exceptions []LegalException, copies []CopyDescriptor) map[string]*LegalException {
	byCopy := make(map[string]*LegalException, len(copies))
	presented := make(map[string]struct{}, len(copies))
	for _, c := range copies {
		presented[c.CopyID] = struct{}{}
	}
	for i := range exceptions {
		for _, id := range exceptions[i].CopyIDs {
			if _, ok := presented[id]; ok {
				byCopy[id] = &exceptions[i]
			}
		}
	}
	return byCopy
}

// resolveItem applies the per-kind rule table Resolve documents.
func resolveItem(req DataSubjectRequest, copy CopyDescriptor, exception *LegalException) (ItemResolution, error) {
	requestAuthority := req.EvidenceID
	mk := func(outcome ItemOutcome, authority string, reason RedactionReason, detail string) ItemResolution {
		return ItemResolution{
			CopyID: copy.CopyID, Class: copy.Class,
			Outcome: outcome, Authority: authority,
			RedactionReason: reason, AppealRoute: appealFor(outcome),
			Detail: detail,
		}
	}
	retain := func(authority string, reason RedactionReason) (ItemResolution, error) {
		if authority == "" {
			return ItemResolution{}, fmt.Errorf("%w: copy %q would be retained under no authority", ErrResolutionBlocked, copy.CopyID)
		}
		return mk(OutcomeRetain, authority, reason, ""), nil
	}

	switch req.Kind {
	case KindErasure:
		if copy.Held {
			return retain("hold:"+copy.HoldAuthority, RedactionNone)
		}
		if exception != nil {
			return retain(exception.Authority, exception.Basis.exceptionRedaction())
		}
		if copy.ErasureFulfilled {
			// A hold or exception above still wins: restore never
			// resurrects held or excepted data into deletability.
			return mk(OutcomeFulfill, requestAuthority, RedactionNone, "RE_DELETE_AFTER_RESTORE"), nil
		}
		switch copy.Capability {
		case DeletionDelete:
			return mk(OutcomeFulfill, requestAuthority, RedactionNone, "DELETE_DIRECTED"), nil
		case DeletionTombstone:
			return mk(OutcomeFulfill, requestAuthority, RedactionNone, "TOMBSTONE_DIRECTED"), nil
		case DeletionAnonymize:
			return mk(OutcomeAnonymize, requestAuthority, RedactionNone, "ANONYMIZE_DIRECTED"), nil
		default: // DeletionNone: immutable by design.
			if copy.RetentionAuthority == "" {
				return ItemResolution{}, fmt.Errorf("%w: immutable copy %q names no retention authority", ErrResolutionBlocked, copy.CopyID)
			}
			return retain("retention:"+copy.RetentionAuthority, RedactionNone)
		}
	case KindAccess:
		if exception != nil && exception.RedactsAccess {
			return mk(OutcomePartial, exception.Authority, exception.Basis.exceptionRedaction(), "ACCESS_REDACTED"), nil
		}
		if copy.Held {
			return mk(OutcomeFulfill, requestAuthority, RedactionNone, "HELD_READ_PERMITTED"), nil
		}
		return mk(OutcomeFulfill, requestAuthority, RedactionNone, ""), nil
	case KindPortability:
		if copy.Class == CopyPrivileged {
			return mk(OutcomeDeny, "POLICY_PORTABILITY_SCOPE", RedactionNone, "PRIVILEGED_EXTRACTS_EXCLUDED"), nil
		}
		if exception != nil && exception.RedactsAccess {
			return mk(OutcomePartial, exception.Authority, exception.Basis.exceptionRedaction(), "ACCESS_REDACTED"), nil
		}
		return mk(OutcomeFulfill, requestAuthority, RedactionNone, ""), nil
	case KindRectification:
		if copy.Held {
			return mk(OutcomePartial, "hold:"+copy.HoldAuthority, RedactionNone, "HELD_CORRECTION_APPENDED"), nil
		}
		if exception != nil {
			return retain(exception.Authority, exception.Basis.exceptionRedaction())
		}
		if copy.Class == CopyBackup || copy.Class == CopyRetained {
			return mk(OutcomePartial, requestAuthority, RedactionNone, "IMMUTABLE_CONVERGES_ON_ROTATION"), nil
		}
		return mk(OutcomeFulfill, requestAuthority, RedactionNone, ""), nil
	case KindRestriction:
		if exception != nil {
			return retain(exception.Authority, exception.Basis.exceptionRedaction())
		}
		if copy.Class == CopyBackup || copy.Class == CopyRetained {
			return mk(OutcomePartial, requestAuthority, RedactionNone, "IMMUTABLE_CONVERGES_ON_RESTORE"), nil
		}
		return mk(OutcomeRestrict, requestAuthority, RedactionNone, ""), nil
	case KindObjection:
		if copy.Held {
			return retain("hold:"+copy.HoldAuthority, RedactionNone)
		}
		if exception != nil {
			return retain(exception.Authority, exception.Basis.exceptionRedaction())
		}
		switch copy.Class {
		case CopyProjection, CopySearch, CopyVector, CopyCache, CopyTelemetry, CopyExport, CopyProvider:
			return mk(OutcomeRestrict, requestAuthority, RedactionNone, ""), nil
		default:
			return mk(OutcomePartial, requestAuthority, RedactionNone, "OBJECTION_RECORDED_CORE_RETAINED"), nil
		}
	default:
		return ItemResolution{}, fmt.Errorf("%w: undeclared request kind %q", ErrResolutionBlocked, string(req.Kind))
	}
}

// --- small canonical helpers (this file's own copies) -----------------------

func compareString(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func sortedStrings(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return out
}

func joinStrings(in []string, sep string) string {
	out := ""
	for i, s := range in {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func exceptionKey(e LegalException) string {
	return joinStrings(sortedStrings(e.CopyIDs), ",") + "|" + e.Authority + "|" + string(e.Basis)
}
