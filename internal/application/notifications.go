// Notification feed backing for NAAS-004.
//
// The transport adapter (internal/transport/notification) forwards the four
// NotificationService RPCs to the transport.NotificationHandler port. This
// file implements that port over the durable inbox store: recipient-owned
// reads through bounded keyset pages with HMAC-signed cursors that bind
// principal, tenant and canonical filters, and read/pin/archive CAS
// mutations that never approve work or resume a workflow.
//
// The recipient and tenant always come from the authenticated principal in
// the context; the wire carries neither. A visibility-check failure fails
// the read outright rather than serving a partial list.
package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	notificationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/notification/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/inbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/list"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// VisibilityChecker rechecks current assignment and workflow access for one
// bounded batch of workflow instance references. It returns the visible
// subset; records outside it are withheld. An error fails the read outright:
// the feed never serves a partial list.
type VisibilityChecker interface {
	VisibleWorkflows(ctx context.Context, tenant uuid.UUID, subject string, instanceIDs []uuid.UUID) (map[uuid.UUID]bool, error)
}

// NotificationFeed implements transport.NotificationHandler over the inbox
// store. The zero value is not usable: NewNotificationFeed validates.
//
// Every call runs in its own short tenant-scoped transaction: the inbox
// tables enforce tenant isolation through row-level security, which only
// admits rows when app.tenant_id names the caller, so sharing a bare
// connection across tenants would silently read nothing.
type NotificationFeed struct {
	db         dbport.Beginner
	tenants    func(values.TenantId) (uuid.UUID, error)
	visibility VisibilityChecker
	secret     string
	now        func() time.Time
}

// NewNotificationFeed builds the feed. db opens the short transactions;
// tenants maps the principal's tenant to its storage id; visibility
// rechecks workflow access per batch; secret signs page cursors; now is the
// clock (nil means time.Now().UTC).
func NewNotificationFeed(db dbport.Beginner, tenants func(values.TenantId) (uuid.UUID, error), visibility VisibilityChecker, secret string, now func() time.Time) (*NotificationFeed, error) {
	if db == nil {
		return nil, errors.New("application: notification feed database is required")
	}
	if tenants == nil {
		return nil, errors.New("application: notification feed tenant resolver is required")
	}
	if visibility == nil {
		return nil, errors.New("application: notification feed visibility checker is required")
	}
	if secret == "" {
		return nil, errors.New("application: notification feed cursor secret is required")
	}
	if now == nil {
		now = time.Now().UTC
	}
	return &NotificationFeed{db: db, tenants: tenants, visibility: visibility, secret: secret, now: now}, nil
}

func (f *NotificationFeed) withTenantTx(ctx context.Context, tenant uuid.UUID, fn func(dbport.Tx) error) error {
	if f.db == nil {
		return envelope.New(envelope.CodeUnavailable, "notification-store", "the notification store is unavailable")
	}
	tx, err := f.db.Begin(ctx)
	if err != nil {
		return envelope.New(envelope.CodeUnavailable, "notification-store", "the notification store is unavailable")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return envelope.New(envelope.CodeUnavailable, "notification-store", "the notification store is unavailable")
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return envelope.New(envelope.CodeUnavailable, "notification-store", "the notification store is unavailable")
	}
	return nil
}

func notificationPrincipal(ctx context.Context) (*trust.Principal, error) {
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil {
		return nil, envelope.New(envelope.CodeUnauthenticated, "notification-principal", "the request carries no verified principal")
	}
	return principal, nil
}

func (f *NotificationFeed) resolveTenant(principal *trust.Principal) (uuid.UUID, error) {
	id, err := f.tenants(principal.Tenant())
	if err != nil {
		return uuid.Nil, envelope.New(envelope.CodePermissionDenied, "notification-tenant", "the principal's tenant is unknown")
	}
	if id == uuid.Nil {
		return uuid.Nil, envelope.New(envelope.CodePermissionDenied, "notification-tenant", "the principal's tenant is unknown")
	}
	return id, nil
}

// filterKey is the canonical membership-affecting filter set bound into page
// cursors: a cursor minted for one filter set never pages another.
func filterKey(purpose, readState string, archived bool) string {
	return purpose + "\x00" + readState + "\x00" + fmt.Sprintf("%t", archived)
}

func encodePosition(at time.Time, id uuid.UUID) string {
	return at.UTC().Format(time.RFC3339Nano) + "|" + id.String()
}

func decodePosition(watermark string) (inbox.Position, error) {
	parts := strings.SplitN(watermark, "|", 2)
	if len(parts) != 2 {
		return inbox.Position{}, errors.New("invalid cursor watermark")
	}
	at, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return inbox.Position{}, errors.New("invalid cursor watermark")
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return inbox.Position{}, errors.New("invalid cursor watermark")
	}
	return inbox.Position{CreatedAt: at, RecordID: id}, nil
}

