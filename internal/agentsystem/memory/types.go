package memory

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrInvalid identifies malformed items and operation requests.
	ErrInvalid = errors.New("agent memory: invalid item or request")
	// ErrDenied identifies an actor or policy refusal.
	ErrDenied = errors.New("agent memory: access denied")
	// ErrStale identifies a changed source, policy or revocation state.
	ErrStale = errors.New("agent memory: source or policy is stale")
	// ErrExpired identifies an item past its serving TTL.
	ErrExpired = errors.New("agent memory: item expired")
	// ErrHeld identifies an item protected by an active legal hold.
	ErrHeld = errors.New("agent memory: item is under legal hold")
	// ErrNotFound identifies an absent item or configured store.
	ErrNotFound = errors.New("agent memory: item not found")
	// ErrIncomplete identifies an unknown or incomplete copy inventory.
	ErrIncomplete = errors.New("agent memory: copy inventory incomplete")
	// ErrDisposition identifies an unresolved or restrictive owner disposition.
	ErrDisposition = errors.New("agent memory: owner disposition unresolved")
)

// Kind distinguishes agent-generated copies that must remain in tenant
// exports, including protected prompts and tool outcomes.
type Kind string

const (
	// KindMemory is an intentionally retained agent memory proposal.
	KindMemory Kind = "MEMORY"
	// KindCache is a rebuildable derived answer or search copy.
	KindCache Kind = "CACHE"
	// KindPrompt is a prompt or model-context record.
	KindPrompt Kind = "PROMPT"
	// KindToolResult is a tool outcome record.
	KindToolResult Kind = "TOOL_RESULT"
	// KindTrace is an agent execution trace.
	KindTrace Kind = "TRACE"
)

// Item is a derived copy with enough owner-issued provenance to reauthorize
// it on every read and to locate it after source revocation.
type Item struct {
	ID                string        `json:"id"`
	TenantID          string        `json:"tenant_id"`
	OwnerID           string        `json:"owner_id"`
	Kind              Kind          `json:"kind"`
	SourceOwner       string        `json:"source_owner"`
	SourceID          string        `json:"source_id"`
	SourceVersion     string        `json:"source_version"`
	SourceDigest      string        `json:"source_digest"`
	Audience          []string      `json:"audience"`
	Purpose           string        `json:"purpose"`
	DataClass         string        `json:"data_class"`
	RetentionPolicyID string        `json:"retention_policy_id"`
	RetentionVersion  string        `json:"retention_version"`
	CreatedAt         time.Time     `json:"created_at"`
	TTL               time.Duration `json:"ttl_ns"`
	Invalidators      []string      `json:"invalidators"`
	Payload           []byte        `json:"payload_base64"`
}

// SourcePin identifies an exact owner-controlled source version and the
// invalidator that makes descendants revocable.
type SourcePin struct {
	TenantID, Owner, ID, Version, Digest string
	Purpose                              string
	Audience                             []string
	Invalidators                         []string
}

// Policy is resolved from the agent's owner at operation time. Caller
// supplied item fields never widen this policy.
type Policy struct {
	TenantID, OwnerID, Purpose string
	Version                    string
	RetentionPolicyID          string
	RetentionVersion           string
	MaxTTL                     time.Duration
	AllowedClasses             []string
	AllowedAudiences           []string
}

// Actor is a server-validated principal. Tenant identity must come from the
// authenticated request boundary, not from an item or payload.
type Actor struct {
	TenantID, PrincipalID string
}

// Operation names the operation whose current owner authority is checked.
type Operation string

const (
	// OperationWrite requests retention of a derived item.
	OperationWrite Operation = "WRITE"
	// OperationRead requests access to one derived item.
	OperationRead Operation = "READ"
	// OperationExport requests a complete tenant inventory.
	OperationExport Operation = "EXPORT"
	// OperationDelete requests policy-authorized destruction.
	OperationDelete Operation = "DELETE"
	// OperationRevoke withdraws source authority for derived copies.
	OperationRevoke Operation = "REVOKE"
	// OperationMaintenance records automatic cleanup discovered during a read.
	OperationMaintenance Operation = "MAINTENANCE"
	// OperationInventory allows operational metadata without payload export.
	OperationInventory Operation = "INVENTORY"
)

// SourceDecision is returned by the owning source service. A false Current
// value is definitive revocation; an error is an unknown state and must deny
// serving data without pretending that destructive disposition is approved.
type SourceDecision struct {
	Current bool
	Version string
	Digest  string
	// DataClass is issued by the source owner for new derived retention.
	DataClass    string
	Audience     []string
	Invalidators []string
}

// DispositionDecision is an explicit owner records-management decision.
// Missing or unresolved decisions block physical deletion.
type DispositionDecision struct {
	Resolved  bool
	CanDelete bool
	Held      bool
	Reason    string
}

// Inventory proves that a store has enumerated every matching copy at a
// stable owner-provided watermark. An empty or incomplete result is never
// accepted as proof that no copies remain.
type Inventory struct {
	Items     []Item
	Complete  bool
	Watermark string
}

// Invalidation is the durable, tenant-scoped receipt that fences a source or
// item before copy deletion begins.
type Invalidation struct {
	TenantID, TargetKind, TargetID string
	ActorID, Reason                string
	Operation                      Operation
	OccurredAt                     time.Time
}

// PolicyResolver reads the current agent-owner memory policy.
type PolicyResolver interface {
	ResolveMemoryPolicy(context.Context, string, string, string) (Policy, error)
}

// SourceResolver rechecks source access and invalidator state against its
// owning service.
type SourceResolver interface {
	CheckMemorySource(context.Context, SourcePin, string) (SourceDecision, error)
}

// Authorizer evaluates current actor authority for an item and operation.
type Authorizer interface {
	AuthorizeMemory(context.Context, Actor, Operation, Item) error
}

// DispositionResolver delegates retention and hold authority to records
// management instead of interpreting a TTL as permission to destroy a record.
type DispositionResolver interface {
	ResolveMemoryDisposition(context.Context, Item, time.Time) (DispositionDecision, error)
}

// CopyStore is one authoritative or derived storage surface. Implementations
// must scope every operation by tenant, expose complete inventories, persist
// invalidation tombstones, and atomically reject Put when either the item or
// source is tombstoned. Search adapters must not bypass the memory service's
// source recheck.
type CopyStore interface {
	Name() string
	Put(context.Context, Item) error
	Get(context.Context, string, string) (Item, error)
	ListTenant(context.Context, string) (Inventory, error)
	ListSource(context.Context, string, string, string) (Inventory, error)
	ListItem(context.Context, string, string) (Inventory, error)
	Delete(context.Context, string, string) error
	RecordInvalidation(context.Context, Invalidation) error
	Invalidated(context.Context, string, string, string) (bool, error)
}

// Config wires the current owner policy and every known raw and derived copy
// store. Missing ports or an empty store set are a configuration failure.
type Config struct {
	Policies    PolicyResolver
	Sources     SourceResolver
	Authorizer  Authorizer
	Disposition DispositionResolver
	Stores      []CopyStore
}

// Metadata deliberately excludes prompt, tool output and other payload bytes.
type Metadata struct {
	ID, SourceOwner, SourceID, Purpose, DataClass string
	Audience                                      []string
	ExpiresAt                                     time.Time
	Held                                          bool
	CanDelete, CanRevoke, CanExport               bool
	Pin                                           SourcePin
}
