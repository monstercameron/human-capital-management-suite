// Package agentaudit records the trusted actor chain and provenance edges for
// work performed by an agent for a signed-in user.
//
// The package deliberately owns a port and a small reference implementation,
// not a second authorization system or an agent-specific database table. A
// durable adapter can append these records beside the existing intent and
// provenance publishers while preserving the same immutable entry shape.
package agentaudit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

const (
	// MaxSubAgentDepth prevents an agent from hiding an unbounded delegation
	// tree in what appears to be one user action.
	MaxSubAgentDepth uint8 = 8

	chainHashDomain = "hcmnext.agent_audit.chain.v1\x00"
)

// EventKind identifies the governed operation represented by an audit entry.
type EventKind string

const (
	EventSkillCall          EventKind = "SKILL_CALL"
	EventApproval           EventKind = "APPROVAL"
	EventIntentOrigin       EventKind = "INTENT_ORIGIN"
	EventWorkflowRun        EventKind = "WORKFLOW_RUN"
	EventConnectorOperation EventKind = "CONNECTOR_OPERATION"
	EventModelCall          EventKind = "MODEL_CALL"
)

func (k EventKind) valid() bool {
	switch k {
	case EventSkillCall, EventApproval, EventIntentOrigin, EventWorkflowRun,
		EventConnectorOperation, EventModelCall:
		return true
	default:
		return false
	}
}

// EdgeKind describes a causal or authorization relationship in the
// provenance graph. From is always the task or the entry being recorded; To
// is a stable reference owned by the linked subsystem.
type EdgeKind string

const (
	EdgeTask         EdgeKind = "TASK"
	EdgeIntent       EdgeKind = "INTENT"
	EdgeApproval     EdgeKind = "APPROVAL"
	EdgeWorkflowRun  EdgeKind = "WORKFLOW_RUN"
	EdgeConnectorOp  EdgeKind = "CONNECTOR_OPERATION"
	EdgeAuthorizedBy EdgeKind = "AUTHORIZED_BY"
	EdgeCaused       EdgeKind = "CAUSED"
)

func (k EdgeKind) valid() bool {
	switch k {
	case EdgeTask, EdgeIntent, EdgeApproval, EdgeWorkflowRun, EdgeConnectorOp,
		EdgeAuthorizedBy, EdgeCaused:
		return true
	default:
		return false
	}
}

// Classification controls field-level projection. It is metadata only; it
// does not grant the viewer access to a field.
type Classification string

const (
	ClassificationPublic       Classification = "PUBLIC"
	ClassificationInternal     Classification = "INTERNAL"
	ClassificationConfidential Classification = "CONFIDENTIAL"
	ClassificationRestricted   Classification = "RESTRICTED"
)

func (c Classification) valid() bool {
	switch c {
	case ClassificationPublic, ClassificationInternal, ClassificationConfidential, ClassificationRestricted:
		return true
	default:
		return false
	}
}

// ActorChain is the server-derived identity carried by every agent audit
// event. The user is the principal; all agent fields describe the actor
// acting for that principal and are not authority-bearing themselves.
type ActorChain struct {
	UserID            string
	AgentVersion      string
	InstallationID    string
	TaskID            string
	PlanRevision      string
	StepID            string
	SubAgentDepth     uint8
	DelegationGrantID string
}

// Validate rejects an incomplete or over-deep actor chain.
func (a ActorChain) Validate() error {
	for field, value := range map[string]string{
		"user_id":             a.UserID,
		"agent_version":       a.AgentVersion,
		"installation_id":     a.InstallationID,
		"task_id":             a.TaskID,
		"plan_revision":       a.PlanRevision,
		"step_id":             a.StepID,
		"delegation_grant_id": a.DelegationGrantID,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("actor chain: %s is required", field)
		}
	}
	if a.SubAgentDepth > MaxSubAgentDepth {
		return fmt.Errorf("actor chain: sub-agent depth %d exceeds maximum %d", a.SubAgentDepth, MaxSubAgentDepth)
	}
	return nil
}