func toProtoRecord(r inbox.WorkflowRecord) *notificationv1.Notification {
	return &notificationv1.Notification{
		Id:                 r.InboxRecordID.String(),
		ReadState:          r.ReadState,
		Archived:           r.Archived,
		Pinned:             r.Pinned,
		Version:            r.Version,
		CreatedAt:          timestamppb.New(r.CreatedAt),
		StateChangedAt:     timestamppb.New(r.StateChangedAt),
		WorkflowInstanceId: r.InstanceID.String(),
		WorkItemId:         r.WorkItemID.String(),
		Purpose:            r.Purpose,
		CorrelationKey:     r.CorrelationID,
	}
}

// ListNotifications implements transport.NotificationHandler.
func (f *NotificationFeed) ListNotifications(ctx context.Context, req *notificationv1.ListNotificationsRequest) (*notificationv1.ListNotificationsResponse, error) {
	principal, err := notificationPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	subject := principal.Subject()
	if subject == "" {
		return nil, envelope.New(envelope.CodeUnauthenticated, "notification-subject", "the principal names no subject")
	}
	tenant, err := f.resolveTenant(principal)
	if err != nil {
		return nil, err
	}
	size, err := list.NormalizePageSize(int(req.GetPageSize()))
	if err != nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "notification-page-size", "page size must be 1..100")
	}
	key := filterKey(req.GetPurpose(), req.GetReadState(), req.GetArchived())
	query := inbox.WorkflowPageQuery{
		PageQuery: inbox.PageQuery{Limit: size, ReadState: req.GetReadState(), Archived: req.GetArchived()},
		Purpose:   req.GetPurpose(),
	}
	if req.GetCursor() != "" {
		payload, err := list.DecodeCursor(req.GetCursor(), f.secret, f.now(), subject, tenant.String(), key)
		if err != nil {
			return nil, envelope.New(envelope.CodeInvalidArgument, "notification-cursor", "the page cursor is forged, expired or bound to another recipient, tenant or filter")
		}
		pos, err := decodePosition(payload.Watermark)
		if err != nil {
			return nil, envelope.New(envelope.CodeInvalidArgument, "notification-cursor", "the page cursor is forged, expired or bound to another recipient, tenant or filter")
		}
		query.After = &pos
	}
	var page inbox.WorkflowPage
	if err := f.withTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var err error
		page, err = (inbox.Store{}).WorkflowNoticesPage(ctx, tx, tenant, subject, query)
		if err != nil {
			if errors.Is(err, inbox.ErrInvalid) {
				return envelope.New(envelope.CodeInvalidArgument, "notification-filter", "the feed filter is invalid")
			}
			return envelope.New(envelope.CodeUnavailable, "notification-store", "the notification store is unavailable")
		}
		return nil
	}); err != nil {
		return nil, err
	}
	visible, err := f.visibility.VisibleWorkflows(ctx, tenant, subject, instanceIDsOf(page.Records))
	if err != nil {
		return nil, envelope.New(envelope.CodeUnavailable, "notification-visibility", "workflow access cannot be rechecked right now")
	}
	out := &notificationv1.ListNotificationsResponse{}
	for _, r := range page.Records {
		if !visible[r.InstanceID] {
			continue
		}
		out.Notifications = append(out.Notifications, toProtoRecord(r))
	}
	if page.Next != nil {
		nonce := make([]byte, 8)
		if _, err := rand.Read(nonce); err != nil {
			return nil, envelope.New(envelope.CodeUnavailable, "notification-cursor", "a continuation cursor cannot be minted right now")
		}
		next, err := list.EncodeCursor(list.CursorPayload{
			Principal: subject,
			Tenant:    tenant.String(),
			Filter:    key,
			Watermark: encodePosition(page.Next.CreatedAt, page.Next.RecordID),
			Version:   1,
			ExpiresAt: f.now().Add(list.CursorTTL).Unix(),
			Nonce:     hex.EncodeToString(nonce),
		}, f.secret)
		if err != nil {
			return nil, envelope.New(envelope.CodeUnavailable, "notification-cursor", "a continuation cursor cannot be minted right now")
		}
		out.NextCursor = next
	}
	return out, nil
}

func instanceIDsOf(records []inbox.WorkflowRecord) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0, len(records))
	for _, r := range records {
		if !seen[r.InstanceID] {
			seen[r.InstanceID] = true
			out = append(out, r.InstanceID)
		}
	}
	return out
}

