package chatrecords

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Reading the audit log (#3780). The log is written by every governed action;
// this file is the one read path. It is body-free by construction: an
// AuditEntry has no field that can hold message text, and the policy evidence
// and digest stay inside the store.

const (
	// AuditPageDefault and AuditPageMax bound one page of the log.
	AuditPageDefault = 50
	AuditPageMax     = 200
	// AuditExportMax bounds one export. A larger log is exported in time slices.
	AuditExportMax = 10000
	// auditReasonLimit bounds the reason shown for one entry.
	auditReasonLimit = 300
)

// AuditFilter narrows the log. Empty fields match everything. Action matches
// an action exactly or a whole family ("moderation" matches
// "moderation.remove"). From is inclusive and To is exclusive.
type AuditFilter struct {
	ActorID        string
	Action         string
	ConversationID string
	From, To       time.Time
}

// AuditQuery is one repository read: events newest first, strictly older than
// the Before sequence when it is set, at most Limit of them.
type AuditQuery struct {
	AuditFilter
	Before uint64
	Limit  int
}

// AuditReadRepository is optional so existing repositories stay source
// compatible. It must be tenant-scoped.
type AuditReadRepository interface {
	ListAudit(ctx context.Context, tenantID string, q AuditQuery) ([]AuditEvent, error)
}

// AuditEntry is what a reader of the log sees of one event.
type AuditEntry struct {
	EventID    string    `json:"event_id"`
	Sequence   uint64    `json:"sequence"`
	ActorID    string    `json:"actor_id"`
	Action     string    `json:"action"`
	TargetType string    `json:"target_type"`
	TargetID   string    `json:"target_id"`
	Reason     string    `json:"reason"`
	At         time.Time `json:"at"`
}

// AuditPage is one page of entries and the cursor of the next, empty at the end.
type AuditPage struct {
	Entries    []AuditEntry `json:"entries"`
	NextCursor string       `json:"next_cursor"`
}

func auditReasonForView(reason string) string {
	r := []rune(strings.TrimSpace(reason))
	if len(r) > auditReasonLimit {
		return string(r[:auditReasonLimit]) + "…"
	}
	return string(r)
}

func auditEntry(e AuditEvent) AuditEntry {
	return AuditEntry{EventID: e.EventID, Sequence: e.Sequence, ActorID: e.ActorID, Action: e.Action, TargetType: e.TargetType, TargetID: e.TargetID, Reason: auditReasonForView(e.Reason), At: e.At.UTC()}
}

// Matches reports whether an event passes the filter on actor, action and
// time. The conversation is not on the event; repositories resolve it.
func (f AuditFilter) Matches(e AuditEvent) bool {
	if f.ActorID != "" && e.ActorID != f.ActorID {
		return false
	}
	if f.Action != "" && e.Action != f.Action && !strings.HasPrefix(e.Action, f.Action+".") {
		return false
	}
	if !f.From.IsZero() && e.At.Before(f.From) {
		return false
	}
	if !f.To.IsZero() && !e.At.Before(f.To) {
		return false
	}
	return true
}

func (f AuditFilter) valid() bool {
	if len(f.ActorID) > 256 || len(f.Action) > 128 || len(f.ConversationID) > 256 {
		return false
	}
	return f.From.IsZero() || f.To.IsZero() || f.From.Before(f.To)
}

func encodeAuditCursor(tenant string, seq uint64) string {
	return base64.RawURLEncoding.EncodeToString([]byte("v1\x00" + tenant + "\x00" + strconv.FormatUint(seq, 10)))
}

// decodeAuditCursor refuses a cursor that was not issued for this tenant, so a
// token can never move a reader into another tenant's log.
func decodeAuditCursor(tenant, cursor string) (uint64, error) {
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, ErrInvalid
	}
	parts := strings.Split(string(raw), "\x00")
	if len(parts) != 3 || parts[0] != "v1" || parts[1] != tenant {
		return 0, ErrInvalid
	}
	seq, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil || seq == 0 {
		return 0, ErrInvalid
	}
	return seq, nil
}

