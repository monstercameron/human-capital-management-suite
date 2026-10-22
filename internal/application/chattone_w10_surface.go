package application

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chattoneExemptions is the part of the chat store that keeps a writer's "Send
// as written" for one revision of one message.
type chattoneExemptions interface {
	RewordExempt(ctx context.Context, tenant, post string, revision uint64) (bool, error)
	SaveRewordExemption(ctx context.Context, tenant, post string, revision uint64, person string) error
}

// exempt reports whether the writer sent this revision as written. A store with
// no exemptions (a fake) answers no.
func (p ChattoneRewordPolicy) exempt(ctx context.Context, tenant, post string, revision uint64) (bool, error) {
	store, ok := p.Store.(chattoneExemptions)
	if !ok || post == "" {
		return false, nil
	}
	return store.RewordExempt(ctx, tenant, post, revision)
}

// chatrenderRewordSettings is the part of the reword policy the writer's side
// reads: what applies to a channel.
type chatrenderRewordSettings interface {
	Effective(ctx context.Context, tenant, channel string) (ChattoneRewordSettings, error)
}

// OthersSee is what the writer is told about how others read their own message
// when it is reworded for them by default: the reworded wording, or that it is
// on its way, or that it is not available. It is nil for any message that is not
// the caller's own, that no rewording applies to, and that others read as
// written by default. Asking for it starts the rewording when nobody has yet.
func (s *ChatRenderingSurface) OthersSee(ctx context.Context, scope chatstore.RenderingScope, post string) (*chatui.ReaderOthersSee, error) {
	if err := s.AuthorizeRendering(ctx, scope, "selection"); err != nil {
		return nil, err
	}
	settings, ok := s.Reword.(chatrenderRewordSettings)
	if !ok {
		return nil, nil
	}
	// Rewording off is the common case: nothing is asked of the message itself.
	setting, err := settings.Effective(ctx, scope.Tenant, scope.Conversation)
	if err != nil {
		return nil, err
	}
	if setting.Mode == chatstore.RewordModeOff {
		return nil, nil
	}
	policy, err := s.Policy.RenderingPolicy(ctx, scope, post)
	if err != nil || !policy.Own || policy.Original.Tenant != scope.Tenant || policy.Original.Message != post {
		return nil, err
	}
	if exemptions, ok := any(s.Store).(chattoneExemptions); ok {
		exempt, err := exemptions.RewordExempt(ctx, scope.Tenant, post, policy.Original.Revision)
		if err != nil {
			return nil, err
		}
		if exempt {
			return &chatui.ReaderOthersSee{State: "written"}, nil
		}
	}
	if !chatrenderOffersKind(policy, chatrender.Reword) {
		return nil, nil
	}
	// Others read the message as written by default (Offered, and members may
	// view originals): there is nothing to tell the writer.
	if setting.Mode != chatstore.RewordModeOn && setting.MembersMayViewOriginal {
		return nil, nil
	}
	out := &chatui.ReaderOthersSee{CanSendAsWritten: setting.MembersMayViewOriginal}
	source := chatrender.Language(policy.Original.SourceLanguage)
	available, err := s.RenderingAvailabilityForTarget(ctx, scope, post, chatrender.Reworded, source)
	if err != nil {
		return nil, err
	}
	for _, r := range available.Renderings {
		if r.Tone == chatrender.Reworded && chatrender.Language(r.Language) == source && !chatrenderKindsInclude(r.Kinds, chatrender.Translate) && chatrender.PermittedRendering(policy, r) {
			out.State, out.Text = "ready", r.Text
			return out, nil
		}
	}
	if available.Failed {
		out.State = "unavailable"
		return out, nil
	}
	if !available.Pending {
		target := policy.Original
		target.Text, target.Kinds, target.Tone, target.Language = "", nil, chatrender.Reworded, source
		if err := s.RequestRendering(ctx, scope, target); err != nil {
			if errors.Is(err, chatrender.ErrDenied) || errors.Is(err, chatrender.ErrUnavailable) {
				out.State = "unavailable"
				return out, nil
			}
			return nil, err
		}
	}
	out.State = "pending"
	return out, nil
}

