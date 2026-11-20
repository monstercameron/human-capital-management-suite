package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatbarrier"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3783: the information barriers page of Chat settings. Reading and
// changing barriers is workspace administration, decided like retention and the
// audit log: by the verified caller's current administrator role, never by a
// field of the request. A barrier's groups are private to administrators.

// ChatBarrierPath serves the barrier list, add and remove calls.
const ChatBarrierPath = "/api/chat/barriers/v1/"

// errChatBarrierUnknownGroup is returned when a rule names a team the people
// directory does not have, which would be a rule that never applies.
var errChatBarrierUnknownGroup = errors.New("chat: barrier group is not a team")

// ChatBarrierRule is one barrier as the settings page shows it.
type ChatBarrierRule struct {
	ID        string
	GroupA    string
	GroupB    string
	CreatedAt time.Time
}

// ChatBarrierList is what the settings page reads. Teams are the groups the
// people directory has, so a rule can be written against a real team.
type ChatBarrierList struct {
	Rules []ChatBarrierRule
	Teams []string
}

// ChatBarrierService is what the HTTP edge calls.
type ChatBarrierService interface {
	ListBarriers(context.Context, chat.Principal) (ChatBarrierList, error)
	AddBarrier(context.Context, chat.Principal, string, string) (ChatBarrierRule, error)
	RemoveBarrier(context.Context, chat.Principal, string) error
}

// chatBarrierAdmin names the workspace administrator behind the context, or
// refuses.
type chatBarrierAdmin func(ctx context.Context, tenant string) (string, error)

// chatBarrierTeams lists the teams of the people directory.
type chatBarrierTeams interface {
	BarrierTeamNames(ctx context.Context, tenant string) ([]string, error)
}

