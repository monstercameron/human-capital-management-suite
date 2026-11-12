package main

import (
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// chatStateWithLocale returns the configuration and model Chat should hold
// once the product route resolves to locale. It is pure: it reads and writes
// nothing outside its arguments, so the native tests can drive it. The
// locale is canonicalized the same way the product shell does, and changed
// reports whether either the configuration or the model moved. Direction is
// not stored: the Chat renderers derive it from model.Locale.
func chatStateWithLocale(cfg journeyclient.Config, model chatui.Model, locale string) (journeyclient.Config, chatui.Model, bool) {
	resolved := productui.ResolveProductLocale(locale).Resolved
	if cfg.Locale == resolved && model.Locale == resolved {
		return cfg, model, false
	}
	cfg.Locale = resolved
	model.Locale = resolved
	model.Number = chatNumberFormatter(resolved)
	return cfg, model, true
}

// adoptChatLocale applies chatStateWithLocale to the shared Chat state. It is
// called from the product route loader when the route's resolved locale
// differs from the one Chat was configured with at boot. A soft locale switch
// keeps the Chat client alive, so without this every Chat read kept the boot
// locale. It reports whether the state changed, so the caller re-reads Chat
// under the new locale instead of adopting an answer computed under the old one.
func (s *chatState) adoptChatLocale(locale string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil || s.cfg.Bearer == "" {
		return false
	}
	cfg, model, changed := chatStateWithLocale(s.cfg, s.model, locale)
	if changed {
		s.cfg, s.model = cfg, model
	}
	return changed
}
