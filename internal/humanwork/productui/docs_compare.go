// Package productui: HUB-033/DOCS-01's version-compare flow. The compare
// dialog never edits: it reads two already-immutable versions side by
// side, chosen from two pickers (From/To) built from
// View.ListDocumentVersions (ListDocumentVersions) and read through
// View.CompareDocumentVersions (GetDocumentVersion). A version the reader
// may not open reports only that it could not be loaded, never its bytes.
package productui

import (
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// docsFetchKey is an effect key for a read that can be retried: the
// document it is for and the retry count, so either change re-runs it.
type docsFetchKey struct {
	ID      string
	Attempt int
}

// docsCompareListState is the compare dialog's fetch outcome for the
// version list backing its two pickers.
type docsCompareListState struct {
	Loading, Failed, Loaded bool
	Versions                []DocumentVersionSummary
}

// docsCompareReadState is the compare dialog's fetch outcome for the two
// chosen versions once both are known.
type docsCompareReadState struct {
	Loading, Failed, Loaded bool
	From, To                DocumentVersionProjection
}

type docsCompareDialogProps struct {
	Locale     string
	DocumentID string
	// Base is the version currently open in the reader; it is used to
	// avoid a network round trip when a picker resolves to it, and as the
	// only comparable version when ListVersions is nil.
	Base                    DocumentVersionProjection
	ListVersions            func(documentID string, done func([]DocumentVersionSummary, error))
	CompareDocumentVersions func(documentID, versionID string, done func(DocumentVersionProjection, error))
	Close                   func()
}

// docsCompareDialog lists the document's versions in two pickers, From and
// To, defaulting to the previous version versus the current one, and reads
// both immutable versions side by side once two distinct versions are
// chosen. Both sides are read-only Markdown; there is no editing surface
// anywhere in this dialog, so a compare can never touch deployed bytes.
func docsCompareDialog(props docsCompareDialogProps) ui.Node {
	locale := props.Locale
	// ResolveProductLocale, not a bare LocaleContext{Resolved: locale}: an
	// incomplete context (no CatalogVersion) makes LocaleContext.normalized
	// treat it as unresolved and fall back to en-US regardless of Resolved,
	// which had been silently defeating this dialog's de-DE/ar dates.
	view := View{Locale: ResolveProductLocale(locale)}
	list := ui.UseState(docsCompareListState{})
	fromID := ui.UseState("")
	toID := ui.UseState("")
	defaulted := ui.UseRef(false)
	read := ui.UseState(docsCompareReadState{})
	close := ui.UseEvent(func(ui.MouseEvent) { props.Close() })
	ui.UseEffect(func() func() { return docsListenEscape(props.Close) })
	useDocsModal(true, "docs-compare-dialog", "#docs-compare-from", docsDialogReturnFallbacks...)
	keydown := ui.UseEvent(func(ui.KeyboardEvent) {})
	// attempt re-runs the version-list fetch: a failed list is a dead end
	// without it, since both pickers depend on it (D-3).
	attempt := ui.UseState(0)
	retry := ui.UseEvent(func(ui.MouseEvent) { attempt.Set(attempt.Get() + 1) })

	ui.UseEffectOf(func() func() {
		if props.ListVersions == nil {
			return nil
		}
		list.Set(docsCompareListState{Loading: true})
		props.ListVersions(props.DocumentID, func(versions []DocumentVersionSummary, err error) {
			// The reply lands off the frame loop; apply it on the loop.
			ui.PostAsync(func() {
				if err != nil {
					list.Set(docsCompareListState{Failed: true})
					return
				}
				list.Set(docsCompareListState{Loaded: true, Versions: versions})
			})
		})
		return nil
	}, docsFetchKey{props.DocumentID, attempt.Get()})

	current := list.Get()
	// Default From/To once the list has loaded: To is the current version
	// (falling back to the last entry), From is the entry immediately
	// before it, so the dialog opens already comparing "previous vs
	// current" without the reader choosing anything.
	if current.Loaded && !defaulted.Get() {
		defaulted.Set(true)
		selectable := docsCompareSelectable(current.Versions)
		toIndex := len(selectable) - 1
		for i, v := range selectable {
			if v.IsCurrent {
				toIndex = i
				break
			}
		}
		if toIndex >= 0 {
			toID.Set(selectable[toIndex].VersionID)
			if toIndex > 0 {
				fromID.Set(selectable[toIndex-1].VersionID)
			}
		}
	}

	resolveVersion := func(id string, done func(DocumentVersionProjection, error)) {
		if id == props.Base.VersionID && props.Base.Readable {
			done(props.Base, nil)
			return
		}
		if props.CompareDocumentVersions == nil {
			done(DocumentVersionProjection{}, nil)
			return
		}
		props.CompareDocumentVersions(props.DocumentID, id, done)
	}

	submit := ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		from, to := fromID.Get(), toID.Get()
		if from == "" || to == "" || from == to || read.Get().Loading {
			return
		}
		read.Set(docsCompareReadState{Loading: true})
		resolveVersion(from, func(fromVersion DocumentVersionProjection, fromErr error) {
			resolveVersion(to, func(toVersion DocumentVersionProjection, toErr error) {
				if fromErr != nil || toErr != nil || !fromVersion.Readable || !toVersion.Readable {
					read.Set(docsCompareReadState{Failed: true})
					return
				}
				read.Set(docsCompareReadState{Loaded: true, From: fromVersion, To: toVersion})
			})
		})
	})

	text := func(key string) string { return docsText(locale, key) }
	fromInput := ui.UseEvent(func(event ui.InputEvent) { fromID.Set(event.GetValue()) })
	toInput := ui.UseEvent(func(event ui.InputEvent) { toID.Set(event.GetValue()) })

	picker := func(id, label, selected string) ui.Node {
		selectable := docsCompareSelectable(current.Versions)
		// Every option carries its time and its author (r4 D-6): a version
		// is a save, and "Sep 19, 2026" alone neither separates two saves
		// on one day nor says whose edit it was.
		showTitle := docsCompareTitlesDiffer(selectable)
		options := []ui.Node{html.Option(html.Props{Value: "", Disabled: true, Selected: selected == ""}, ui.Text(text("compare_choose")))}
		for _, v := range selectable {
			label := docsCompareOptionLabel(view.Locale, text, v, true, showTitle)
			if author := docsCompareAuthor(view, v.AuthorID); author != "" {
				label += " · " + author
			}
			options = append(options, html.Option(html.Props{Value: v.VersionID, Selected: v.VersionID == selected}, ui.Text(label)))
		}
		onInput := fromInput
		if id == "docs-compare-to" {
			onInput = toInput
		}
		return html.Div(html.Props{Class: "docs-compare-field"},
			html.Label(html.Props{For: id}, ui.Text(label)),
			html.Select(html.Props{ID: id, OnInput: onInput, Disabled: !current.Loaded || len(selectable) == 0}, options...),
		)
	}

	body := []ui.Node{
		html.Form(html.Props{Class: "docs-compare-form", OnSubmit: submit},
			picker("docs-compare-from", text("compare_from"), fromID.Get()),
			picker("docs-compare-to", text("compare_to"), toID.Get()),
			html.Button(html.Props{Class: "button primary", Type: "submit", Disabled: !current.Loaded || fromID.Get() == "" || toID.Get() == "" || fromID.Get() == toID.Get() || read.Get().Loading}, ui.Text(text("compare_action"))),
		),
	}
	// A document with one version has nothing to compare: two pickers, one
	// of them empty, read as broken. Say so instead (D-3).
	if current.Loaded && len(docsCompareSelectable(current.Versions)) < 2 {
		body = []ui.Node{html.P(html.Props{Class: "docs-compare-status docs-compare-single", Raw: map[string]any{"role": "status"}}, ui.Text(text("compare_single")))}
	}
	switch {
	case current.Loading:
		body = append(body, html.P(html.Props{Class: "docs-compare-status", Raw: map[string]any{"role": "status", "aria-live": "polite"}}, ui.Text(text("compare_versions_loading"))))
	case current.Failed:
		body = append(body, html.Div(html.Props{Class: "docs-compare-retry"},
			html.P(html.Props{Class: "docs-compare-status is-alert", Raw: map[string]any{"role": "alert"}}, ui.Text(text("compare_versions_failed"))),
			html.Button(html.Props{Class: "button secondary", Type: "button", OnClick: retry}, ui.Text(text("compare_versions_retry"))),
		))
	}
	readState := read.Get()
	switch {
	case readState.Loading:
		body = append(body, html.P(html.Props{Class: "docs-compare-status", Raw: map[string]any{"role": "status", "aria-live": "polite"}}, ui.Text(text("compare_loading"))))
	case readState.Failed:
		body = append(body, html.P(html.Props{Class: "docs-compare-status is-alert", Raw: map[string]any{"role": "alert"}}, ui.Text(text("compare_failed"))))
	case readState.Loaded:
		body = append(body, html.Div(html.Props{Class: "docs-compare", Aria: map[string]string{"label": text("compare_heading")}},
			docsCompareColumn(props.Locale, text("compare_from"), readState.From),
			docsCompareColumn(props.Locale, text("compare_to"), readState.To),
		))
	}
	return docsDialog("docs-compare-dialog", text("compare_heading"), text("compare_close"), close, keydown, body...)
}