// FromIntentOrigin binds an agent chain to an already trusted intent origin.
// The origin is checked before any fields are copied, so a caller cannot use
// an audit record to turn an untrusted origin into evidence.
func FromIntentOrigin(origin intent.Origin, chain ActorChain) (ActorChain, error) {
	if err := origin.Validate(); err != nil {
		return ActorChain{}, fmt.Errorf("agent audit: invalid intent origin: %w", err)
	}
	if origin.Kind != intent.OriginAgent {
		return ActorChain{}, fmt.Errorf("agent audit: origin kind %s is not AGENT", origin.Kind)
	}
	if origin.Initiator.Kind != intent.InitiatorAgent {
		return ActorChain{}, fmt.Errorf("agent audit: origin initiator %s is not AGENT", origin.Initiator.Kind)
	}
	if err := chain.Validate(); err != nil {
		return ActorChain{}, err
	}
	if origin.Tenant.String() == "" {
		return ActorChain{}, errors.New("agent audit: intent origin has no tenant")
	}
	grantMatched := slices.Contains(origin.DelegationRefs, chain.DelegationGrantID)
	for _, hop := range origin.OnBehalfOf {
		grantMatched = grantMatched || hop.DelegationID == chain.DelegationGrantID
	}
	if !grantMatched {
		return ActorChain{}, fmt.Errorf("agent audit: delegation grant %q is absent from the trusted origin", chain.DelegationGrantID)
	}
	return chain, nil
}

// FromAdmission turns an already admitted agentsecurity tool call into an
// audit entry. The security gateway remains the owner of tool admission; this
// adapter only carries its stable receipt fields into provenance.
func FromAdmission(eventID, tenantID string, actor ActorChain, admission agentsecurity.Admission, resultDigest string, edges []Edge, occurredAt time.Time) (Entry, error) {
	if strings.TrimSpace(admission.AgentID) == "" || strings.TrimSpace(admission.Tenant) == "" || strings.TrimSpace(admission.Tool) == "" || strings.TrimSpace(admission.ArgsDigest) == "" {
		return Entry{}, fmt.Errorf("%w: admitted call lacks agent, tenant, tool or argument digest", ErrInvalidEntry)
	}
	if admission.Tenant != tenantID {
		return Entry{}, fmt.Errorf("%w: admission tenant %q does not match entry tenant %q", ErrTenantBoundary, admission.Tenant, tenantID)
	}
	entry := Entry{EventID: eventID, TenantID: tenantID, Kind: EventSkillCall, Actor: actor, Action: admission.Tool, ArgumentsDigest: admission.ArgsDigest, ResultDigest: resultDigest, Edges: edges, OccurredAt: occurredAt}
	if err := entry.validate(); err != nil {
		return Entry{}, fmt.Errorf("%w: %v", ErrInvalidEntry, err)
	}
	return entry, nil
}

// Field is an audit-safe field. Values are never accepted as authority; they
// are evidence for a projection and are redacted according to the viewer.
type Field struct {
	Name           string
	Value          string
	Classification Classification
}

func (f Field) validate() error {
	if strings.TrimSpace(f.Name) == "" {
		return errors.New("audit field name is required")
	}
	if !f.Classification.valid() {
		return fmt.Errorf("audit field %q has invalid classification %q", f.Name, f.Classification)
	}
	return nil
}

// Edge is a typed join to a task, intent, approval, workflow run or connector
// journal entry. References are opaque IDs owned by the linked package.
type Edge struct {
	Kind EdgeKind
	From string
	To   string
}

func (e Edge) validate(taskID string) error {
	if !e.Kind.valid() {
		return fmt.Errorf("audit edge has invalid kind %q", e.Kind)
	}
	if strings.TrimSpace(e.From) == "" || strings.TrimSpace(e.To) == "" {
		return errors.New("audit edge requires from and to references")
	}
	if e.Kind == EdgeTask && e.From != taskID {
		return fmt.Errorf("task edge starts at %q, want actor task %q", e.From, taskID)
	}
	return nil
}

