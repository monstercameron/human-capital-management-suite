// Incident forensic acquisition with bounded chain of custody
// (FORENSIC-001).
//
// A dual-authorized acquisition plan pins the incident, purpose, scope,
// data classes, sources, query, tool and actors before any byte is
// collected. Collection captures content-addressed, minimized artifacts
// with source integrity, and every handoff appends a custody receipt to an
// append-only chain value. Compartment and residency are enforced: raw
// evidence never crosses either boundary — only a separately digested,
// redacted disclosure travels. Retention (legal hold plus disposition) is
// required up front and re-checked at export, and the export package
// verifies independently by recomputation.
//
// The package stays kernel-pure: incidents come from this package's own
// lifecycle, hold identity is a reference (RECORDS-HOLD-001 owns the hold
// itself), and all times are injected.
package incidentstate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	// ErrInvalidAcquisition reports a malformed plan, scope, source or
	// retention.
	ErrInvalidAcquisition = errors.New("incident: invalid forensic acquisition")
	// ErrAcquisitionAuthority reports missing dual authorization,
	// untrusted time or an incident state that forbids collection.
	ErrAcquisitionAuthority = errors.New("incident: forensic acquisition lacks authority")
	// ErrOvercollection reports a collection outside the authorized scope.
	ErrOvercollection = errors.New("incident: collection exceeds the authorized scope")
	// ErrCustodyBroken reports a chain-of-custody gap or forgery.
	ErrCustodyBroken = errors.New("incident: chain of custody is broken")
	// ErrCompartmentCrossing reports raw evidence offered to another
	// compartment or region.
	ErrCompartmentCrossing = errors.New("incident: raw evidence cannot cross compartment or region")
	// ErrExportUnverifiable reports an export whose contents do not verify.
	ErrExportUnverifiable = errors.New("incident: forensic export does not verify")
)

// ForensicDataClass is the closed collection vocabulary. Only named
// classes may be collected; anything else is overcollection by definition.
type ForensicDataClass string

const (
	DataClassLog    ForensicDataClass = "log"
	DataClassTrace  ForensicDataClass = "trace"
	DataClassConfig ForensicDataClass = "config"
	DataClassMemory ForensicDataClass = "memory"
)

// ParseForensicDataClass resolves the closed vocabulary.
func ParseForensicDataClass(raw string) (ForensicDataClass, error) {
	switch ForensicDataClass(strings.ToLower(strings.TrimSpace(raw))) {
	case DataClassLog:
		return DataClassLog, nil
	case DataClassTrace:
		return DataClassTrace, nil
	case DataClassConfig:
		return DataClassConfig, nil
	case DataClassMemory:
		return DataClassMemory, nil
	default:
		return "", fmt.Errorf("%w: data class %q is not collectible", ErrInvalidAcquisition, raw)
	}
}

// AcquisitionScope bounds one collection: explicit tenants, classes,
// sources, a bounded query and hard artifact/byte ceilings. Empty and
// wildcard scopes are refused so an investigator can never collect an
// entire tenant or system "by default".
type AcquisitionScope struct {
	Tenants      []string
	DataClasses  []ForensicDataClass
	Sources      []string
	Query        string
	MaxArtifacts uint
	MaxBytes     uint64
}

// RetentionPolicy binds one collection to its legal hold and disposition.
// Both halves are required: evidence without a hold reference or without
// a disposition deadline cannot be acquired.
type RetentionPolicy struct {
	HoldID      string
	RetainUntil time.Time
	Disposition string
}

// AcquisitionPlan is one dual-authorized collection plan.
type AcquisitionPlan struct {
	PlanID       string              `json:"plan_id"`
	IncidentID   string              `json:"incident_id"`
	Purpose      string              `json:"purpose"`
	Scope        acquisitionScopeDoc `json:"scope"`
	Tool         string              `json:"tool"`
	Actors       [2]string           `json:"actors"`
	Compartment  string              `json:"compartment"`
	Region       string              `json:"region"`
	Retention    retentionDoc        `json:"retention"`
	AuthorizedAt string              `json:"authorized_at"`
	TrustedTime  bool                `json:"trusted_time"`
	Digest       string              `json:"digest"`
}

