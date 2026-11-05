// Package device owns the SMB device connection boundary: versioned
// device profiles, tenant/site-fenced bindings with pairing generations,
// typed observation ingest, and generation-bound command dispatch.
//
// The boundary preserves domain ownership: an observation is evidence a
// domain operation later consumes; device acceptance never completes
// business work. Device adapters stay behind the existing read-only SPI:
// profiles conform against adapter manifests, writes need an explicit
// release allowlist backed by a signed amendment, and the P1A
// observation-only gate stays intact.
//
// The registry is hermetic: no storage, clocks, or network. Restarts
// restore through Export/Import snapshots; durable rows, RLS, and served
// transport stay with the owning integration stores and edges.
package device

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/spi"
)

var (
	ErrDeviceInvalid             = errors.New("device: device request is invalid")
	ErrDeviceNotFound            = errors.New("device: device record is not visible")
	ErrDeviceConformance         = errors.New("device: profile does not conform to its adapter")
	ErrDeviceIdentity            = errors.New("device: device identity is not trusted")
	ErrDeviceGeneration          = errors.New("device: pairing generation is stale")
	ErrDeviceTransition          = errors.New("device: lifecycle transition is not legal")
	ErrDeviceIdempotencyConflict = errors.New("device: idempotency key was reused for another request")
	ErrDeviceResponseLost        = errors.New("device: device receipt recorded but response lost")
	ErrDeviceSnapshot            = errors.New("device: snapshot cannot be restored")
)

// Read opcode vocabulary. Anything outside read./observe. is not a
// read-only operation and needs an explicit release allowlist entry.
const (
	opcodeReadPrefix    = "read."
	opcodeObservePrefix = "observe."
)

// DeviceProfile pins what one device family may do: protocol and adapter
// version, vendor/product match, supported platforms, the observation
// schema it emits, the opcodes it may receive, budgets, and the physical
// risk class. ReleaseWrites explicitly authorizes later-phase hardware
// writes; empty means observation-only.
type DeviceProfile struct {
	ProfileID         string
	Protocol          string
	AdapterVersion    string
	VendorMatch       string
	ProductMatch      string
	SupportedOS       []string
	SupportedArch     []string
	ObservationSchema string
	AllowedOperations []string
	ReleaseWrites     []string
	FrameBudgetMs     int
	QueueBudget       int
	PhysicalRisk      string
}

// DeviceFingerprint is the trusted device identity triple. A serial
// number alone is never identity: the same serial on another host or
// model never auto-pairs.
type DeviceFingerprint struct {
	Serial string
	HostID string
	Model  string
}

// DeviceBinding references one connector connection plus tenant/site,
// device identity, pairing generation, policy, and lifecycle status.
type DeviceBinding struct {
	BindingID         string
	TenantID          string
	SiteID            string
	ConnectorID       string
	ProfileID         string
	Fingerprint       DeviceFingerprint
	PairingGeneration uint64
	PriorBindingID    string
	Policy            string
	Status            connectivity.LifecycleState
	Revision          uint64
	PairedBy          string
	PairedAt          time.Time
	LastSeen          time.Time
	Name              string
}

// DeviceObservation is one typed device reading. It changes no domain
// state; a domain operation later consumes it by reference.
type DeviceObservation struct {
	ObservationID string
	BindingID     string
	Generation    uint64
	TenantID      string
	SiteID        string
	OperatorRef   string
	SessionRef    string
	CapturedAt    time.Time
	ReceivedAt    time.Time
	Sequence      uint64
	RawDigest     string
	Units         string
	Quality       string
	Schema        string
	Consumed      bool
	ConsumedBy    string
}

// DeviceCommand is one generation-bound queued opcode. Physical outcomes
// map onto operation states; acceptance is never completion.
type DeviceCommand struct {
	CommandID      string
	BindingID      string
	Generation     uint64
	Opcode         string
	Payload        string
	State          operation.State
	IdempotencyKey string
	Actor          string
	At             time.Time
}

// BindRequest pairs one device.
type BindRequest struct {
	TenantID    string
	SiteID      string
	ConnectorID string
	ProfileID   string
	Fingerprint DeviceFingerprint
	Policy      string
	Actor       string
	At          time.Time
}