// Entry is one immutable agent-for-user event. Raw prompts, credentials and
// connector payloads do not belong here; callers provide digests and
// deliberately classified summary fields instead.
type Entry struct {
	EventID         string
	TenantID        string
	Kind            EventKind
	Actor           ActorChain
	Action          string
	ArgumentsDigest string
	ResultDigest    string
	ApprovalDigest  string
	Fields          []Field
	Edges           []Edge
	OccurredAt      time.Time
}

func (e Entry) validate() error {
	if strings.TrimSpace(e.EventID) == "" {
		return errors.New("audit entry event id is required")
	}
	if strings.TrimSpace(e.TenantID) == "" {
		return errors.New("audit entry tenant id is required")
	}
	if !e.Kind.valid() {
		return fmt.Errorf("audit entry has invalid event kind %q", e.Kind)
	}
	if err := e.Actor.Validate(); err != nil {
		return err
	}
	if e.OccurredAt.IsZero() {
		return errors.New("audit entry occurred-at is required")
	}
	if strings.TrimSpace(e.Action) == "" {
		return errors.New("audit entry action is required")
	}
	fieldNames := make(map[string]struct{}, len(e.Fields))
	for _, field := range e.Fields {
		if err := field.validate(); err != nil {
			return err
		}
		if _, exists := fieldNames[field.Name]; exists {
			return fmt.Errorf("audit field %q is duplicated", field.Name)
		}
		fieldNames[field.Name] = struct{}{}
	}
	if len(e.Edges) == 0 {
		return errors.New("audit entry must join its task to at least one provenance reference")
	}
	hasTask := false
	joinedKind := requiredJoin(e.Kind)
	hasJoinedKind := joinedKind == ""
	for _, edge := range e.Edges {
		if err := edge.validate(e.Actor.TaskID); err != nil {
			return err
		}
		if edge.Kind == EdgeTask && edge.To == e.EventID {
			hasTask = true
		}
		if edge.Kind == joinedKind {
			hasJoinedKind = true
		}
	}
	if !hasTask {
		return fmt.Errorf("audit entry %q has no task edge to itself", e.EventID)
	}
	if !hasJoinedKind {
		return fmt.Errorf("audit entry %q of kind %s has no %s provenance edge", e.EventID, e.Kind, joinedKind)
	}
	return nil
}

// requiredJoin identifies the direct provenance edge that must accompany an
// event whose purpose is to report a linked subsystem operation. Skill and
// model calls may be read-only and only require their task edge; the other
// event kinds must prove the corresponding intent, approval, workflow or
// connector journal reference.
func requiredJoin(kind EventKind) EdgeKind {
	switch kind {
	case EventApproval:
		return EdgeApproval
	case EventIntentOrigin:
		return EdgeIntent
	case EventWorkflowRun:
		return EdgeWorkflowRun
	case EventConnectorOperation:
		return EdgeConnectorOp
	default:
		return ""
	}
}

// Record is the persisted chain link. Created is only a response flag and is
// excluded from the hash, so retries cannot change evidence.
type Record struct {
	Entry
	Sequence   uint64
	PrevHash   string
	ChainHash  string
	RecordedAt time.Time
	Created    bool
}

// FieldView is the query projection of Field. Redacted values retain a digest
// and classification so an auditor can see that evidence exists without
// receiving the protected value.
type FieldView struct {
	Name           string
	Value          string
	Digest         string
	Classification Classification
	Redacted       bool
}

// View is the redacted, read-only audit projection returned to a caller.
type View struct {
	EventID         string
	TenantID        string
	Kind            EventKind
	Actor           ActorChain
	Action          string
	ArgumentsDigest string
	ResultDigest    string
	ApprovalDigest  string
	Fields          []FieldView
	Edges           []Edge
	OccurredAt      time.Time
	Sequence        uint64
	PrevHash        string
	ChainHash       string
}

// Viewer is the authorization context for an audit query. User viewers see
// only their own chain; auditor viewers see the tenant's entries but still
// need explicit field access for non-public fields.
type Viewer struct {
	TenantID      string
	UserID        string
	Role          ViewerRole
	AllowedFields map[string]bool
}