type acquisitionScopeDoc struct {
	Tenants      []string `json:"tenants"`
	DataClasses  []string `json:"data_classes"`
	Sources      []string `json:"sources"`
	Query        string   `json:"query"`
	MaxArtifacts uint     `json:"max_artifacts"`
	MaxBytes     uint64   `json:"max_bytes"`
}

type retentionDoc struct {
	HoldID      string `json:"hold_id"`
	RetainUntil string `json:"retain_until"`
	Disposition string `json:"disposition"`
}

// collectibleStates are the incident states in which forensic collection
// is meaningful: declared or later, excluding redirects whose evidence
// lives on the surviving incident.
var collectibleStates = map[State]bool{
	Declared: true, Mitigating: true, Monitoring: true,
	Resolved: true, Reviewed: true, Reopened: true,
}

// PlanAcquisition authorizes one bounded collection. It requires a
// declared-or-later incident, a non-empty purpose, an explicit bounded
// scope, a named tool, two distinct actors, a compartment and region, a
// complete retention policy and a trusted authorization time.
func PlanAcquisition(incident Incident, purpose string, scope AcquisitionScope, tool string, actors [2]string, compartment, region string, retention RetentionPolicy, now time.Time) (AcquisitionPlan, error) {
	if strings.TrimSpace(incident.ID) == "" {
		return AcquisitionPlan{}, fmt.Errorf("%w: incident identity is required", ErrInvalidAcquisition)
	}
	if !collectibleStates[incident.State] {
		return AcquisitionPlan{}, fmt.Errorf("%w: incident is %s", ErrAcquisitionAuthority, incident.State)
	}
	if strings.TrimSpace(purpose) == "" {
		return AcquisitionPlan{}, fmt.Errorf("%w: purpose is required", ErrInvalidAcquisition)
	}
	if err := checkScope(scope); err != nil {
		return AcquisitionPlan{}, err
	}
	if strings.TrimSpace(tool) == "" {
		return AcquisitionPlan{}, fmt.Errorf("%w: collection tool identity is required", ErrInvalidAcquisition)
	}
	if strings.TrimSpace(actors[0]) == "" || strings.TrimSpace(actors[1]) == "" {
		return AcquisitionPlan{}, fmt.Errorf("%w: two authorizing actors are required", ErrAcquisitionAuthority)
	}
	if actors[0] == actors[1] {
		return AcquisitionPlan{}, fmt.Errorf("%w: authorizing actors must differ", ErrAcquisitionAuthority)
	}
	if strings.TrimSpace(compartment) == "" || strings.TrimSpace(region) == "" {
		return AcquisitionPlan{}, fmt.Errorf("%w: compartment and region are required", ErrInvalidAcquisition)
	}
	if strings.TrimSpace(retention.HoldID) == "" || retention.RetainUntil.IsZero() || strings.TrimSpace(retention.Disposition) == "" {
		return AcquisitionPlan{}, fmt.Errorf("%w: hold, retention deadline and disposition are required", ErrInvalidAcquisition)
	}
	if now.IsZero() {
		return AcquisitionPlan{}, fmt.Errorf("%w: trusted authorization time is required", ErrAcquisitionAuthority)
	}
	planID := "forensic-" + incident.ID
	plan := AcquisitionPlan{
		PlanID: planID, IncidentID: incident.ID, Purpose: purpose,
		Scope: acquisitionScopeDoc{
			Tenants:     append([]string(nil), scope.Tenants...),
			DataClasses: forensicClasses(scope.DataClasses),
			Sources:     append([]string(nil), scope.Sources...),
			Query:       scope.Query, MaxArtifacts: scope.MaxArtifacts, MaxBytes: scope.MaxBytes,
		},
		Tool: tool, Actors: actors, Compartment: compartment, Region: region,
		Retention: retentionDoc{
			HoldID: retention.HoldID, RetainUntil: retention.RetainUntil.UTC().Format(time.RFC3339),
			Disposition: retention.Disposition,
		},
		AuthorizedAt: now.UTC().Format(time.RFC3339), TrustedTime: true,
	}
	plan.Digest = hashForensic(planDigestView(plan))
	return plan, nil
}

