package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const ChattonePath = "/api/chat-writing-style"

type ChattoneDraft struct {
	ConversationID string `json:"conversation_id"`
	Draft          string `json:"draft"`
	StyleID        string `json:"style_id"`
}
type ChattoneReply struct {
	Draft      string                  `json:"draft,omitempty"`
	Suggestion *chatrewrite.Suggestion `json:"suggestion,omitempty"`
	Styles     []chatrewrite.Style     `json:"styles,omitempty"`
	Enabled    bool                    `json:"enabled"`
}
type ChattoneSurface interface {
	RewriteDraft(context.Context, ChattoneDraft) (ChattoneReply, error)
	ReadSuggestion(context.Context, string) (ChattoneReply, error)
}

// ChattoneConversationSource must read through the reader rendering selection, never raw post bodies.
// It receives the verified, currently authorized tenant/person/conversation triple.
type ChattoneConversationSource interface {
	WritingContext(context.Context, chatrewrite.Identity) (chatrewrite.ConversationFacts, []string, error)
}
type ChattoneAuthority interface {
	AuthorizeWritingStyle(context.Context, chatrewrite.Identity) error
}

// ChattoneChatAuthority uses the same live membership checks as persona Chat.
type ChattoneChatAuthority struct{ Chat *PersonaChatSurface }

func (a ChattoneChatAuthority) AuthorizeWritingStyle(ctx context.Context, id chatrewrite.Identity) error {
	p, _, err := a.Chat.member(ctx, id.Conversation)
	if err != nil {
		return err
	}
	if p.Tenant().String() != id.Tenant || p.Subject() != id.Person {
		return personachat.ErrDenied
	}
	return nil
}

// errChattoneNotComposed means this installation did not compose the writing
// style service. It still satisfies chatrewrite.ErrUnavailable, but a read
// answers it with a typed "not available" body instead of an error status.
var errChattoneNotComposed = fmt.Errorf("%w: writing style service is not composed", chatrewrite.ErrUnavailable)

// ChatFeatureUnavailable is the success-status body every Chat read answers
// with when its service is not composed, so a client can tell "this feature is
// off" from a failure and stop asking.
type ChatFeatureUnavailable struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
}

func chatFeatureUnavailableWrite(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(ChatFeatureUnavailable{Available: false, Reason: reason})
}

type ChattoneService struct {
	Rewrite        *chatrewrite.Service
	Suggestions    *chatrewrite.Suggestions
	Authority      ChattoneAuthority
	Administration ChattoneAdministration
	Conversations  ChattoneConversationSource
	// Settings, when set, is where a workspace's choice is kept. Each
	// workspace's saved setting is read once per process, before its first use.
	Settings       ChattoneSettingsStore
	Now            func() time.Time
	settingsMu     sync.Mutex
	settingsLoaded map[string]bool
}

func (s *ChattoneService) authorize(ctx context.Context, conversation string) (chatrewrite.Identity, error) {
	p, ok := trust.FromContext(ctx)
	now := time.Now()
	if s != nil && s.Now != nil {
		now = s.Now()
	}
	if !ok || p == nil || !p.ExpiresAt().After(now) {
		return chatrewrite.Identity{}, personachat.ErrUnauthenticated
	}
	if p.SubjectKind() != trust.SubjectKindHuman {
		return chatrewrite.Identity{}, personachat.ErrDenied
	}
	id := chatrewrite.Identity{Tenant: p.Tenant().String(), Person: p.Subject(), Conversation: conversation}
	if !id.Valid() || strings.TrimSpace(conversation) != conversation || len(conversation) > 200 {
		return id, chatrewrite.ErrInvalid
	}
	if s == nil || s.Authority == nil || s.Conversations == nil || s.Rewrite == nil || s.Rewrite.Registry == nil || s.Suggestions == nil {
		return id, errChattoneNotComposed
	}
	if err := s.Authority.AuthorizeWritingStyle(ctx, id); err != nil {
		return id, err
	}
	if err := s.ensureSettings(ctx, id.Tenant); err != nil {
		return id, err
	}
	return id, nil
}
func (s *ChattoneService) RewriteDraft(ctx context.Context, in ChattoneDraft) (ChattoneReply, error) {
	id, err := s.authorize(ctx, in.ConversationID)
	if err != nil {
		return ChattoneReply{}, err
	}
	_, enabled := s.Rewrite.Registry.Styles(id.Tenant)
	if !enabled {
		return ChattoneReply{}, chatrewrite.ErrDisabled
	}
	_, recent, err := s.Conversations.WritingContext(ctx, id)
	if err != nil {
		return ChattoneReply{}, err
	}
	text, err := s.Rewrite.Rewrite(ctx, chatrewrite.Request{Identity: id, Draft: in.Draft, StyleID: in.StyleID, Context: recent})
	if err != nil {
		return ChattoneReply{}, err
	}
	return ChattoneReply{Draft: text, Enabled: true}, nil
}
func (s *ChattoneService) ReadSuggestion(ctx context.Context, conversation string) (ChattoneReply, error) {
	id, err := s.authorize(ctx, conversation)
	if err != nil {
		return ChattoneReply{}, err
	}
	styles, enabled := s.Rewrite.Registry.Styles(id.Tenant)
	if !enabled {
		return ChattoneReply{Enabled: false}, nil
	}
	facts, _, err := s.Conversations.WritingContext(ctx, id)
	if err != nil {
		return ChattoneReply{}, err
	}
	suggestion, err := s.Suggestions.Read(ctx, id, facts)
	if err != nil {
		return ChattoneReply{}, err
	}
	return ChattoneReply{Suggestion: &suggestion, Styles: styles, Enabled: true}, nil
}

