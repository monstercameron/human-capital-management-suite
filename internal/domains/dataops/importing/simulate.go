package importing

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/importdraft"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Simulation errors are deliberately small and matchable by callers.
var (
	ErrSimulationInput           = errors.New("importing: simulation input is invalid")
	ErrSimulationConflict        = errors.New("importing: simulation conflict")
	ErrSimulationSigningRequired = errors.New("importing: simulation signing authority is required")
	ErrSimulationSignature       = errors.New("importing: simulation signature failed")
)

// Verifier authenticates canonical simulation payloads without granting the
// recovery path signing authority.
type Verifier interface {
	Authority() string
	Verify(payload []byte, signature string) error
}

// Signer signs and verifies canonical simulation payloads. Implementations
// belong to the composition root; this package never creates credentials.
type Signer interface {
	Verifier
	Sign(payload []byte) (string, error)
}

// SimulationSubject is the trusted subject binding for a staged row. The
// simulator never derives a domain subject identifier from CSV content.
type SimulationSubject struct {
	Kind            string
	SubjectID       string
	AuthorityDomain string
}

// ImportSimulation is a signed, zero-effect projection of an import.
type ImportSimulation struct {
	Tenant              string
	OrganizationScopeID string
	LegalEntityID       string
	EffectiveTime       string
	BatchDigest         string
	MappingDigest       string
	ValidationDigest    string
	SnapshotDigest      string
	ModelDigest         string
	Creates             int
	Changes             int
	NoOps               int
	Errors              int
	Conflicts           int
	PlannedWrites       []DraftWrite
	Obligations         []string
	Drafts              []BusinessIntentDraft
	// Governance dimensions are explicit so a preview cannot hide review work.
	AuthorityRequired   int
	ApprovalRequired    int
	LegalReviewRequired int
	SideEffectCount     int
	EstimatedCost       int64
	CommittedEffects    int
	Digest              string
	Signature           string
	SigningAuthority    string
	ZeroEffects         bool
}

// SimulationInput supplies validated pipeline output and a read-only baseline.
type SimulationInput struct {
	Batch               Batch
	Mapping             MappingProfile
	Validation          Result
	Registry            *model.Registry
	Current             map[string]map[string]string
	Resources           map[string]map[string]values.ResourceKey
	CurrentRevisions    map[string]map[string]values.RevisionToken
	IntentType          string
	Subjects            map[string]SimulationSubject
	OrganizationScopeID string
	LegalEntityID       string
	EffectiveTime       values.EffectiveInterval
	Obligations         []string
	Conflicts           []SimulationConflict
	ApprovalRequired    int
	LegalReviewRequired int
	SideEffectCount     int
	EstimatedCost       int64
	Signer              Signer
}

type SimulateImportRequest = SimulationInput

