package agentcontrols

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

const AnnouncementPath = Path + "/announcements"

type AnnouncementDraft struct {
	ID               string                  `json:"id"`
	InstallationID   string                  `json:"installation_id"`
	PersonaID        string                  `json:"persona_id"`
	ConversationID   string                  `json:"conversation_id"`
	Instruction      string                  `json:"instruction"`
	Documents        []agentdocref.Reference `json:"documents"`
	Cadence          string                  `json:"cadence"`
	Time             string                  `json:"time"`
	Zone             string                  `json:"zone"`
	Weekdays         []int                   `json:"weekdays"`
	MonthDay         int                     `json:"month_day"`
	ExpectedRevision uint64                  `json:"expected_revision"`
	IdempotencyKey   string                  `json:"idempotency_key"`
	PreviewDigest    string                  `json:"preview_digest,omitempty"`
}

type AnnouncementCommand struct {
	ID               string `json:"id"`
	Action           string `json:"action"`
	ExpectedRevision uint64 `json:"expected_revision"`
	IdempotencyKey   string `json:"idempotency_key"`
	PreviewDigest    string `json:"preview_digest,omitempty"`
	RetryOccurrence  string `json:"retry_occurrence,omitempty"`
}

type AnnouncementPreview struct {
	Text, PublicStatus string
	Digest             string
	Public             bool
	Sources            []AnnouncementPreviewSource
}

type AnnouncementPreviewSource struct{ Title, Href string }

type AnnouncementReply struct {
	Snapshot productui.AgentAnnouncementsSnapshot `json:"snapshot"`
	Preview  *AnnouncementPreview                 `json:"preview,omitempty"`
}

// AnnouncementSurface derives actor and tenant exclusively from verified
// context. The HTTP body supplies no principal and no tenant.
type AnnouncementSurface interface {
	ListAnnouncements(context.Context) (AnnouncementReply, error)
	PreviewAnnouncement(context.Context, AnnouncementDraft) (AnnouncementReply, error)
	CreateAnnouncement(context.Context, AnnouncementDraft) (AnnouncementReply, error)
	UpdateAnnouncement(context.Context, AnnouncementDraft) (AnnouncementReply, error)
	ControlAnnouncement(context.Context, AnnouncementCommand) (AnnouncementReply, error)
}

type AnnouncementHandler struct{ Surface AnnouncementSurface }

func (h AnnouncementHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if h.Surface == nil {
		writeAnnouncementReply(w, AnnouncementReply{}, ErrUnavailable)
		return
	}
	var reply AnnouncementReply
	var err error
	switch {
	case r.URL.Path == AnnouncementPath && r.Method == http.MethodGet:
		reply, err = h.Surface.ListAnnouncements(r.Context())
	case r.URL.Path == AnnouncementPath+"/preview" && r.Method == http.MethodPost:
		var draft AnnouncementDraft
		if err = decodeAnnouncementRequest(w, r, &draft); err == nil {
			reply, err = h.Surface.PreviewAnnouncement(r.Context(), draft)
		}
	case r.URL.Path == AnnouncementPath && r.Method == http.MethodPost:
		var draft AnnouncementDraft
		if err = decodeAnnouncementRequest(w, r, &draft); err == nil {
			reply, err = h.Surface.CreateAnnouncement(r.Context(), draft)
		}
	case r.URL.Path == AnnouncementPath+"/update" && r.Method == http.MethodPost:
		var draft AnnouncementDraft
		if err = decodeAnnouncementRequest(w, r, &draft); err == nil {
			reply, err = h.Surface.UpdateAnnouncement(r.Context(), draft)
		}
	case r.URL.Path == AnnouncementPath+"/control" && r.Method == http.MethodPost:
		var command AnnouncementCommand
		if err = decodeAnnouncementRequest(w, r, &command); err == nil {
			reply, err = h.Surface.ControlAnnouncement(r.Context(), command)
		}
	default:
		http.NotFound(w, r)
		return
	}
	writeAnnouncementReply(w, reply, err)
}

func decodeAnnouncementRequest(w http.ResponseWriter, r *http.Request, value any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 24<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || decoder.Decode(new(any)) != io.EOF {
		return ErrInvalid
	}
	return nil
}

func writeAnnouncementReply(w http.ResponseWriter, reply AnnouncementReply, err error) {
	if err == nil {
		_ = json.NewEncoder(w).Encode(reply)
		return
	}
	status, code := http.StatusServiceUnavailable, "unavailable"
	switch {
	case errors.Is(err, ErrUnauthenticated):
		status, code = http.StatusUnauthorized, "unauthenticated"
	case errors.Is(err, ErrDenied):
		status, code = http.StatusForbidden, "denied"
	case errors.Is(err, ErrInvalid):
		status, code = http.StatusBadRequest, "invalid"
	case errors.Is(err, ErrConflict):
		status, code = http.StatusConflict, "conflict"
	default:
		// The body never carries the cause. A developer cell can opt in to
		// seeing it in the server log, as the other agent surfaces do.
		if os.Getenv("HCMNEXT_AGENT_DEBUG_CAUSES") == "1" {
			slog.Warn("hcmnext.agent_announcement_unavailable", "cause", err.Error())
		}
	}
	w.WriteHeader(status)
	body := map[string]string{"error": code}
	var plain interface{ AnnouncementReason() string }
	if errors.As(err, &plain) {
		body["reason"] = plain.AnnouncementReason()
	}
	_ = json.NewEncoder(w).Encode(body)
}
