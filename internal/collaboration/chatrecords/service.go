package chatrecords

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Service struct {
	Repo  Repository
	Auth  Authorizer
	Clock func() time.Time
}

func (s *Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}
func (s *Service) authorize(ctx context.Context, actor, tenant, action, target string) (string, error) {
	if s.Repo == nil || strings.TrimSpace(actor) == "" || strings.TrimSpace(tenant) == "" {
		return "", ErrInvalid
	}
	if s.Auth == nil {
		return "", ErrUnauthorized
	}
	return s.Auth.Authorize(ctx, actor, tenant, action, target)
}

func (s *Service) Audit(ctx context.Context, actor, tenant, action, target string, prior uint64, reason string) (AuditEvent, error) {
	evidence, err := s.authorize(ctx, actor, tenant, action, target)
	if err != nil {
		return AuditEvent{}, err
	}
	if strings.TrimSpace(reason) == "" {
		return AuditEvent{}, ErrInvalid
	}
	events, err := s.Repo.Events(ctx, tenant)
	if err != nil {
		return AuditEvent{}, err
	}
	seq := uint64(len(events) + 1)
	at := s.now()
	e := AuditEvent{TenantID: tenant, EventID: fmt.Sprintf("%s-%d", target, seq), Sequence: seq, ActorID: actor, Action: action, TargetType: "chat", TargetID: target, PriorRevision: prior, Reason: reason, PolicyEvidence: evidence, At: at}
	e.Digest = digestEvent(e)
	return e, nil
}

func (s *Service) AppendRecord(ctx context.Context, actor string, r Record, action, reason string) (AuditEvent, error) {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = s.now()
	}
	e, err := s.Audit(ctx, actor, r.TenantID, action, r.RecordID, r.Revision, reason)
	if err != nil {
		return AuditEvent{}, err
	}
	if atomic, ok := s.Repo.(interface {
		AppendEvent(context.Context, Record, AuditEvent, OutboxEvent) (AuditEvent, error)
	}); ok {
		return atomic.AppendEvent(ctx, r, e, OutboxEvent{ID: e.EventID, Sequence: e.Sequence, EventID: e.EventID})
	}
	if err = s.Repo.Append(ctx, r, e, OutboxEvent{ID: e.EventID, Sequence: e.Sequence, EventID: e.EventID}); err != nil {
		return AuditEvent{}, err
	}
	return e, nil
}

func (s *Service) PlaceHold(ctx context.Context, actor string, h Hold) error {
	if h.PlacedAt.IsZero() {
		h.PlacedAt = s.now()
	}
	evidence, err := s.authorize(ctx, actor, h.TenantID, "hold.place", h.HoldID)
	if err != nil {
		return err
	}
	if h.PlacedBy != actor {
		return ErrUnauthorized
	}
	_ = evidence
	return s.Repo.PutHold(ctx, h)
}

func (s *Service) Export(ctx context.Context, actor, tenant, exportID string) (Export, error) {
	if _, err := s.authorize(ctx, actor, tenant, "records.export", exportID); err != nil {
		return Export{}, err
	}
	rows, err := s.Repo.List(ctx, tenant)
	if err != nil {
		return Export{}, err
	}
	// Export the complete tenant inventory. Legal holds govern disposition;
	// they must not make historical records disappear from an export.
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.RecordID)
	}
	sort.Strings(ids)
	e := Export{TenantID: tenant, ExportID: exportID, RecordIDs: ids, CreatedAt: s.now()}
	e.Digest = digestStrings(ids)
	if err = s.Repo.PutExport(ctx, e); err != nil {
		return Export{}, err
	}
	return e, nil
}

func (s *Service) Dispose(ctx context.Context, actor string, r Record, reason string) error {
	if _, err := s.authorize(ctx, actor, r.TenantID, "records.dispose", r.RecordID); err != nil {
		return err
	}
	holds, err := s.Repo.Holds(ctx, r.TenantID)
	if err != nil {
		return err
	}
	for _, h := range holds {
		if h.Active() {
			for _, id := range r.HoldIDs {
				if id == h.HoldID {
					return ErrHeld
				}
			}
		}
	}
	r.Disposition = "DISPOSED"
	_, err = s.AppendRecord(ctx, actor, r, "records.dispose", reason)
	return err
}

