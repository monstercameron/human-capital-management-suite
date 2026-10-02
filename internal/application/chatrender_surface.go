package application

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

// RenderingPolicySource resolves current channel policy and an authorized
// original through the chat service. It may never use an unscoped raw GetPost.
type RenderingPolicySource interface {
	AuthorizeRendering(context.Context, chatstore.RenderingScope, string) error
	RenderingPolicy(context.Context, chatstore.RenderingScope, string) (chatrender.Policy, error)
}

// RenderingMemberLocaleSource fills only the aggregate for members who have no
// explicit Chat language setting. Individual interface locales remain private.
type RenderingMemberLocaleSource interface {
	DefaultConversationLanguages(context.Context, chatstore.RenderingScope) (map[string]int, error)
}
type ChatRenderingSurface struct {
	*chatstore.Store
	Policy          RenderingPolicySource
	InterfaceLocale string
	Locales         RenderingMemberLocaleSource
	// Languages is what administrators control about translation (CHATLANG-006).
	Languages *ChatlangGovernance
	// Reword decides the tone a reader is given where rewording is allowed
	// (ChattoneRewordPolicy); nil leaves the reader's own tone as stored.
	Reword interface {
		ReaderTone(ctx context.Context, tenant, person, channel string, def chatrender.Tone) (chatrender.Tone, error)
	}
}

func chatrenderOffersKind(policy chatrender.Policy, kind chatrender.Kind) bool {
	for _, k := range policy.AllowedKinds {
		if k == kind {
			return true
		}
	}
	return false
}

