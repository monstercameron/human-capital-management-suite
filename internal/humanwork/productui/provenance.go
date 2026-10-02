package productui

import (
	"hash/fnv"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ProvenanceProjection is an already-authorized, display-safe projection of
// the canonical evidence and lineage contracts. It intentionally has no
// evidence reference, policy reference, digest, graph identifier, presence
// reason, confidence score, or permission. Those values may be protected and
// are not part of the visual grammar.
//
// Bound distinguishes "no provenance binding was supplied for this surface"
// from a supplied binding whose individual values are redacted, unknown, or
// temporarily unavailable. An unbound projection renders nothing.
type ProvenanceProjection struct {
	Bound             bool
	AuthorityKind     values.Presence[ProvenanceAuthority]
	AuthoritySystem   values.Presence[string]
	EvidenceSource    values.Presence[string]
	LineageStatus     values.Presence[ProvenanceLineage]
	SourceVersion     values.Presence[string]
	EffectiveAt       values.Presence[time.Time]
	RecordedAt        values.Presence[time.Time]
	HasOpaqueBoundary bool
}

// ProvenanceBinding is a readable alias for callers composing a page.
type ProvenanceBinding = ProvenanceProjection

// ProjectAuthorizedCanonicalProvenance, which fills a projection from the
// canonical service messages, is in chatperf2_provenance_canonical.go: it is
// the one part of this page that needs those messages, and the browser client
// does not carry them.

type ProvenancePresentationProps struct {
	I18nProps
	// IDSeed is hashed before emission so source and graph identifiers cannot
	// become DOM IDs or selectors.
	IDSeed     string
	Projection ProvenanceProjection
}

type provenanceItem struct {
	kind, label, value, valueKey, glyph, tone, dateTime string
}

// ProvenancePresentation presents independent source-authority, evidence,
// completeness, and time facts. It never derives confidence, aggregate trust,
// access, permission, or an available action from those facts.
func ProvenancePresentation(props ProvenancePresentationProps) ui.Node {
	if !props.Projection.Bound {
		return html.Fragment()
	}

	groupLabel := props.Text("provenance.group")
	sectionProps := html.Props{Class: "provenance-grammar", Aria: map[string]string{"label": groupLabel}}
	headingProps := html.Props{Class: "provenance-heading"}
	if id := stableProvenanceID(props.IDSeed); id != "" {
		sectionProps.ID = id
		headingProps.ID = id + "-heading"
		sectionProps.Aria = map[string]string{"labelledby": headingProps.ID}
	}

	p := props.Projection
	primary := []provenanceItem{
		provenanceAuthorityItem(props.Locale, p.AuthorityKind),
		provenanceTextItem(props.Locale, "authority-system", props.Text("provenance.authority_system_label"), p.AuthoritySystem),
		provenanceTextItem(props.Locale, "evidence-source", props.Text("provenance.evidence_source_label"), p.EvidenceSource),
		provenanceLineageItem(props.Locale, p.LineageStatus),
	}
	metadata := []provenanceItem{
		provenanceTextItem(props.Locale, "source-version", props.Text("provenance.source_version"), p.SourceVersion),
		provenanceTimeItem(props.Locale, "effective-at", props.Text("provenance.effective_at"), p.EffectiveAt),
		provenanceTimeItem(props.Locale, "recorded-at", props.Text("provenance.recorded_at"), p.RecordedAt),
	}

	children := []ui.Node{
		html.H3(headingProps, ui.Text(groupLabel)),
		html.Tag("dl", html.Props{Class: "provenance-item-list"}, provenanceItemNodes(primary)...),
		html.Tag("dl", html.Props{Class: "provenance-meta"}, provenanceItemNodes(metadata)...),
	}
	if p.HasOpaqueBoundary {
		children = append(children, html.P(html.Props{Class: "provenance-opaque-boundary"},
			html.Span(html.Props{Class: "provenance-boundary-glyph", Raw: map[string]any{"aria-hidden": "true"}}, ui.Text("■")),
			ui.Text(props.Text("provenance.opaque_boundary")),
		))
	}
	children = append(children, html.P(html.Props{Class: "provenance-boundary"}, ui.Text(props.Text("provenance.boundary"))))
	return html.Section(sectionProps, children...)
}

func provenanceItemNodes(items []provenanceItem) []ui.Node {
	children := make([]ui.Node, 0, len(items))
	for _, item := range items {
		value := ui.Node(ui.Text(item.value))
		if item.dateTime != "" {
			value = html.Time(html.Props{Raw: map[string]any{"datetime": item.dateTime}}, ui.Text(item.value))
		}
		children = append(children, html.Div(html.Props{
			Class: "provenance-item provenance-tone-" + item.tone,
			Raw:   map[string]any{"data-provenance-kind": item.kind, "data-provenance-value": item.valueKey},
		},
			html.Span(html.Props{Class: "provenance-item-glyph", Raw: map[string]any{"aria-hidden": "true"}}, ui.Text(item.glyph)),
			html.Tag("dt", html.Props{Class: "provenance-item-name"}, ui.Text(item.label)),
			html.Tag("dd", html.Props{Class: "provenance-item-value"}, value),
		))
	}
	return children
}

func provenanceAuthorityItem(locale LocaleContext, presence values.Presence[ProvenanceAuthority]) provenanceItem {
	label := locale.Text("provenance.source_authority_label")
	if value, ok := presence.Get(); ok {
		key, glyph, tone, valid := authorityKindToken(value)
		if !valid {
			return provenanceInvalidItem("source-authority", label, locale)
		}
		return provenanceItem{"source-authority", label, locale.Text("provenance.authority." + key), key, glyph, tone, ""}
	}
	return provenancePresenceItem(locale, "source-authority", label, presence.State())
}

func provenanceLineageItem(locale LocaleContext, presence values.Presence[ProvenanceLineage]) provenanceItem {
	label := locale.Text("provenance.completeness_label")
	if value, ok := presence.Get(); ok {
		key, glyph, tone, valid := lineageToken(value)
		if !valid {
			return provenanceInvalidItem("lineage-completeness", label, locale)
		}
		return provenanceItem{"lineage-completeness", label, locale.Text("provenance.completeness." + key), key, glyph, tone, ""}
	}
	return provenancePresenceItem(locale, "lineage-completeness", label, presence.State())
}

func provenanceTextItem(locale LocaleContext, kind, label string, presence values.Presence[string]) provenanceItem {
	if value, ok := presence.Get(); ok {
		value = strings.TrimSpace(value)
		if value == "" {
			return provenanceInvalidItem(kind, label, locale)
		}
		return provenanceItem{kind, label, value, "present", "●", "neutral", ""}
	}
	return provenancePresenceItem(locale, kind, label, presence.State())
}

func provenanceTimeItem(locale LocaleContext, kind, label string, presence values.Presence[time.Time]) provenanceItem {
	if value, ok := presence.Get(); ok {
		if value.IsZero() {
			return provenanceInvalidItem(kind, label, locale)
		}
		return provenanceItem{kind, label, formatProvenanceTime(locale, value), "present", "◷", "neutral", value.UTC().Format(time.RFC3339)}
	}
	return provenancePresenceItem(locale, kind, label, presence.State())
}

func provenancePresenceItem(locale LocaleContext, kind, label string, state values.PresenceState) provenanceItem {
	key, glyph, tone := provenancePresenceToken(state)
	return provenanceItem{kind, label, locale.Text("provenance.value." + key), key, glyph, tone, ""}
}

func provenanceInvalidItem(kind, label string, locale LocaleContext) provenanceItem {
	return provenanceItem{kind, label, locale.Text("provenance.value.invalid"), "invalid", "!", "invalid", ""}
}

func provenancePresenceToken(state values.PresenceState) (key, glyph, tone string) {
	switch state {
	case values.PresenceAbsent:
		return "not_supplied", "—", "muted"
	case values.PresenceNull:
		return "null", "∅", "muted"
	case values.PresenceUnknown:
		return "unknown", "?", "unknown"
	case values.PresenceRedacted:
		return "redacted", "■", "muted"
	case values.PresenceUnavailable:
		return "unavailable", "…", "unknown"
	case values.PresenceNotApplicable:
		return "not_applicable", "–", "muted"
	default:
		return "invalid", "!", "invalid"
	}
}

func authorityKindToken(value ProvenanceAuthority) (key, glyph, tone string, valid bool) {
	switch value {
	case ProvenanceAuthorityLocal:
		return "local_authoritative", "◆", "info", true
	case ProvenanceAuthorityExternal:
		return "external_observation", "○", "neutral", true
	case ProvenanceAuthorityDerived:
		return "derived", "∴", "neutral", true
	default:
		return "", "", "", false
	}
}

func lineageToken(value ProvenanceLineage) (key, glyph, tone string, valid bool) {
	switch value {
	case ProvenanceLineageComplete:
		return "complete", "≡", "neutral", true
	case ProvenanceLineagePartial:
		return "partial", "◌", "caution", true
	case ProvenanceLineageUnknown:
		return "unknown", "?", "unknown", true
	default:
		return "", "", "", false
	}
}

func formatProvenanceTime(locale LocaleContext, value time.Time) string {
	locale = locale.normalized()
	location, err := time.LoadLocation(locale.TimeZone)
	if err != nil {
		location = time.UTC
	}
	local := value.In(location)
	return locale.FormatDate(value) + " · " + local.Format("15:04 MST")
}

func stableProvenanceID(seed string) string {
	seed = strings.TrimSpace(seed)
	if seed == "" {
		return ""
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(seed))
	return "provenance-" + strconv.FormatUint(h.Sum64(), 36)
}
