package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Manager enforces current owner policy before any read, write, export, or
// disposition. It is safe only when every raw and derived copy surface is
// registered in Config.Stores.
type Manager struct {
	mu          sync.RWMutex
	policies    PolicyResolver
	sources     SourceResolver
	authorizer  Authorizer
	disposition DispositionResolver
	stores      []CopyStore
	now         func() time.Time
	blocked     map[string]struct{}
}

// NewManager requires every security and owner-policy port plus an injected
// clock. It refuses partial wiring rather than silently skipping a control.
func NewManager(config Config, now func() time.Time) (*Manager, error) {
	if config.Policies == nil || config.Sources == nil || config.Authorizer == nil || config.Disposition == nil || now == nil || len(config.Stores) == 0 {
		return nil, fmt.Errorf("%w: all owner ports, clock, and copy stores are required", ErrInvalid)
	}
	seen := make(map[string]struct{}, len(config.Stores))
	stores := make([]CopyStore, 0, len(config.Stores))
	for _, store := range config.Stores {
		if store == nil || strings.TrimSpace(store.Name()) == "" {
			return nil, fmt.Errorf("%w: every copy store needs a name", ErrInvalid)
		}
		if _, ok := seen[store.Name()]; ok {
			return nil, fmt.Errorf("%w: duplicate copy store %q", ErrInvalid, store.Name())
		}
		seen[store.Name()] = struct{}{}
		stores = append(stores, store)
	}
	return &Manager{policies: config.Policies, sources: config.Sources, authorizer: config.Authorizer, disposition: config.Disposition, stores: stores, now: now, blocked: make(map[string]struct{})}, nil
}

// Put writes a proposed derived item only after its owner, policy, source,
// audience, and actor have all been checked. A failed multi-store write is
// rolled back; an incomplete rollback is returned as an error.
func (m *Manager) Put(ctx context.Context, actor Actor, item Item) error {
	if err := m.validateRequest(actor, item); err != nil {
		return err
	}
	if m.isBlocked(item.TenantID, "ITEM", item.ID) || m.isBlocked(item.TenantID, "SOURCE", SourceTarget(item.SourceOwner, item.SourceID)) {
		return ErrStale
	}
	if err := m.checkNotInvalidated(ctx, item); err != nil {
		return err
	}
	if err := m.validatePolicy(ctx, item); err != nil {
		return err
	}
	if err := m.checkSource(ctx, item); err != nil {
		return err
	}
	if err := m.authorizer.AuthorizeMemory(ctx, actor, OperationWrite, cloneItem(item)); err != nil {
		return errors.Join(ErrDenied, err)
	}
	written := make([]CopyStore, 0, len(m.stores))
	for _, store := range m.stores {
		if err := store.Put(ctx, cloneItem(item)); err != nil {
			m.block(item.TenantID, "ITEM", item.ID)
			tombstoneErr := m.recordInvalidation(ctx, actor, "ITEM", item.ID, "WRITE_ABORTED", OperationWrite)
			rollbackErr := m.rollback(ctx, item, written)
			return errors.Join(fmt.Errorf("agent memory: put in %s: %w", store.Name(), err), tombstoneErr, rollbackErr)
		}
		written = append(written, store)
	}
	return nil
}