// OverlayChattone shares HTTP-edge admission with adjacent Chat surfaces.
func OverlayChattone(next http.Handler, surface ChattoneSurface, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	h := ChattoneHandler{Surface: surface}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ChattonePath && !strings.HasPrefix(r.URL.Path, ChattonePath+"/") {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			chattoneWrite(w, ChattoneReply{}, personachat.ErrUnauthenticated)
			return
		}
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

type ChattoneHandler struct{ Surface ChattoneSurface }

func (h ChattoneHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if isNilPersonaOutputPort(h.Surface) {
		if r.Method == http.MethodGet && r.URL.Path == ChattonePath+"/suggestion" {
			chatFeatureUnavailableWrite(w, "writing_style_not_composed")
			return
		}
		chattoneWrite(w, ChattoneReply{}, chatrewrite.ErrUnavailable)
		return
	}
	var reply ChattoneReply
	var err error
	switch {
	case r.URL.Path == ChattonePath+"/suggestion" && r.Method == http.MethodGet:
		reply, err = h.Surface.ReadSuggestion(r.Context(), r.URL.Query().Get("conversation_id"))
		if errors.Is(err, errChattoneNotComposed) {
			chatFeatureUnavailableWrite(w, "writing_style_not_composed")
			return
		}
	case r.URL.Path == ChattonePath+"/styles" && r.Method == http.MethodPost:
		admin, ok := h.Surface.(interface {
			ConfigureWritingStyles(context.Context, ChattoneAdminConfig) (ChattoneReply, error)
		})
		if !ok {
			err = personachat.ErrDenied
			break
		}
		var config ChattoneAdminConfig
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 24<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&config) != nil || decoder.Decode(new(any)) != io.EOF {
			err = chatrewrite.ErrInvalid
		} else {
			reply, err = admin.ConfigureWritingStyles(r.Context(), config)
		}
	case r.URL.Path == ChattonePath+"/rewrite" && r.Method == http.MethodPost:
		var draft ChattoneDraft
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 24<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&draft) != nil || decoder.Decode(new(any)) != io.EOF {
			err = chatrewrite.ErrInvalid
		} else {
			reply, err = h.Surface.RewriteDraft(r.Context(), draft)
		}
	default:
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_found"})
		return
	}
	chattoneWrite(w, reply, err)
}
func chattoneWrite(w http.ResponseWriter, reply ChattoneReply, err error) {
	if err == nil {
		_ = json.NewEncoder(w).Encode(reply)
		return
	}
	status, code := http.StatusServiceUnavailable, "unavailable"
	switch {
	case errors.Is(err, personachat.ErrUnauthenticated):
		status, code = http.StatusUnauthorized, "unauthenticated"
	case errors.Is(err, personachat.ErrDenied):
		status, code = http.StatusForbidden, "denied"
	case errors.Is(err, chatrewrite.ErrDisabled):
		status, code = http.StatusForbidden, "disabled"
	case errors.Is(err, chatrewrite.ErrInvalid):
		status, code = http.StatusBadRequest, "invalid"
	case errors.Is(err, chatrewrite.ErrLimit):
		status, code = http.StatusTooManyRequests, "limit"
	case errors.Is(err, chatrewrite.ErrPreservation):
		status, code = http.StatusUnprocessableEntity, "preservation"
	case errors.Is(err, chatrewrite.ErrPolicy):
		status, code = http.StatusUnprocessableEntity, "policy"
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
