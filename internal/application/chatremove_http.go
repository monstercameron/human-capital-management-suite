package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const ChatModerationPath = "/api/chat/moderation"

// ChatModerationReports is implemented by ChatExtensions, so ReportAbuse and
// the JSON form reach the same report service rather than separate RPCs.
type ChatModerationReports interface {
	Report(context.Context, chat.Principal, chatrecords.Report) error
}
type ChatModerationAdmission interface {
	CanModerate(context.Context, chat.Principal, string, string, string) error
	CanReportMessage(context.Context, chat.Principal, string, string, string) error
	AssignModerationPermission(context.Context, chat.Principal, string, string, string, string, bool) error
}

type ChatModerationHTTP struct {
	Service     *chat.ModerationService
	Reports     ChatModerationReports
	Permissions ChatModerationAdmission
	PageContext ChatModerationContext
	Directory   ChatModerationDirectory
}

type chatremoveInput struct {
	HostTenantID                                                    string
	Removal                                                         chat.RemovalRequest
	ConversationID, PostID, CaseID, Action, ReasonCode, Note, Query string
	Role, Permission                                                string
	Allowed                                                         bool
}

type chatremoveError struct {
	Code      string `json:"code"`
	Retryable bool   `json:"retryable"`
}

func chatremoveHTTPError(w http.ResponseWriter, err error) {
	status, code, retry := http.StatusInternalServerError, "internal", false
	switch {
	case errors.Is(err, chat.ErrUnauthenticated):
		status, code = http.StatusUnauthorized, "unauthenticated"
	case errors.Is(err, chat.ErrPermissionDenied):
		status, code = http.StatusForbidden, "permission_denied"
	case errors.Is(err, chat.ErrInvalidArgument):
		status, code = http.StatusBadRequest, "invalid_argument"
	case errors.Is(err, chat.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, chat.ErrConflict):
		status, code = http.StatusConflict, "conflict"
	case errors.Is(err, chat.ErrUnavailable):
		status, code, retry = http.StatusServiceUnavailable, "unavailable", true
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(chatremoveError{Code: code, Retryable: retry})
}

func (h ChatModerationHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	principal, ok := trust.FromContext(r.Context())
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || !principal.ExpiresAt().After(time.Now()) {
		chatremoveHTTPError(w, chat.ErrUnauthenticated)
		return
	}
	p := chat.Principal{TenantID: principal.Tenant().String(), SubjectID: principal.Subject()}
	if h.Service == nil || h.Service.Store == nil {
		chatremoveHTTPError(w, chat.ErrUnavailable)
		return
	}
	var input chatremoveInput
	if r.Method == http.MethodGet {
		input.Query = r.URL.Query().Get("query")
	} else if r.Method == http.MethodPost {
		if media := strings.Split(r.Header.Get("Content-Type"), ";")[0]; media != "application/json" {
			chatremoveHTTPError(w, chat.ErrInvalidArgument)
			return
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 24<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			chatremoveHTTPError(w, chat.ErrInvalidArgument)
			return
		}
		if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
			chatremoveHTTPError(w, chat.ErrInvalidArgument)
			return
		}
	} else {
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, ChatModerationPath)
	if (r.Method == http.MethodGet && path != "" && path != "/notices" && path != "/summary") || (r.Method == http.MethodPost && (path == "" || path == "/notices" || path == "/summary")) {
		chatremoveHTTPError(w, chat.ErrInvalidArgument)
		return
	}
	var result any
	var err error
	switch path {
	case "":
		var items []chat.ModerationItem
		items, err = h.Service.Queue(r.Context(), p, p.TenantID, input.Query)
		var names map[string]string
		if err == nil && h.Directory != nil {
			ids := []string{}
			for _, item := range items {
				ids = append(ids, item.AuthorID)
				if item.ReporterID != "" {
					ids = append(ids, item.ReporterID)
				}
			}
			names, err = h.Directory.ModerationNames(r.Context(), p, p.TenantID, ids)
		}
		result = struct {
			Items []chat.ModerationItem
			Count int
			Names map[string]string
		}{items, len(items), names}
	case "/preview":
		input.Removal.Principal, input.Removal.TenantID = p, p.TenantID
		result, err = h.Service.Preview(r.Context(), input.Removal)
	case "/apply":
		input.Removal.Principal, input.Removal.TenantID = p, p.TenantID
		var count int
		count, err = h.Service.Apply(r.Context(), input.Removal)
		result = struct{ Count int }{count}
	case "/review":
		result, err = h.Service.Store.ReviewRemoved(r.Context(), p, p.TenantID, input.ConversationID, input.PostID, input.Note, time.Now().UTC())
	case "/summary":
		result, err = h.Service.Summary(r.Context(), p, p.TenantID)
	case "/notices":
		host := r.URL.Query().Get("tenant")
		if host == "" {
			host = p.TenantID
		}
		result, err = h.Service.Store.ModerationNotices(r.Context(), p, host)
	case "/appeal":
		host := input.HostTenantID
		if host == "" {
			host = p.TenantID
		}
		err = h.Service.Store.AppealRemoval(r.Context(), p, host, input.ConversationID, input.PostID, time.Now().UTC())
		result = map[string]bool{"accepted": true}
	case "/resolve":
		reason := input.Note
		if input.Action == "remove" {
			if !chat.ValidRemovalReason(input.ReasonCode) {
				err = chat.ErrInvalidArgument
				break
			}
			reason = input.ReasonCode
			if input.Note != "" {
				reason += " — " + input.Note
			}
		}
		if strings.TrimSpace(reason) == "" {
			// Dismiss and Restore need no explanation from the moderator; the
			// record still says why, in a copy key shown in the reader's language.
			switch input.Action {
			case "dismiss":
				reason = "no_action"
			case "restore":
				reason = "restored"
			}
		}
		err = h.Service.Resolve(r.Context(), p, p.TenantID, input.CaseID, input.Action, reason)
		result = map[string]bool{"resolved": true}
	case "/permissions":
		if h.Permissions == nil {
			err = chat.ErrUnavailable
			break
		}
		err = h.Permissions.AssignModerationPermission(r.Context(), p, p.TenantID, input.ConversationID, input.Role, input.Permission, input.Allowed)
		result = map[string]bool{"assigned": true}
	case "/report":
		if h.Reports == nil || h.Permissions == nil {
			err = chat.ErrUnavailable
			break
		}
		if !chat.ValidRemovalReason(input.ReasonCode) || len(input.Note) > 2000 {
			err = chat.ErrInvalidArgument
			break
		}
		if err = h.Permissions.CanReportMessage(r.Context(), p, p.TenantID, input.ConversationID, input.PostID); err != nil {
			break
		}
		reason := input.ReasonCode
		if input.Note != "" {
			reason += " — " + input.Note
		}
		err = h.Reports.Report(r.Context(), p, chatrecords.Report{ReportID: uuid.NewString(), ConversationID: input.ConversationID, TargetID: input.PostID, Reason: reason})
		result = map[string]bool{"reported": true}
	default:
		err = chat.ErrNotFound
	}
	if err != nil {
		chatremoveHTTPError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

func OverlayChatModeration(next http.Handler, h ChatModerationHTTP, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ChatModerationPagePath && r.URL.Path != ChatModerationPath && !strings.HasPrefix(r.URL.Path, ChatModerationPath+"/") {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(denied.HTTPStatus())
			_ = json.NewEncoder(w).Encode(chatremoveError{Code: "request_denied"})
			return
		}
		if r.URL.Path == ChatModerationPagePath {
			ChatModerationPageHTTP{HTTP: h, Context: h.PageContext, Directory: h.Directory}.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}