func checkScope(scope AcquisitionScope) error {
	if len(scope.Tenants) == 0 || len(scope.DataClasses) == 0 || len(scope.Sources) == 0 {
		return fmt.Errorf("%w: tenants, data classes and sources must be explicit", ErrOvercollection)
	}
	for _, tenant := range scope.Tenants {
		if strings.TrimSpace(tenant) == "" || tenant == "*" {
			return fmt.Errorf("%w: tenant scope must name tenants, never everything", ErrOvercollection)
		}
	}
	for _, class := range scope.DataClasses {
		if _, err := ParseForensicDataClass(string(class)); err != nil {
			return err
		}
	}
	for _, source := range scope.Sources {
		if strings.TrimSpace(source) == "" || source == "*" {
			return fmt.Errorf("%w: source scope must name sources, never everything", ErrOvercollection)
		}
	}
	if strings.TrimSpace(scope.Query) == "" {
		return fmt.Errorf("%w: collection query is required", ErrInvalidAcquisition)
	}
	if scope.MaxArtifacts == 0 || scope.MaxBytes == 0 {
		return fmt.Errorf("%w: artifact and byte ceilings are required", ErrInvalidAcquisition)
	}
	return nil
}

func forensicClasses(classes []ForensicDataClass) []string {
	out := make([]string, 0, len(classes))
	for _, class := range classes {
		out = append(out, string(class))
	}
	sort.Strings(out)
	return out
}

func planDigestView(plan AcquisitionPlan) any {
	return struct {
		PlanID      string              `json:"plan_id"`
		IncidentID  string              `json:"incident_id"`
		Purpose     string              `json:"purpose"`
		Scope       acquisitionScopeDoc `json:"scope"`
		Tool        string              `json:"tool"`
		Actors      [2]string           `json:"actors"`
		Compartment string              `json:"compartment"`
		Region      string              `json:"region"`
		Retention   retentionDoc        `json:"retention"`
		At          string              `json:"authorized_at"`
	}{plan.PlanID, plan.IncidentID, plan.Purpose, plan.Scope, plan.Tool, plan.Actors, plan.Compartment, plan.Region, plan.Retention, plan.AuthorizedAt}
}

// Verify recomputes the plan digest and fails closed on tampering.
func (p AcquisitionPlan) Verify() error {
	if p.Digest == "" {
		return fmt.Errorf("%w: plan digest is missing", ErrInvalidAcquisition)
	}
	if hashForensic(planDigestView(p)) != p.Digest {
		return fmt.Errorf("%w: plan digest mismatch", ErrInvalidAcquisition)
	}
	return nil
}

// SourceEvidence is the caller-observed source material for one artifact.
type SourceEvidence struct {
	SourceRef  string
	DataClass  ForensicDataClass
	SourceHash string
	ByteCount  uint64
}

// Artifact is one minimized, content-addressed collection result. It
// carries hashes and custody metadata only; source bytes live in the
// separately access-controlled evidence store, never in this record.
type Artifact struct {
	ArtifactID  string            `json:"artifact_id"`
	PlanID      string            `json:"plan_id"`
	SourceRef   string            `json:"source_ref"`
	DataClass   ForensicDataClass `json:"data_class"`
	SourceHash  string            `json:"source_hash"`
	ByteCount   uint64            `json:"byte_count"`
	Compartment string            `json:"compartment"`
	Region      string            `json:"region"`
	CollectedAt string            `json:"collected_at"`
	Digest      string            `json:"digest"`
}