func (s *Service) Report(ctx context.Context, actor string, p Report) error {
	if p.ReporterID != actor {
		return ErrUnauthorized
	}
	if _, err := s.authorize(ctx, actor, p.TenantID, "moderation.report", p.TargetID); err != nil {
		return err
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = s.now()
	}
	if p.State == "" {
		p.State = "OPEN"
	}
	return s.Repo.PutReport(ctx, p)
}
func (s *Service) Moderate(ctx context.Context, actor, tenant, caseID, action, target, reason, evidenceRef string) error {
	if strings.TrimSpace(evidenceRef) == "" {
		return ErrPrivateEvidence
	}
	if _, err := s.authorize(ctx, actor, tenant, "moderation."+action, target); err != nil {
		return err
	}
	return s.Repo.PutCaseAction(ctx, tenant, CaseAction{CaseID: caseID, Action: action, ActorID: actor, Reason: reason, EvidenceRef: evidenceRef, At: s.now()})
}

// ModerateAudited is the served moderation path. The caller supplies the
// current conversation revision from its authorization read; the repository
// assigns the tenant event sequence and commits action, audit, and outbox
// together.
func (s *Service) ModerateAudited(ctx context.Context, actor, tenant, conversation, caseID, action, target, reason, evidenceRef string, priorRevision uint64) (AuditEvent, error) {
	if strings.TrimSpace(evidenceRef) == "" {
		return AuditEvent{}, ErrPrivateEvidence
	}
	if strings.TrimSpace(conversation) == "" || strings.TrimSpace(reason) == "" || strings.TrimSpace(caseID) == "" || strings.TrimSpace(action) == "" || strings.TrimSpace(target) == "" || priorRevision == 0 {
		return AuditEvent{}, ErrInvalid
	}
	repo, ok := s.Repo.(AuditedCaseActionRepository)
	if !ok {
		return AuditEvent{}, ErrAuditUnavailable
	}
	evidence, err := s.authorize(ctx, actor, tenant, "moderation."+action, target)
	if err != nil {
		return AuditEvent{}, err
	}
	at := s.now()
	entry := CaseAction{CaseID: caseID, Action: action, ActorID: actor, Reason: reason, EvidenceRef: evidenceRef, At: at}
	event := AuditEvent{TenantID: tenant, ActorID: actor, Action: "moderation." + action, TargetType: "chat_post", TargetID: target, PriorRevision: priorRevision, Reason: reason, PolicyEvidence: evidence, At: at}
	return repo.PutCaseActionAudited(ctx, tenant, conversation, entry, event)
}
func (s *Service) Snapshot(ctx context.Context, actor, tenant string) (Snapshot, error) {
	if _, err := s.authorize(ctx, actor, tenant, "records.backup", tenant); err != nil {
		return Snapshot{}, err
	}
	return s.Repo.Snapshot(ctx, tenant)
}
func (s *Service) Restore(ctx context.Context, actor string, snap Snapshot) (ReconcileResult, error) {
	if _, err := s.authorize(ctx, actor, snap.TenantID, "records.restore", snap.TenantID); err != nil {
		return ReconcileResult{}, err
	}
	if err := s.Repo.Restore(ctx, snap); err != nil {
		return ReconcileResult{}, err
	}
	return s.Repo.Reconcile(ctx, snap.TenantID)
}
func (s *Service) Reconcile(ctx context.Context, actor, tenant string) (ReconcileResult, error) {
	if _, err := s.authorize(ctx, actor, tenant, "records.reconcile", tenant); err != nil {
		return ReconcileResult{}, err
	}
	return s.Repo.Reconcile(ctx, tenant)
}

func digestEvent(e AuditEvent) string {
	e.Digest = ""
	return digestStrings([]string{e.TenantID, e.EventID, fmt.Sprint(e.Sequence), e.ActorID, e.Action, e.TargetID, fmt.Sprint(e.PriorRevision), e.Reason, e.PolicyEvidence, e.At.UTC().Format(time.RFC3339Nano)})
}

// DigestEvent returns the canonical body-free audit digest for durable adapters.
func DigestEvent(e AuditEvent) string { return digestEvent(e) }
func digestStrings(v []string) string {
	h := sha256.New()
	for _, x := range v {
		_, _ = h.Write([]byte(x))
		_, _ = h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

var _ = errors.Is