// IngestRequest records one observation.
type IngestRequest struct {
	TenantID       string
	SiteID         string
	BindingID      string
	Generation     uint64
	OperatorRef    string
	SessionRef     string
	CapturedAt     time.Time
	ReceivedAt     time.Time
	Sequence       uint64
	RawDigest      string
	Units          string
	Quality        string
	Schema         string
	IdempotencyKey string
	At             time.Time
}

// CommandRequest queues one opcode.
type CommandRequest struct {
	TenantID       string
	SiteID         string
	BindingID      string
	Generation     uint64
	Opcode         string
	Payload        string
	IdempotencyKey string
	Actor          string
	At             time.Time
}

// DeviceRow is one integration-hub list row.
type DeviceRow struct {
	Name       string
	Site       string
	DeviceType string
	Connection string
	Status     string
	LastSeen   time.Time
}

// DeviceDetail is the device detail view-state.
type DeviceDetail struct {
	Name              string
	Status            string
	StatusExplanation string
	ProfileVersion    string
	SoftwareVersion   string
	SupportedActions  []string
	PairingGeneration uint64
	LastSeen          time.Time
}

type deviceEffect struct {
	Fingerprint string `json:"fingerprint"`
	ReceiptID   string `json:"receipt_id"`
}

// Registry is the concurrency-safe in-memory device port. AfterCommit is
// a fault-injection seam: when non-nil it runs after a receipt commits
// but before the response, so tests prove a lost response still replays
// without a duplicate record.
type Registry struct {
	mu           sync.Mutex
	profiles     map[string]DeviceProfile
	bindings     map[string]*DeviceBinding
	observations map[string]*DeviceObservation
	obsByBinding map[string][]string
	commands     map[string]*DeviceCommand
	effects      map[string]deviceEffect
	AfterCommit  func() error
}

// NewRegistry builds the device port.
func NewRegistry() *Registry {
	return &Registry{
		profiles:     make(map[string]DeviceProfile),
		bindings:     make(map[string]*DeviceBinding),
		observations: make(map[string]*DeviceObservation),
		obsByBinding: make(map[string][]string),
		commands:     make(map[string]*DeviceCommand),
		effects:      make(map[string]deviceEffect),
	}
}

func deviceKey(tenant, site, bindingID string) string {
	return tenant + "\x00" + site + "\x00" + bindingID
}

func cloneBinding(in DeviceBinding) DeviceBinding {
	return in
}

func cloneObservation(in DeviceObservation) DeviceObservation {
	return in
}

func checkActor(actor, tenant, site string) error {
	if strings.TrimSpace(actor) == "" || strings.TrimSpace(tenant) == "" || strings.TrimSpace(site) == "" {
		return fmt.Errorf("%w: actor, tenant, and site are required", ErrDeviceNotFound)
	}
	return nil
}

// Conform validates a profile against its adapter manifest: the manifest
// itself must publish, vendor/product/version must match exactly, and
// every allowed opcode must be a read/observe operation or an explicitly
// released write backed by a signed amendment. Unknown opcodes and
// write-through-read fail here and again at dispatch.
func Conform(profile DeviceProfile, manifest spi.AdapterManifest) error {
	if strings.TrimSpace(profile.ProfileID) == "" || strings.TrimSpace(profile.Protocol) == "" {
		return fmt.Errorf("%w: profile id and protocol are required", ErrDeviceConformance)
	}
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("%w: adapter manifest: %v", ErrDeviceConformance, err)
	}
	if profile.VendorMatch != manifest.Vendor || profile.ProductMatch != manifest.Product {
		return fmt.Errorf("%w: profile %s/%s does not match adapter %s/%s",
			ErrDeviceConformance, profile.VendorMatch, profile.ProductMatch, manifest.Vendor, manifest.Product)
	}
	if profile.AdapterVersion != manifest.Version {
		return fmt.Errorf("%w: profile pins adapter %s, manifest is %s",
			ErrDeviceConformance, profile.AdapterVersion, manifest.Version)
	}
	if len(profile.AllowedOperations) == 0 {
		return fmt.Errorf("%w: profile allows no operations", ErrDeviceConformance)
	}
	released := make(map[string]bool, len(profile.ReleaseWrites))
	for _, opcode := range profile.ReleaseWrites {
		released[opcode] = true
	}
	for _, opcode := range profile.AllowedOperations {
		switch {
		case strings.HasPrefix(opcode, opcodeReadPrefix), strings.HasPrefix(opcode, opcodeObservePrefix):
			continue
		case released[opcode]:
			if len(spi.DeclaredAmendments()) == 0 {
				return fmt.Errorf("%w: opcode %q needs a signed release amendment", ErrDeviceConformance, opcode)
			}
		default:
			return fmt.Errorf("%w: opcode %q is not declared", ErrDeviceConformance, opcode)
		}
	}
	return nil
}