// SimulateImport constructs ordered BusinessIntent drafts without committing.
// It performs no storage, connector, clock, or intent-runtime operation.
func SimulateImport(in SimulationInput) (ImportSimulation, error) {
	if err := in.Batch.Validate(); err != nil {
		return ImportSimulation{}, fmt.Errorf("%w: batch: %v", ErrSimulationInput, err)
	}
	if in.Registry == nil {
		return ImportSimulation{}, fmt.Errorf("%w: registry is nil", ErrSimulationInput)
	}
	compiled, err := Compile(in.Registry, MappingSpecInput{Version: in.Mapping.Version, Fields: in.Mapping.Fields})
	if err != nil || compiled.Digest != in.Mapping.Digest {
		return ImportSimulation{}, fmt.Errorf("%w: mapping is not the canonical compiled revision", ErrSimulationInput)
	}
	in.Mapping = compiled
	revalidated, err := ValidateBatch(in.Batch, in.Mapping, in.Registry)
	if err != nil || validationDigest(revalidated) != validationDigest(in.Validation) {
		return ImportSimulation{}, fmt.Errorf("%w: validation is not the canonical result", ErrSimulationInput)
	}
	in.Validation = revalidated
	if in.Mapping.Digest == "" || in.Validation.Summary.BatchDigest != in.Batch.Digest() || in.Validation.Summary.MappingDigest != in.Mapping.Digest {
		return ImportSimulation{}, fmt.Errorf("%w: pipeline outputs do not match batch and mapping", ErrSimulationInput)
	}
	if in.Signer == nil || in.Signer.Authority() == "" {
		return ImportSimulation{}, ErrSimulationSigningRequired
	}
	if in.ApprovalRequired < 0 || in.LegalReviewRequired < 0 || in.SideEffectCount < 0 || in.EstimatedCost < 0 {
		return ImportSimulation{}, fmt.Errorf("%w: governance counts must be non-negative", ErrSimulationInput)
	}
	if in.IntentType == "" {
		return ImportSimulation{}, fmt.Errorf("%w: intent type is required", ErrSimulationInput)
	}
	if in.OrganizationScopeID == "" || in.LegalEntityID == "" {
		return ImportSimulation{}, fmt.Errorf("%w: organization scope and legal entity are required", ErrSimulationInput)
	}
	if err := in.EffectiveTime.Validate(); err != nil {
		return ImportSimulation{}, fmt.Errorf("%w: effective time: %v", ErrSimulationInput, err)
	}

	out := ImportSimulation{Tenant: in.Batch.Source().Tenant.String(), OrganizationScopeID: in.OrganizationScopeID, LegalEntityID: in.LegalEntityID, EffectiveTime: in.EffectiveTime.String(), BatchDigest: in.Batch.Digest(), MappingDigest: in.Mapping.Digest, ValidationDigest: validationDigest(in.Validation), SnapshotDigest: snapshotDigest(in.Current), ModelDigest: in.Registry.Digest(), Obligations: append([]string(nil), in.Obligations...), ZeroEffects: true, SigningAuthority: in.Signer.Authority(), ApprovalRequired: in.ApprovalRequired, LegalReviewRequired: in.LegalReviewRequired, SideEffectCount: in.SideEffectCount, EstimatedCost: in.EstimatedCost}
	sort.Strings(out.Obligations)
	conflicts := make(map[string][]SimulationConflict)
	usedConflicts := make(map[string]struct{})
	for _, c := range in.Conflicts {
		if c.RowID == "" || c.Property == "" || c.ExistingIntent == "" || c.EvidenceRef == "" {
			return ImportSimulation{}, fmt.Errorf("%w: conflict requires row, property, existing intent, and evidence", ErrSimulationConflict)
		}
		k := c.RowID + "\x00" + c.Property
		conflicts[k] = append(conflicts[k], c)
	}
	for key := range conflicts {
		sort.Slice(conflicts[key], func(i, j int) bool {
			if conflicts[key][i].EvidenceRef != conflicts[key][j].EvidenceRef {
				return conflicts[key][i].EvidenceRef < conflicts[key][j].EvidenceRef
			}
			return conflicts[key][i].ExistingIntent < conflicts[key][j].ExistingIntent
		})
		for i := 1; i < len(conflicts[key]); i++ {
			if conflicts[key][i] == conflicts[key][i-1] {
				return ImportSimulation{}, fmt.Errorf("%w: duplicate conflict evidence %s", ErrSimulationConflict, conflicts[key][i].EvidenceRef)
			}
		}
	}
	if len(in.Validation.Rows) != len(in.Batch.Rows()) {
		return ImportSimulation{}, fmt.Errorf("%w: validation row count does not match batch", ErrSimulationInput)
	}
	activeRows := make(map[string]struct{}, len(in.Validation.Rows))
	usedSubjects := make(map[string]struct{}, len(in.Subjects))
	for i, row := range in.Batch.Rows() {
		subject, ok := in.Subjects[row.ID()]
		if !ok || subject.Kind == "" || subject.SubjectID == "" || subject.AuthorityDomain == "" {
			return ImportSimulation{}, fmt.Errorf("%w: row %s has no trusted subject", ErrSimulationInput, row.ID())
		}
		usedSubjects[row.ID()] = struct{}{}
		d := BusinessIntentDraft{Ordinal: i, RowID: row.ID(), IntentType: in.IntentType, SubjectKind: subject.Kind, SubjectID: subject.SubjectID, AuthorityDomain: subject.AuthorityDomain}
		rr := in.Validation.Rows[i]
		if rr.RowID != row.ID() {
			return ImportSimulation{}, fmt.Errorf("%w: validation row %d does not match the batch", ErrSimulationInput, i)
		}
		d.Errors = append([]ValidationError(nil), rr.Errors...)
		if len(rr.Errors) != 0 {
			d.Status = "ERROR"
			out.Errors++
			out.Drafts = append(out.Drafts, d)
			continue
		}
		if _, duplicate := activeRows[row.ID()]; duplicate {
			return ImportSimulation{}, fmt.Errorf("%w: valid duplicate row %s is ambiguous", ErrSimulationInput, row.ID())
		}
		activeRows[row.ID()] = struct{}{}
		mapped, err := in.Mapping.Apply(in.Batch.Header(), row)
		if err != nil {
			return ImportSimulation{}, fmt.Errorf("%w: apply row %s: %v", ErrSimulationInput, row.ID(), err)
		}
		baseline, exists := in.Current[row.ID()]
		changed := false
		for _, mv := range mapped {
			if !mv.OK {
				continue
			}
			resolution, err := in.Registry.ResolveProperty(mv.Target)
			if err != nil {
				return ImportSimulation{}, fmt.Errorf("%w: property %s: %v", ErrSimulationInput, mv.Target, err)
			}
			d.AuthorityRefs = appendUnique(d.AuthorityRefs, resolution.Property.AuthorityRef)
			resource, resourceOK := in.Resources[row.ID()][string(mv.Target)]
			revision, revisionOK := in.CurrentRevisions[row.ID()][string(mv.Target)]
			if !resourceOK || resource.Validate() != nil || resource.Tenant != in.Batch.Source().Tenant || !revisionOK || !revision.IsSpecified() {
				return ImportSimulation{}, fmt.Errorf("%w: row %s property %s lacks a trusted tenant resource or revision", ErrSimulationInput, row.ID(), mv.Target)
			}
			w := DraftWrite{Property: string(mv.Target), Proposed: mv.Value, AuthorityRef: resolution.Property.AuthorityRef, ResourceKey: resource.String(), BaselineRevision: revision.String()}
			if exists {
				w.Current, w.CurrentPresent = baseline[string(mv.Target)]
				if !w.CurrentPresent || w.Current != w.Proposed {
					changed = true
				}
			} else {
				changed = true
			}
			d.Writes = append(d.Writes, w)
			conflictKey := row.ID() + "\x00" + string(mv.Target)
			for _, c := range conflicts[conflictKey] {
				usedConflicts[conflictKey] = struct{}{}
				d.Conflicts = append(d.Conflicts, c)
			}
		}
		sort.Strings(d.AuthorityRefs)
		// Structured conflicts follow one canonical total order (property, evidence, intent);
		// ConflictRefs are derived from that sequence so the verifier's index match holds.
		sort.SliceStable(d.Conflicts, func(i, j int) bool {
			a, b := d.Conflicts[i], d.Conflicts[j]
			if a.Property != b.Property {
				return a.Property < b.Property
			}
			if a.EvidenceRef != b.EvidenceRef {
				return a.EvidenceRef < b.EvidenceRef
			}
			return a.ExistingIntent < b.ExistingIntent
		})
		for _, c := range d.Conflicts {
			d.ConflictRefs = append(d.ConflictRefs, c.EvidenceRef)
		}
		if len(d.ConflictRefs) > 0 {
			d.Status = "CONFLICT"
			out.Conflicts++
			out.Drafts = append(out.Drafts, d)
			continue
		}
		if exists && !changed {
			d.Status = "NO_OP"
			out.NoOps++
		} else if exists {
			d.Status = "CHANGE"
			out.Changes++
		} else {
			d.Status = "CREATE"
			out.Creates++
		}
		if d.Status == "CREATE" || d.Status == "CHANGE" {
			out.PlannedWrites = append(out.PlannedWrites, cloneWrites(d.Writes)...)
		}
		out.Drafts = append(out.Drafts, d)
	}
	if len(usedSubjects) != len(in.Subjects) {
		return ImportSimulation{}, fmt.Errorf("%w: subject binding does not match the batch", ErrSimulationInput)
	}
	if len(usedConflicts) != len(conflicts) {
		return ImportSimulation{}, fmt.Errorf("%w: conflict does not match a mapped batch property", ErrSimulationConflict)
	}
	if out.AuthorityRequired == 0 {
		for _, d := range out.Drafts {
			if d.Status == "CREATE" || d.Status == "CHANGE" {
				out.AuthorityRequired++
			}
		}
	}
	w := importdraft.NewCanon("hcmnext.dataops.importing.ImportSimulation", 3)
	canonicalSimulation(&w, out)
	out.Digest = w.DigestHex()
	payload := []byte(wireSimulation(out))
	sig, err := in.Signer.Sign(payload)
	if err != nil {
		return ImportSimulation{}, fmt.Errorf("%w: %v", ErrSimulationSignature, err)
	}
	if sig == "" || sig == out.Digest {
		return ImportSimulation{}, fmt.Errorf("%w: signer returned an unverified/empty signature", ErrSimulationSignature)
	}
	if err := in.Signer.Verify(payload, sig); err != nil {
		return ImportSimulation{}, fmt.Errorf("%w: signer did not verify its signature: %v", ErrSimulationSignature, err)
	}
	out.Signature = sig
	return cloneSimulation(out), nil
}

