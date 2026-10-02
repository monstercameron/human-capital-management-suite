package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const ChatgatePath = "/api/chat/gates"

type ChatgateRequest struct {
	Reviews                                                                  []chatgate.ReviewDecision
	Conversation, Action, Key, Version, SubmissionID, Person, Reason, Locale string
	ExpectedRevision, SubmissionRevision                                     uint64
	Definition                                                               chatgate.Definition
	Answers                                                                  map[string]json.RawMessage
	Installation                                                             chatgate.Installation
}
type ChatgateReply struct {
	View   *chatui.GateView `json:"view,omitempty"`
	Result any              `json:"result,omitempty"`
}
type ChatgateSurface interface {
	GateRequest(context.Context, ChatgateRequest) (ChatgateReply, error)
}
type ChatgateDirectory interface {
	GateDirectory(context.Context, chatgate.Actor, chatgate.Scope) (map[string][]chatui.GateChoice, map[string]string, string, error)
}

// ChatgateReviewers returns only administrator names the applicant may discover.
type ChatgateReviewers interface {
	GateReviewers(context.Context, chatgate.Actor, chatgate.Scope) ([]string, error)
}

// ChatgateApplication derives identity from the verified session. No request
// accepts tenant IDs, principals, directory facts or export permissions.
type ChatgateApplication struct {
	Clock     func() time.Time
	Service   chatgate.ChannelGateServiceV1
	Directory ChatgateDirectory
}

func (a *ChatgateApplication) GateRequest(ctx context.Context, r ChatgateRequest) (ChatgateReply, error) {
	if a == nil || a.Service == nil {
		return ChatgateReply{}, chatgate.ErrUnavailable
	}
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil {
		return ChatgateReply{}, chatgate.ErrDenied
	}
	now := time.Now()
	if a.Clock != nil {
		now = a.Clock()
	}
	if !now.Before(p.ExpiresAt()) || now.Before(p.IssuedAt()) {
		return ChatgateReply{}, chatgate.ErrDenied
	}
	actor := chatgate.Actor{Tenant: p.Tenant().String(), Person: p.Subject()}
	scope := chatgate.Scope{Tenant: actor.Tenant, Conversation: r.Conversation}
	c := chatgate.Command{Actor: actor, Scope: scope, Key: r.Key, ExpectedRevision: r.ExpectedRevision}
	var result any
	var err error
	switch r.Action {
	case "", "get":
	case "define":
		err = a.Service.Define(ctx, c, r.Definition)
	case "publish":
		var version chatgate.Version
		version, err = chatgate.ParseVersion(r.Version)
		if err == nil {
			result, err = a.Service.Publish(ctx, c, version)
		}
	case "pause":
		err = a.Service.Lifecycle(ctx, c, "paused")
	case "retire":
		err = a.Service.Lifecycle(ctx, c, "retired")
	case "submit":
		result, err = a.Service.Submit(ctx, c, r.Version, r.Answers)
	case "save":
		err = a.Service.SaveDraft(ctx, c, r.Version, r.Answers)
	case "withdraw":
		err = a.Service.Withdraw(ctx, c, r.SubmissionID, r.SubmissionRevision)
	case "admit", "decline":
		result, err = a.Service.Review(ctx, c, r.SubmissionID, r.SubmissionRevision, r.Action == "admit", r.Reason)
	case "bulk-admit", "bulk-decline":
		for i := range r.Reviews {
			r.Reviews[i].Admit = r.Action == "bulk-admit"
		}
		err = a.Service.ReviewMany(ctx, c, r.Reviews, r.Reason)
	case "install":
		err = a.Service.Install(ctx, c, r.Installation)
	case "try":
		result, err = a.Service.TryAs(ctx, actor, scope, r.Person, r.Definition, r.Answers)
	case "export":
		if r.Person == "" {
			result, err = a.Service.ExportAllCSV(ctx, actor, scope)
		} else {
			result, err = a.Service.ExportCSV(ctx, chatgate.ReadRequest{Actor: actor, Scope: scope, Person: r.Person, Purpose: "Gate answers CSV export", Export: true})
		}
	default:
		return ChatgateReply{}, chatgate.ErrInvalid
	}
	if err != nil {
		return ChatgateReply{}, err
	}
	if r.Action == "export" || r.Action == "try" {
		return ChatgateReply{Result: result}, nil
	}
	gate, err := a.Service.Get(ctx, actor, scope, true)
	admin := err == nil
	if errors.Is(err, chatgate.ErrDenied) {
		gate, err = a.Service.Get(ctx, actor, scope, false)
	}
	if err != nil {
		return ChatgateReply{}, err
	}
	locale := r.Locale
	if locale != "ar" && locale != "de-DE" {
		locale = "en-US"
	}
	v := chatui.GateView{Locale: locale, Conversation: r.Conversation, Gate: gate, Administrator: admin, State: "ready", Names: map[string]string{}}
	v.ConsumerNames = a.Service.ConsumerLabels(locale)
	for _, d := range gate.Versions {
		if d.Version.String() == gate.Current {
			v.Controls = a.Service.RenderControls(d, locale)
		}
	}
	if admin && gate.Draft != nil {
		v.Controls = a.Service.RenderControls(*gate.Draft, locale)
	}
	if a.Directory != nil {
		v.Directory, v.Names, v.ChannelPurpose, err = a.Directory.GateDirectory(ctx, actor, scope)
		if err != nil {
			return ChatgateReply{}, err
		}
	}
	if directory, ok := a.Directory.(ChatgateReviewers); ok {
		v.Reviewers, err = directory.GateReviewers(ctx, actor, scope)
		if err != nil {
			return ChatgateReply{}, err
		}
	}
	if admin {
		v.Submissions, err = a.Service.ListSubmissions(ctx, actor, scope, "")
		if err != nil {
			return ChatgateReply{}, err
		}
		v.VisibleAnswers = map[string]map[string]json.RawMessage{}
		for _, sub := range v.Submissions {
			if sub.Status == "withdrawn" || sub.Status == "superseded" {
				continue
			}
			values, e := a.Service.ReadAnswers(ctx, chatgate.ReadRequest{Actor: actor, Scope: scope, Person: sub.Person, Purpose: "Gate answers view"})
			if e != nil {
				return ChatgateReply{}, e
			}
			v.VisibleAnswers[sub.Person] = values
		} // Export permission is evaluated by the service, never by this display flag.
		v.ExportAllowed = a.Service.CanExport(ctx, actor, scope)
	} else {
		v.Submission, err = a.Service.MySubmission(ctx, actor, scope)
		if err != nil {
			return ChatgateReply{}, err
		}
		if v.Submission != nil && v.Submission.Status != "withdrawn" {
			v.Answers, err = a.Service.ReadAnswers(ctx, chatgate.ReadRequest{Actor: actor, Scope: scope, Person: actor.Person, Purpose: "My answers"})
		} else {
			v.Answers, err = a.Service.DraftAnswers(ctx, actor, scope)
		}
		if err != nil {
			return ChatgateReply{}, err
		}
		v.Reads, err = a.Service.Reads(ctx, actor, scope)
		if err != nil {
			return ChatgateReply{}, err
		}
		v.Held, err = a.Service.HasHold(ctx, actor, scope)
		if err != nil {
			return ChatgateReply{}, err
		}
	}
	return ChatgateReply{View: &v, Result: result}, nil
}

