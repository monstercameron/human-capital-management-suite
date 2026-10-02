package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ChattoneRewordStore is the part of the chat store that keeps the
// administrator's reword settings and each person's own choice.
type ChattoneRewordStore interface {
	LoadRewordSettings(ctx context.Context, tenant string) ([]chatstore.RewordSetting, error)
	SaveRewordSetting(ctx context.Context, in chatstore.RewordSetting) (int64, error)
	DeleteRewordOverride(ctx context.Context, tenant, channel string) error
	LoadReaderChoices(ctx context.Context, tenant, person string) ([]chatstore.ReaderChoice, error)
	SaveReaderChoice(ctx context.Context, tenant, person, channel, tone string) error
	DeleteReaderChoice(ctx context.Context, tenant, person, channel string) error
}

// ChattoneRewordSettings is what applies to one channel after the channel's
// override is laid over the workspace's choice.
type ChattoneRewordSettings struct {
	// Mode is "off", "offered" or "on" (the administrator's "Reword heated
	// messages"): off never rewords; offered keeps a reworded rendering
	// available to readers who choose it; on shows every heated message
	// reworded.
	Mode string `json:"mode"`
	// MembersMayViewOriginal is the administrator's "Members may view messages
	// as written".
	MembersMayViewOriginal bool `json:"members_may_view_original"`
	// Override reports that the channel's own row is what applies.
	Override bool `json:"override"`
}

// ChattoneRewordPolicy turns the stored settings into the one decision every
// surface asks: what may this reader be given for this message. It is a pure
// function of the settings, the message's heat and who is reading, so no
// surface needs its own rule.
type ChattoneRewordPolicy struct {
	Store ChattoneRewordStore
	// Filters, when set, lets a workspace's own filters ask for rewording with the
	// "reword" action.
	Filters interface {
		Evaluate(ctx context.Context, in chatfilter.Input, record bool) (chatfilter.Result, error)
	}
}

var errChattoneRewordNotComposed = errors.New("application: reword settings are not composed")

// Effective is the setting that applies to a channel: the channel's override
// when it has one, else the workspace's, else the default (off, and members may
// view the original).
func (p ChattoneRewordPolicy) Effective(ctx context.Context, tenant, channel string) (ChattoneRewordSettings, error) {
	if p.Store == nil {
		return ChattoneRewordSettings{}, errChattoneRewordNotComposed
	}
	rows, err := p.Store.LoadRewordSettings(ctx, tenant)
	if err != nil {
		return ChattoneRewordSettings{}, err
	}
	return chattoneRewordEffective(rows, channel), nil
}

func chattoneRewordEffective(rows []chatstore.RewordSetting, channel string) ChattoneRewordSettings {
	out := ChattoneRewordSettings{Mode: chatstore.RewordModeOff, MembersMayViewOriginal: true}
	for _, row := range rows {
		if row.Channel == "" {
			out.Mode, out.MembersMayViewOriginal = row.Mode, row.MembersMayViewOriginal
		}
	}
	if channel != "" {
		for _, row := range rows {
			if row.Channel == channel {
				out = ChattoneRewordSettings{Mode: row.Mode, MembersMayViewOriginal: row.MembersMayViewOriginal, Override: true}
			}
		}
	}
	return out
}