// VerifyImportSimulation verifies a recovered simulation against a trusted
// signer. Signing authority is supplied by that signer, not by request data.
func VerifyImportSimulation(out ImportSimulation, signer Verifier) error {
	if signer == nil || signer.Authority() == "" || signer.Authority() != out.SigningAuthority {
		return ErrSimulationSignature
	}
	if err := validateSimulationShape(out); err != nil {
		return err
	}
	w := importdraft.NewCanon("hcmnext.dataops.importing.ImportSimulation", 3)
	canonicalSimulation(&w, out)
	if subtle.ConstantTimeCompare([]byte(out.Digest), []byte(w.DigestHex())) != 1 {
		return ErrSimulationSignature
	}
	if err := signer.Verify([]byte(wireSimulation(out)), out.Signature); err != nil {
		return fmt.Errorf("%w: %v", ErrSimulationSignature, err)
	}
	return nil
}

func validateSimulationShape(out ImportSimulation) error {
	if out.Tenant == "" || out.OrganizationScopeID == "" || out.LegalEntityID == "" || out.EffectiveTime == "" ||
		out.BatchDigest == "" || out.MappingDigest == "" || out.ValidationDigest == "" || out.SnapshotDigest == "" || out.ModelDigest == "" ||
		out.Digest == "" || out.Signature == "" || !out.ZeroEffects || out.CommittedEffects != 0 ||
		out.Creates < 0 || out.Changes < 0 || out.NoOps < 0 || out.Errors < 0 || out.Conflicts < 0 ||
		out.AuthorityRequired < 0 || out.ApprovalRequired < 0 || out.LegalReviewRequired < 0 ||
		out.SideEffectCount < 0 || out.EstimatedCost < 0 {
		return ErrSimulationSignature
	}
	creates, changes, noOps, invalid, conflicts := 0, 0, 0, 0, 0
	planned := make([]DraftWrite, 0, len(out.PlannedWrites))
	activeRows := make(map[string]struct{}, len(out.Drafts))
	for ordinal, draft := range out.Drafts {
		if draft.Ordinal != ordinal || draft.RowID == "" || draft.IntentType == "" || draft.SubjectKind == "" || draft.SubjectID == "" || draft.AuthorityDomain == "" {
			return ErrSimulationSignature
		}
		for _, write := range draft.Writes {
			if write.Property == "" || write.AuthorityRef == "" || write.ResourceKey == "" || write.BaselineRevision == "" {
				return ErrSimulationSignature
			}
		}
		switch draft.Status {
		case "CREATE":
			if len(draft.Writes) == 0 || len(draft.Errors) != 0 || len(draft.Conflicts) != 0 {
				return ErrSimulationSignature
			}
			if _, duplicate := activeRows[draft.RowID]; duplicate {
				return ErrSimulationSignature
			}
			activeRows[draft.RowID] = struct{}{}
			creates++
			planned = append(planned, draft.Writes...)
		case "CHANGE":
			if len(draft.Writes) == 0 || len(draft.Errors) != 0 || len(draft.Conflicts) != 0 {
				return ErrSimulationSignature
			}
			if _, duplicate := activeRows[draft.RowID]; duplicate {
				return ErrSimulationSignature
			}
			activeRows[draft.RowID] = struct{}{}
			changes++
			planned = append(planned, draft.Writes...)
		case "NO_OP":
			if len(draft.Writes) == 0 || len(draft.Errors) != 0 || len(draft.Conflicts) != 0 {
				return ErrSimulationSignature
			}
			if _, duplicate := activeRows[draft.RowID]; duplicate {
				return ErrSimulationSignature
			}
			activeRows[draft.RowID] = struct{}{}
			noOps++
		case "ERROR":
			if len(draft.Errors) == 0 || len(draft.Writes) != 0 || len(draft.Conflicts) != 0 {
				return ErrSimulationSignature
			}
			invalid++
		case "CONFLICT":
			if len(draft.Conflicts) == 0 || len(draft.ConflictRefs) != len(draft.Conflicts) || len(draft.Errors) != 0 {
				return ErrSimulationSignature
			}
			for i, conflict := range draft.Conflicts {
				if conflict.RowID != draft.RowID || conflict.ExistingIntent == "" || conflict.EvidenceRef != draft.ConflictRefs[i] {
					return ErrSimulationSignature
				}
			}
			if _, duplicate := activeRows[draft.RowID]; duplicate {
				return ErrSimulationSignature
			}
			activeRows[draft.RowID] = struct{}{}
			conflicts++
		default:
			return ErrSimulationSignature
		}
	}
	if creates != out.Creates || changes != out.Changes || noOps != out.NoOps || invalid != out.Errors || conflicts != out.Conflicts || len(planned) != len(out.PlannedWrites) {
		return ErrSimulationSignature
	}
	if out.AuthorityRequired != creates+changes {
		return ErrSimulationSignature
	}
	for i := range planned {
		if planned[i] != out.PlannedWrites[i] {
			return ErrSimulationSignature
		}
	}
	return nil
}