func (s *ChatRenderingSurface) ListRenderings(ctx context.Context, scope chatstore.RenderingScope, posts []string) ([]chatrender.Rendering, error) {
	if err := s.AuthorizeRendering(ctx, scope, "list"); err != nil {
		return nil, err
	}
	rows, err := s.Store.ListRenderings(ctx, scope, posts)
	if err != nil {
		return nil, err
	}
	return s.permittedRenderings(ctx, scope, rows)
}
func (s *ChatRenderingSurface) SearchRenderings(ctx context.Context, scope chatstore.RenderingScope, query string) ([]chatrender.Rendering, error) {
	if err := s.AuthorizeRendering(ctx, scope, "search"); err != nil {
		return nil, err
	}
	rows, err := s.Store.SearchRenderings(ctx, scope, query)
	if err != nil {
		return nil, err
	}
	return s.permittedRenderings(ctx, scope, rows)
}
func (s *ChatRenderingSurface) permittedRenderings(ctx context.Context, scope chatstore.RenderingScope, rows []chatrender.Rendering) ([]chatrender.Rendering, error) {
	out := []chatrender.Rendering{}
	policies := map[string]chatrender.Policy{}
	var err error
	for _, row := range rows {
		policy, ok := policies[row.Message]
		if !ok {
			policy, err = s.Policy.RenderingPolicy(ctx, scope, row.Message)
			if err != nil {
				return nil, err
			}
			policies[row.Message] = policy
		}
		if chatrender.PermittedRendering(policy, row) {
			out = append(out, row)
		}
	}
	return out, nil
}
func (s *ChatRenderingSurface) RequestRendering(ctx context.Context, scope chatstore.RenderingScope, target chatrender.Rendering) error {
	if err := s.AuthorizeRendering(ctx, scope, "request"); err != nil {
		return err
	}
	policy, err := s.Policy.RenderingPolicy(ctx, scope, target.Message)
	if err != nil {
		return err
	}
	if policy.Original.Tenant != scope.Tenant || policy.Original.Message != target.Message || policy.Original.Revision != target.Revision {
		return chatrender.ErrDenied
	}
	if policy.RequireReworded {
		target.Tone = chatrender.Reworded
	}
	if policy.RequireMask {
		target.Kinds = append(target.Kinds, chatrender.Mask)
	}
	if target.Tone == chatrender.Reworded {
		target.Kinds = append(target.Kinds, chatrender.Reword)
	}
	if chatrender.Language(target.Language) != chatrender.Language(policy.Original.SourceLanguage) {
		target.Kinds = append(target.Kinds, chatrender.Translate)
	}
	for _, kind := range target.Kinds {
		allowed := false
		for _, registered := range policy.AllowedKinds {
			if registered == kind {
				allowed = true
			}
		}
		if !allowed {
			return chatrender.ErrDenied
		}
	}
	return s.Store.RequestRendering(ctx, scope, target)
}
func (s *ChatRenderingSurface) LanguageSettings(ctx context.Context, scope chatstore.RenderingScope, locale string) (chatrender.Preference, error) {
	if err := s.AuthorizeRendering(ctx, scope, "settings"); err != nil {
		return chatrender.Preference{}, err
	}
	return s.Store.LanguageSettings(ctx, scope, locale)
}
func (s *ChatRenderingSurface) PutLanguageSettings(ctx context.Context, scope chatstore.RenderingScope, pref chatrender.Preference) error {
	if err := s.AuthorizeRendering(ctx, scope, "settings"); err != nil {
		return err
	}
	return s.Store.PutLanguageSettings(ctx, scope, pref)
}
func (s *ChatRenderingSurface) ReportRendering(ctx context.Context, scope chatstore.RenderingScope, target chatrender.Rendering, reason string) error {
	if err := s.AuthorizeRendering(ctx, scope, "report"); err != nil {
		return err
	}
	return s.Store.ReportRendering(ctx, scope, target, reason)
}
func (s *ChatRenderingSurface) CorrectRevisionLanguage(ctx context.Context, scope chatstore.RenderingScope, post string, revision uint64, language string) error {
	if err := s.AuthorizeRendering(ctx, scope, "correct-language"); err != nil {
		return err
	}
	return s.Store.CorrectRevisionLanguage(ctx, scope, post, revision, language)
}
func (s *ChatRenderingSurface) ConversationLanguages(ctx context.Context, scope chatstore.RenderingScope) (map[string]int, error) {
	if err := s.AuthorizeRendering(ctx, scope, "languages"); err != nil {
		return nil, err
	}
	counts, err := s.Store.ConversationLanguages(ctx, scope)
	if err != nil {
		return nil, err
	}
	if unset := counts["und"]; unset > 0 && s.Locales != nil {
		defaults, err := s.Locales.DefaultConversationLanguages(ctx, scope)
		if err != nil {
			return nil, err
		}
		total := 0
		for language, n := range defaults {
			if !chatrender.Supported(language) || chatrender.Language(language) == "und" || n < 0 {
				return nil, chatrender.ErrInvalid
			}
			total += n
			counts[chatrender.Language(language)] += n
		}
		if total != unset {
			return nil, chatrender.ErrInvalid
		}
		delete(counts, "und")
	}
	return counts, nil
}
func (s *ChatRenderingSurface) AuthorizeRendering(ctx context.Context, scope chatstore.RenderingScope, action string) error {
	if s == nil || s.Store == nil || s.Policy == nil {
		return chatrender.ErrUnavailable
	}
	return s.Policy.AuthorizeRendering(ctx, scope, action)
}
func (s *ChatRenderingSurface) ReadRenderingSelection(ctx context.Context, scope chatstore.RenderingScope, post string) (chatrender.Rendering, chatrender.Mark, error) {
	if err := s.AuthorizeRendering(ctx, scope, "selection"); err != nil {
		return chatrender.Rendering{}, chatrender.Mark{}, err
	}
	policy, err := s.Policy.RenderingPolicy(ctx, scope, post)
	if err != nil {
		return chatrender.Rendering{}, chatrender.Mark{}, err
	}
	if policy.Original.Tenant != scope.Tenant || policy.Original.Message != post {
		return chatrender.Rendering{}, chatrender.Mark{}, chat.ErrPermissionDenied
	}
	pref, err := s.LanguageSettings(ctx, scope, s.InterfaceLocale)
	if err != nil {
		return chatrender.Rendering{}, chatrender.Mark{}, err
	}
	// CHATLANG-006: where policy does not offer translation (workspace or
	// channel off, no engine, text the filters mask) the reader simply reads the
	// original; it is not a failed translation.
	// CHATLANG-008: a person's own message is never translated back to them.
	if !chatrenderOffersTranslation(policy) || policy.Own {
		pref.Translate, pref.SourceOverrides = false, nil
	}
	// CHATTONE: the reader's own tone choice applies only where the policy allows
	// rewording; anywhere else the message is read as written.
	if chatrenderOffersKind(policy, chatrender.Reword) && s.Reword != nil {
		if tone, err := s.Reword.ReaderTone(ctx, scope.Tenant, scope.Principal.SubjectID, scope.Conversation, pref.Tone); err == nil {
			pref.Tone = tone
		}
	} else {
		pref.Tone = chatrender.AsWritten
	}
	if policy.RequireMask {
		source, ok := s.Policy.(interface {
			MaskedRendering(context.Context, chatstore.RenderingScope, chatrender.Policy) (chatrender.Rendering, error)
		})
		if ok {
			masked, err := source.MaskedRendering(ctx, scope, policy)
			if err != nil {
				return chatrender.Rendering{}, chatrender.Mark{}, err
			}
			selected, mark := chatrender.SelectForReader(policy, pref, chatrender.Available{Renderings: []chatrender.Rendering{masked}})
			return selected, mark, nil
		}
	}
	targets := chatrender.RequestsForPolicy(policy, []chatrender.Preference{pref})
	tone, language := chatrender.AsWritten, policy.Original.SourceLanguage
	if len(targets) > 0 {
		tone, language = targets[0].Tone, targets[0].Language
	}
	available, err := s.RenderingAvailabilityForTarget(ctx, scope, post, tone, language)
	if err != nil {
		return chatrender.Rendering{}, chatrender.Mark{}, err
	}
	if !available.Pending && !available.Failed {
		for _, target := range targets {
			found := false
			for _, r := range available.Renderings {
				if r.Tone == target.Tone && r.Language == target.Language && chatrender.PermittedRendering(policy, r) {
					found = true
				}
			}
			if !found {
				if err = s.RequestRendering(ctx, scope, target); err != nil {
					if errors.Is(err, chatrender.ErrDenied) || errors.Is(err, chatrender.ErrUnavailable) {
						available.Failed = true
						break
					}
					return chatrender.Rendering{}, chatrender.Mark{}, err
				}
				available.Pending = true
				available.WaitExpired = false
			}
		}
	}
	rendering, mark := chatrender.SelectForReader(policy, pref, available)
	return rendering, mark, nil
}

var _ ChatRenderingPort = (*ChatRenderingSurface)(nil)

func (s *ChatRenderingSurface) SearchMessageLanguages(ctx context.Context, scope chatstore.RenderingScope, language string) ([]chatstore.MessageLanguage, error) {
	if err := s.AuthorizeRendering(ctx, scope, "search-language"); err != nil {
		return nil, err
	}
	rows, err := s.Store.SearchMessageLanguages(ctx, scope, language)
	if err != nil {
		return nil, err
	}
	out := []chatstore.MessageLanguage{}
	for _, row := range rows {
		policy, err := s.Policy.RenderingPolicy(ctx, scope, row.Message)
		if errors.Is(err, chat.ErrPermissionDenied) || errors.Is(err, chat.ErrNotFound) || errors.Is(err, chatrender.ErrDenied) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if policy.Original.Revision == row.Revision {
			out = append(out, row)
		}
	}
	return out, nil
}