// Acquire collects one artifact inside the plan's scope and opens its
// custody chain with the first receipt. Sources or classes outside the
// scope, empty integrity hashes and oversized captures are refused.
func Acquire(plan AcquisitionPlan, source SourceEvidence, now time.Time) (Artifact, CustodyChain, error) {
	if err := plan.Verify(); err != nil {
		return Artifact{}, CustodyChain{}, err
	}
	if now.IsZero() {
		return Artifact{}, CustodyChain{}, fmt.Errorf("%w: trusted collection time is required", ErrAcquisitionAuthority)
	}
	if !scopeHas(plan.Scope.Sources, source.SourceRef) {
		return Artifact{}, CustodyChain{}, fmt.Errorf("%w: source %q is outside the authorized scope", ErrOvercollection, source.SourceRef)
	}
	class, err := ParseForensicDataClass(string(source.DataClass))
	if err != nil {
		return Artifact{}, CustodyChain{}, err
	}
	if !scopeHas(plan.Scope.DataClasses, string(class)) {
		return Artifact{}, CustodyChain{}, fmt.Errorf("%w: data class %q is outside the authorized scope", ErrOvercollection, class)
	}
	if strings.TrimSpace(source.SourceHash) == "" {
		return Artifact{}, CustodyChain{}, fmt.Errorf("%w: source integrity hash is required", ErrInvalidAcquisition)
	}
	if source.ByteCount == 0 || source.ByteCount > plan.Scope.MaxBytes {
		return Artifact{}, CustodyChain{}, fmt.Errorf("%w: byte count %d exceeds the authorized ceiling %d", ErrOvercollection, source.ByteCount, plan.Scope.MaxBytes)
	}
	artifact := Artifact{
		PlanID: plan.PlanID, SourceRef: source.SourceRef, DataClass: class,
		SourceHash: source.SourceHash, ByteCount: source.ByteCount,
		Compartment: plan.Compartment, Region: plan.Region,
		CollectedAt: now.UTC().Format(time.RFC3339),
	}
	artifact.ArtifactID = hashForensic(struct {
		Plan   string `json:"plan_id"`
		Source string `json:"source_ref"`
		Hash   string `json:"source_hash"`
		At     string `json:"collected_at"`
	}{artifact.PlanID, artifact.SourceRef, artifact.SourceHash, artifact.CollectedAt})
	artifact.Digest = hashForensic(artifactDigestView(artifact))
	chain, err := BeginCustody(artifact, plan.Actors[0], now)
	if err != nil {
		return Artifact{}, CustodyChain{}, err
	}
	return artifact, chain, nil
}

func artifactDigestView(artifact Artifact) any {
	return struct {
		ArtifactID  string            `json:"artifact_id"`
		PlanID      string            `json:"plan_id"`
		SourceRef   string            `json:"source_ref"`
		DataClass   ForensicDataClass `json:"data_class"`
		SourceHash  string            `json:"source_hash"`
		ByteCount   uint64            `json:"byte_count"`
		Compartment string            `json:"compartment"`
		Region      string            `json:"region"`
		CollectedAt string            `json:"collected_at"`
	}{artifact.ArtifactID, artifact.PlanID, artifact.SourceRef, artifact.DataClass, artifact.SourceHash, artifact.ByteCount, artifact.Compartment, artifact.Region, artifact.CollectedAt}
}

// Verify recomputes the artifact digest and fails closed on tampering.
func (a Artifact) Verify() error {
	if a.Digest == "" || a.ArtifactID == "" {
		return fmt.Errorf("%w: artifact identity is missing", ErrInvalidAcquisition)
	}
	if hashForensic(artifactDigestView(a)) != a.Digest {
		return fmt.Errorf("%w: artifact digest mismatch", ErrInvalidAcquisition)
	}
	return nil
}

// AuthorizeMove refuses every raw-evidence move across compartment or
// region. Only a redacted Disclosure travels; raw artifacts stay where
// the plan compartmented them.
func AuthorizeMove(artifact Artifact, compartment, region string) error {
	if compartment != artifact.Compartment || region != artifact.Region {
		return fmt.Errorf("%w: %s/%s cannot move to %s/%s", ErrCompartmentCrossing,
			artifact.Compartment, artifact.Region, compartment, region)
	}
	return nil
}

// CustodyReceipt is one append-only handoff record.
type CustodyReceipt struct {
	ArtifactID     string `json:"artifact_id"`
	Custodian      string `json:"custodian"`
	Sequence       uint64 `json:"sequence"`
	At             string `json:"at"`
	ArtifactDigest string `json:"artifact_digest"`
	Digest         string `json:"digest"`
}