// RegisterProfile stores a conformed profile. Identical re-registration
// is a no-op; a changed profile under the same id conflicts.
func (r *Registry) RegisterProfile(profile DeviceProfile, manifest spi.AdapterManifest) error {
	if err := Conform(profile, manifest); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, ok := r.profiles[profile.ProfileID]; ok {
		if fmt.Sprintf("%#v", prior) != fmt.Sprintf("%#v", profile) {
			return fmt.Errorf("%w: profile %q changed under its id", ErrDeviceInvalid, profile.ProfileID)
		}
		return nil
	}
	profile.SupportedOS = append([]string(nil), profile.SupportedOS...)
	profile.SupportedArch = append([]string(nil), profile.SupportedArch...)
	profile.AllowedOperations = append([]string(nil), profile.AllowedOperations...)
	profile.ReleaseWrites = append([]string(nil), profile.ReleaseWrites...)
	r.profiles[profile.ProfileID] = profile
	return nil
}

func (r *Registry) visibleLocked(tenant, site, bindingID string) (*DeviceBinding, bool) {
	binding, ok := r.bindings[deviceKey(tenant, site, bindingID)]
	if !ok {
		return nil, false
	}
	return binding, true
}

// Bind pairs one device at generation 1 in DRAFT. The fingerprint triple
// must be complete: a serial alone never binds.
func (r *Registry) Bind(_ context.Context, req BindRequest) (DeviceBinding, error) {
	if err := checkActor(req.Actor, req.TenantID, req.SiteID); err != nil {
		return DeviceBinding{}, err
	}
	if strings.TrimSpace(req.ConnectorID) == "" || strings.TrimSpace(req.ProfileID) == "" || strings.TrimSpace(req.Policy) == "" {
		return DeviceBinding{}, fmt.Errorf("%w: connector, profile, and policy are required", ErrDeviceInvalid)
	}
	if strings.TrimSpace(req.Fingerprint.Serial) == "" || strings.TrimSpace(req.Fingerprint.HostID) == "" || strings.TrimSpace(req.Fingerprint.Model) == "" {
		return DeviceBinding{}, fmt.Errorf("%w: serial, host, and model are all required", ErrDeviceIdentity)
	}
	if req.At.IsZero() {
		return DeviceBinding{}, fmt.Errorf("%w: bind time is required", ErrDeviceInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	profile, ok := r.profiles[req.ProfileID]
	if !ok {
		return DeviceBinding{}, fmt.Errorf("%w: profile %q is not registered", ErrDeviceInvalid, req.ProfileID)
	}
	binding := DeviceBinding{
		BindingID: uuid.NewString(), TenantID: req.TenantID, SiteID: req.SiteID,
		ConnectorID: req.ConnectorID, ProfileID: req.ProfileID, Fingerprint: req.Fingerprint,
		PairingGeneration: 1, Policy: req.Policy, Status: connectivity.StateDraft,
		Revision: 1, PairedBy: req.Actor, PairedAt: req.At,
		Name: fmt.Sprintf("%s %s", profile.ProductMatch, req.Fingerprint.Serial),
	}
	r.bindings[deviceKey(req.TenantID, req.SiteID, binding.BindingID)] = &binding
	return cloneBinding(binding), nil
}

// Transition moves one binding along the shared connection lifecycle.
// Illegal edges refuse and mutate nothing.
func (r *Registry) Transition(_ context.Context, tenant, site, bindingID, actor string, to connectivity.LifecycleState, at time.Time) (DeviceBinding, error) {
	if err := checkActor(actor, tenant, site); err != nil {
		return DeviceBinding{}, err
	}
	if at.IsZero() {
		return DeviceBinding{}, fmt.Errorf("%w: transition time is required", ErrDeviceInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	binding, visible := r.visibleLocked(tenant, site, bindingID)
	if !visible {
		return DeviceBinding{}, ErrDeviceNotFound
	}
	if !connectivity.IsLegalTransition(binding.Status, to) {
		return DeviceBinding{}, fmt.Errorf("%w: %s -> %s", ErrDeviceTransition, binding.Status, to)
	}
	binding.Status = to
	binding.Revision++
	return cloneBinding(*binding), nil
}

// Load returns one authorized binding. Foreign and missing bindings read
// identically.
func (r *Registry) Load(_ context.Context, tenant, site, bindingID, actor string) (DeviceBinding, bool) {
	if err := checkActor(actor, tenant, site); err != nil {
		return DeviceBinding{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	binding, visible := r.visibleLocked(tenant, site, bindingID)
	if !visible {
		return DeviceBinding{}, false
	}
	return cloneBinding(*binding), true
}

// Revoke ends one binding's pairing: the generation bumps and the status
// moves to REVOKED, which has no outgoing edges. Stale observations and
// queued commands under older generations refuse from here on.
func (r *Registry) Revoke(_ context.Context, tenant, site, bindingID, actor, reason string, at time.Time) (DeviceBinding, error) {
	if err := checkActor(actor, tenant, site); err != nil {
		return DeviceBinding{}, err
	}
	if strings.TrimSpace(reason) == "" || at.IsZero() {
		return DeviceBinding{}, fmt.Errorf("%w: reason and time are required", ErrDeviceInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	binding, visible := r.visibleLocked(tenant, site, bindingID)
	if !visible {
		return DeviceBinding{}, ErrDeviceNotFound
	}
	if !connectivity.IsLegalTransition(binding.Status, connectivity.StateRevoked) {
		return DeviceBinding{}, fmt.Errorf("%w: %s -> REVOKED", ErrDeviceTransition, binding.Status)
	}
	binding.Status = connectivity.StateRevoked
	binding.PairingGeneration++
	binding.Revision++
	return cloneBinding(*binding), nil
}

// Repair explicitly re-pairs new identifiers after a revoke or
// quarantine. Revoked bindings are never resurrected: repair issues a
// fresh DRAFT binding whose generation continues the lineage, so the
// same identifiers on another host pair only through this deliberate,
// audited call.
func (r *Registry) Repair(_ context.Context, tenant, site, bindingID, actor string, fingerprint DeviceFingerprint, at time.Time) (DeviceBinding, error) {
	if err := checkActor(actor, tenant, site); err != nil {
		return DeviceBinding{}, err
	}
	if strings.TrimSpace(fingerprint.Serial) == "" || strings.TrimSpace(fingerprint.HostID) == "" || strings.TrimSpace(fingerprint.Model) == "" {
		return DeviceBinding{}, fmt.Errorf("%w: serial, host, and model are all required", ErrDeviceIdentity)
	}
	if at.IsZero() {
		return DeviceBinding{}, fmt.Errorf("%w: repair time is required", ErrDeviceInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	prior, visible := r.visibleLocked(tenant, site, bindingID)
	if !visible {
		return DeviceBinding{}, ErrDeviceNotFound
	}
	if prior.Status != connectivity.StateRevoked && prior.Status != connectivity.StateQuarantined {
		return DeviceBinding{}, fmt.Errorf("%w: only a revoked or quarantined binding re-pairs", ErrDeviceTransition)
	}
	next := DeviceBinding{
		BindingID: uuid.NewString(), TenantID: prior.TenantID, SiteID: prior.SiteID,
		ConnectorID: prior.ConnectorID, ProfileID: prior.ProfileID, Fingerprint: fingerprint,
		PairingGeneration: prior.PairingGeneration + 1, PriorBindingID: prior.BindingID,
		Policy: prior.Policy, Status: connectivity.StateDraft,
		Revision: 1, PairedBy: actor, PairedAt: at, Name: prior.Name,
	}
	r.bindings[deviceKey(tenant, site, next.BindingID)] = &next
	return cloneBinding(next), nil
}

// Ingest records one typed observation. The binding must be active at the
// observation's generation with a strictly increasing sequence; the
// operator must be a human or session principal, never the device
// itself. Ingest changes zero domain state.
func (r *Registry) Ingest(_ context.Context, req IngestRequest) (DeviceObservation, error) {
	if err := checkActor(req.OperatorRef, req.TenantID, req.SiteID); err != nil {
		return DeviceObservation{}, err
	}
	if strings.HasPrefix(req.OperatorRef, "device:") {
		return DeviceObservation{}, fmt.Errorf("%w: device identity cannot impersonate the operator", ErrDeviceIdentity)
	}
	if strings.TrimSpace(req.BindingID) == "" || strings.TrimSpace(req.RawDigest) == "" || strings.TrimSpace(req.Schema) == "" {
		return DeviceObservation{}, fmt.Errorf("%w: binding, digest, and schema are required", ErrDeviceInvalid)
	}
	if req.CapturedAt.IsZero() || req.ReceivedAt.IsZero() || req.At.IsZero() || strings.TrimSpace(req.IdempotencyKey) == "" {
		return DeviceObservation{}, fmt.Errorf("%w: capture/receipt times and idempotency key are required", ErrDeviceInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	binding, visible := r.visibleLocked(req.TenantID, req.SiteID, req.BindingID)
	if !visible {
		return DeviceObservation{}, ErrDeviceNotFound
	}
	if req.Generation != binding.PairingGeneration {
		return DeviceObservation{}, fmt.Errorf("%w: observation cites %d, binding is at %d", ErrDeviceGeneration, req.Generation, binding.PairingGeneration)
	}
	if binding.Status != connectivity.StateActive && binding.Status != connectivity.StateDegraded {
		return DeviceObservation{}, fmt.Errorf("%w: binding is %s", ErrDeviceTransition, binding.Status)
	}
	profile := r.profiles[binding.ProfileID]
	if req.Schema != profile.ObservationSchema {
		return DeviceObservation{}, fmt.Errorf("%w: schema %q is not %q", ErrDeviceInvalid, req.Schema, profile.ObservationSchema)
	}
	fingerprint := strings.Join([]string{req.TenantID, req.SiteID, req.BindingID, fmt.Sprint(req.Generation),
		fmt.Sprint(req.Sequence), req.RawDigest, req.Units, req.Quality, req.Schema}, "\x00")
	if prior, ok := r.effects[req.IdempotencyKey]; ok {
		if prior.Fingerprint != fingerprint {
			return DeviceObservation{}, fmt.Errorf("%w: key was reused for another observation", ErrDeviceIdempotencyConflict)
		}
		return cloneObservation(*r.observations[prior.ReceiptID]), nil
	}
	for _, id := range r.obsByBinding[deviceKey(req.TenantID, req.SiteID, req.BindingID)] {
		if r.observations[id].Sequence >= req.Sequence {
			return DeviceObservation{}, fmt.Errorf("%w: sequence %d is not increasing", ErrDeviceInvalid, req.Sequence)
		}
	}
	obs := DeviceObservation{
		ObservationID: uuid.NewString(), BindingID: req.BindingID, Generation: req.Generation,
		TenantID: req.TenantID, SiteID: req.SiteID, OperatorRef: req.OperatorRef, SessionRef: req.SessionRef,
		CapturedAt: req.CapturedAt, ReceivedAt: req.ReceivedAt, Sequence: req.Sequence,
		RawDigest: req.RawDigest, Units: req.Units, Quality: req.Quality, Schema: req.Schema,
	}
	r.observations[obs.ObservationID] = &obs
	key := deviceKey(req.TenantID, req.SiteID, req.BindingID)
	r.obsByBinding[key] = append(r.obsByBinding[key], obs.ObservationID)
	r.effects[req.IdempotencyKey] = deviceEffect{Fingerprint: fingerprint, ReceiptID: obs.ObservationID}
	binding.LastSeen = req.ReceivedAt
	out := cloneObservation(obs)
	if r.AfterCommit != nil {
		if crash := r.AfterCommit(); crash != nil {
			return DeviceObservation{}, fmt.Errorf("%w: %w", ErrDeviceResponseLost, crash)
		}
	}
	return out, nil
}

// PendingObservations is the domain-consumer pull surface: the stock or
// job owner lists unconsumed observations in sequence order and converts
// them into intent through its own authorized action. Devices never push
// business effects.
func (r *Registry) PendingObservations(_ context.Context, tenant, site, bindingID, actor string) ([]DeviceObservation, error) {
	if err := checkActor(actor, tenant, site); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, visible := r.visibleLocked(tenant, site, bindingID); !visible {
		return nil, ErrDeviceNotFound
	}
	var pending []DeviceObservation
	for _, id := range r.obsByBinding[deviceKey(tenant, site, bindingID)] {
		obs := r.observations[id]
		if !obs.Consumed {
			pending = append(pending, cloneObservation(*obs))
		}
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].Sequence < pending[j].Sequence })
	return pending, nil
}

// ConsumeObservation records one domain operation's conversion of an
// observation into intent. The consumer ref (a stock operation, a job
// step) is the audit link between evidence and effect.
func (r *Registry) ConsumeObservation(_ context.Context, tenant, site, bindingID, observationID, consumerRef, actor string, at time.Time) (DeviceObservation, error) {
	if err := checkActor(actor, tenant, site); err != nil {
		return DeviceObservation{}, err
	}
	if strings.TrimSpace(consumerRef) == "" || at.IsZero() {
		return DeviceObservation{}, fmt.Errorf("%w: consumer ref and time are required", ErrDeviceInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, visible := r.visibleLocked(tenant, site, bindingID); !visible {
		return DeviceObservation{}, ErrDeviceNotFound
	}
	obs, ok := r.observations[observationID]
	if !ok || obs.BindingID != bindingID || obs.TenantID != tenant || obs.SiteID != site {
		return DeviceObservation{}, ErrDeviceNotFound
	}
	if obs.Consumed {
		return DeviceObservation{}, fmt.Errorf("%w: observation is already consumed by %s", ErrDeviceInvalid, obs.ConsumedBy)
	}
	obs.Consumed, obs.ConsumedBy = true, consumerRef
	return cloneObservation(*obs), nil
}

// QueueCommand queues one opcode for dispatch. The opcode must be in the
// profile's allowlist with a live generation; unreleased writes and
// unknown opcodes fail dispatch exactly as they fail conformance, so the
// read-only adapter boundary holds at runtime too.
func (r *Registry) QueueCommand(_ context.Context, req CommandRequest) (DeviceCommand, error) {
	if err := checkActor(req.Actor, req.TenantID, req.SiteID); err != nil {
		return DeviceCommand{}, err
	}
	if strings.TrimSpace(req.BindingID) == "" || strings.TrimSpace(req.Opcode) == "" || strings.TrimSpace(req.IdempotencyKey) == "" || req.At.IsZero() {
		return DeviceCommand{}, fmt.Errorf("%w: binding, opcode, key, and time are required", ErrDeviceInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	binding, visible := r.visibleLocked(req.TenantID, req.SiteID, req.BindingID)
	if !visible {
		return DeviceCommand{}, ErrDeviceNotFound
	}
	if req.Generation != binding.PairingGeneration {
		return DeviceCommand{}, fmt.Errorf("%w: command cites %d, binding is at %d", ErrDeviceGeneration, req.Generation, binding.PairingGeneration)
	}
	if binding.Status != connectivity.StateActive && binding.Status != connectivity.StateDegraded {
		return DeviceCommand{}, fmt.Errorf("%w: binding is %s", ErrDeviceTransition, binding.Status)
	}
	profile := r.profiles[binding.ProfileID]
	allowed, released := false, false
	for _, opcode := range profile.AllowedOperations {
		if opcode == req.Opcode {
			allowed = true
		}
	}
	for _, opcode := range profile.ReleaseWrites {
		if opcode == req.Opcode {
			released = true
		}
	}
	if !allowed {
		return DeviceCommand{}, fmt.Errorf("%w: opcode %q is not in the profile allowlist", ErrDeviceConformance, req.Opcode)
	}
	if released && len(spi.DeclaredAmendments()) == 0 {
		return DeviceCommand{}, fmt.Errorf("%w: opcode %q needs a signed release amendment", ErrDeviceConformance, req.Opcode)
	}
	fingerprint := strings.Join([]string{req.TenantID, req.SiteID, req.BindingID, fmt.Sprint(req.Generation), req.Opcode, req.Payload}, "\x00")
	if prior, ok := r.effects[req.IdempotencyKey]; ok {
		if prior.Fingerprint != fingerprint {
			return DeviceCommand{}, fmt.Errorf("%w: key was reused for another command", ErrDeviceIdempotencyConflict)
		}
		return *r.commands[prior.ReceiptID], nil
	}
	cmd := DeviceCommand{
		CommandID: uuid.NewString(), BindingID: req.BindingID, Generation: req.Generation,
		Opcode: req.Opcode, Payload: req.Payload, State: operation.StateQueued,
		IdempotencyKey: req.IdempotencyKey, Actor: req.Actor, At: req.At,
	}
	r.commands[cmd.CommandID] = &cmd
	r.effects[req.IdempotencyKey] = deviceEffect{Fingerprint: fingerprint, ReceiptID: cmd.CommandID}
	out := cmd
	if r.AfterCommit != nil {
		if crash := r.AfterCommit(); crash != nil {
			return DeviceCommand{}, fmt.Errorf("%w: %w", ErrDeviceResponseLost, crash)
		}
	}
	return out, nil
}

// DeviceList serves the hub list: one row per binding at the caller's
// site with every required column populated.
func (r *Registry) DeviceList(_ context.Context, tenant, site, actor string) ([]DeviceRow, error) {
	if err := checkActor(actor, tenant, site); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var rows []DeviceRow
	for key, binding := range r.bindings {
		if !strings.HasPrefix(key, tenant+"\x00"+site+"\x00") {
			continue
		}
		profile := r.profiles[binding.ProfileID]
		rows = append(rows, DeviceRow{
			Name:       binding.Name,
			Site:       binding.SiteID,
			DeviceType: profile.ProductMatch,
			Connection: profile.Protocol,
			Status:     string(binding.Status),
			LastSeen:   binding.LastSeen,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows, nil
}

// DeviceDetail serves the device detail view-state: supported actions,
// software/profile versions, and a truthful status explanation.
func (r *Registry) DeviceDetail(_ context.Context, tenant, site, bindingID, actor string) (DeviceDetail, bool) {
	if err := checkActor(actor, tenant, site); err != nil {
		return DeviceDetail{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	binding, visible := r.visibleLocked(tenant, site, bindingID)
	if !visible {
		return DeviceDetail{}, false
	}
	profile := r.profiles[binding.ProfileID]
	return DeviceDetail{
		Name:              binding.Name,
		Status:            string(binding.Status),
		StatusExplanation: deviceStatusExplanation(binding.Status),
		ProfileVersion:    profile.ProfileID + "@" + profile.AdapterVersion,
		SoftwareVersion:   profile.AdapterVersion,
		SupportedActions:  append([]string(nil), profile.AllowedOperations...),
		PairingGeneration: binding.PairingGeneration,
		LastSeen:          binding.LastSeen,
	}, true
}

func deviceStatusExplanation(status connectivity.LifecycleState) string {
	switch status {
	case connectivity.StateDraft:
		return "Paired but not yet validated; complete validation before use."
	case connectivity.StateValidating:
		return "Validating credentials, scope, and profile conformance."
	case connectivity.StateReady:
		return "Validated and ready; activate to accept observations."
	case connectivity.StateActive:
		return "Active; observations are accepted under the current pairing."
	case connectivity.StateDegraded:
		return "Active with reduced function; observations are accepted, commands may queue."
	case connectivity.StateSuspended:
		return "Suspended; no observations or commands until re-enabled."
	case connectivity.StateQuarantined:
		return "Held out of use pending review."
	case connectivity.StateRevoked:
		return "Revoked; re-pair explicitly to use this device again."
	default:
		return "Unknown status."
	}
}

// UnsupportedDeviceFallback guides operators of unlisted hardware to
// supported manual paths. It never suggests disabling security.
func UnsupportedDeviceFallback(deviceName, capability string) string {
	return fmt.Sprintf("%s is not a supported device for %s. Use keyboard mode or manual entry to continue without it.", deviceName, capability)
}

type deviceSnapshot struct {
	Profiles     map[string]DeviceProfile     `json:"profiles"`
	Bindings     map[string]DeviceBinding     `json:"bindings"`
	Observations map[string]DeviceObservation `json:"observations"`
	ObsByBinding map[string][]string          `json:"obs_by_binding"`
	Commands     map[string]DeviceCommand     `json:"commands"`
	Effects      map[string]deviceEffect      `json:"effects"`
}

// Export captures the registry for restart recovery.
func (r *Registry) Export() ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot := deviceSnapshot{
		Profiles:     make(map[string]DeviceProfile, len(r.profiles)),
		Bindings:     make(map[string]DeviceBinding, len(r.bindings)),
		Observations: make(map[string]DeviceObservation, len(r.observations)),
		ObsByBinding: make(map[string][]string, len(r.obsByBinding)),
		Commands:     make(map[string]DeviceCommand, len(r.commands)),
		Effects:      make(map[string]deviceEffect, len(r.effects)),
	}
	for id, profile := range r.profiles {
		profile.SupportedOS = append([]string(nil), profile.SupportedOS...)
		profile.SupportedArch = append([]string(nil), profile.SupportedArch...)
		profile.AllowedOperations = append([]string(nil), profile.AllowedOperations...)
		profile.ReleaseWrites = append([]string(nil), profile.ReleaseWrites...)
		snapshot.Profiles[id] = profile
	}
	for key, binding := range r.bindings {
		snapshot.Bindings[key] = cloneBinding(*binding)
	}
	for id, obs := range r.observations {
		snapshot.Observations[id] = cloneObservation(*obs)
	}
	for key, ids := range r.obsByBinding {
		snapshot.ObsByBinding[key] = append([]string(nil), ids...)
	}
	for id, cmd := range r.commands {
		snapshot.Commands[id] = *cmd
	}
	for key, effect := range r.effects {
		snapshot.Effects[key] = effect
	}
	return json.Marshal(snapshot)
}

// Import restores a snapshot previously captured by Export. Lifecycle
// states, generations, and references revalidate so corrupt snapshots
// fail closed.
func (r *Registry) Import(data []byte) error {
	var snapshot deviceSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("%w: snapshot is not valid device state: %v", ErrDeviceSnapshot, err)
	}
	for key, binding := range snapshot.Bindings {
		if binding.BindingID == "" || binding.TenantID == "" || binding.SiteID == "" || binding.PairingGeneration == 0 {
			return fmt.Errorf("%w: snapshot binding is incomplete", ErrDeviceSnapshot)
		}
		if key != deviceKey(binding.TenantID, binding.SiteID, binding.BindingID) {
			return fmt.Errorf("%w: snapshot key does not match its binding", ErrDeviceSnapshot)
		}
		legal := false
		for _, state := range connectivity.LifecycleStates() {
			if binding.Status == state {
				legal = true
			}
		}
		if !legal {
			return fmt.Errorf("%w: snapshot status %q is not declared", ErrDeviceSnapshot, binding.Status)
		}
	}
	for id, obs := range snapshot.Observations {
		if id == "" || obs.BindingID == "" || obs.TenantID == "" {
			return fmt.Errorf("%w: snapshot observation is incomplete", ErrDeviceSnapshot)
		}
	}
	for key, effect := range snapshot.Effects {
		if key == "" || effect.Fingerprint == "" || effect.ReceiptID == "" {
			return fmt.Errorf("%w: snapshot idempotency record is incomplete", ErrDeviceSnapshot)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.profiles == nil {
		r.profiles = make(map[string]DeviceProfile)
	}
	if r.bindings == nil {
		r.bindings = make(map[string]*DeviceBinding)
	}
	if r.observations == nil {
		r.observations = make(map[string]*DeviceObservation)
	}
	if r.obsByBinding == nil {
		r.obsByBinding = make(map[string][]string)
	}
	if r.commands == nil {
		r.commands = make(map[string]*DeviceCommand)
	}
	if r.effects == nil {
		r.effects = make(map[string]deviceEffect)
	}
	for id, profile := range snapshot.Profiles {
		stored := profile
		r.profiles[id] = stored
	}
	for key, binding := range snapshot.Bindings {
		stored := binding
		r.bindings[key] = &stored
	}
	for key, ids := range snapshot.ObsByBinding {
		r.obsByBinding[key] = append([]string(nil), ids...)
	}
	for id, obs := range snapshot.Observations {
		stored := obs
		r.observations[id] = &stored
	}
	for id, cmd := range snapshot.Commands {
		stored := cmd
		r.commands[id] = &stored
	}
	for key, effect := range snapshot.Effects {
		r.effects[key] = effect
	}
	return nil
}