func (s *Service) auditReader() (AuditReadRepository, error) {
	repo, ok := s.Repo.(AuditReadRepository)
	if !ok {
		return nil, ErrAuditUnavailable
	}
	return repo, nil
}

// ListAudit reads one page of the tenant's audit log, newest first. The
// authorizer decides who may read it before anything is read.
func (s *Service) ListAudit(ctx context.Context, actor, tenant string, f AuditFilter, cursor string, limit int) (AuditPage, error) {
	if _, err := s.authorize(ctx, actor, tenant, "audit.read", tenant); err != nil {
		return AuditPage{}, err
	}
	if !f.valid() {
		return AuditPage{}, ErrInvalid
	}
	before, err := decodeAuditCursor(tenant, cursor)
	if err != nil {
		return AuditPage{}, err
	}
	repo, err := s.auditReader()
	if err != nil {
		return AuditPage{}, err
	}
	if limit <= 0 {
		limit = AuditPageDefault
	}
	if limit > AuditPageMax {
		limit = AuditPageMax
	}
	rows, err := repo.ListAudit(ctx, tenant, AuditQuery{AuditFilter: f, Before: before, Limit: limit + 1})
	if err != nil {
		return AuditPage{}, err
	}
	page := AuditPage{Entries: make([]AuditEntry, 0, min(len(rows), limit))}
	for i, e := range rows {
		if i == limit {
			page.NextCursor = encodeAuditCursor(tenant, rows[limit-1].Sequence)
			break
		}
		page.Entries = append(page.Entries, auditEntry(e))
	}
	return page, nil
}

// auditCSVCell keeps a spreadsheet from running a cell as a formula.
func auditCSVCell(v string) string {
	if v != "" && strings.ContainsAny(v[:1], "=+-@\t\r") {
		return "'" + v
	}
	return v
}

// ExportAuditCSV returns the filtered log as CSV and the number of events in
// it. The export is itself recorded before any byte is handed over: when it
// cannot be recorded, nothing is returned.
func (s *Service) ExportAuditCSV(ctx context.Context, actor, tenant string, f AuditFilter) ([]byte, int, error) {
	if _, err := s.authorize(ctx, actor, tenant, "audit.export", tenant); err != nil {
		return nil, 0, err
	}
	if !f.valid() {
		return nil, 0, ErrInvalid
	}
	repo, err := s.auditReader()
	if err != nil {
		return nil, 0, err
	}
	var entries []AuditEntry
	var before uint64
	for len(entries) <= AuditExportMax {
		rows, err := repo.ListAudit(ctx, tenant, AuditQuery{AuditFilter: f, Before: before, Limit: AuditPageMax})
		if err != nil {
			return nil, 0, err
		}
		for _, e := range rows {
			entries = append(entries, auditEntry(e))
		}
		if len(rows) < AuditPageMax {
			break
		}
		before = rows[len(rows)-1].Sequence
	}
	if len(entries) > AuditExportMax {
		return nil, 0, ErrInvalid
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"time", "actor", "action", "target_type", "target", "reason", "event"})
	for _, e := range entries {
		_ = w.Write([]string{e.At.Format(time.RFC3339), auditCSVCell(e.ActorID), auditCSVCell(e.Action), auditCSVCell(e.TargetType), auditCSVCell(e.TargetID), auditCSVCell(e.Reason), auditCSVCell(e.EventID)})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, 0, err
	}
	reason := fmt.Sprintf("Exported %d audit events", len(entries))
	id := fmt.Sprintf("audit-export:%d", s.now().UnixNano())
	if _, err := s.AppendRecord(ctx, actor, Record{TenantID: tenant, RecordID: id, Kind: KindDerived, SourceID: id, Revision: 1}, "audit.export", reason); err != nil {
		return nil, 0, err
	}
	return buf.Bytes(), len(entries), nil
}