// SendAsWritten makes others read one of the caller's own messages as written.
// It is refused for anyone else's message, and where the administrator does not
// let members read messages as written.
func (s *ChatRenderingSurface) SendAsWritten(ctx context.Context, scope chatstore.RenderingScope, post string) error {
	if err := s.AuthorizeRendering(ctx, scope, "send-as-written"); err != nil {
		return err
	}
	policy, err := s.Policy.RenderingPolicy(ctx, scope, post)
	if err != nil {
		return err
	}
	if !policy.Own || policy.Original.Tenant != scope.Tenant || policy.Original.Message != post {
		return chatrender.ErrDenied
	}
	settings, ok := s.Reword.(chatrenderRewordSettings)
	exemptions, saves := any(s.Store).(chattoneExemptions)
	if !ok || !saves {
		return chatrender.ErrUnavailable
	}
	setting, err := settings.Effective(ctx, scope.Tenant, scope.Conversation)
	if err != nil {
		return err
	}
	if setting.Mode == chatstore.RewordModeOff || !setting.MembersMayViewOriginal {
		return chatrender.ErrDenied
	}
	return exemptions.SaveRewordExemption(ctx, scope.Tenant, post, policy.Original.Revision, scope.Principal.SubjectID)
}

// ExtendReaderSelection adds what the page needs beyond the selection itself:
// the writer's "Others see" for their own message, and whether the translation
// shown is of a pair that passed only the lower quality mark. It is best effort:
// a failure here leaves the selection as it was and never blanks the page.
func (s *ChatRenderingSurface) ExtendReaderSelection(ctx context.Context, scope chatstore.RenderingScope, id string, sel *chatui.ReaderSelection) {
	if s == nil || sel == nil {
		return
	}
	if sel.Mark.State == "ready" && chatrenderKindsInclude(sel.Mark.Kinds, chatrender.Translate) && s.Languages != nil &&
		s.Languages.PairDecision(sel.Rendering.SourceLanguage, sel.Rendering.Language) == chatlang.GateOfferMarked {
		sel.Inaccurate = true
	}
	if see, err := s.OthersSee(ctx, scope, id); err == nil {
		sel.OthersSee = see
	}
}

// chatrenderReaderExtras and chatrenderSendAsWritten are what the rendering
// handler asks of a port beyond the base interface.
type chatrenderReaderExtras interface {
	ExtendReaderSelection(ctx context.Context, scope chatstore.RenderingScope, id string, sel *chatui.ReaderSelection)
}
type chatrenderSendAsWritten interface {
	SendAsWritten(ctx context.Context, scope chatstore.RenderingScope, post string) error
}

var (
	_ chatrenderReaderExtras  = (*ChatRenderingSurface)(nil)
	_ chatrenderSendAsWritten = (*ChatRenderingSurface)(nil)
)

// ReadingLanguageOf is the language a person reads Chat in, for an agent that
// answers them (CHATLANG-005): their own choice, else the language they were
// last seen using Chat in. It is "" when it cannot be read, and the agent then
// answers as it did before. Only the language's tag leaves this method.
func (s *ChatRenderingSurface) ReadingLanguageOf(ctx context.Context, tenant, person string) string {
	if s == nil || s.Store == nil || tenant == "" || person == "" {
		return ""
	}
	pref, err := s.Store.LanguageSettings(ctx, chatstore.RenderingScope{Principal: chat.Principal{TenantID: tenant, SubjectID: person}, Tenant: tenant}, "")
	if err != nil {
		return ""
	}
	return chatrender.Language(pref.ReadingLanguage)
}

// PersonaReaderLanguageMessage is the developer message that tells an agent the
// language of the person it answers; nothing is added when the language is not
// one the product offers.
func PersonaReaderLanguageMessage(language string) (agentmodel.ModelMessage, bool) {
	instruction := chatlang.ReaderLanguageInstruction(language)
	if instruction == "" {
		return agentmodel.ModelMessage{}, false
	}
	return agentmodel.ModelMessage{Role: agentmodel.RoleDeveloper, Content: instruction}, true
}