// Apply adjusts the rendering policy for one message and one reader. Only a
// message the cheap screen reads as heated is ever touched: a calm or firm
// message keeps the policy it had, an abusive one follows the hard filters, and
// text the filters mask is not reworded. The writer always keeps both views.
// Where members may not view originals, a heated message is required reworded
// for them whatever the mode, so the original is in nothing they receive (and
// when the rewording is not ready they are told so, never shown the original).
// Where they may, "on" makes reworded their default (see ReaderTone) and the
// original stays one press away and is the stated fallback.
func (p ChattoneRewordPolicy) Apply(ctx context.Context, tenant, channel, authorID string, author bool, body string, policy *chatrender.Policy) error {
	if policy == nil || policy.RequireMask {
		return nil
	}
	heat := chatrewrite.ScreenHeat(body)
	if heat == chatrewrite.HeatAbusive {
		return nil
	}
	setting, err := p.Effective(ctx, tenant, channel)
	if err != nil {
		return err
	}
	if setting.Mode == chatstore.RewordModeOff {
		return nil
	}
	// A message the word lists pass is still reworded where one of the
	// workspace's filters says "reword" about it (the filter action).
	if heat != chatrewrite.HeatHeated {
		hit, err := p.filterSaysReword(ctx, tenant, channel, authorID, body)
		if err != nil {
			return err
		}
		if !hit {
			return nil
		}
	}
	allowed := false
	for _, kind := range policy.AllowedKinds {
		allowed = allowed || kind == chatrender.Reword
	}
	if !allowed {
		policy.AllowedKinds = append(policy.AllowedKinds, chatrender.Reword)
	}
	if author {
		policy.AllowOriginal = true
		return nil
	}
	if !setting.MembersMayViewOriginal {
		policy.RequireReworded = true
	}
	policy.AllowOriginal = policy.AllowOriginal && setting.MembersMayViewOriginal
	return nil
}

// filterSaysReword reports whether an enforced filter hit on the message has the
// "reword" action. Dry-run hits only record and never act.
func (p ChattoneRewordPolicy) filterSaysReword(ctx context.Context, tenant, channel, authorID, body string) (bool, error) {
	if p.Filters == nil {
		return false, nil
	}
	result, err := p.Filters.Evaluate(ctx, chatfilter.Input{Tenant: tenant, Channel: channel, Subject: authorID, Body: body}, false)
	if err != nil {
		return false, err
	}
	for _, hit := range result.Hits {
		if hit.Action == ChattoneRewordFilterAction && !hit.DryRun {
			return true, nil
		}
	}
	return false, nil
}

// ChattoneRewordFilterAction is the name of the filter action that asks for a
// message to be reworded (registered in the filter registry).
const ChattoneRewordFilterAction = "reword"

// ReaderTone is the tone a reader gets for heated messages in a channel: their
// own override for the channel, else their general choice, else the workspace
// default ("on" is reworded, "offered" is def, the reader's tone before this
// feature). A reader whose administrator does not allow originals is not
// offered the choice, and is given reworded.
func (p ChattoneRewordPolicy) ReaderTone(ctx context.Context, tenant, person, channel string, def chatrender.Tone) (chatrender.Tone, error) {
	if p.Store == nil {
		return def, errChattoneRewordNotComposed
	}
	setting, err := p.Effective(ctx, tenant, channel)
	if err != nil {
		return def, err
	}
	if setting.Mode == chatstore.RewordModeOff {
		return def, nil
	}
	if !setting.MembersMayViewOriginal {
		return chatrender.Reworded, nil
	}
	choices, err := p.Store.LoadReaderChoices(ctx, tenant, person)
	if err != nil {
		return def, err
	}
	tone, found := def, false
	if setting.Mode == chatstore.RewordModeOn {
		tone = chatrender.Reworded
	}
	for _, c := range choices {
		if c.Channel == "" && !found {
			tone = chatrender.Tone(c.Tone)
		}
		if channel != "" && c.Channel == channel {
			tone, found = chatrender.Tone(c.Tone), true
		}
	}
	return tone, nil
}

// ChattoneRewordView is what the page shows about rewording for one
// conversation: what applies, whether the caller may change the workspace's
// choice, and the caller's own choice.
type ChattoneRewordView struct {
	Available bool `json:"available"`
	// Workspace is the workspace's own setting; Channel is what applies to this
	// conversation (the workspace's, or the channel's override).
	Workspace ChattoneRewordSettings `json:"workspace"`
	Channel   ChattoneRewordSettings `json:"channel"`
	CanAdmin  bool                   `json:"can_administer"`
	// ChoiceAllowed is false where members may not view originals: the choice
	// does not exist there. General and ForChannel are the caller's own choices
	// ("as-written" or "reworded"; empty is none).
	ChoiceAllowed bool   `json:"choice_allowed"`
	General       string `json:"general_choice,omitempty"`
	ForChannel    string `json:"channel_choice,omitempty"`
}