// docsCompareSelectable is the version list filtered to entries a picker
// can meaningfully offer: a redacted entry carries no title or date, so it
// is left out rather than shown as a blank, unexplained row.
func docsCompareSelectable(versions []DocumentVersionSummary) []DocumentVersionSummary {
	out := make([]DocumentVersionSummary, 0, len(versions))
	for _, v := range versions {
		if v.Redacted {
			continue
		}
		out = append(out, v)
	}
	return out
}

// docsCompareTitlesDiffer reports whether the versions carry more than one
// title. Every version is of the same document, so the title is usually
// identical on every option and only makes the pickers overflow a phone
// sheet ("Sep 19, 2026 — Lactation accommodation poli…", D-1); it is shown
// only when a rename makes it tell the versions apart.
func docsCompareTitlesDiffer(versions []DocumentVersionSummary) bool {
	for _, v := range versions {
		if v.Title != versions[0].Title {
			return true
		}
	}
	return false
}

// docsCompareOptionLabel renders one picker option: the localized date,
// the title when showTitle (the versions were renamed), and — marking the
// current version — an explicit suffix so the default choice reads as
// intentional, not arbitrary.
func docsCompareOptionLabel(locale LocaleContext, text func(string) string, v DocumentVersionSummary, includeTime, showTitle bool) string {
	label := docsCompareDateLabel(locale, v.CreatedAt, includeTime)
	if showTitle && v.Title != "" {
		label += " — " + v.Title
	}
	if v.IsCurrent {
		label += " (" + text("compare_current") + ")"
	}
	return label
}

