package application

import (
	"context"
	"errors"
	"net/http"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3778, slice 1: the retention page's read-only preview. Like saving the
// policy, it is workspace administration decided by the verified caller's
// current administrator role. It deletes nothing and writes nothing.

// ChatRetentionPreviewPath serves the preview.
const ChatRetentionPreviewPath = "/api/chat/retention/v1/preview"

// PreviewRetention reports what the saved policy would remove, for a workspace
// administrator.
func (s *ChatExtensions) PreviewRetention(ctx context.Context, p chatcore.Principal) (chatrecords.RetentionPreviewReport, error) {
	if s == nil {
		return chatrecords.RetentionPreviewReport{}, chatcore.ErrUnavailable
	}
	ctx, err := s.retentionContext(ctx, p)
	if err != nil {
		return chatrecords.RetentionPreviewReport{}, err
	}
	return s.Records.PreviewRetention(ctx, p.SubjectID, p.TenantID)
}

// ChatRetentionPreviewService is what the HTTP edge calls.
type ChatRetentionPreviewService interface {
	PreviewRetention(context.Context, chatcore.Principal) (chatrecords.RetentionPreviewReport, error)
}

type chatRetentionPreviewKind struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// chatRetentionPreviewJSON is the wire answer: counts only, never record ids,
// conversation ids or message text.
type chatRetentionPreviewJSON struct {
	Configured     bool                       `json:"configured"`
	Mode           string                     `json:"mode"`
	Scanned        int                        `json:"scanned"`
	WouldRemove    []chatRetentionPreviewKind `json:"would_remove"`
	Total          int                        `json:"total"`
	HeldSkipped    int                        `json:"held_skipped"`
	UnknownSkipped int                        `json:"unknown_skipped"`
	UsageUnknown   bool                       `json:"usage_unknown"`
}

func chatRetentionPreviewWire(r chatrecords.RetentionPreviewReport) chatRetentionPreviewJSON {
	out := chatRetentionPreviewJSON{Configured: r.PolicyConfigured, Mode: string(r.Mode), Scanned: r.Scanned, WouldRemove: []chatRetentionPreviewKind{}, Total: r.WouldRemoveTotal, HeldSkipped: r.HeldSkipped, UnknownSkipped: r.UnknownSkipped, UsageUnknown: r.UsageUnknown}
	for _, c := range r.WouldRemove {
		out.WouldRemove = append(out.WouldRemove, chatRetentionPreviewKind{Kind: string(c.Kind), Count: c.Count})
	}
	return out
}

// ChatRetentionPreviewHTTP answers the preview. It is a POST so the answer is
// never cached or prefetched, and it takes no body.
type ChatRetentionPreviewHTTP struct{ Service ChatRetentionPreviewService }

func chatRetentionPreviewError(w http.ResponseWriter, err error) {
	status, code := http.StatusServiceUnavailable, "unavailable"
	if errors.Is(err, chatcore.ErrPermissionDenied) || errors.Is(err, chatrecords.ErrUnauthorized) {
		status, code = http.StatusForbidden, "permission_denied"
	}
	chatAuditJSON(w, status, chatAuditCode{code})
}

func (h ChatRetentionPreviewHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := trust.FromContext(r.Context())
	_, admitted := transport.InvocationFromContext(r.Context())
	if !ok || p == nil || !admitted {
		chatRetentionPreviewError(w, chatcore.ErrPermissionDenied)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		chatAuditJSON(w, http.StatusMethodNotAllowed, chatAuditCode{"method_not_allowed"})
		return
	}
	if h.Service == nil {
		chatRetentionPreviewError(w, chatcore.ErrUnavailable)
		return
	}
	report, err := h.Service.PreviewRetention(r.Context(), chatcore.Principal{TenantID: p.Tenant().String(), SubjectID: p.Subject()})
	if err != nil {
		chatRetentionPreviewError(w, err)
		return
	}
	chatAuditJSON(w, http.StatusOK, chatRetentionPreviewWire(report))
}

// OverlayChatRetentionPreview serves the preview beside the other chat overlays.
func OverlayChatRetentionPreview(next http.Handler, service ChatRetentionPreviewService, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ChatRetentionPreviewPath {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			chatAuditJSON(w, denied.HTTPStatus(), chatAuditCode{"permission_denied"})
			return
		}
		ChatRetentionPreviewHTTP{Service: service}.ServeHTTP(w, r.WithContext(ctx))
	})
}
