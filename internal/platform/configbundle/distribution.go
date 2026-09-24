package configbundle

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/tenant"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	platformconfig "github.com/monstercameron/human-capital-management-suite/internal/platform/configregistry"
)

// DistributionVersion is the desired-state envelope contract version.
const DistributionVersion = 1

// DesiredStateRecord is a signed desired-state publication for exactly one
// tenant and cell. The envelope signature covers the epoch and placement
// digest as well as the signed bundle digest.
type DesiredStateRecord struct {
	TenantID        string          `json:"tenant_id"`
	CellID          string          `json:"cell_id"`
	Bundle          SignedBundle    `json:"bundle"`
	SignedBundle    SignedBundle    `json:"signed_bundle,omitempty"`
	Epoch           uint64          `json:"epoch"`
	PlacementDigest string          `json:"placement_digest"`
	DesiredAt       time.Time       `json:"desired_at"`
	PublishedAt     time.Time       `json:"published_at,omitempty"`
	Digest          string          `json:"digest"`
	Signature       BundleSignature `json:"signature"`
}

type DesiredState = DesiredStateRecord
type Publication = DesiredStateRecord

// ConfigurationObject is re-exported for callers constructing an offline
// receiver around the same immutable registry port used by Compile.
type ConfigurationObject = platformconfig.ConfigurationObject

// Explain is safe to include in audit records: it contains identities and
// digests, never signature bytes, private keys, or configuration bodies.
func (r DesiredStateRecord) Explain() string {
	b := desiredBundle(r)
	return fmt.Sprintf("desired configuration bundle=%s epoch=%d tenant=%s cell=%s placement=%s", b.Bundle.BundleID, r.Epoch, r.TenantID, r.CellID, r.PlacementDigest)
}

// FreshnessPolicy bounds use of an offline snapshot. After MaxAge the
// receiver is DEGRADED; after HardMaxAge it is BLOCKED.
type FreshnessPolicy struct {
	MaxAge     time.Duration
	HardMaxAge time.Duration
}

func (p FreshnessPolicy) Validate() error {
	if p.MaxAge <= 0 || p.HardMaxAge <= 0 || p.HardMaxAge <= p.MaxAge {
		return distributionRefusal("INVALID_FRESHNESS_POLICY", "freshness", "max age and hard max age must be positive, with hard max age greater than max age", ErrInvalidFreshnessPolicy)
	}
	return nil
}

// ServiceState is the explicit state a runtime can report while offline.
type ServiceState string

const (
	ServiceReady    ServiceState = "READY"
	ServiceDegraded ServiceState = "DEGRADED"
	ServiceBlocked  ServiceState = "BLOCKED"
)

type Freshness = ServiceState

type ApplyDecision string

const (
	ApplyAccepted   ApplyDecision = "ACCEPTED"
	ApplyIdempotent ApplyDecision = "IDEMPOTENT"
	ApplyRefused    ApplyDecision = "REFUSED"
)

// Snapshot is the all-or-nothing active configuration image.
type Snapshot struct {
	TenantID        string
	CellID          string
	BundleID        string
	BundleDigest    string
	DesiredDigest   string
	Epoch           uint64
	PlacementDigest string
	AppliedAt       time.Time
	Bundle          Bundle
	Objects         []ConfigurationObject
}

func (s Snapshot) ObjectsCopy() []ConfigurationObject {
	out := make([]ConfigurationObject, len(s.Objects))
	for i, object := range s.Objects {
		out[i] = cloneConfigurationObject(object)
	}
	return out
}

func (s Snapshot) Explain() string {
	return fmt.Sprintf("active configuration bundle=%s epoch=%d tenant=%s cell=%s digest=%s", s.BundleID, s.Epoch, s.TenantID, s.CellID, s.BundleDigest)
}

// ApplyResult includes the retained last-known-good image on a refusal.
type ApplyResult struct {
	Decision    ApplyDecision
	State       ServiceState
	Snapshot    Snapshot
	HasSnapshot bool
}