type ViewerRole string

const (
	ViewerUser    ViewerRole = "USER"
	ViewerAuditor ViewerRole = "AUDITOR"
)

func (v Viewer) valid() error {
	if strings.TrimSpace(v.TenantID) == "" || strings.TrimSpace(v.UserID) == "" {
		return errors.New("audit viewer tenant and user are required")
	}
	if v.Role != ViewerUser && v.Role != ViewerAuditor {
		return fmt.Errorf("audit viewer role %q is not permitted", v.Role)
	}
	return nil
}

// Query restricts a tenant and optionally a task or event kind.
type Query struct {
	Viewer Viewer
	TaskID string
	Kinds  []EventKind
}

// Store is the seam used by intent, approval, workflow and connector
// publishers. Implementations must make Append idempotent by EventID and must
// preserve the per-tenant hash chain.
type Store interface {
	Append(context.Context, Entry) (Record, error)
	Query(context.Context, Query) ([]View, error)
	Verify(context.Context, string) error
}

var (
	ErrInvalidEntry      = errors.New("invalid audit entry")
	ErrDuplicateConflict = errors.New("audit event id conflict")
	ErrTenantBoundary    = errors.New("audit tenant boundary violation")
	ErrUnauthorized      = errors.New("audit query unauthorized")
	ErrChainTampered     = errors.New("audit chain tampered")
)

// MemoryStore is the deterministic reference implementation for tests and
// local composition. It is append-only from the public API and synchronizes
// per-store state without a package-level registry.
type MemoryStore struct {
	mu      sync.RWMutex
	chains  map[string][]Record
	byEvent map[string]Record
	clock   func() time.Time
}

// NewMemoryStore returns an empty audit store. A clock option is intentionally
// not exposed: tests can use Append entries with fixed OccurredAt values and
// RecordedAt is evidence of persistence, not material event identity.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{chains: make(map[string][]Record), byEvent: make(map[string]Record), clock: func() time.Time { return time.Now().UTC() }}
}

// Append validates and records one event. A byte-identical retry returns the
// existing record with Created=false; a reused event ID with changed content
// is refused.
func (s *MemoryStore) Append(ctx context.Context, entry Entry) (Record, error) {
	if err := contextErr(ctx); err != nil {
		return Record{}, err
	}
	if s == nil {
		return Record{}, fmt.Errorf("%w: nil store", ErrInvalidEntry)
	}
	if err := entry.validate(); err != nil {
		return Record{}, fmt.Errorf("%w: %v", ErrInvalidEntry, err)
	}
	entry = cloneEntry(entry)
	s.mu.Lock()
	defer s.mu.Unlock()
	if prior, ok := s.byEvent[eventKey(entry.TenantID, entry.EventID)]; ok {
		if canonicalEntry(prior.Entry) != canonicalEntry(entry) {
			return Record{}, fmt.Errorf("%w: %s", ErrDuplicateConflict, entry.EventID)
		}
		prior.Created = false
		return cloneRecord(prior), nil
	}
	chain := s.chains[entry.TenantID]
	prev := ""
	if len(chain) > 0 {
		prev = chain[len(chain)-1].ChainHash
	}
	record := Record{Entry: entry, Sequence: uint64(len(chain) + 1), PrevHash: prev, RecordedAt: s.clock().UTC(), Created: true}
	record.ChainHash = hashRecord(record)
	s.chains[entry.TenantID] = append(chain, record)
	s.byEvent[eventKey(entry.TenantID, entry.EventID)] = record
	return cloneRecord(record), nil
}