// CustodyChain is the append-only handoff history for one artifact. It is
// a value: transfers return the next chain, and verification recomputes
// every receipt.
type CustodyChain struct {
	ArtifactID string           `json:"artifact_id"`
	Receipts   []CustodyReceipt `json:"receipts"`
}

// BeginCustody opens the chain with the collecting custodian.
func BeginCustody(artifact Artifact, custodian string, now time.Time) (CustodyChain, error) {
	if err := artifact.Verify(); err != nil {
		return CustodyChain{}, err
	}
	if strings.TrimSpace(custodian) == "" || now.IsZero() {
		return CustodyChain{}, fmt.Errorf("%w: custodian and trusted time are required", ErrInvalidAcquisition)
	}
	chain := CustodyChain{ArtifactID: artifact.ArtifactID}
	receipt := CustodyReceipt{
		ArtifactID: artifact.ArtifactID, Custodian: custodian, Sequence: 1,
		At: now.UTC().Format(time.RFC3339), ArtifactDigest: artifact.Digest,
	}
	receipt.Digest = hashForensic(receiptDigestView(receipt))
	chain.Receipts = []CustodyReceipt{receipt}
	return chain, nil
}

// TransferCustody appends one handoff. The existing chain must verify, the
// custodian must be named, and time must not move backward.
func TransferCustody(chain CustodyChain, custodian string, now time.Time) (CustodyChain, error) {
	if err := VerifyCustody(chain); err != nil {
		return CustodyChain{}, err
	}
	if strings.TrimSpace(custodian) == "" {
		return CustodyChain{}, fmt.Errorf("%w: custodian is required", ErrInvalidAcquisition)
	}
	if now.IsZero() {
		return CustodyChain{}, fmt.Errorf("%w: trusted transfer time is required", ErrAcquisitionAuthority)
	}
	last := chain.Receipts[len(chain.Receipts)-1]
	lastAt, err := time.Parse(time.RFC3339, last.At)
	if err != nil || now.Before(lastAt) {
		return CustodyChain{}, fmt.Errorf("%w: transfer time moves backward", ErrCustodyBroken)
	}
	next := CustodyChain{ArtifactID: chain.ArtifactID, Receipts: append([]CustodyReceipt(nil), chain.Receipts...)}
	receipt := CustodyReceipt{
		ArtifactID: chain.ArtifactID, Custodian: custodian, Sequence: last.Sequence + 1,
		At: now.UTC().Format(time.RFC3339), ArtifactDigest: last.ArtifactDigest,
	}
	receipt.Digest = hashForensic(receiptDigestView(receipt))
	next.Receipts = append(next.Receipts, receipt)
	return next, nil
}

func receiptDigestView(receipt CustodyReceipt) any {
	return struct {
		ArtifactID     string `json:"artifact_id"`
		Custodian      string `json:"custodian"`
		Sequence       uint64 `json:"sequence"`
		At             string `json:"at"`
		ArtifactDigest string `json:"artifact_digest"`
	}{receipt.ArtifactID, receipt.Custodian, receipt.Sequence, receipt.At, receipt.ArtifactDigest}
}

// VerifyCustody recomputes every receipt and checks sequence continuity,
// artifact binding and time order. Any gap or forgery fails closed.
func VerifyCustody(chain CustodyChain) error {
	if strings.TrimSpace(chain.ArtifactID) == "" || len(chain.Receipts) == 0 {
		return fmt.Errorf("%w: chain is empty", ErrCustodyBroken)
	}
	var previous time.Time
	for i, receipt := range chain.Receipts {
		if receipt.ArtifactID != chain.ArtifactID {
			return fmt.Errorf("%w: receipt %d binds another artifact", ErrCustodyBroken, i)
		}
		if receipt.Sequence != uint64(i+1) {
			return fmt.Errorf("%w: receipt %d breaks the sequence", ErrCustodyBroken, i)
		}
		if hashForensic(receiptDigestView(receipt)) != receipt.Digest {
			return fmt.Errorf("%w: receipt %d digest mismatch", ErrCustodyBroken, i)
		}
		at, err := time.Parse(time.RFC3339, receipt.At)
		if err != nil || (!previous.IsZero() && at.Before(previous)) {
			return fmt.Errorf("%w: receipt %d time moves backward", ErrCustodyBroken, i)
		}
		previous = at
	}
	return nil
}

