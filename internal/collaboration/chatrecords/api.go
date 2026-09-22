// Package chatrecords owns chat governance records. It stores metadata and
// evidence, never message bodies, and is deliberately independent of core
// workflow storage.
package chatrecords

import (
	"context"
	"time"
)

type Kind string

const (
	KindPost         Kind = "POST"
	KindEdit         Kind = "EDIT"
	KindTombstone    Kind = "TOMBSTONE"
	KindReaction     Kind = "REACTION"
	KindFile         Kind = "FILE"
	KindVoice        Kind = "VOICE"
	KindVideo        Kind = "VIDEO"
	KindAgentOutput  Kind = "AGENT_OUTPUT"
	KindDerived      Kind = "DERIVED_COPY"
	KindConversation Kind = "CONVERSATION"
	KindMembership   Kind = "MEMBERSHIP"
	KindAppChange    Kind = "APP_CHANGE"
	KindShareLink    Kind = "SHARE_LINK"
)

type Record struct {
	TenantID       string    `json:"tenant_id"`
	ConversationID string    `json:"conversation_id"`
	RecordID       string    `json:"record_id"`
	Kind           Kind      `json:"kind"`
	SourceID       string    `json:"source_id"`
	Revision       uint64    `json:"revision"`
	CreatedAt      time.Time `json:"created_at"`
	HoldIDs        []string  `json:"hold_ids,omitempty"`
	Disposition    string    `json:"disposition"`
	DerivedFrom    string    `json:"derived_from,omitempty"`
}

type AuditEvent struct {
	TenantID       string    `json:"tenant_id"`
	EventID        string    `json:"event_id"`
	Sequence       uint64    `json:"sequence"`
	ActorID        string    `json:"actor_id"`
	Action         string    `json:"action"`
	TargetType     string    `json:"target_type"`
	TargetID       string    `json:"target_id"`
	PriorRevision  uint64    `json:"prior_revision"`
	Reason         string    `json:"reason"`
	PolicyEvidence string    `json:"policy_evidence"`
	At             time.Time `json:"at"`
	Digest         string    `json:"digest"`
}

type Hold struct {
	TenantID   string     `json:"tenant_id"`
	HoldID     string     `json:"hold_id"`
	MatterRef  string     `json:"matter_ref"`
	Reason     string     `json:"reason"`
	PlacedBy   string     `json:"placed_by"`
	PlacedAt   time.Time  `json:"placed_at"`
	ReleasedAt *time.Time `json:"released_at,omitempty"`
}

func (h Hold) Active() bool { return h.ReleasedAt == nil }

type Export struct {
	TenantID  string    `json:"tenant_id"`
	ExportID  string    `json:"export_id"`
	RecordIDs []string  `json:"record_ids"`
	Digest    string    `json:"digest"`
	CreatedAt time.Time `json:"created_at"`
}

type Report struct {
	TenantID       string    `json:"tenant_id"`
	ReportID       string    `json:"report_id"`
	ConversationID string    `json:"conversation_id"`
	TargetID       string    `json:"target_id"`
	ReporterID     string    `json:"reporter_id"`
	Reason         string    `json:"reason"`
	CreatedAt      time.Time `json:"created_at"`
	State          string    `json:"state"`
}

type CaseAction struct {
	CaseID      string    `json:"case_id"`
	Action      string    `json:"action"`
	ActorID     string    `json:"actor_id"`
	Reason      string    `json:"reason"`
	EvidenceRef string    `json:"evidence_ref"`
	At          time.Time `json:"at"`
}

type Snapshot struct {
	TenantID  string        `json:"tenant_id"`
	Watermark uint64        `json:"watermark"`
	Records   []Record      `json:"records"`
	Events    []AuditEvent  `json:"events"`
	Outbox    []OutboxEvent `json:"outbox"`
	Digest    string        `json:"digest"`
	// RawTables is the canonical row payload for every chat-owned relation.
	// Durable adapters populate it so restore covers the complete chat plane.
	RawTables map[string][]byte `json:"raw_tables,omitempty"`
}

type OutboxEvent struct {
	ID        string `json:"id"`
	Sequence  uint64 `json:"sequence"`
	EventID   string `json:"event_id"`
	Delivered bool   `json:"delivered"`
}

type Repository interface {
	Append(ctx context.Context, record Record, event AuditEvent, outbox OutboxEvent) error
	List(ctx context.Context, tenantID string) ([]Record, error)
	Events(ctx context.Context, tenantID string) ([]AuditEvent, error)
	PutHold(ctx context.Context, hold Hold) error
	Holds(ctx context.Context, tenantID string) ([]Hold, error)
	PutExport(ctx context.Context, export Export) error
	Snapshot(ctx context.Context, tenantID string) (Snapshot, error)
	Restore(ctx context.Context, snapshot Snapshot) error
	Reconcile(ctx context.Context, tenantID string) (ReconcileResult, error)
	PutReport(ctx context.Context, report Report) error
	Reports(ctx context.Context, tenantID string) ([]Report, error)
	PutCaseAction(ctx context.Context, tenantID string, action CaseAction) error
}

type ReconcileResult struct {
	Records            int
	Events             int
	Outbox             int
	MissingOutbox      int
	DuplicateSequences int
	Ready              bool
}

// Authorizer is called before every governance side effect. It must derive
// the actor from trusted authentication and return a policy revision/evidence
// token; callers cannot supply a body or broad private-history permission.
type Authorizer interface {
	Authorize(ctx context.Context, actorID, tenantID, action, targetID string) (policyEvidence string, err error)
}