// Read returns a copy only when current owner policy, actor authorization,
// source version, audience, invalidators, TTL, and all copy-store tombstones
// still agree. Stale-source cleanup is best-effort after access is denied.
func (m *Manager) Read(ctx context.Context, actor Actor, storeName, itemID string) (Item, error) {
	if m == nil || actor.TenantID == "" || actor.PrincipalID == "" || storeName == "" || itemID == "" {
		return Item{}, ErrInvalid
	}
	store, ok := m.store(storeName)
	if !ok {
		return Item{}, ErrNotFound
	}
	item, err := store.Get(ctx, actor.TenantID, itemID)
	if err != nil {
		return Item{}, fmt.Errorf("agent memory: read %s: %w", storeName, err)
	}
	if item.TenantID != actor.TenantID || item.ID != itemID {
		return Item{}, ErrDenied
	}
	if m.isBlocked(item.TenantID, "ITEM", item.ID) || m.isBlocked(item.TenantID, "SOURCE", SourceTarget(item.SourceOwner, item.SourceID)) {
		return Item{}, ErrStale
	}
	if err := validateItem(item); err != nil {
		return Item{}, err
	}
	if !m.now().UTC().Before(item.CreatedAt.Add(item.TTL)) {
		return Item{}, ErrExpired
	}
	if err := m.validatePolicy(ctx, item); err != nil {
		return Item{}, err
	}
	if err := m.authorizer.AuthorizeMemory(ctx, actor, OperationRead, cloneItem(item)); err != nil {
		return Item{}, errors.Join(ErrDenied, err)
	}
	if err := m.checkNotInvalidated(ctx, item); err != nil {
		if errors.Is(err, ErrStale) {
			return Item{}, errors.Join(err, m.purge(ctx, item, "SOURCE_REVOKED"))
		}
		return Item{}, err
	}
	if err := m.checkSource(ctx, item); err != nil {
		if errors.Is(err, ErrStale) {
			return Item{}, errors.Join(err, m.purge(ctx, item, "SOURCE_REVOKED"))
		}
		return Item{}, err
	}
	return cloneItem(item), nil
}

// RevokeSource first records a tenant-scoped invalidation in every registered
// store, then removes copies the records owner has dispositioned. If an
// inventory or deletion fails, the invalidation remains and reads fail closed.
func (m *Manager) RevokeSource(ctx context.Context, actor Actor, pin SourcePin, reason string) error {
	if m == nil || actor.TenantID == "" || actor.PrincipalID == "" || pin.TenantID == "" || pin.ID == "" || reason == "" || actor.TenantID != pin.TenantID {
		return ErrInvalid
	}
	if err := validatePin(pin); err != nil {
		return err
	}
	control := Item{TenantID: pin.TenantID, OwnerID: pin.Owner, Purpose: pin.Purpose, SourceOwner: pin.Owner, SourceID: pin.ID, SourceVersion: pin.Version, SourceDigest: pin.Digest, Audience: slices.Clone(pin.Audience), Invalidators: slices.Clone(pin.Invalidators)}
	if err := m.authorizer.AuthorizeMemory(ctx, actor, OperationRevoke, control); err != nil {
		return errors.Join(ErrDenied, err)
	}
	m.block(pin.TenantID, "SOURCE", SourceTarget(pin.Owner, pin.ID))
	if err := m.recordInvalidation(ctx, actor, "SOURCE", SourceTarget(pin.Owner, pin.ID), reason, OperationRevoke); err != nil {
		return err
	}
	var all []Item
	for _, store := range m.stores {
		inventory, err := store.ListSource(ctx, pin.TenantID, pin.Owner, pin.ID)
		if err != nil {
			return errors.Join(ErrIncomplete, fmt.Errorf("%s inventory: %w", store.Name(), err))
		}
		if !complete(inventory) {
			return fmt.Errorf("%w: %s source inventory has no complete watermark", ErrIncomplete, store.Name())
		}
		for _, item := range inventory.Items {
			if item.TenantID != pin.TenantID || item.SourceOwner != pin.Owner || item.SourceID != pin.ID {
				return errors.Join(ErrIncomplete, ErrDenied)
			}
			all = append(all, item)
		}
	}
	all, err := uniqueItems(all)
	if err != nil {
		return err
	}
	return m.deleteRevokedCopies(ctx, all)
}