func appendUnique(in []string, value string) []string {
	for _, v := range in {
		if v == value {
			return in
		}
	}
	return append(in, value)
}
func canonicalSimulation(w **importdraft.Canon, out ImportSimulation) {
	c := *w
	c.Str(out.Tenant).Str(out.OrganizationScopeID).Str(out.LegalEntityID).Str(out.EffectiveTime)
	c.Str(out.BatchDigest).Str(out.MappingDigest).Str(out.ValidationDigest).Str(out.SnapshotDigest).Str(out.ModelDigest).U64(uint64(out.Creates)).U64(uint64(out.Changes)).U64(uint64(out.NoOps)).U64(uint64(out.Errors)).U64(uint64(out.Conflicts)).U64(uint64(out.AuthorityRequired)).U64(uint64(out.ApprovalRequired)).U64(uint64(out.LegalReviewRequired)).U64(uint64(out.SideEffectCount)).I64(out.EstimatedCost).U64(uint64(out.CommittedEffects)).Bool(out.ZeroEffects).Str(out.SigningAuthority)
	for _, d := range out.Drafts {
		c.I64(int64(d.Ordinal)).Str(d.RowID).Str(d.IntentType).Str(d.SubjectKind).Str(d.SubjectID).Str(d.AuthorityDomain).Strings(d.AuthorityRefs).Str(d.Status).Strings(d.ConflictRefs)
		for _, conflict := range d.Conflicts {
			c.Str(conflict.RowID).Str(conflict.Property).Str(conflict.ExistingIntent).Str(conflict.EvidenceRef)
		}
		for _, x := range d.Writes {
			c.Str(x.Property).Str(x.Current).Bool(x.CurrentPresent).Str(x.Proposed).Str(x.AuthorityRef).Str(x.ResourceKey).Str(x.BaselineRevision)
		}
		for _, e := range d.Errors {
			canonicalValidationError(c, e)
		}
	}
	for _, x := range out.PlannedWrites {
		c.Str(x.Property).Str(x.Current).Bool(x.CurrentPresent).Str(x.Proposed).Str(x.AuthorityRef).Str(x.ResourceKey).Str(x.BaselineRevision)
	}
	c.Strings(out.Obligations)
}
func wireSimulation(out ImportSimulation) string {
	w := importdraft.NewCanon("hcmnext.dataops.importing.ImportSimulationPayload", 3)
	canonicalSimulation(&w, out)
	return string(w.Bytes())
}
func Simulate(in SimulationInput) (ImportSimulation, error) { return SimulateImport(in) }