// Query projects only records the viewer may see and redacts each field
// independently. A USER cannot use a task ID to cross into another user's
// audit history.
func (s *MemoryStore) Query(ctx context.Context, q Query) ([]View, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, ErrInvalidEntry
	}
	if err := q.Viewer.valid(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnauthorized, err)
	}
	for _, kind := range q.Kinds {
		if !kind.valid() {
			return nil, fmt.Errorf("%w: invalid event kind %q", ErrInvalidEntry, kind)
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]View, 0)
	for _, record := range s.chains[q.Viewer.TenantID] {
		if q.Viewer.Role == ViewerUser && record.Actor.UserID != q.Viewer.UserID {
			continue
		}
		if q.TaskID != "" && record.Actor.TaskID != q.TaskID {
			continue
		}
		if len(q.Kinds) > 0 && !slices.Contains(q.Kinds, record.Kind) {
			continue
		}
		result = append(result, project(record, q.Viewer))
	}
	return result, nil
}

// Verify checks a tenant's complete chain against its canonical entries.
func (s *MemoryStore) Verify(ctx context.Context, tenantID string) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if s == nil || strings.TrimSpace(tenantID) == "" {
		return fmt.Errorf("%w: tenant is required", ErrInvalidEntry)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return VerifyChain(s.chains[tenantID])
}

// VerifyChain verifies a copied chain and is useful to durable adapters and
// recovery tests that read records independently of MemoryStore.
func VerifyChain(chain []Record) error {
	prev := ""
	for i, record := range chain {
		if record.Sequence != uint64(i+1) || record.PrevHash != prev || record.ChainHash != hashRecord(record) {
			return fmt.Errorf("%w: sequence %d", ErrChainTampered, record.Sequence)
		}
		prev = record.ChainHash
	}
	return nil
}

// CanonicalEntry returns the stable bytes that are hashed and compared for
// idempotent retries. It is exported so a connector or intent publisher can
// bind its own receipt digest to exactly the audit material.
func CanonicalEntry(entry Entry) string { return canonicalEntry(entry) }

func canonicalEntry(entry Entry) string {
	entry = cloneEntry(entry)
	sort.Slice(entry.Fields, func(i, j int) bool { return entry.Fields[i].Name < entry.Fields[j].Name })
	sort.Slice(entry.Edges, func(i, j int) bool {
		if entry.Edges[i].Kind != entry.Edges[j].Kind {
			return entry.Edges[i].Kind < entry.Edges[j].Kind
		}
		if entry.Edges[i].From != entry.Edges[j].From {
			return entry.Edges[i].From < entry.Edges[j].From
		}
		return entry.Edges[i].To < entry.Edges[j].To
	})
	b, _ := json.Marshal(entry)
	return string(b)
}

func hashRecord(record Record) string {
	material := struct {
		Sequence uint64
		PrevHash string
		Entry    string
	}{record.Sequence, record.PrevHash, canonicalEntry(record.Entry)}
	sum := sha256.Sum256(append([]byte(chainHashDomain), mustJSON(material)...))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func project(record Record, viewer Viewer) View {
	result := View{EventID: record.EventID, TenantID: record.TenantID, Kind: record.Kind, Actor: record.Actor, Action: record.Action, ArgumentsDigest: record.ArgumentsDigest, ResultDigest: record.ResultDigest, ApprovalDigest: record.ApprovalDigest, OccurredAt: record.OccurredAt, Sequence: record.Sequence, PrevHash: record.PrevHash, ChainHash: record.ChainHash, Edges: slices.Clone(record.Edges)}
	result.Fields = make([]FieldView, 0, len(record.Fields))
	for _, field := range record.Fields {
		allowed := field.Classification == ClassificationPublic || viewer.AllowedFields[field.Name]
		view := FieldView{Name: field.Name, Classification: field.Classification}
		if allowed {
			view.Value = field.Value
		} else {
			view.Redacted = true
			view.Digest = digest(field.Value)
		}
		result.Fields = append(result.Fields, view)
	}
	return result
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func mustJSON(value any) []byte {
	b, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return b
}

func cloneEntry(entry Entry) Entry {
	entry.Fields = slices.Clone(entry.Fields)
	entry.Edges = slices.Clone(entry.Edges)
	return entry
}

func cloneRecord(record Record) Record {
	record.Entry = cloneEntry(record.Entry)
	return record
}

func eventKey(tenantID, eventID string) string { return tenantID + "\x00" + eventID }
