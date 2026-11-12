package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// TestChatLocaleFollowsSoftSwitch walks en-US to de-DE to ar and back. After
// each step the configuration locale, the model locale, the count formatter
// and the direction derived from the model locale all name the same locale,
// with nothing left on the boot locale.
func TestChatLocaleFollowsSoftSwitch(t *testing.T) {
	cfg := journeyclient.Config{Locale: "en-US"}
	model := chatui.Model{Locale: "en-US", Number: chatNumberFormatter("en-US")}

	steps := []struct {
		locale    string
		wantDir   string
		wantCount string
	}{
		{"de-DE", "ltr", "12"},
		{"ar", "rtl", localizeDigits("12", productui.ResolveProductLocale("ar"))},
		{"en-US", "ltr", "12"},
	}
	for _, step := range steps {
		var changed bool
		cfg, model, changed = chatStateWithLocale(cfg, model, step.locale)
		if !changed {
			t.Fatalf("%s: switch reported no change", step.locale)
		}
		if cfg.Locale != step.locale || model.Locale != step.locale {
			t.Fatalf("%s: stale locale cfg=%q model=%q", step.locale, cfg.Locale, model.Locale)
		}
		if got := model.Number(12); got != step.wantCount {
			t.Fatalf("%s: count formatter = %q, want %q", step.locale, got, step.wantCount)
		}
		if dir := string(productui.ResolveProductLocale(model.Locale).Direction); dir != step.wantDir {
			t.Fatalf("%s: direction = %q, want %q", step.locale, dir, step.wantDir)
		}
	}
	if step, _, changed := chatStateWithLocale(cfg, model, "en-US"); changed || step.Locale != "en-US" {
		t.Fatalf("same locale reported a change: changed=%v locale=%q", changed, step.Locale)
	}
}

// TestChatLocaleCanonicalizesUnsupported keeps an unsupported request on the
// product fallback, the same locale the shell renders.
func TestChatLocaleCanonicalizesUnsupported(t *testing.T) {
	cfg, model, changed := chatStateWithLocale(journeyclient.Config{Locale: "de-DE"}, chatui.Model{Locale: "de-DE"}, "fr-FR")
	if !changed || cfg.Locale != productui.DefaultProductLocale || model.Locale != productui.DefaultProductLocale {
		t.Fatalf("unsupported locale: changed=%v cfg=%q model=%q", changed, cfg.Locale, model.Locale)
	}
}