// Delete removes one derived item under current actor and records policy.
// Tombstones are written before bytes are removed, so partial failures deny
// reads rather than resurrecting a surviving cache copy.
func (m *Manager) Delete(ctx context.Context, actor Actor, itemID, reason string) error {
	if m == nil || actor.TenantID == "" || actor.PrincipalID == "" || itemID == "" || reason == "" {
		return ErrInvalid
	}
	items, err := m.itemsByID(ctx, actor.TenantID, itemID)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return ErrNotFound
	}
	item := items[0]
	if err := m.authorizer.AuthorizeMemory(ctx, actor, OperationDelete, cloneItem(item)); err != nil {
		return errors.Join(ErrDenied, err)
	}
	if err := m.dispositionAllowed(ctx, item); err != nil {
		return err
	}
	return m.deleteCopies(ctx, actor, items, "ITEM", itemID, reason, OperationDelete)
}

// Sweep applies owner disposition to each unique tenant item. TTL expiry
// prevents reads, but it never authorizes destruction by itself.
func (m *Manager) Sweep(ctx context.Context, actor Actor) error {
	if m == nil || actor.TenantID == "" || actor.PrincipalID == "" {
		return ErrInvalid
	}
	items, err := m.itemsForTenant(ctx, actor.TenantID)
	if err != nil {
		return err
	}
	var failures []error
	for _, item := range items {
		decision, err := m.disposition.ResolveMemoryDisposition(ctx, cloneItem(item), m.now().UTC())
		if err != nil {
			failures = append(failures, fmt.Errorf("%s disposition: %w", item.ID, err))
			continue
		}
		if !decision.Resolved {
			failures = append(failures, fmt.Errorf("%s: %w", item.ID, ErrDisposition))
			continue
		}
		if decision.Held || !decision.CanDelete {
			continue
		}
		if err := m.authorizer.AuthorizeMemory(ctx, actor, OperationDelete, cloneItem(item)); err != nil {
			failures = append(failures, errors.Join(ErrDenied, err))
			continue
		}
		if err := m.deleteCopies(ctx, actor, []Item{item}, "ITEM", item.ID, decision.Reason, OperationDelete); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// ExportTenant returns a deterministic complete inventory, including prompt
// and tool-result records. A missing store, stale source, malformed cross-
// tenant row, or unauthorized record fails the entire export without a
// partial response.
func (m *Manager) ExportTenant(ctx context.Context, actor Actor) ([]Item, error) {
	if m == nil || actor.TenantID == "" || actor.PrincipalID == "" {
		return nil, ErrInvalid
	}
	items, err := m.itemsForTenant(ctx, actor.TenantID)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if err := validateItem(item); err != nil {
			return nil, errors.Join(ErrIncomplete, err)
		}
		if err := m.validatePolicy(ctx, item); err != nil {
			return nil, errors.Join(ErrIncomplete, err)
		}
		if err := m.authorizer.AuthorizeMemory(ctx, actor, OperationExport, cloneItem(item)); err != nil {
			return nil, errors.Join(ErrDenied, err)
		}
		if err := m.checkNotInvalidated(ctx, item); err != nil {
			return nil, errors.Join(ErrIncomplete, err)
		}
		if err := m.checkSource(ctx, item); err != nil {
			return nil, errors.Join(ErrIncomplete, err)
		}
	}
	return cloneItems(items), nil
}

// MarshalExport encodes a stable JSON array so signed tenant exports do not
// depend on store enumeration order.
func MarshalExport(items []Item) ([]byte, error) {
	ordered := cloneItems(items)
	for _, item := range ordered {
		if err := validateItem(item); err != nil {
			return nil, err
		}
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	return json.Marshal(ordered)
}

func (m *Manager) validateRequest(actor Actor, item Item) error {
	if m == nil || actor.TenantID == "" || actor.PrincipalID == "" || actor.TenantID != item.TenantID {
		return ErrDenied
	}
	if err := validateItem(item); err != nil {
		return err
	}
	if item.CreatedAt.After(m.now().UTC()) {
		return ErrInvalid
	}
	return nil
}

func (m *Manager) validatePolicy(ctx context.Context, item Item) error {
	policy, err := m.policies.ResolveMemoryPolicy(ctx, item.TenantID, item.OwnerID, item.Purpose)
	if err != nil {
		return fmt.Errorf("agent memory: resolve owner policy: %w", err)
	}
	if policy.TenantID != item.TenantID || policy.OwnerID != item.OwnerID || policy.Purpose != item.Purpose || policy.Version == "" || policy.RetentionPolicyID == "" || policy.RetentionVersion == "" {
		return ErrStale
	}
	if item.RetentionPolicyID != policy.RetentionPolicyID || item.RetentionVersion != policy.RetentionVersion || item.TTL > policy.MaxTTL || policy.MaxTTL <= 0 || !contains(policy.AllowedClasses, item.DataClass) || !containsAll(policy.AllowedAudiences, item.Audience) {
		return ErrDenied
	}
	return nil
}

func (m *Manager) checkSource(ctx context.Context, item Item) error {
	decision, err := m.sources.CheckMemorySource(ctx, sourcePin(item), item.Purpose)
	if err != nil {
		return fmt.Errorf("agent memory: source recheck: %w", err)
	}
	if !decision.Current || decision.Version != item.SourceVersion || decision.Digest != item.SourceDigest || decision.DataClass != item.DataClass || !sameSet(decision.Audience, item.Audience) || !sameSet(decision.Invalidators, item.Invalidators) {
		return ErrStale
	}
	return nil
}

func (m *Manager) checkNotInvalidated(ctx context.Context, item Item) error {
	if m.isBlocked(item.TenantID, "SOURCE", SourceTarget(item.SourceOwner, item.SourceID)) || m.isBlocked(item.TenantID, "ITEM", item.ID) {
		return ErrStale
	}
	for _, store := range m.stores {
		for _, target := range []struct{ kind, id string }{{"SOURCE", SourceTarget(item.SourceOwner, item.SourceID)}, {"ITEM", item.ID}} {
			invalid, err := store.Invalidated(ctx, item.TenantID, target.kind, target.id)
			if err != nil {
				return fmt.Errorf("agent memory: check %s invalidation: %w", store.Name(), err)
			}
			if invalid {
				return ErrStale
			}
		}
	}
	return nil
}

func (m *Manager) recordInvalidation(ctx context.Context, actor Actor, kind, id, reason string, operation Operation) error {
	if actor.TenantID == "" || actor.PrincipalID == "" || kind == "" || id == "" || reason == "" || operation == "" {
		return ErrInvalid
	}
	var failures []error
	for _, store := range m.stores {
		receipt := Invalidation{TenantID: actor.TenantID, TargetKind: kind, TargetID: id, ActorID: actor.PrincipalID, Reason: reason, Operation: operation, OccurredAt: m.now().UTC()}
		if err := store.RecordInvalidation(ctx, receipt); err != nil {
			failures = append(failures, fmt.Errorf("agent memory: tombstone %s: %w", store.Name(), err))
		}
	}
	if len(failures) != 0 {
		return errors.Join(ErrIncomplete, errors.Join(failures...))
	}
	return nil
}

func (m *Manager) purge(ctx context.Context, item Item, reason string) error {
	// A stale copy is not authority to permanently revoke every later source
	// version. Explicit RevokeSource owns the broader source fence.
	m.block(item.TenantID, "ITEM", item.ID)
	actor := Actor{TenantID: item.TenantID, PrincipalID: "agent-memory-maintenance"}
	if err := m.recordInvalidation(ctx, actor, "ITEM", item.ID, reason, OperationMaintenance); err != nil {
		return err
	}
	return m.deleteCopies(ctx, actor, []Item{item}, "ITEM", item.ID, reason, OperationMaintenance)
}

func (m *Manager) deleteCopies(ctx context.Context, actor Actor, items []Item, kind, target, reason string, operation Operation) error {
	if len(items) == 0 {
		return nil
	}
	m.block(itemsTenant(items), kind, target)
	if actor.TenantID != itemsTenant(items) {
		return ErrDenied
	}
	if err := m.recordInvalidation(ctx, actor, kind, target, reason, operation); err != nil {
		return err
	}
	var failures []error
	for _, store := range m.stores {
		for _, item := range items {
			if err := store.Delete(ctx, item.TenantID, item.ID); err != nil {
				failures = append(failures, fmt.Errorf("agent memory: delete %s/%s: %w", store.Name(), item.ID, err))
			}
		}
	}
	if len(failures) != 0 {
		return errors.Join(ErrIncomplete, errors.Join(failures...))
	}
	return nil
}

// deleteRevokedCopies removes only copies that the records owner has
// dispositioned. The source tombstone already hides every copy, including
// held records that must remain stored and unavailable until a later sweep.
func (m *Manager) deleteRevokedCopies(ctx context.Context, items []Item) error {
	var failures []error
	for _, item := range items {
		decision, err := m.disposition.ResolveMemoryDisposition(ctx, cloneItem(item), m.now().UTC())
		if err != nil {
			failures = append(failures, fmt.Errorf("%s disposition: %w", item.ID, err))
			continue
		}
		if !decision.Resolved {
			failures = append(failures, fmt.Errorf("%s: %w", item.ID, ErrDisposition))
			continue
		}
		if decision.Held || !decision.CanDelete {
			continue
		}
		dispositionActor := Actor{TenantID: item.TenantID, PrincipalID: "records-disposition"}
		if err := m.recordInvalidation(ctx, dispositionActor, "ITEM", item.ID, decision.Reason, OperationDelete); err != nil {
			failures = append(failures, err)
			continue
		}
		if err := m.deleteBytes(ctx, item); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (m *Manager) deleteBytes(ctx context.Context, item Item) error {
	var failures []error
	for _, store := range m.stores {
		if err := store.Delete(ctx, item.TenantID, item.ID); err != nil {
			failures = append(failures, fmt.Errorf("agent memory: delete %s/%s: %w", store.Name(), item.ID, err))
		}
	}
	if len(failures) != 0 {
		return errors.Join(ErrIncomplete, errors.Join(failures...))
	}
	return nil
}

func (m *Manager) dispositionAllowed(ctx context.Context, item Item) error {
	decision, err := m.disposition.ResolveMemoryDisposition(ctx, cloneItem(item), m.now().UTC())
	if err != nil {
		return fmt.Errorf("agent memory: owner disposition: %w", err)
	}
	if !decision.Resolved {
		return ErrDisposition
	}
	if decision.Held {
		return ErrHeld
	}
	if !decision.CanDelete {
		return ErrDisposition
	}
	return nil
}

func (m *Manager) itemsForTenant(ctx context.Context, tenant string) ([]Item, error) {
	var items []Item
	for _, store := range m.stores {
		inventory, err := store.ListTenant(ctx, tenant)
		if err != nil {
			return nil, errors.Join(ErrIncomplete, fmt.Errorf("%s tenant inventory: %w", store.Name(), err))
		}
		if !complete(inventory) {
			return nil, fmt.Errorf("%w: %s tenant inventory has no complete watermark", ErrIncomplete, store.Name())
		}
		for _, item := range inventory.Items {
			if item.TenantID != tenant {
				return nil, errors.Join(ErrIncomplete, ErrDenied)
			}
			items = append(items, item)
		}
	}
	return uniqueItems(items)
}

func (m *Manager) itemsByID(ctx context.Context, tenant, id string) ([]Item, error) {
	var items []Item
	for _, store := range m.stores {
		inventory, err := store.ListItem(ctx, tenant, id)
		if err != nil {
			return nil, errors.Join(ErrIncomplete, fmt.Errorf("%s item inventory: %w", store.Name(), err))
		}
		if !complete(inventory) {
			return nil, fmt.Errorf("%w: %s item inventory has no complete watermark", ErrIncomplete, store.Name())
		}
		for _, item := range inventory.Items {
			if item.TenantID != tenant || item.ID != id {
				return nil, errors.Join(ErrIncomplete, ErrDenied)
			}
			items = append(items, item)
		}
	}
	return uniqueItems(items)
}

func (m *Manager) store(name string) (CopyStore, bool) {
	for _, store := range m.stores {
		if store.Name() == name {
			return store, true
		}
	}
	return nil, false
}

// block is a process-local safety latch used while durable tombstones are
// being written. Production CopyStore implementations must persist tombstones
// so the fence survives restart.
func (m *Manager) block(tenant, kind, id string) {
	m.mu.Lock()
	m.blocked[scopeKey(tenant, kind, id)] = struct{}{}
	m.mu.Unlock()
}

func (m *Manager) isBlocked(tenant, kind, id string) bool {
	m.mu.RLock()
	_, ok := m.blocked[scopeKey(tenant, kind, id)]
	m.mu.RUnlock()
	return ok
}

func (m *Manager) rollback(ctx context.Context, item Item, written []CopyStore) error {
	var failures []error
	for i := len(written) - 1; i >= 0; i-- {
		if err := written[i].Delete(ctx, item.TenantID, item.ID); err != nil {
			failures = append(failures, fmt.Errorf("agent memory: rollback %s: %w", written[i].Name(), err))
		}
	}
	if len(failures) != 0 {
		return errors.Join(ErrIncomplete, errors.Join(failures...))
	}
	return nil
}

func validateItem(item Item) error {
	if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.TenantID) == "" || strings.TrimSpace(item.OwnerID) == "" || strings.TrimSpace(item.Purpose) == "" || strings.TrimSpace(item.DataClass) == "" || strings.TrimSpace(item.RetentionPolicyID) == "" || strings.TrimSpace(item.RetentionVersion) == "" || item.CreatedAt.IsZero() || item.TTL <= 0 || len(item.Payload) == 0 || !validKind(item.Kind) {
		return ErrInvalid
	}
	if strings.TrimSpace(item.SourceOwner) == "" || strings.TrimSpace(item.SourceID) == "" || strings.TrimSpace(item.SourceVersion) == "" || strings.TrimSpace(item.SourceDigest) == "" || len(item.Audience) == 0 || len(item.Invalidators) == 0 || !uniqueNonempty(item.Audience) || !uniqueNonempty(item.Invalidators) {
		return ErrInvalid
	}
	return nil
}

func validatePin(pin SourcePin) error {
	if pin.TenantID == "" || pin.Owner == "" || pin.ID == "" || pin.Version == "" || pin.Digest == "" || len(pin.Audience) == 0 || len(pin.Invalidators) == 0 || !uniqueNonempty(pin.Audience) || !uniqueNonempty(pin.Invalidators) {
		return ErrInvalid
	}
	return nil
}

func validKind(kind Kind) bool {
	switch kind {
	case KindMemory, KindCache, KindPrompt, KindToolResult, KindTrace:
		return true
	default:
		return false
	}
}

func sourcePin(item Item) SourcePin {
	return SourcePin{TenantID: item.TenantID, Owner: item.SourceOwner, ID: item.SourceID, Version: item.SourceVersion, Digest: item.SourceDigest, Audience: slices.Clone(item.Audience), Invalidators: slices.Clone(item.Invalidators)}
}

func uniqueItems(items []Item) ([]Item, error) {
	byID := make(map[string]Item, len(items))
	for _, item := range items {
		if err := validateItem(item); err != nil {
			return nil, errors.Join(ErrIncomplete, err)
		}
		if prior, ok := byID[item.ID]; ok {
			if !sameItem(prior, item) {
				return nil, fmt.Errorf("%w: conflicting copies for %q", ErrIncomplete, item.ID)
			}
			continue
		}
		byID[item.ID] = cloneItem(item)
	}
	ordered := make([]Item, 0, len(byID))
	for _, item := range byID {
		ordered = append(ordered, item)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	return ordered, nil
}

func complete(inventory Inventory) bool {
	return inventory.Complete && strings.TrimSpace(inventory.Watermark) != ""
}

func sameItem(a, b Item) bool {
	return a.ID == b.ID && a.TenantID == b.TenantID && a.OwnerID == b.OwnerID && a.Kind == b.Kind && a.SourceOwner == b.SourceOwner && a.SourceID == b.SourceID && a.SourceVersion == b.SourceVersion && a.SourceDigest == b.SourceDigest && sameSet(a.Audience, b.Audience) && a.Purpose == b.Purpose && a.DataClass == b.DataClass && a.RetentionPolicyID == b.RetentionPolicyID && a.RetentionVersion == b.RetentionVersion && a.CreatedAt.Equal(b.CreatedAt) && a.TTL == b.TTL && sameSet(a.Invalidators, b.Invalidators) && string(a.Payload) == string(b.Payload)
}

func cloneItems(items []Item) []Item {
	out := make([]Item, len(items))
	for i := range items {
		out[i] = cloneItem(items[i])
	}
	return out
}

func cloneItem(item Item) Item {
	item.Audience = slices.Clone(item.Audience)
	item.Invalidators = slices.Clone(item.Invalidators)
	item.Payload = slices.Clone(item.Payload)
	return item
}

func itemsTenant(items []Item) string {
	if len(items) == 0 {
		return ""
	}
	return items[0].TenantID
}

func contains(values []string, value string, more ...string) bool {
	if !slices.Contains(values, value) {
		return false
	}
	for _, candidate := range more {
		if !slices.Contains(values, candidate) {
			return false
		}
	}
	return true
}

func containsAll(values, required []string) bool {
	for _, value := range required {
		if !slices.Contains(values, value) {
			return false
		}
	}
	return len(required) > 0
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) || !uniqueNonempty(a) || !uniqueNonempty(b) {
		return false
	}
	for _, value := range a {
		if !slices.Contains(b, value) {
			return false
		}
	}
	return true
}

func uniqueNonempty(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return false
		}
		if _, ok := seen[value]; ok {
			return false
		}
		seen[value] = struct{}{}
	}
	return len(seen) == len(values)
}

func scopeKey(tenant, kind, id string) string { return fmt.Sprintf("%s\x00%s\x00%s", tenant, kind, id) }

// SourceTarget keeps equal opaque source IDs from distinct owners independent.
func SourceTarget(owner, id string) string { return fmt.Sprintf("%d:%s%s", len(owner), owner, id) }

// InventoryMetadata requires a separate current grant from payload export.
// Missing hold decisions fail the entire inventory. Expired and invalidated
// held copies remain visible as metadata while Read rejects their payloads.
func (m *Manager) InventoryMetadata(ctx context.Context, actor Actor) ([]Metadata, error) {
	if m == nil || actor.TenantID == "" || actor.PrincipalID == "" {
		return nil, ErrInvalid
	}
	items, err := m.itemsForTenant(ctx, actor.TenantID)
	if err != nil {
		return nil, err
	}
	out := []Metadata{}
	for _, item := range items {
		if err = m.authorizer.AuthorizeMemory(ctx, actor, OperationInventory, cloneItem(item)); err != nil {
			return nil, errors.Join(ErrDenied, err)
		}
		decision, err := m.disposition.ResolveMemoryDisposition(ctx, cloneItem(item), m.now().UTC())
		if err != nil || !decision.Resolved {
			return nil, errors.Join(ErrDisposition, err)
		}
		pin := sourcePin(item)
		pin.Purpose = item.Purpose
		control := Item{TenantID: item.TenantID, OwnerID: item.SourceOwner, Purpose: item.Purpose}
		out = append(out, Metadata{ID: item.ID, SourceOwner: item.SourceOwner, SourceID: item.SourceID, Purpose: item.Purpose, DataClass: item.DataClass, Audience: slices.Clone(item.Audience), ExpiresAt: item.CreatedAt.Add(item.TTL), Held: decision.Held, Pin: pin, CanDelete: !decision.Held && decision.CanDelete && m.authorizer.AuthorizeMemory(ctx, actor, OperationDelete, cloneItem(item)) == nil, CanRevoke: m.authorizer.AuthorizeMemory(ctx, actor, OperationRevoke, control) == nil, CanExport: m.authorizer.AuthorizeMemory(ctx, actor, OperationExport, cloneItem(item)) == nil})
	}
	return out, nil
}