// docsCompareAuthor names a version's author for the picker: "You" for
// the viewer, the directory name otherwise, and nothing when only a raw
// identifier is known.
func docsCompareAuthor(view View, authorID string) string {
	if strings.TrimSpace(authorID) == "" {
		return ""
	}
	if authorID == docsViewer(view) {
		return docsText(view.Locale.Resolved, "you")
	}
	name := docsOwnerName(view, authorID)
	if name == "" || name == authorID || strings.HasPrefix(name, "hc-") {
		return ""
	}
	return name
}

// docsCompareDateLabel formats a version's timestamp the same way the
// library list does (docsWhen/docsShortDateLabel's locale-aware month-day
// order and digits), but always with its year, since a picker never has
// "today" to omit it against: "Sep 19, 2026" (en-US), "19. Sep. 2026"
// (de-DE), Arabic-Indic day/year with the Arabic month name (ar). Time is
// appended only when the caller says two entries would otherwise share a
// date.
func docsCompareDateLabel(locale LocaleContext, at time.Time, includeTime bool) string {
	zone, zoneErr := time.LoadLocation(locale.normalized().TimeZone)
	if zoneErr != nil {
		zone = time.UTC
	}
	local := at.In(zone)
	resolved := locale.Resolved
	var date string
	switch resolved {
	case "de-DE":
		date = strconv.Itoa(local.Day()) + ". " + docsShortMonthsDE[local.Month()-1] + ". " + strconv.Itoa(local.Year())
	case "ar":
		date = docsLocaleDigits(resolved, strconv.Itoa(local.Day())) + " " + docsMonthsAR[local.Month()-1] + " " + docsLocaleDigits(resolved, strconv.Itoa(local.Year()))
	default:
		date = local.Format("Jan 2, 2006")
	}
	if !includeTime {
		return date
	}
	timeLabel := docsLocaleDigits(resolved, local.Format("15:04"))
	if resolved == "" || resolved == DefaultProductLocale {
		timeLabel = local.Format("3:04 PM")
	}
	return date + ", " + timeLabel
}

