package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3995: the approval clarification routes, under /api/chat/ (the prefix
// the page's content security policy lets the client call):
//
//	GET  /api/chat/journeys/{id}/clarification   what this person may see and ask
//	POST /api/chat/journeys/{id}/clarification   ask the requester a reviewed question
const (
	approvalClarificationPrefix = "/api/chat/journeys/"
	approvalClarificationSuffix = "/clarification"
)

// ApprovalClarificationsPort is the use case as the HTTP surface uses it.
type ApprovalClarificationsPort interface {
	View(context.Context, *trust.Principal, string) (ApprovalClarificationView, error)
	Ask(context.Context, *trust.Principal, ApprovalClarificationAsk) (ApprovalClarificationAsked, error)
}

func approvalClarificationID(path string) (string, bool) {
	rest, ok := strings.CutPrefix(path, approvalClarificationPrefix)
	if !ok {
		return "", false
	}
	escaped, ok := strings.CutSuffix(rest, approvalClarificationSuffix)
	if !ok || escaped == "" || strings.Contains(escaped, "/") {
		return "", false
	}
	id, err := url.PathUnescape(escaped)
	if err != nil || id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

type approvalClarificationCommand struct {
	Revision       string `json:"revision"`
	Question       string `json:"question"`
	IdempotencyKey string `json:"idempotency_key"`
}

func approvalClarificationHTTPError(w http.ResponseWriter, err error) {
	status, code := 0, ""
	switch {
	case errors.Is(err, ErrApprovalClarificationStale):
		status, code = http.StatusConflict, "revision_changed"
	case errors.Is(err, ErrApprovalClarificationClosed):
		status, code = http.StatusConflict, "case_closed"
	case errors.Is(err, ErrChatWorkConflict):
		status, code = http.StatusConflict, "conflict"
	case errors.Is(err, ErrApprovalClarificationRecipient):
		status, code = http.StatusUnprocessableEntity, "no_recipient"
	}
	if status == 0 {
		chatMessageWorkHTTPError(w, err)
		return
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(chatMessageWorkError{Code: code})
}

// ApprovalClarificationsHandler answers the routes for an admitted person.
type ApprovalClarificationsHandler struct{ Clarifications ApprovalClarificationsPort }

func (h ApprovalClarificationsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	journeyID, ok := approvalClarificationID(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(chatMessageWorkError{Code: "method_not_allowed"})
		return
	}
	verified, ok := trust.FromContext(r.Context())
	if !ok || verified == nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(chatMessageWorkError{Code: "unauthenticated"})
		return
	}
	if verified.SubjectKind() != trust.SubjectKindHuman {
		chatMessageWorkHTTPError(w, ErrChatWorkDenied)
		return
	}
	if h.Clarifications == nil {
		chatMessageWorkHTTPError(w, ErrChatWorkUnavailable)
		return
	}
	if r.Method == http.MethodPost {
		var command approvalClarificationCommand
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&command) != nil || decoder.Decode(new(any)) != io.EOF {
			chatMessageWorkHTTPError(w, ErrChatWorkInvalid)
			return
		}
		asked, err := h.Clarifications.Ask(r.Context(), verified, ApprovalClarificationAsk{
			JourneyID: journeyID, Revision: command.Revision, Question: command.Question, IdempotencyKey: command.IdempotencyKey,
		})
		if err != nil {
			approvalClarificationHTTPError(w, err)
			return
		}
		if asked.Created {
			w.WriteHeader(http.StatusCreated)
		}
		_ = json.NewEncoder(w).Encode(asked)
		return
	}
	view, err := h.Clarifications.View(r.Context(), verified, journeyID)
	if err != nil {
		approvalClarificationHTTPError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(view)
}

// OverlayApprovalClarifications mounts the routes with the admission the
// neighbouring Chat surfaces use; other paths fall through untouched.
func OverlayApprovalClarifications(next http.Handler, port ApprovalClarificationsPort, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := ApprovalClarificationsHandler{Clarifications: port}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := approvalClarificationID(r.URL.Path); !ok {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(denied.HTTPStatus())
			_ = json.NewEncoder(w).Encode(chatMessageWorkError{Code: "request_denied"})
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}
