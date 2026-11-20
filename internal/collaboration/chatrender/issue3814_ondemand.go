package chatrender

import "context"

// Issue 3814: a reader can ask for one message to be translated even when their
// settings would leave it as written (a language they read a little of, or
// automatic translation switched off). The ask is carried on the request, never
// stored on the message, and it widens nothing: the channel's policy still has
// to offer translation, and own, masked or held text is refused.

type onDemandTranslationKey struct{}

// Reasons a message asked for on demand is not translated, in Mark.Refused.
const (
	// RefusedPolicy: the channel or the text does not allow a translation (the
	// workspace or channel has translation off, the text is masked or restricted,
	// or the reader wrote it).
	RefusedPolicy = "policy"
	// RefusedSameLanguage: the message is already in the language the reader reads.
	RefusedSameLanguage = "same_language"
)

// WithOnDemandTranslation marks a read as the reader's own press on "Translate"
// for the message being read.
func WithOnDemandTranslation(ctx context.Context) context.Context {
	return context.WithValue(ctx, onDemandTranslationKey{}, true)
}

// OnDemandTranslation reports whether the read was asked for by that press.
func OnDemandTranslation(ctx context.Context) bool {
	on, _ := ctx.Value(onDemandTranslationKey{}).(bool)
	return on
}

// WithTranslationAsked returns the preference with translation switched on for
// one source language, whatever the reader's standing choice was. The reader's
// stored settings are not touched; this is a copy for one read.
func WithTranslationAsked(p Preference, source string) Preference {
	source = Language(source)
	if source == "" || source == "und" {
		return p
	}
	further := make([]string, 0, len(p.FurtherLanguages))
	for _, l := range p.FurtherLanguages {
		if Language(l) != source {
			further = append(further, l)
		}
	}
	p.FurtherLanguages = further
	overrides := make(map[string]bool, len(p.SourceOverrides)+1)
	for tag, on := range p.SourceOverrides {
		if Language(tag) != source {
			overrides[tag] = on
		}
	}
	overrides[source] = true
	p.SourceOverrides = overrides
	p.Translate = true
	return p
}
