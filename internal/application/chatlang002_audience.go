package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ChatlangAudienceView is the answer to "who will read this in another
// language": for a message written in Language, how many other people in the
// conversation read a translation of it, per language they read it in. It is
// counts only: never who, and never a person's settings. Offered is false
// where the workspace or the channel does not translate, and then Readers is
// empty, so the composer says nothing.
type ChatlangAudienceView struct {
	Language string         `json:"language"`
	Offered  bool           `json:"offered"`
	Readers  map[string]int `json:"readers"`
}

// TranslationOffered reports whether translation is on for the caller's
// workspace: an engine is composed and an administrator has turned it on. A
// surface with no governance offers none.
func (s *ChatRenderingSurface) TranslationOffered(ctx context.Context, tenant string) bool {
	return s != nil && s.Languages.Allows(ctx, tenant, "")
}

// AudienceView answers for the caller as the writer. With no language it
// assumes the message is written in the writer's own reading language, which is
// what a person types in unless they say otherwise.
func (s *ChatRenderingSurface) AudienceView(ctx context.Context, scope chatstore.RenderingScope, language string) (ChatlangAudienceView, error) {
	if err := s.AuthorizeRendering(ctx, scope, "audience"); err != nil {
		return ChatlangAudienceView{}, err
	}
	view := ChatlangAudienceView{Readers: map[string]int{}}
	language = chatrender.Language(language)
	if language == "" {
		writer, err := s.LanguageSettings(ctx, scope, "")
		if err != nil {
			return view, err
		}
		language = chatrender.Language(writer.ReadingLanguage)
	}
	if !chatrender.Supported(language) {
		return view, chatrender.ErrInvalid
	}
	view.Language = language
	view.Offered = s.Languages.Allows(ctx, scope.Tenant, scope.Conversation)
	if !view.Offered || language == "und" {
		return view, nil
	}
	counts, err := s.Store.ConversationAudience(ctx, scope, language)
	if err != nil {
		return view, err
	}
	view.Readers = counts
	return view, nil
}

// chatlangAudiencePort and chatlangLocalePort are optional capabilities of a
// rendering port, as search is: a port that does not have them answers "not
// available", and the existing port interface is unchanged.
type chatlangAudiencePort interface {
	AudienceView(context.Context, chatstore.RenderingScope, string) (ChatlangAudienceView, error)
}
type chatlangLocalePort interface {
	RecordInterfaceLocale(context.Context, chatstore.RenderingScope, string) error
}

// chatlangTranslationFeature is the "translation" entry of the Chat feature
// answer: translation is on for the caller's workspace. It is never read from
// a guess: a port that cannot say answers false.
func chatlangTranslationFeature(ctx context.Context, port ChatRenderingPort) bool {
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || isNilPersonaOutputPort(port) {
		return false
	}
	offer, ok := port.(interface {
		TranslationOffered(context.Context, string) bool
	})
	return ok && offer.TranslationOffered(ctx, p.Tenant().String())
}
