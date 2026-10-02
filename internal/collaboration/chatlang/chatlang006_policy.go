package chatlang

import (
	"sort"
	"strings"
	"time"
)

// DefaultMonthlyBudgetMicros is the workspace's monthly translation limit when
// an administrator has not set one: five units of currency, in millionths.
const DefaultMonthlyBudgetMicros int64 = 5_000_000

// Switch is a setting that a channel may inherit from its workspace.
type Switch string

const (
	Inherit Switch = "inherit"
	On      Switch = "on"
	Off     Switch = "off"
)

// External is whether text may be sent to an engine outside the deployment.
type External string

const (
	ExternalInherit External = "inherit"
	ExternalAllowed External = "allowed"
	ExternalBarred  External = "barred"
)

// Workspace is what an administrator sets for the whole workspace. The zero
// value is "translation off".
type Workspace struct {
	Enabled bool `json:"enabled"`
	// Languages are the reading languages offered; empty offers every one the
	// product supports.
	Languages []string `json:"languages"`
	// BudgetMicros is the monthly limit; 0 means DefaultMonthlyBudgetMicros.
	BudgetMicros int64 `json:"budget_micros"`
	// ExternalAllowed lets text go to an engine outside the deployment. It
	// defaults to allowed so that a workspace that turns translation on gets it.
	ExternalAllowed bool `json:"external_allowed"`
	// Formality is "formal" or "informal" per language, empty for the engine's
	// own choice.
	Formality       map[string]string `json:"formality"`
	GlossaryVersion int64             `json:"glossary_version"`
	Revision        int64             `json:"revision"`
}

// Channel is what an administrator or channel manager sets for one channel.
type Channel struct {
	Translation Switch   `json:"translation"`
	External    External `json:"external"`
}

// Reason names why a translation was not allowed. It is a stable code, never
// a sentence.
type Reason string

const (
	Allowed              Reason = ""
	ReasonWorkspaceOff   Reason = "workspace_off"
	ReasonChannelOff     Reason = "channel_off"
	ReasonLanguage       Reason = "language_not_offered"
	ReasonExternalBarred Reason = "external_barred"
	ReasonBudget         Reason = "budget"
)

// Budget is the effective monthly limit.
func (w Workspace) Budget() int64 {
	if w.BudgetMicros > 0 {
		return w.BudgetMicros
	}
	return DefaultMonthlyBudgetMicros
}

// Offers reports whether the language may be a reading language here.
func (w Workspace) Offers(language string) bool {
	if len(w.Languages) == 0 {
		return true
	}
	for _, l := range w.Languages {
		if l == language {
			return true
		}
	}
	return false
}

// ChannelOn reports whether translation is on in the channel, ignoring
// language, engine and budget.
func ChannelOn(w Workspace, c Channel) Reason {
	if !w.Enabled {
		return ReasonWorkspaceOff
	}
	if c.Translation == Off {
		return ReasonChannelOff
	}
	return Allowed
}

// ExternalOK reports whether an external engine may be used in the channel.
func ExternalOK(w Workspace, c Channel) bool {
	switch c.External {
	case ExternalAllowed:
		return w.ExternalAllowed
	case ExternalBarred:
		return false
	}
	return w.ExternalAllowed
}

// Decide is the single decision: whether a message in this channel may be
// translated into target by an engine that is, or is not, outside the
// deployment, given what has already been spent this month. The workspace
// switch is a ceiling: a channel may turn translation off but cannot turn it
// on in a workspace that has it off.
func Decide(w Workspace, c Channel, target string, externalEngine bool, spentMicros int64) Reason {
	if reason := ChannelOn(w, c); reason != Allowed {
		return reason
	}
	if !w.Offers(target) {
		return ReasonLanguage
	}
	if externalEngine && !ExternalOK(w, c) {
		return ReasonExternalBarred
	}
	if spentMicros >= w.Budget() {
		return ReasonBudget
	}
	return Allowed
}

// Month is the budget period of a moment, in UTC.
func Month(at time.Time) (from, to time.Time) {
	at = at.UTC()
	from = time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, time.UTC)
	return from, from.AddDate(0, 1, 0)
}

// Supported reading languages an administrator may offer; kept here so the
// policy check does not import the rendering package.
var supported = []string{"ar", "de", "en", "es", "fr", "hi", "ja", "pt"}

// SupportedLanguages returns the languages the product translates between.
func SupportedLanguages() []string { return append([]string(nil), supported...) }

// Normalize validates a workspace setting and returns it in canonical form.
func (w Workspace) Normalize() (Workspace, error) {
	seen := map[string]bool{}
	var languages []string
	for _, l := range w.Languages {
		l = strings.ToLower(strings.TrimSpace(l))
		if !isSupported(l) {
			return Workspace{}, ErrInvalid
		}
		if !seen[l] {
			seen[l] = true
			languages = append(languages, l)
		}
	}
	sort.Strings(languages)
	w.Languages = languages
	if w.BudgetMicros < 0 || w.BudgetMicros > 100_000_000_000 {
		return Workspace{}, ErrInvalid
	}
	formality := map[string]string{}
	for l, f := range w.Formality {
		l, f = strings.ToLower(strings.TrimSpace(l)), strings.ToLower(strings.TrimSpace(f))
		if !isSupported(l) || (f != "" && f != "formal" && f != "informal") {
			return Workspace{}, ErrInvalid
		}
		if f != "" {
			formality[l] = f
		}
	}
	w.Formality = formality
	return w, nil
}

// Normalize validates a channel setting.
func (c Channel) Normalize() (Channel, error) {
	if c.Translation == "" {
		c.Translation = Inherit
	}
	if c.External == "" {
		c.External = ExternalInherit
	}
	switch c.Translation {
	case Inherit, On, Off:
	default:
		return Channel{}, ErrInvalid
	}
	switch c.External {
	case ExternalInherit, ExternalAllowed, ExternalBarred:
	default:
		return Channel{}, ErrInvalid
	}
	return c, nil
}

func isSupported(language string) bool {
	for _, l := range supported {
		if l == language {
			return true
		}
	}
	return false
}

// ValidTerm reports whether a glossary entry may be stored.
func ValidTerm(t Term) bool {
	t.Source = strings.TrimSpace(t.Source)
	if t.Source == "" || len(t.Source) > 120 || strings.ContainsAny(t.Source, "⟦⟧\n") {
		return false
	}
	if t.Language == "" {
		return t.Target == ""
	}
	t.Target = strings.TrimSpace(t.Target)
	return isSupported(t.Language) && t.Target != "" && len(t.Target) <= 120 && !strings.ContainsAny(t.Target, "⟦⟧\n")
}