// ReceiverOptions defines the local trust, placement, scope and freshness
// boundary. It has no transport or persistence dependency.
type ReceiverOptions struct {
	Scope             Scope
	Placement         tenant.Placement
	PlacementKey      [32]byte
	Freshness         FreshnessPolicy
	MaxAge            time.Duration
	HardMaxAge        time.Duration
	Now               func() time.Time
	Environment       string
	TrustProfile      string
	RuntimeVersion    string
	AntiRollbackFloor string
}

// Receiver verifies desired state and swaps one local active snapshot under
// a mutex only after the complete exact-object closure has been resolved.
type Receiver struct {
	keys    KeyResolver
	store   Store
	options ReceiverOptions
	now     func() time.Time

	mu     sync.RWMutex
	active *Snapshot
}

// NewReceiver accepts ReceiverOptions (or its pointer), Scope,
// tenant.Placement, FreshnessPolicy, and a func() time.Time as options.
func NewReceiver(keys KeyResolver, store Store, options ...any) *Receiver {
	r := &Receiver{keys: keys, store: store, now: func() time.Time { return time.Now().UTC() }}
	for _, option := range options {
		switch value := option.(type) {
		case ReceiverOptions:
			r.options = value
		case *ReceiverOptions:
			if value != nil {
				r.options = *value
			}
		case Scope:
			r.options.Scope = value
		case tenant.Placement:
			r.options.Placement = value
		case FreshnessPolicy:
			r.options.Freshness = value
		case func() time.Time:
			r.now = value
		}
	}
	if r.options.Freshness.MaxAge == 0 {
		r.options.Freshness.MaxAge = r.options.MaxAge
	}
	if r.options.Freshness.HardMaxAge == 0 {
		r.options.Freshness.HardMaxAge = r.options.HardMaxAge
	}
	if r.options.Now != nil {
		r.now = r.options.Now
	}
	return r
}

func NewDistributionReceiver(keys KeyResolver, store Store, options ...any) *Receiver {
	return NewReceiver(keys, store, options...)
}