// BarrierTeamNames lists the distinct teams of the workspace's people.
func (f currentRoleChatFacts) BarrierTeamNames(ctx context.Context, tenant string) ([]string, error) {
	people, err := f.GatePeople(ctx, tenant)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := []string{}
	for _, p := range people {
		label := chatbarrier.Label(p.Team)
		if label != "" && !seen[chatbarrier.Key(label)] {
			seen[chatbarrier.Key(label)] = true
			out = append(out, label)
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out, nil
}

type chatBarrierApplication struct {
	Rules chatBarrierRules
	Admin chatBarrierAdmin
	Teams chatBarrierTeams
	NewID func() string
}

var _ ChatBarrierService = chatBarrierApplication{}

func (a chatBarrierApplication) authorize(ctx context.Context, p chat.Principal) (string, error) {
	if a.Rules == nil || a.Admin == nil {
		return "", chat.ErrUnavailable
	}
	if p.TenantID == "" || p.SubjectID == "" {
		return "", chat.ErrPermissionDenied
	}
	actor, err := a.Admin(ctx, p.TenantID)
	if err != nil || actor != p.SubjectID {
		return "", chat.ErrPermissionDenied
	}
	return actor, nil
}

func (a chatBarrierApplication) teams(ctx context.Context, tenant string) ([]string, bool) {
	if a.Teams == nil {
		return nil, false
	}
	names, err := a.Teams.BarrierTeamNames(ctx, tenant)
	return names, err == nil
}

func (a chatBarrierApplication) ListBarriers(ctx context.Context, p chat.Principal) (ChatBarrierList, error) {
	if _, err := a.authorize(ctx, p); err != nil {
		return ChatBarrierList{}, err
	}
	rules, err := a.Rules.ListBarrierRules(ctx, p.TenantID)
	if err != nil {
		return ChatBarrierList{}, chat.ErrUnavailable
	}
	out := ChatBarrierList{Rules: make([]ChatBarrierRule, 0, len(rules)), Teams: []string{}}
	for _, r := range rules {
		out.Rules = append(out.Rules, ChatBarrierRule{ID: r.ID, GroupA: r.LabelA, GroupB: r.LabelB, CreatedAt: r.CreatedAt})
	}
	if names, ok := a.teams(ctx, p.TenantID); ok {
		out.Teams = names
	}
	return out, nil
}

func (a chatBarrierApplication) AddBarrier(ctx context.Context, p chat.Principal, groupA, groupB string) (ChatBarrierRule, error) {
	actor, err := a.authorize(ctx, p)
	if err != nil {
		return ChatBarrierRule{}, err
	}
	id := uuid.NewString()
	if a.NewID != nil {
		id = a.NewID()
	}
	rule, err := chatbarrier.NewRule(id, groupA, groupB, actor, time.Now().UTC())
	if err != nil {
		return ChatBarrierRule{}, chat.ErrInvalidArgument
	}
	// A rule against a name the directory does not have would look like
	// protection and protect nothing, so it is refused when the directory can say.
	if names, ok := a.teams(ctx, p.TenantID); ok {
		known := map[string]bool{}
		for _, n := range names {
			known[chatbarrier.Key(n)] = true
		}
		if !known[rule.GroupA] || !known[rule.GroupB] {
			return ChatBarrierRule{}, errChatBarrierUnknownGroup
		}
	}
	saved, err := a.Rules.AddBarrierRule(ctx, p.TenantID, rule)
	if err != nil {
		return ChatBarrierRule{}, err
	}
	return ChatBarrierRule{ID: saved.ID, GroupA: saved.LabelA, GroupB: saved.LabelB, CreatedAt: saved.CreatedAt}, nil
}

func (a chatBarrierApplication) RemoveBarrier(ctx context.Context, p chat.Principal, id string) error {
	actor, err := a.authorize(ctx, p)
	if err != nil {
		return err
	}
	if strings.TrimSpace(id) == "" {
		return chat.ErrInvalidArgument
	}
	return a.Rules.RemoveBarrierRule(ctx, p.TenantID, strings.TrimSpace(id), actor)
}

// newChatBarrierApplication composes the admin surface from the chat store, the
// workspace administrator check and the people directory. It is nil when the
// product has no administrator check to decide with.
func newChatBarrierApplication(store *chatstore.Store, grants *ChatCompanyGrants, facts ChatAuthorityFacts) ChatBarrierService {
	// Without a people directory there are no teams to keep apart, so the
	// surface is not offered: a rule that nothing could apply would only look
	// like protection.
	teams, ok := chatBarrierDirectory(facts).(chatBarrierTeams)
	if store == nil || grants == nil || !ok {
		return nil
	}
	return chatBarrierApplication{Rules: store, Teams: teams, Admin: func(ctx context.Context, tenant string) (string, error) {
		return grants.admin(ctx, tenant, time.Now().UTC())
	}}
}

// ChatBarrierHTTP answers list, add and remove. All three are POST so a group
// name never sits in a page address.
type ChatBarrierHTTP struct{ Service ChatBarrierService }

type chatBarrierRequest struct {
	GroupA, GroupB, ID string
}

type chatBarrierCode struct {
	Code string `json:"code"`
}

func chatBarrierJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func chatBarrierError(w http.ResponseWriter, err error) {
	status, code := http.StatusServiceUnavailable, "unavailable"
	switch {
	case errors.Is(err, chat.ErrPermissionDenied):
		status, code = http.StatusForbidden, "permission_denied"
	case errors.Is(err, errChatBarrierUnknownGroup):
		status, code = http.StatusBadRequest, "unknown_group"
	case errors.Is(err, chat.ErrInvalidArgument):
		status, code = http.StatusBadRequest, "invalid_request"
	case errors.Is(err, chat.ErrAlreadyExists):
		status, code = http.StatusConflict, "already_exists"
	case errors.Is(err, chat.ErrConflict):
		status, code = http.StatusConflict, "limit_reached"
	case errors.Is(err, chat.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	}
	chatBarrierJSON(w, status, chatBarrierCode{code})
}

func (h ChatBarrierHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	actor, ok := trust.FromContext(r.Context())
	_, admitted := transport.InvocationFromContext(r.Context())
	if !ok || actor == nil || !admitted {
		chatBarrierError(w, chat.ErrPermissionDenied)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		chatBarrierJSON(w, http.StatusMethodNotAllowed, chatBarrierCode{"method_not_allowed"})
		return
	}
	action := strings.TrimPrefix(r.URL.Path, ChatBarrierPath)
	if action != "list" && action != "add" && action != "remove" {
		chatBarrierJSON(w, http.StatusNotFound, chatBarrierCode{"not_found"})
		return
	}
	if h.Service == nil {
		chatBarrierError(w, chat.ErrUnavailable)
		return
	}
	var q chatBarrierRequest
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	d.DisallowUnknownFields()
	if d.Decode(&q) != nil || d.Decode(new(any)) != io.EOF {
		chatBarrierError(w, chat.ErrInvalidArgument)
		return
	}
	p := chat.Principal{TenantID: actor.Tenant().String(), SubjectID: actor.Subject()}
	switch action {
	case "list":
		list, err := h.Service.ListBarriers(r.Context(), p)
		if err != nil {
			chatBarrierError(w, err)
			return
		}
		chatBarrierJSON(w, http.StatusOK, list)
	case "add":
		rule, err := h.Service.AddBarrier(r.Context(), p, q.GroupA, q.GroupB)
		if err != nil {
			chatBarrierError(w, err)
			return
		}
		chatBarrierJSON(w, http.StatusOK, rule)
	default:
		if err := h.Service.RemoveBarrier(r.Context(), p, q.ID); err != nil {
			chatBarrierError(w, err)
			return
		}
		chatBarrierJSON(w, http.StatusOK, struct{ Removed bool }{true})
	}
}

// OverlayChatBarriers serves the barrier calls beside the other chat overlays.
func OverlayChatBarriers(next http.Handler, service ChatBarrierService, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, ChatBarrierPath) {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			chatBarrierJSON(w, denied.HTTPStatus(), chatBarrierCode{"permission_denied"})
			return
		}
		ChatBarrierHTTP{Service: service}.ServeHTTP(w, r.WithContext(ctx))
	})
}