// docsCompareColumn renders one immutable version's title and Markdown.
// Nothing here is editable: this is the reader's plain-text view
// (docsASTMarkdownNodes), the same renderer the open document uses.
func docsCompareColumn(locale, heading string, version DocumentVersionProjection) ui.Node {
	view := View{Locale: ResolveProductLocale(locale)}
	return html.Section(html.Props{Class: "docs-compare-side", Aria: map[string]string{"label": heading + ": " + version.Title}},
		html.H3(html.Props{Class: "docs-compare-side-head"}, ui.Text(heading), html.Span(html.Props{Class: "docs-compare-side-title"}, ui.Text(" — "+version.Title))),
		html.Div(html.Props{Class: "docs-markdown docs-compare-body", Dir: docsContentDirection(version.Markdown)}, docsASTMarkdownNodes(view, version.Markdown)...),
	)
}

// docsCompareStylesheet lays the two versions side by side above 40rem and
// stacks them below it, so the compare view stays legible at desktop and
// narrow widths alike (HUB-033).
func docsCompareStylesheet() string {
	return `
/* D-1: a wrapping flex row sized each field to its select's intrinsic
   width (the longest option), so on a phone sheet both pickers ran past
   the edge and on desktop they sat at 480px with the button aligned to
   "To" only. A grid gives From and To equal shares and puts the button on
   the same baseline; the form stacks when its container is narrow. */
#docs-compare-dialog .docs-dialog-body{container-type:inline-size;grid-template-columns:minmax(0,1fr)}
.docs-compare-form{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr) auto;gap:var(--hcm-space-2);align-items:end;margin-block-end:var(--hcm-space-2)}
.docs-compare-field{display:flex;flex-direction:column;gap:var(--hcm-space-1);min-width:0}
.docs-compare-field label{font-weight:600}
.docs-compare-field select{width:100%;max-width:100%;min-width:0;text-overflow:ellipsis;min-height:var(--hcm-control-height);padding:var(--hcm-space-1) var(--hcm-space-2);font:inherit;color:var(--ink);background:var(--surface);border:1px solid var(--control-border);border-radius:var(--hcm-radius-control)}
.docs-compare-status{margin:var(--hcm-space-2) 0}
.docs-compare-status.is-alert{color:var(--hcm-color-danger,#7a1f1f)}
.docs-compare-retry{display:flex;flex-wrap:wrap;align-items:center;gap:var(--hcm-space-1) var(--hcm-space-2);margin:var(--hcm-space-2) 0}
.docs-compare-retry .docs-compare-status{margin:0;flex:1 1 14rem}
.docs-compare{display:grid;grid-template-columns:1fr 1fr;gap:var(--hcm-space-3)}
.docs-compare-side{min-width:0;padding:var(--hcm-space-2);border:1px solid var(--line);border-radius:var(--hcm-radius-control)}
.docs-compare-side-head{margin:0 0 var(--hcm-space-2);font-size:var(--hcm-font-size-body)}
.docs-compare-side-title{font-weight:400;color:var(--muted)}
.docs-compare-body{max-height:32rem;overflow:auto;overflow-wrap:break-word}
/* Two columns inside a 34rem dialog were ~15rem wide, and the reader's
   1.5rem heading broke mid-word ("accommodati / on"). The compare dialog is
   wide, and headings inside a column step down a size (D-3). */
#docs-compare-dialog{width:min(64rem,100%)}
.docs-compare-body :is(h2,h3,h4){overflow-wrap:normal;hyphens:auto}
.docs-compare-body h2{font-size:1.25rem}
.docs-compare-body h3{font-size:1.0625rem;margin-block:1.25rem .35rem}
@media (max-width:40rem){.docs-compare{grid-template-columns:1fr}}
@media (max-width:40rem){.docs-compare-form{grid-template-columns:minmax(0,1fr)}.docs-compare-form>.button{justify-self:start}}
@container (max-width:36rem){.docs-compare-form{grid-template-columns:minmax(0,1fr)}.docs-compare-form>.button{justify-self:start}}
`
}