func (r *Receiver) Snapshot() (Snapshot, bool) {
	if r == nil {
		return Snapshot{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.active == nil {
		return Snapshot{}, false
	}
	return cloneSnapshot(*r.active), true
}

func (r *Receiver) LastKnownGood() (Snapshot, bool) { return r.Snapshot() }

func (r *Receiver) StateAt(now time.Time) ServiceState {
	if r == nil {
		return ServiceBlocked
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.stateLocked(now)
}

func (r *Receiver) State() ServiceState                 { return r.StateAt(r.now().UTC()) }
func (r *Receiver) FreshnessAt(now time.Time) Freshness { return r.StateAt(now) }

// Receive verifies and atomically applies a record. Invalid input leaves the
// active image unchanged and returns its current bounded state.
func (r *Receiver) Receive(record DesiredStateRecord, placements ...tenant.Placement) (ApplyResult, error) {
	if r == nil {
		return ApplyResult{Decision: ApplyRefused, State: ServiceBlocked}, distributionRefusal("NIL_RECEIVER", "receiver", "receiver is nil", ErrDistributionRefused)
	}
	if len(placements) > 1 {
		return r.refused(distributionRefusal("TOO_MANY_PLACEMENTS", "placement", "at most one placement is accepted", ErrInvalidPlacementContext))
	}
	placement := r.options.Placement
	if len(placements) == 1 {
		placement = placements[0]
	}
	if err := r.validateRecord(record, placement); err != nil {
		return r.refused(err)
	}
	objects, err := r.resolveObjects(desiredBundle(record).Bundle)
	if err != nil {
		return r.refused(err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	currentEpoch := uint64(0)
	if r.active != nil {
		currentEpoch = r.active.Epoch
	}
	if record.Epoch < currentEpoch {
		return r.refusedLocked(distributionRefusal("EPOCH_REPLAY", "epoch", "desired state is older than the active epoch", ErrEpochReplay))
	}
	bundle := desiredBundle(record).Bundle
	if record.Epoch == currentEpoch && r.active != nil {
		if record.Digest == r.active.DesiredDigest {
			return ApplyResult{Decision: ApplyIdempotent, State: r.stateLocked(r.now().UTC()), Snapshot: cloneSnapshot(*r.active), HasSnapshot: true}, nil
		}
		return r.refusedLocked(distributionRefusal("EPOCH_CONFLICT", "epoch", "the active epoch already names another desired state", ErrEpochConflict))
	}
	snapshot := Snapshot{TenantID: record.TenantID, CellID: record.CellID, BundleID: bundle.BundleID, BundleDigest: bundle.Digest, DesiredDigest: record.Digest, Epoch: record.Epoch, PlacementDigest: record.PlacementDigest, AppliedAt: r.now().UTC(), Bundle: cloneBundle(bundle), Objects: objects}
	r.active = &snapshot
	return ApplyResult{Decision: ApplyAccepted, State: r.stateLocked(snapshot.AppliedAt), Snapshot: cloneSnapshot(snapshot), HasSnapshot: true}, nil
}

func (r *Receiver) Apply(record DesiredStateRecord, placements ...tenant.Placement) error {
	_, err := r.Receive(record, placements...)
	return err
}

func (r *Receiver) Bootstrap(record DesiredStateRecord, placements ...tenant.Placement) (ApplyResult, error) {
	return r.Receive(record, placements...)
}

// Accept is a descriptive alias for Receive.
func (r *Receiver) Accept(record DesiredStateRecord, placements ...tenant.Placement) (ApplyResult, error) {
	return r.Receive(record, placements...)
}

func (r *Receiver) stateLocked(now time.Time) ServiceState {
	if r.active == nil || r.options.Freshness.Validate() != nil {
		return ServiceBlocked
	}
	age := now.Sub(r.active.AppliedAt)
	if age < 0 || age <= r.options.Freshness.MaxAge {
		return ServiceReady
	}
	if age <= r.options.Freshness.HardMaxAge {
		return ServiceDegraded
	}
	return ServiceBlocked
}

func (r *Receiver) refused(err error) (ApplyResult, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.refusedLocked(err)
}

func (r *Receiver) refusedLocked(err error) (ApplyResult, error) {
	result := ApplyResult{Decision: ApplyRefused, State: r.stateLocked(r.now().UTC())}
	if r.active != nil {
		result.Snapshot, result.HasSnapshot = cloneSnapshot(*r.active), true
	}
	return result, err
}

func (r *Receiver) validateRecord(record DesiredStateRecord, placement tenant.Placement) error {
	if err := r.options.Freshness.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(record.TenantID) == "" || strings.TrimSpace(record.CellID) == "" {
		return distributionRefusal("MISSING_SCOPE", "scope", "tenant and cell are required", ErrDistributionRefused)
	}
	scope := Scope{TenantID: record.TenantID, CellID: record.CellID}
	if r.options.Scope.TenantID != "" && r.options.Scope != scope {
		return distributionRefusal("SCOPE_MISMATCH", "scope", "desired state is outside the receiver scope", ErrActivationScopeMismatch)
	}
	if placement.Tenant != record.TenantID || placement.Cell != record.CellID {
		return distributionRefusal("STALE_PLACEMENT", "placement", "placement tenant or cell differs from desired state", ErrStalePlacement)
	}
	placementDigest, err := placement.Digest()
	if err != nil || !strings.EqualFold(record.PlacementDigest, placementDigest) {
		return distributionRefusal("STALE_PLACEMENT", "placement_digest", "desired state was published for a different placement", ErrStalePlacement)
	}
	if r.options.PlacementKey != [32]byte{} && tenant.Verify(placement, r.options.PlacementKey) != nil {
		return distributionRefusal("INVALID_PLACEMENT_SIGNATURE", "placement.signature", "placement authority verification failed", ErrStalePlacement)
	}
	bundle := desiredBundle(record)
	bundleScope := bundle.Bundle.TargetScope
	if bundleScope.TenantID == "" {
		bundleScope = bundle.Bundle.Scope
	}
	if bundle.Bundle.BundleID == "" || bundleScope != scope || record.Epoch == 0 {
		return distributionRefusal("INVALID_DESIRED_STATE", "record", "desired state identity, epoch, and target scope are required", ErrDistributionRefused)
	}
	if r.keys == nil {
		return distributionRefusal("UNKNOWN_SIGNING_KEY", "signature.key_handle", "no signing-key resolver is configured", ErrUnknownSigningKey)
	}
	handle, version := bundle.Signature.KeyHandle, bundle.Signature.KeyVersion
	if handle == "" {
		handle = bundle.Signature.KeyID
	}
	key, found, err := r.keys.ResolveBundleKey(handle, version)
	if err != nil {
		return distributionRefusal("SIGNING_KEY_LOOKUP_FAILED", "signature.key_handle", "signing-key lookup failed", err)
	}
	if !found || key.Handle != handle || key.Version != version {
		return distributionRefusal("UNKNOWN_SIGNING_KEY", "signature.key_handle", "signing key handle/version is not trusted", ErrUnknownSigningKey)
	}
	if key.Revoked {
		return distributionRefusal("REVOKED_SIGNING_KEY", "signature.key_handle", "signing key is revoked", ErrRevokedSigningKey)
	}
	if r.options.TrustProfile != "" && key.TrustProfile != "" && key.TrustProfile != r.options.TrustProfile {
		return distributionRefusal("TRUST_PROFILE_MISMATCH", "trust_profile", "signing key is outside the receiver trust profile", ErrTrustProfileMismatch)
	}
	if r.options.RuntimeVersion != "" && compareRuntimeVersion(r.options.RuntimeVersion, bundle.Bundle.MinimumRuntimeVersion) < 0 {
		return distributionRefusal("RUNTIME_BELOW_BUNDLE_FLOOR", "runtime_version", "receiver runtime is below the bundle floor", ErrBelowRollbackFloor)
	}
	if r.options.AntiRollbackFloor != "" && compareRuntimeVersion(bundle.Bundle.MinimumRuntimeVersion, r.options.AntiRollbackFloor) < 0 {
		return distributionRefusal("BELOW_ROLLBACK_FLOOR", "minimum_runtime_version", "bundle is below the receiver rollback floor", ErrBelowRollbackFloor)
	}
	if err := record.Verify(key.PublicKey); err != nil {
		return err
	}
	return nil
}

func (r *Receiver) resolveObjects(bundle Bundle) ([]ConfigurationObject, error) {
	objects := bundle.OrderedObjects()
	if len(objects) == 0 || len(bundle.Roots) == 0 {
		return nil, distributionRefusal("PARTIAL_BUNDLE", "bundle.objects", "bundle has no complete pinned object closure", ErrPartialBundle)
	}
	if r.store == nil {
		return nil, distributionRefusal("UNKNOWN_CONFIG", "bundle.objects", "no configuration registry is available", ErrUnknownConfig)
	}
	byRef := make(map[ObjectRef]IncludedObject, len(objects))
	resolved := make([]ConfigurationObject, 0, len(objects))
	for _, included := range objects {
		if included.Ref.Revision == 0 || included.Digest == "" {
			return nil, distributionRefusal("PARTIAL_BUNDLE", refKey(included.Ref), "bundle object is not fully pinned and digested", ErrPartialBundle)
		}
		if _, exists := byRef[included.Ref]; exists {
			return nil, distributionRefusal("PARTIAL_BUNDLE", refKey(included.Ref), "bundle contains a duplicate object reference", ErrPartialBundle)
		}
		byRef[included.Ref] = included
		obj, found, err := r.store.GetObject(included.Ref)
		if err != nil {
			return nil, distributionRefusal("UNKNOWN_CONFIG", refKey(included.Ref), "configuration object lookup failed", err)
		}
		if !found {
			return nil, distributionRefusal("UNKNOWN_CONFIG", refKey(included.Ref), "configuration object is not published locally", ErrUnknownConfig)
		}
		if err := obj.Verify(); err != nil || obj.Digest() != included.Digest || (included.BodyDigest != "" && obj.CanonicalBodyDigest != included.BodyDigest) {
			return nil, distributionRefusal("UNKNOWN_CONFIG", refKey(included.Ref), "local configuration object does not match bundle identity", ErrUnknownConfig)
		}
		resolved = append(resolved, cloneConfigurationObject(obj))
	}
	for _, root := range bundle.Roots {
		if _, ok := byRef[root]; !ok {
			return nil, distributionRefusal("PARTIAL_BUNDLE", refKey(root), "bundle omits a declared root object", ErrPartialBundle)
		}
	}
	for _, obj := range resolved {
		deps, err := ExtractDependencies(obj.Body)
		if err != nil {
			return nil, distributionRefusal("PARTIAL_BUNDLE", refKey(obj.Ref()), "configuration dependency declaration is malformed", ErrPartialBundle)
		}
		for _, dep := range deps {
			depRef := dep.Ref
			if depRef.Scope.TenantID == "" {
				depRef.Scope = obj.Scope
			}
			included, ok := byRef[depRef]
			if !ok {
				return nil, distributionRefusal("PARTIAL_BUNDLE", refKey(depRef), "bundle omits a declared dependency", ErrPartialBundle)
			}
			if dep.Digest != "" && !strings.EqualFold(strings.TrimPrefix(dep.Digest, "sha256:"), strings.TrimPrefix(included.Digest, "sha256:")) {
				return nil, distributionRefusal("PARTIAL_BUNDLE", refKey(depRef), "dependency digest is not the pinned bundle digest", ErrPartialBundle)
			}
		}
	}
	return resolved, nil
}

// Distributor is a pure in-memory publisher. Its records are transport-
// neutral values and may be persisted or carried on offline bootstrap media.
type Distributor struct {
	keyHandle  string
	keyVersion string
	privateKey ed25519.PrivateKey
	now        func() time.Time

	mu      sync.RWMutex
	records map[Scope]DesiredStateRecord
}

func NewDistributor(keyHandle, keyVersion string, privateKey ed25519.PrivateKey, options ...any) (*Distributor, error) {
	if strings.TrimSpace(keyHandle) == "" || strings.TrimSpace(keyVersion) == "" || len(privateKey) != ed25519.PrivateKeySize {
		return nil, distributionRefusal("INVALID_SIGNING_KEY", "signing_key", "key handle, key version, and a standard Ed25519 private key are required", ErrSigningRefused)
	}
	d := &Distributor{keyHandle: strings.TrimSpace(keyHandle), keyVersion: strings.TrimSpace(keyVersion), privateKey: append(ed25519.PrivateKey(nil), privateKey...), now: func() time.Time { return time.Now().UTC() }, records: make(map[Scope]DesiredStateRecord)}
	for _, option := range options {
		if clock, ok := option.(func() time.Time); ok {
			d.now = clock
		}
	}
	return d, nil
}

// Publish signs and records one desired state. Epoch monotonicity is scoped
// to tenant/cell, allowing the same epoch to be published independently to
// two cells.
func (d *Distributor) Publish(bundle Bundle, placement tenant.Placement, epoch uint64) (DesiredStateRecord, error) {
	if d == nil {
		return DesiredStateRecord{}, distributionRefusal("NIL_DISTRIBUTOR", "distributor", "distributor is nil", ErrDistributionRefused)
	}
	if err := placement.Validate(); err != nil {
		return DesiredStateRecord{}, distributionRefusal("INVALID_PLACEMENT", "placement", "placement is invalid", ErrStalePlacement)
	}
	bundleScope := bundle.TargetScope
	if bundleScope.TenantID == "" {
		bundleScope = bundle.Scope
	}
	if bundleScope != (Scope{TenantID: placement.Tenant, CellID: placement.Cell}) {
		return DesiredStateRecord{}, distributionRefusal("SCOPE_MISMATCH", "scope", "bundle and placement scopes differ", ErrActivationScopeMismatch)
	}
	if epoch == 0 {
		return DesiredStateRecord{}, distributionRefusal("INVALID_EPOCH", "epoch", "desired-state epoch must be positive", ErrInvalidEpoch)
	}
	signed, err := SignBundle(bundle, d.keyHandle, d.keyVersion, d.privateKey)
	if err != nil {
		return DesiredStateRecord{}, err
	}
	placementDigest, err := placement.Digest()
	if err != nil {
		return DesiredStateRecord{}, distributionRefusal("INVALID_PLACEMENT", "placement", "placement cannot be digested", ErrStalePlacement)
	}
	record := DesiredStateRecord{TenantID: placement.Tenant, CellID: placement.Cell, Bundle: signed, SignedBundle: signed, Epoch: epoch, PlacementDigest: placementDigest, DesiredAt: d.now().UTC()}
	record.Digest, err = record.DigestValue()
	if err != nil {
		return DesiredStateRecord{}, err
	}
	bytes, err := digestBytes(record.Digest)
	if err != nil {
		return DesiredStateRecord{}, err
	}
	record.Signature = BundleSignature{Algorithm: "ed25519", KeyHandle: d.keyHandle, KeyVersion: d.keyVersion, KeyID: d.keyHandle, Value: hex.EncodeToString(ed25519.Sign(d.privateKey, bytes))}
	scope := Scope{TenantID: record.TenantID, CellID: record.CellID}
	d.mu.Lock()
	defer d.mu.Unlock()
	if prior, ok := d.records[scope]; ok {
		if epoch < prior.Epoch {
			return DesiredStateRecord{}, distributionRefusal("EPOCH_REPLAY", "epoch", "desired-state epoch is older than the published epoch", ErrEpochReplay)
		}
		if epoch == prior.Epoch {
			if prior.Digest == record.Digest {
				return cloneDesiredState(prior), nil
			}
			return DesiredStateRecord{}, distributionRefusal("EPOCH_CONFLICT", "epoch", "published epoch already names another desired state", ErrEpochConflict)
		}
	}
	d.records[scope] = cloneDesiredState(record)
	return cloneDesiredState(record), nil
}

func (d *Distributor) Desired(scope Scope) (DesiredStateRecord, bool) {
	if d == nil {
		return DesiredStateRecord{}, false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	record, ok := d.records[scope]
	return cloneDesiredState(record), ok
}

// PublishBundle is a descriptive alias for Publish.
func (d *Distributor) PublishBundle(bundle Bundle, placement tenant.Placement, epoch uint64) (DesiredStateRecord, error) {
	return d.Publish(bundle, placement, epoch)
}

// PublishDesiredState is a descriptive alias for Publish.
func (d *Distributor) PublishDesiredState(bundle Bundle, placement tenant.Placement, epoch uint64) (DesiredStateRecord, error) {
	return d.Publish(bundle, placement, epoch)
}

func (d *Distributor) Explain() string {
	return "desired-state distribution v1: signed tenant/cell records with monotonic epochs and placement digests"
}

func ExplainDistribution() string {
	return "desired-state distribution v1: signed records, atomic local snapshots, bounded stale use, and fail-closed expiry"
}

func (r DesiredStateRecord) DigestValue() (string, error) {
	bundle := desiredBundle(r).Bundle
	when := r.DesiredAt
	if when.IsZero() {
		when = r.PublishedAt
	}
	if strings.TrimSpace(r.TenantID) == "" || strings.TrimSpace(r.CellID) == "" || bundle.BundleID == "" || bundle.Digest == "" || r.Epoch == 0 || strings.TrimSpace(r.PlacementDigest) == "" || when.IsZero() {
		return "", distributionRefusal("INVALID_DESIRED_STATE", "record", "desired state has incomplete canonical identity", ErrDistributionRefused)
	}
	canonical := canonicalbytes.New("hcmnext.platform.configbundle.DesiredStateRecord", DistributionVersion).
		String("tenant_id", r.TenantID).String("cell_id", r.CellID).
		String("bundle_id", bundle.BundleID).String("bundle_digest", bundle.Digest).
		Int("epoch", int64(r.Epoch)).String("placement_digest", r.PlacementDigest).
		String("desired_at", when.UTC().Format(time.RFC3339Nano)).
		String("signer_key_handle", desiredBundle(r).Signature.KeyHandle).
		String("signer_key_version", desiredBundle(r).Signature.KeyVersion)
	encoded, err := canonical.Bytes()
	if err != nil {
		return "", err
	}
	return canonicalbytes.Digest(encoded), nil
}

func (r DesiredStateRecord) Verify(publicKey ed25519.PublicKey) error {
	signed := desiredBundle(r)
	if err := VerifyBundleSignature(signed, publicKey); err != nil {
		return distributionRefusal("INVALID_SIGNATURE", "bundle.signature", "bundle signature verification failed", ErrInvalidSignature)
	}
	if err := validateSignatureIdentity(r.Signature); err != nil {
		return err
	}
	if r.Signature.KeyHandle != signed.Signature.KeyHandle || r.Signature.KeyVersion != signed.Signature.KeyVersion {
		return distributionRefusal("SIGNER_MISMATCH", "signature", "record and bundle signatures name different keys", ErrInvalidSignature)
	}
	digest, err := r.DigestValue()
	if err != nil {
		return err
	}
	if digest != r.Digest {
		return distributionRefusal("DESIRED_STATE_MUTATED", "digest", "recorded desired-state digest differs from canonical content", ErrInvalidSignature)
	}
	encoded, err := digestBytes(digest)
	if err != nil {
		return err
	}
	signature, err := hex.DecodeString(r.Signature.Value)
	if err != nil || !ed25519.Verify(publicKey, encoded, signature) {
		return distributionRefusal("INVALID_SIGNATURE", "signature.value", "desired-state envelope signature verification failed", ErrInvalidSignature)
	}
	return nil
}

func desiredBundle(record DesiredStateRecord) SignedBundle {
	bundle := record.Bundle
	if bundle.Bundle.BundleID == "" {
		bundle = record.SignedBundle
	}
	return bundle
}

func cloneDesiredState(record DesiredStateRecord) DesiredStateRecord {
	record.Bundle = cloneSignedBundle(record.Bundle)
	record.SignedBundle = cloneSignedBundle(record.SignedBundle)
	return record
}

func cloneSignedBundle(signed SignedBundle) SignedBundle {
	signed.Bundle = cloneBundle(signed.Bundle)
	return signed
}

func cloneBundle(bundle Bundle) Bundle {
	bundle.Roots = append([]ObjectRef(nil), bundle.Roots...)
	bundle.Objects = append([]IncludedObject(nil), bundle.Objects...)
	bundle.Dependencies = append([]IncludedObject(nil), bundle.Dependencies...)
	return bundle
}

func cloneConfigurationObject(object ConfigurationObject) ConfigurationObject {
	object.Body = append([]byte(nil), object.Body...)
	return object
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	snapshot.Bundle = cloneBundle(snapshot.Bundle)
	snapshot.Objects = snapshot.ObjectsCopy()
	return snapshot
}

var (
	ErrDistributionRefused     = errors.New("configbundle: desired-state distribution refused")
	ErrUnknownConfig           = errors.New("configbundle: desired state references unknown configuration")
	ErrPartialBundle           = errors.New("configbundle: desired state contains a partial bundle")
	ErrStalePlacement          = errors.New("configbundle: desired state placement is stale")
	ErrInvalidPlacementContext = errors.New("configbundle: placement context is invalid")
	ErrInvalidFreshnessPolicy  = errors.New("configbundle: freshness policy is invalid")
)

func distributionRefusal(code, ref, detail string, cause error) error {
	return &Error{Code: code, Ref: ref, Detail: detail, Cause: cause}
}