// ChattoneRewordAdmin is an administrator's change. Scope is "workspace" or
// "channel"; Clear returns a channel to the workspace's choice.
type ChattoneRewordAdmin struct {
	ConversationID         string `json:"conversation_id"`
	Scope                  string `json:"scope"`
	Mode                   string `json:"mode"`
	MembersMayViewOriginal bool   `json:"members_may_view_original"`
	Clear                  bool   `json:"clear"`
}

// ChattoneRewordChoice is a person's change to their own reading choice. Scope
// is "general" or "channel"; Clear removes the choice.
type ChattoneRewordChoice struct {
	ConversationID string `json:"conversation_id"`
	Scope          string `json:"scope"`
	Tone           string `json:"tone"`
	Clear          bool   `json:"clear"`
}

// ChattoneRewordSurface is the optional part of a writing-style surface that
// serves the reword settings.
type ChattoneRewordSurface interface {
	ReadReword(context.Context, string) (ChattoneRewordView, error)
	AdministerReword(context.Context, ChattoneRewordAdmin) (ChattoneRewordView, error)
	ChooseReword(context.Context, ChattoneRewordChoice) (ChattoneRewordView, error)
}

// rewordIdentity checks the session is a signed-in person who may write in the
// conversation. The reword settings do not depend on the writing-style model
// being composed, so this does not use the writing-style gate.
func (s *ChattoneService) rewordIdentity(ctx context.Context, conversation string) (chatrewrite.Identity, error) {
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
	if s == nil || s.Reword == nil || s.Reword.Store == nil || s.Authority == nil {
		return id, errChattoneNotComposed
	}
	if err := s.Authority.AuthorizeWritingStyle(ctx, id); err != nil {
		return id, err
	}
	return id, nil
}

func (s *ChattoneService) rewordView(ctx context.Context, id chatrewrite.Identity) (ChattoneRewordView, error) {
	rows, err := s.Reword.Store.LoadRewordSettings(ctx, id.Tenant)
	if err != nil {
		return ChattoneRewordView{}, err
	}
	view := ChattoneRewordView{Available: true, Workspace: chattoneRewordEffective(rows, ""), Channel: chattoneRewordEffective(rows, id.Conversation)}
	view.ChoiceAllowed = view.Channel.Mode != chatstore.RewordModeOff && view.Channel.MembersMayViewOriginal
	if s.Administration != nil && s.Administration.ManageWritingStyles(ctx, id) == nil {
		view.CanAdmin = true
	}
	choices, err := s.Reword.Store.LoadReaderChoices(ctx, id.Tenant, id.Person)
	if err != nil {
		return ChattoneRewordView{}, err
	}
	for _, c := range choices {
		switch c.Channel {
		case "":
			view.General = c.Tone
		case id.Conversation:
			view.ForChannel = c.Tone
		}
	}
	return view, nil
}

// ReadReword answers for the caller's own workspace and conversation only.
func (s *ChattoneService) ReadReword(ctx context.Context, conversation string) (ChattoneRewordView, error) {
	id, err := s.rewordIdentity(ctx, conversation)
	if err != nil {
		return ChattoneRewordView{}, err
	}
	return s.rewordView(ctx, id)
}

// AdministerReword changes the workspace's choice, or one channel's override.
// Only a workspace administrator may; the caller learns nothing about the
// workspace's settings from a refusal.
func (s *ChattoneService) AdministerReword(ctx context.Context, in ChattoneRewordAdmin) (ChattoneRewordView, error) {
	id, err := s.rewordIdentity(ctx, in.ConversationID)
	if err != nil {
		return ChattoneRewordView{}, err
	}
	if s.Administration == nil {
		return ChattoneRewordView{}, personachat.ErrDenied
	}
	if err := s.Administration.ManageWritingStyles(ctx, id); err != nil {
		return ChattoneRewordView{}, err
	}
	channel := ""
	switch in.Scope {
	case "workspace":
		if in.Clear {
			return ChattoneRewordView{}, chatrewrite.ErrInvalid
		}
	case "channel":
		channel = id.Conversation
	default:
		return ChattoneRewordView{}, chatrewrite.ErrInvalid
	}
	if in.Clear {
		if err := s.Reword.Store.DeleteRewordOverride(ctx, id.Tenant, channel); err != nil {
			return ChattoneRewordView{}, err
		}
	} else if _, err := s.Reword.Store.SaveRewordSetting(ctx, chatstore.RewordSetting{Tenant: id.Tenant, Channel: channel, Mode: in.Mode, MembersMayViewOriginal: in.MembersMayViewOriginal, UpdatedBy: id.Person}); err != nil {
		if errors.Is(err, chatstore.ErrRewordInvalid) {
			return ChattoneRewordView{}, chatrewrite.ErrInvalid
		}
		return ChattoneRewordView{}, err
	}
	return s.rewordView(ctx, id)
}