type ChatgateHTTP struct{ Surface ChatgateSurface }

func (h ChatgateHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if h.Surface == nil {
		chatgateHTTPError(w, chatgate.ErrUnavailable)
		return
	}
	req := ChatgateRequest{}
	if r.Method == http.MethodGet {
		req.Conversation = r.URL.Query().Get("conversation")
		req.Locale = r.URL.Query().Get("locale")
		req.Action = "get"
	} else if r.Method == http.MethodPost {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 24<<10))
		if e := decoder.Decode(&req); e != nil {
			chatgateHTTPError(w, chatgate.ErrInvalid)
			return
		}
		var trailing any
		if e := decoder.Decode(&trailing); !errors.Is(e, io.EOF) {
			chatgateHTTPError(w, chatgate.ErrInvalid)
			return
		}
	} else {
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if len(req.Conversation) > 256 || strings.TrimSpace(req.Conversation) == "" {
		chatgateHTTPError(w, chatgate.ErrInvalid)
		return
	}
	reply, e := h.Surface.GateRequest(r.Context(), req)
	if e != nil {
		chatgateHTTPError(w, e)
		return
	}
	_ = json.NewEncoder(w).Encode(reply)
}
func chatgateHTTPError(w http.ResponseWriter, e error) {
	code, status := "unavailable", http.StatusServiceUnavailable
	switch {
	case errors.Is(e, chatfilter.ErrBlocked):
		// CHATMOD-002: a refused answer names its field and says why.
		code, status = "content_blocked", 422
	case errors.Is(e, chatgate.ErrInvalid):
		code, status = "invalid_argument", 400
	case errors.Is(e, chatgate.ErrRequired):
		code, status = "answers_required", 422
	case errors.Is(e, chatgate.ErrDenied):
		code, status = "permission_denied", 403
	case errors.Is(e, chatgate.ErrNotFound):
		code, status = "not_found", 404
	case errors.Is(e, chatgate.ErrConflict):
		code, status = "conflict", 409
	case errors.Is(e, chatgate.ErrHeld):
		code, status = "legal_hold", 409
	}
	w.WriteHeader(status)
	fields := map[string]string{}
	var field chatgate.FieldError
	if errors.As(e, &field) {
		fields[field.Field] = code
	}
	_ = json.NewEncoder(w).Encode(struct {
		Error  string            `json:"error"`
		Fields map[string]string `json:"fields,omitempty"`
	}{code, fields})
}
func OverlayChatgates(next http.Handler, surface ChatgateSurface, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ChatgatePath {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(denied.HTTPStatus())
			_ = json.NewEncoder(w).Encode(struct {
				Error string `json:"error"`
			}{"permission_denied"})
			return
		}
		ChatgateHTTP{Surface: surface}.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ChatgateJoinAdapter is installed on chat.Service by the composition root.
// Direct adds check the target's answers under its own tenant and identity.
type ChatgateJoinAdapter struct{ Service *chatgate.Service }

func (a ChatgateJoinAdapter) CheckMembership(ctx context.Context, _ chat.Principal, m chat.Membership) error {
	if a.Service == nil {
		return chat.ErrUnavailable
	}
	if m.HomeTenantID != m.TenantID {
		return chat.ErrPermissionDenied
	}
	e := a.Service.CanJoin(ctx, chatgate.Actor{Tenant: m.TenantID, Person: m.SubjectID}, chatgate.Scope{Tenant: m.TenantID, Conversation: m.ConversationID})
	if e != nil {
		return chat.ErrPermissionDenied
	}
	return nil
}