// Disclosure is the separately redacted package for audiences outside the
// investigation compartment. It carries references and redaction reasons
// only — never source bytes or hashes.
type Disclosure struct {
	ArtifactID  string   `json:"artifact_id"`
	PlanID      string   `json:"plan_id"`
	Compartment string   `json:"compartment"`
	Redactions  []string `json:"redactions"`
	Digest      string   `json:"digest"`
}

// Redact produces the disclosure package for one verified artifact with
// verified custody. At least one redaction reason is required so nothing
// leaves the compartment unredacted by accident.
func Redact(artifact Artifact, chain CustodyChain, compartment string, redactions []string) (Disclosure, error) {
	if err := artifact.Verify(); err != nil {
		return Disclosure{}, err
	}
	if err := VerifyCustody(chain); err != nil {
		return Disclosure{}, err
	}
	if chain.ArtifactID != artifact.ArtifactID {
		return Disclosure{}, fmt.Errorf("%w: custody binds another artifact", ErrCustodyBroken)
	}
	if strings.TrimSpace(compartment) == "" {
		return Disclosure{}, fmt.Errorf("%w: disclosure compartment is required", ErrInvalidAcquisition)
	}
	if len(redactions) == 0 {
		return Disclosure{}, fmt.Errorf("%w: at least one redaction reason is required", ErrInvalidAcquisition)
	}
	for _, reason := range redactions {
		if strings.TrimSpace(reason) == "" {
			return Disclosure{}, fmt.Errorf("%w: redaction reasons must be named", ErrInvalidAcquisition)
		}
	}
	disclosure := Disclosure{
		ArtifactID: artifact.ArtifactID, PlanID: artifact.PlanID,
		Compartment: compartment, Redactions: append([]string(nil), redactions...),
	}
	disclosure.Digest = hashForensic(struct {
		ArtifactID  string   `json:"artifact_id"`
		PlanID      string   `json:"plan_id"`
		Compartment string   `json:"compartment"`
		Artifact    string   `json:"artifact_digest"`
		Redactions  []string `json:"redactions"`
	}{disclosure.ArtifactID, disclosure.PlanID, disclosure.Compartment, artifact.Digest, disclosure.Redactions})
	return disclosure, nil
}

// ExportPackage is the independently verifiable collection record: plan
// and artifact digests, custody digests and retention, sealed with its own
// digest.
type ExportPackage struct {
	PlanID          string       `json:"plan_id"`
	PlanDigest      string       `json:"plan_digest"`
	ArtifactDigests []string     `json:"artifact_digests"`
	CustodyDigests  []string     `json:"custody_digests"`
	Retention       retentionDoc `json:"retention"`
	ExportedAt      string       `json:"exported_at"`
	Digest          string       `json:"digest"`
}