// ChooseReword changes the caller's own choice. It is refused where the
// administrator has not allowed originals (the choice does not exist there) and
// is never able to name another person.
func (s *ChattoneService) ChooseReword(ctx context.Context, in ChattoneRewordChoice) (ChattoneRewordView, error) {
	id, err := s.rewordIdentity(ctx, in.ConversationID)
	if err != nil {
		return ChattoneRewordView{}, err
	}
	channel := ""
	switch in.Scope {
	case "general":
		if in.Clear {
			return ChattoneRewordView{}, chatrewrite.ErrInvalid
		}
	case "channel":
		channel = id.Conversation
	default:
		return ChattoneRewordView{}, chatrewrite.ErrInvalid
	}
	view, err := s.rewordView(ctx, id)
	if err != nil {
		return ChattoneRewordView{}, err
	}
	if !view.ChoiceAllowed {
		return ChattoneRewordView{}, personachat.ErrDenied
	}
	if in.Clear {
		err = s.Reword.Store.DeleteReaderChoice(ctx, id.Tenant, id.Person, channel)
	} else {
		err = s.Reword.Store.SaveReaderChoice(ctx, id.Tenant, id.Person, channel, in.Tone)
	}
	if errors.Is(err, chatstore.ErrRewordInvalid) {
		return ChattoneRewordView{}, chatrewrite.ErrInvalid
	}
	if err != nil {
		return ChattoneRewordView{}, err
	}
	return s.rewordView(ctx, id)
}

var _ ChattoneRewordSurface = (*ChattoneService)(nil)

var errChattoneRewordNotFound = errors.New("application: unknown reword path")

// chattoneRewordServe routes the reword settings: a read of what applies to a
// conversation, an administrator's change and a person's own choice.
func chattoneRewordServe(surface ChattoneSurface, w http.ResponseWriter, r *http.Request) (ChattoneRewordView, error) {
	rewording, ok := surface.(ChattoneRewordSurface)
	if !ok {
		return ChattoneRewordView{}, chatrewrite.ErrUnavailable
	}
	decode := func(into any) error {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(into) != nil || decoder.Decode(new(any)) != io.EOF {
			return chatrewrite.ErrInvalid
		}
		return nil
	}
	switch {
	case r.URL.Path == ChattonePath+"/reword" && r.Method == http.MethodGet:
		return rewording.ReadReword(r.Context(), r.URL.Query().Get("conversation_id"))
	case r.URL.Path == ChattonePath+"/reword/admin" && r.Method == http.MethodPost:
		var in ChattoneRewordAdmin
		if err := decode(&in); err != nil {
			return ChattoneRewordView{}, err
		}
		return rewording.AdministerReword(r.Context(), in)
	case r.URL.Path == ChattonePath+"/reword/choice" && r.Method == http.MethodPost:
		var in ChattoneRewordChoice
		if err := decode(&in); err != nil {
			return ChattoneRewordView{}, err
		}
		return rewording.ChooseReword(r.Context(), in)
	}
	return ChattoneRewordView{}, errChattoneRewordNotFound
}

func chattoneRewordWrite(w http.ResponseWriter, view ChattoneRewordView, err error) {
	switch {
	case err == nil:
		_ = json.NewEncoder(w).Encode(view)
	case errors.Is(err, errChattoneRewordNotFound):
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_found"})
	case errors.Is(err, errChattoneNotComposed):
		// A read from a deployment without the settings is "off", not a failure.
		chatFeatureUnavailableWrite(w, "reword_not_composed")
	default:
		chattoneWrite(w, ChattoneReply{}, err)
	}
}
