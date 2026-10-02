// Package chatrender owns derived message views, never the message record.
package chatrender

import (
	"errors"
	"strings"
)

var (
	ErrInvalid     = errors.New("chatrender: invalid request")
	ErrDenied      = errors.New("chatrender: access denied")
	ErrUnavailable = errors.New("chatrender: producer unavailable")
	ErrLease       = errors.New("chatrender: expired or superseded lease")
)

type Tone string

const (
	AsWritten Tone = "as-written"
	Reworded  Tone = "reworded"
)

type Kind string

const (
	Mask      Kind = "mask"
	Reword    Kind = "reworded"
	Translate Kind = "translated"
)

type ProducerIdentity struct {
	Provider          string `json:"provider"`
	Model             string `json:"model"`
	InstructionDigest string `json:"instruction_digest"`
	GlossaryVersion   string `json:"glossary_version"`
}
type Checks struct {
	Meaning      bool     `json:"meaning"`
	Placeholders bool     `json:"placeholders"`
	Failures     []string `json:"failures,omitempty"`
}
type Rendering struct {
	Tenant         string             `json:"tenant"`
	Message        string             `json:"message"`
	Revision       uint64             `json:"revision"`
	Tone           Tone               `json:"tone"`
	Language       string             `json:"language"`
	Text           string             `json:"text"`
	SourceLanguage string             `json:"source_language"`
	Kinds          []Kind             `json:"kinds"`
	Producer       ProducerIdentity   `json:"producer"`
	Producers      []ProducerIdentity `json:"producers,omitempty"`
	Checks         Checks             `json:"checks"`
	Confidence     float64            `json:"confidence"`
	CostReference  string             `json:"cost_reference"`
}
type Policy struct {
	Original        Rendering
	AllowOriginal   bool
	RequireMask     bool
	RequireReworded bool
	AllowedKinds    []Kind
	// Own is true when the reader wrote this message. A person's own message is
	// never translated back to them (CHATLANG-008).
	Own bool
}
type Preference struct {
	Tone             Tone            `json:"tone"`
	ReadingLanguage  string          `json:"reading_language"`
	FurtherLanguages []string        `json:"further_languages"`
	Translate        bool            `json:"translate"`
	SourceOverrides  map[string]bool `json:"source_overrides,omitempty"`
}
type Mark struct {
	State            string `json:"state"`
	Kinds            []Kind `json:"kinds,omitempty"`
	SourceLanguage   string `json:"source_language,omitempty"`
	CanShowOriginal  bool   `json:"can_show_original"`
	CanShowAsWritten bool   `json:"can_show_as_written"`
	// Wanted names what this reader's settings and the channel's policy asked
	// for when the state is not "ready" ("pending", "fallback", "unavailable"):
	// a client says "Translating…" or "Not translated" from it, never from a guess.
	Wanted []Kind `json:"wanted,omitempty"`
}
type Available struct {
	Renderings []Rendering
	Pending    bool
	// WaitExpired is calculated from the durable request time by the caller.
	WaitExpired bool
	Failed      bool
}