func cloneWrites(in []DraftWrite) []DraftWrite { return append([]DraftWrite(nil), in...) }

func snapshotDigest(current map[string]map[string]string) string {
	w := importdraft.NewCanon("hcmnext.dataops.importing.Snapshot", 1)
	rows := make([]string, 0, len(current))
	for row := range current {
		rows = append(rows, row)
	}
	sort.Strings(rows)
	for _, row := range rows {
		w.Str(row)
		props := make([]string, 0, len(current[row]))
		for property := range current[row] {
			props = append(props, property)
		}
		sort.Strings(props)
		for _, property := range props {
			w.Str(property).Str(current[row][property])
		}
	}
	return w.DigestHex()
}

func validationDigest(result Result) string {
	w := importdraft.NewCanon("hcmnext.dataops.importing.ValidationResult", 1)
	w.Str(result.Summary.BatchDigest).Str(result.Summary.MappingDigest).I64(int64(result.Summary.TotalRows)).I64(int64(result.Summary.ValidRows)).I64(int64(result.Summary.InvalidRows)).I64(int64(result.Summary.WarningCount))
	rules := make([]string, 0, len(result.Summary.ByRule))
	for rule := range result.Summary.ByRule {
		rules = append(rules, rule)
	}
	sort.Strings(rules)
	for _, rule := range rules {
		w.Str(rule).I64(int64(result.Summary.ByRule[rule]))
	}
	for _, row := range result.Rows {
		w.Str(row.RowID).Bool(row.Valid)
		for _, issue := range row.Errors {
			canonicalValidationError(w, issue)
		}
	}
	for _, warning := range result.Warnings {
		canonicalValidationError(w, warning)
	}
	return w.DigestHex()
}

func canonicalValidationError(w *importdraft.Canon, issue ValidationError) {
	w.Str(issue.Identity).Str(issue.BatchID).Str(issue.RowID).Str(issue.Property).Str(issue.RuleID).Str(issue.Severity.String()).Str(issue.Message)
}

func cloneSimulation(in ImportSimulation) ImportSimulation {
	out := in
	out.Obligations = append([]string(nil), in.Obligations...)
	out.PlannedWrites = cloneWrites(in.PlannedWrites)
	out.Drafts = make([]BusinessIntentDraft, len(in.Drafts))
	for i, draft := range in.Drafts {
		out.Drafts[i] = draft
		out.Drafts[i].AuthorityRefs = append([]string(nil), draft.AuthorityRefs...)
		out.Drafts[i].Writes = cloneWrites(draft.Writes)
		out.Drafts[i].Errors = append([]ValidationError(nil), draft.Errors...)
		out.Drafts[i].ConflictRefs = append([]string(nil), draft.ConflictRefs...)
		out.Drafts[i].Conflicts = append([]SimulationConflict(nil), draft.Conflicts...)
	}
	return out
}
