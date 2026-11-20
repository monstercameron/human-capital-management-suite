package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// The audit log page of Chat settings (#3780). Reading and exporting the log is
// workspace administration, decided the way retention is: by the verified
// caller's current administrator role, never by a field of the request.

// ChatAuditPath serves the audit log's list and CSV export.
const ChatAuditPath = "/api/chat/audit/v1/"

func (s *ChatExtensions) auditContext(ctx context.Context, p chatcore.Principal) (context.Context, error) {
	return s.retentionContext(ctx, p)
}

// ListAudit reads one page of the workspace's audit log for a workspace
// administrator.
func (s *ChatExtensions) ListAudit(ctx context.Context, p chatcore.Principal, f chatrecords.AuditFilter, cursor string, limit int) (chatrecords.AuditPage, error) {
	if s == nil {
		return chatrecords.AuditPage{}, chatcore.ErrUnavailable
	}
	ctx, err := s.auditContext(ctx, p)
	if err != nil {
		return chatrecords.AuditPage{}, err
	}
	return s.Records.ListAudit(ctx, p.SubjectID, p.TenantID, f, cursor, limit)
}

// ExportAudit returns the filtered log as CSV. The export is recorded in the log
// before it is handed over.
func (s *ChatExtensions) ExportAudit(ctx context.Context, p chatcore.Principal, f chatrecords.AuditFilter) ([]byte, int, error) {
	if s == nil {
		return nil, 0, chatcore.ErrUnavailable
	}
	ctx, err := s.auditContext(ctx, p)
	if err != nil {
		return nil, 0, err
	}
	return s.Records.ExportAuditCSV(ctx, p.SubjectID, p.TenantID, f)
}

// ChatAuditService is what the HTTP edge calls.
type ChatAuditService interface {
	ListAudit(context.Context, chatcore.Principal, chatrecords.AuditFilter, string, int) (chatrecords.AuditPage, error)
	ExportAudit(context.Context, chatcore.Principal, chatrecords.AuditFilter) ([]byte, int, error)
}

// ChatAuditHTTP answers list and export. Both are POST so filters never sit in
// a page address.
type ChatAuditHTTP struct{ Service ChatAuditService }

type chatAuditRequest struct {
	Actor, Action, Conversation string
	// FromDate and ToDate are calendar days (YYYY-MM-DD, UTC); ToDate is included.
	FromDate, ToDate string
	Cursor           string
	Limit            int
}

func (q chatAuditRequest) filter() (chatrecords.AuditFilter, error) {
	f := chatrecords.AuditFilter{ActorID: strings.TrimSpace(q.Actor), Action: strings.TrimSpace(q.Action), ConversationID: strings.TrimSpace(q.Conversation)}
	var err error
	if q.FromDate != "" {
		if f.From, err = time.Parse("2006-01-02", q.FromDate); err != nil {
			return f, chatcore.ErrInvalidArgument
		}
	}
	if q.ToDate != "" {
		day, err := time.Parse("2006-01-02", q.ToDate)
		if err != nil {
			return f, chatcore.ErrInvalidArgument
		}
		f.To = day.AddDate(0, 0, 1)
	}
	if !f.From.IsZero() && !f.To.IsZero() && !f.From.Before(f.To) {
		return f, chatcore.ErrInvalidArgument
	}
	return f, nil
}

type chatAuditCode struct {
	Code string `json:"code"`
}

func chatAuditJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func chatAuditError(w http.ResponseWriter, err error) {
	status, code := http.StatusServiceUnavailable, "unavailable"
	switch {
	case errors.Is(err, chatcore.ErrPermissionDenied), errors.Is(err, chatrecords.ErrUnauthorized):
		status, code = http.StatusForbidden, "permission_denied"
	case errors.Is(err, chatcore.ErrInvalidArgument), errors.Is(err, chatrecords.ErrInvalid):
		status, code = http.StatusBadRequest, "invalid_request"
	}
	chatAuditJSON(w, status, chatAuditCode{code})
}

func (h ChatAuditHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := trust.FromContext(r.Context())
	_, admitted := transport.InvocationFromContext(r.Context())
	if !ok || p == nil || !admitted {
		chatAuditError(w, chatcore.ErrPermissionDenied)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		chatAuditJSON(w, http.StatusMethodNotAllowed, chatAuditCode{"method_not_allowed"})
		return
	}
	action := strings.TrimPrefix(r.URL.Path, ChatAuditPath)
	if action != "list" && action != "export" {
		chatAuditJSON(w, http.StatusNotFound, chatAuditCode{"not_found"})
		return
	}
	if h.Service == nil {
		chatAuditError(w, chatcore.ErrUnavailable)
		return
	}
	var q chatAuditRequest
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	d.DisallowUnknownFields()
	if d.Decode(&q) != nil || d.Decode(new(any)) != io.EOF {
		chatAuditError(w, chatcore.ErrInvalidArgument)
		return
	}
	filter, err := q.filter()
	if err != nil {
		chatAuditError(w, err)
		return
	}
	principal := chatcore.Principal{TenantID: p.Tenant().String(), SubjectID: p.Subject()}
	if action == "list" {
		page, err := h.Service.ListAudit(r.Context(), principal, filter, q.Cursor, q.Limit)
		if err != nil {
			chatAuditError(w, err)
			return
		}
		chatAuditJSON(w, http.StatusOK, page)
		return
	}
	body, _, err := h.Service.ExportAudit(r.Context(), principal, filter)
	if err != nil {
		chatAuditError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="chat-audit-log.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(body)
}

// OverlayChatAudit serves the audit log beside the other chat overlays.
func OverlayChatAudit(next http.Handler, service ChatAuditService, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, ChatAuditPath) {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			chatAuditJSON(w, denied.HTTPStatus(), chatAuditCode{"permission_denied"})
			return
		}
		ChatAuditHTTP{Service: service}.ServeHTTP(w, r.WithContext(ctx))
	})
}