func Language(tag string) string {
	tag = strings.TrimSpace(tag)
	if end := strings.IndexAny(tag, "-_,;"); end >= 0 {
		tag = tag[:end]
	}
	return strings.ToLower(tag)
}
func Supported(tag string) bool {
	switch Language(tag) {
	case "en", "de", "fr", "es", "pt", "ar", "ja", "hi", "und":
		return true
	}
	return false
}
func DefaultPreference(locale string) Preference {
	language := Language(locale)
	if !Supported(language) || language == "und" {
		language = "en"
	}
	// CHATLANG-002: a person who has not chosen reads in their interface language
	// and gets translations wherever the workspace offers them; a workspace that
	// does not offer translation (the default) never produces one.
	return Preference{Tone: AsWritten, ReadingLanguage: language, Translate: true}
}
func (p Preference) Validate() error {
	if (p.Tone != AsWritten && p.Tone != Reworded) || !Supported(p.ReadingLanguage) || Language(p.ReadingLanguage) == "und" {
		return ErrInvalid
	}
	for _, l := range p.FurtherLanguages {
		if !Supported(l) || Language(l) == "und" {
			return ErrInvalid
		}
	}
	seenOverrides := map[string]bool{}
	for l := range p.SourceOverrides {
		if !Supported(l) || Language(l) == "und" {
			return ErrInvalid
		}
		if seenOverrides[Language(l)] {
			return ErrInvalid
		}
		seenOverrides[Language(l)] = true
	}
	return nil
}
func WantsTranslation(p Preference, source string) bool {
	source = Language(source)
	if source == "" || source == "und" || source == Language(p.ReadingLanguage) {
		return false
	}
	for _, l := range p.FurtherLanguages {
		if Language(l) == source {
			return false
		}
	}
	if on, ok := p.SourceOverrides[source]; ok {
		return on
	}
	best := ""
	on := false
	for tag, value := range p.SourceOverrides {
		if Language(tag) == source && (best == "" || tag < best) {
			best = tag
			on = value
		}
	}
	if best != "" {
		return on
	}
	return p.Translate
}
func hasKind(kinds []Kind, kind Kind) bool {
	for _, k := range kinds {
		if k == kind {
			return true
		}
	}
	return false
}
func allowed(p Policy, r Rendering) bool {
	if r.Tenant != p.Original.Tenant || r.Message != p.Original.Message || r.Revision != p.Original.Revision || !r.Checks.Meaning || !r.Checks.Placeholders || len(r.Checks.Failures) > 0 {
		return false
	}
	if Language(r.SourceLanguage) != Language(p.Original.SourceLanguage) {
		return false
	}
	if p.RequireMask && !hasKind(r.Kinds, Mask) || p.RequireReworded && r.Tone != Reworded {
		return false
	}
	for _, k := range r.Kinds {
		if !hasKind(p.AllowedKinds, k) {
			return false
		}
	}
	return len(r.Kinds) > 0
}

// PermittedRendering applies the same policy checks to list and export reads.
func PermittedRendering(p Policy, r Rendering) bool { return allowed(p, r) }

// Select is pure: time, authorization and fetching happen before this call.
// A forbidden original is never used as a fallback, even after a failed job.
func Select(p Policy, pref Preference, a Available) (Rendering, Mark) {
	tone := pref.Tone
	if tone == "" {
		tone = AsWritten
	}
	if p.RequireReworded {
		tone = Reworded
	}
	language := Language(p.Original.SourceLanguage)
	// A channel whose policy does not allow translation shows the original; the
	// reader's wish for a translation is not a request nobody can fill.
	translating := WantsTranslation(pref, language) && hasKind(p.AllowedKinds, Translate)
	if translating {
		language = Language(pref.ReadingLanguage)
	}
	mark := Mark{SourceLanguage: p.Original.SourceLanguage, CanShowOriginal: p.AllowOriginal && !p.RequireMask && !p.RequireReworded, CanShowAsWritten: !p.RequireReworded}
	wanted := func(m Mark) Mark {
		if tone == Reworded {
			m.Wanted = append(m.Wanted, Reword)
		}
		if translating {
			m.Wanted = append(m.Wanted, Translate)
		}
		return m
	}
	if mark.CanShowOriginal && tone == AsWritten && language == Language(p.Original.SourceLanguage) {
		mark.State = "original"
		return p.Original, mark
	}
	for _, r := range a.Renderings {
		if r.Tone == tone && Language(r.Language) == language && allowed(p, r) {
			mark.State = "ready"
			mark.Kinds = append([]Kind(nil), r.Kinds...)
			return r, mark
		}
	}
	if a.Pending && !a.WaitExpired && !a.Failed {
		mark.State = "pending"
		return Rendering{}, wanted(mark)
	}
	if mark.CanShowOriginal {
		mark.State = "original"
		if a.Pending || a.Failed || tone != AsWritten || language != Language(p.Original.SourceLanguage) {
			mark.State = "fallback"
			return p.Original, wanted(mark)
		}
		return p.Original, mark
	}
	mark.State = "unavailable"
	return Rendering{}, wanted(mark)
}

// SelectForReader is the common boundary for timeline, search and agent views.
func SelectForReader(p Policy, pref Preference, a Available) (Rendering, Mark) {
	return Select(p, pref, a)
}