// Export seals one collection for independent verification. Every
// artifact and chain must verify, every chain must bind a sealed
// artifact, and the retention deadline must still hold: expired evidence
// is due for disposition, not for export.
func Export(plan AcquisitionPlan, artifacts []Artifact, chains []CustodyChain, now time.Time) (ExportPackage, error) {
	if err := plan.Verify(); err != nil {
		return ExportPackage{}, err
	}
	if now.IsZero() {
		return ExportPackage{}, fmt.Errorf("%w: trusted export time is required", ErrAcquisitionAuthority)
	}
	retainUntil, err := time.Parse(time.RFC3339, plan.Retention.RetainUntil)
	if err != nil || now.After(retainUntil) {
		return ExportPackage{}, fmt.Errorf("%w: retention expired; evidence is due for disposition", ErrExportUnverifiable)
	}
	if len(artifacts) == 0 {
		return ExportPackage{}, fmt.Errorf("%w: export names no artifacts", ErrInvalidAcquisition)
	}
	if uint(len(artifacts)) > plan.Scope.MaxArtifacts {
		return ExportPackage{}, fmt.Errorf("%w: %d artifacts exceed the authorized ceiling %d", ErrOvercollection, len(artifacts), plan.Scope.MaxArtifacts)
	}
	byID := make(map[string]Artifact, len(artifacts))
	digests := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		if err := artifact.Verify(); err != nil {
			return ExportPackage{}, err
		}
		if artifact.PlanID != plan.PlanID {
			return ExportPackage{}, fmt.Errorf("%w: artifact %s belongs to another plan", ErrExportUnverifiable, artifact.ArtifactID)
		}
		if _, ok := byID[artifact.ArtifactID]; ok {
			return ExportPackage{}, fmt.Errorf("%w: duplicate artifact %s", ErrInvalidAcquisition, artifact.ArtifactID)
		}
		byID[artifact.ArtifactID] = artifact
		digests = append(digests, artifact.Digest)
	}
	sort.Strings(digests)
	custody := make([]string, 0, len(chains))
	for _, chain := range chains {
		if err := VerifyCustody(chain); err != nil {
			return ExportPackage{}, err
		}
		artifact, ok := byID[chain.ArtifactID]
		if !ok {
			return ExportPackage{}, fmt.Errorf("%w: custody binds an unsealed artifact", ErrExportUnverifiable)
		}
		if chain.Receipts[len(chain.Receipts)-1].ArtifactDigest != artifact.Digest {
			return ExportPackage{}, fmt.Errorf("%w: custody binds another artifact revision", ErrExportUnverifiable)
		}
		custody = append(custody, chain.Receipts[len(chain.Receipts)-1].Digest)
	}
	sort.Strings(custody)
	pkg := ExportPackage{
		PlanID: plan.PlanID, PlanDigest: plan.Digest,
		ArtifactDigests: digests, CustodyDigests: custody,
		Retention: plan.Retention, ExportedAt: now.UTC().Format(time.RFC3339),
	}
	pkg.Digest = hashForensic(struct {
		PlanID    string       `json:"plan_id"`
		Plan      string       `json:"plan_digest"`
		Artifacts []string     `json:"artifact_digests"`
		Custody   []string     `json:"custody_digests"`
		Retention retentionDoc `json:"retention"`
		At        string       `json:"exported_at"`
	}{pkg.PlanID, pkg.PlanDigest, pkg.ArtifactDigests, pkg.CustodyDigests, pkg.Retention, pkg.ExportedAt})
	return pkg, nil
}

// VerifyExport recomputes the export seal so any party can verify the
// package without the source evidence.
func VerifyExport(pkg ExportPackage) error {
	if pkg.Digest == "" {
		return fmt.Errorf("%w: export digest is missing", ErrExportUnverifiable)
	}
	recomputed := ExportPackage{
		PlanID: pkg.PlanID, PlanDigest: pkg.PlanDigest,
		ArtifactDigests: append([]string(nil), pkg.ArtifactDigests...),
		CustodyDigests:  append([]string(nil), pkg.CustodyDigests...),
		Retention:       pkg.Retention, ExportedAt: pkg.ExportedAt,
	}
	recomputed.Digest = hashForensic(struct {
		PlanID    string       `json:"plan_id"`
		Plan      string       `json:"plan_digest"`
		Artifacts []string     `json:"artifact_digests"`
		Custody   []string     `json:"custody_digests"`
		Retention retentionDoc `json:"retention"`
		At        string       `json:"exported_at"`
	}{recomputed.PlanID, recomputed.PlanDigest, recomputed.ArtifactDigests, recomputed.CustodyDigests, recomputed.Retention, recomputed.ExportedAt})
	if recomputed.Digest != pkg.Digest {
		return fmt.Errorf("%w: export digest mismatch", ErrExportUnverifiable)
	}
	return nil
}

func scopeHas(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func hashForensic(view any) string {
	b, err := json.Marshal(view)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