func parseRecordID(id string) (uuid.UUID, error) {
	recordID, err := uuid.Parse(strings.TrimSpace(id))
	if err != nil {
		return uuid.Nil, envelope.New(envelope.CodeInvalidArgument, "notification-id", "the notification id is not a UUID")
	}
	return recordID, nil
}

func projectInboxError(op string, recordID uuid.UUID, err error) error {
	switch {
	case errors.Is(err, inbox.ErrVersionConflict):
		return envelope.New(envelope.CodeFailedPrecondition, "notification-version", fmt.Sprintf("%s refused: the notice changed under the presented version", op))
	case errors.Is(err, inbox.ErrNotFound):
		return envelope.New(envelope.CodeNotFound, "notification-record", "the notice does not exist for this recipient")
	case errors.Is(err, inbox.ErrInvalid):
		return envelope.New(envelope.CodeInvalidArgument, "notification-record", "the notice reference is invalid")
	default:
		return envelope.New(envelope.CodeUnavailable, "notification-store", "the notification store is unavailable")
	}
}

// MarkNotificationRead implements transport.NotificationHandler: recording a
// read never approves work or resumes a workflow.
func (f *NotificationFeed) MarkNotificationRead(ctx context.Context, req *notificationv1.MarkNotificationReadRequest) (*notificationv1.MarkNotificationReadResponse, error) {
	rec, err := f.transition(ctx, req.GetId(), req.GetExpectedVersion(), func(s inbox.Store, ctx context.Context, ex inbox.Executor, tenant uuid.UUID, subject string, recordID uuid.UUID, version uint64, at time.Time) error {
		return s.MarkRead(ctx, ex, tenant, subject, recordID, version, at)
	})
	if err != nil {
		return nil, err
	}
	return &notificationv1.MarkNotificationReadResponse{Notification: rec}, nil
}

// ArchiveNotification implements transport.NotificationHandler.
func (f *NotificationFeed) ArchiveNotification(ctx context.Context, req *notificationv1.ArchiveNotificationRequest) (*notificationv1.ArchiveNotificationResponse, error) {
	rec, err := f.transition(ctx, req.GetId(), req.GetExpectedVersion(), func(s inbox.Store, ctx context.Context, ex inbox.Executor, tenant uuid.UUID, subject string, recordID uuid.UUID, version uint64, at time.Time) error {
		return s.Archive(ctx, ex, tenant, subject, recordID, req.GetArchived(), version, at)
	})
	if err != nil {
		return nil, err
	}
	return &notificationv1.ArchiveNotificationResponse{Notification: rec}, nil
}

// PinNotification implements transport.NotificationHandler.
func (f *NotificationFeed) PinNotification(ctx context.Context, req *notificationv1.PinNotificationRequest) (*notificationv1.PinNotificationResponse, error) {
	rec, err := f.transition(ctx, req.GetId(), req.GetExpectedVersion(), func(s inbox.Store, ctx context.Context, ex inbox.Executor, tenant uuid.UUID, subject string, recordID uuid.UUID, version uint64, at time.Time) error {
		return s.Pin(ctx, ex, tenant, subject, recordID, req.GetPinned(), version, at)
	})
	if err != nil {
		return nil, err
	}
	return &notificationv1.PinNotificationResponse{Notification: rec}, nil
}

func (f *NotificationFeed) transition(ctx context.Context, id string, version uint64, apply func(inbox.Store, context.Context, inbox.Executor, uuid.UUID, string, uuid.UUID, uint64, time.Time) error) (*notificationv1.Notification, error) {
	principal, err := notificationPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	subject := principal.Subject()
	if subject == "" {
		return nil, envelope.New(envelope.CodeUnauthenticated, "notification-subject", "the principal names no subject")
	}
	tenant, err := f.resolveTenant(principal)
	if err != nil {
		return nil, err
	}
	recordID, err := parseRecordID(id)
	if err != nil {
		return nil, err
	}
	now := f.now()
	var loaded inbox.Record
	if err := f.withTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if err := apply(inbox.Store{}, ctx, tx, tenant, subject, recordID, version, now); err != nil {
			return projectInboxError("mutation", recordID, err)
		}
		var err error
		loaded, err = (inbox.Store{}).Load(ctx, tx, tenant, subject, recordID)
		if err != nil {
			return projectInboxError("reload", recordID, err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return &notificationv1.Notification{
		Id:             loaded.InboxRecordID.String(),
		ReadState:      loaded.ReadState,
		Archived:       loaded.Archived,
		Pinned:         loaded.Pinned,
		Version:        loaded.Version,
		CreatedAt:      timestamppb.New(loaded.CreatedAt),
		StateChangedAt: timestamppb.New(loaded.StateChangedAt),
	}, nil
}
