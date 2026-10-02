package productui

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
)

// AgentDocumentPickerState describes the visible state of the reusable hub
// document picker. Browser adapters own the asynchronous search and publish
// one of these states; the component never performs I/O while rendering.
type AgentDocumentPickerState string

const (
	AgentDocumentPickerIdle    AgentDocumentPickerState = "idle"
	AgentDocumentPickerLoading AgentDocumentPickerState = "loading"
	AgentDocumentPickerReady   AgentDocumentPickerState = "ready"
	AgentDocumentPickerEmpty   AgentDocumentPickerState = "empty"
	AgentDocumentPickerFailed  AgentDocumentPickerState = "failed"
	AgentDocumentPickerLimit   AgentDocumentPickerState = "limit"
)

// AgentDocumentSuggestion is an authorized documentation-hub search result.
// PublishedVersion is the one-based current published version at search time.
type AgentDocumentSuggestion struct {
	DocumentID       string
	Title            string
	Location         string
	Updated          string
	PublishedVersion uint64
}

// AgentDocumentReferencePickerModel is shared by the persona editor and the
// Agents composer. Persona references use the default PINNED mode; a request
// picker can set DefaultVersionMode to LATEST_PUBLISHED.
type AgentDocumentReferencePickerModel struct {
	ID                 string
	Available          bool
	State              AgentDocumentPickerState
	DefaultVersionMode string
	ReferenceLimit     int
	LockedVersionMode  bool
	References         []PersonaAdminDocumentReference
	Suggestions        []AgentDocumentSuggestion
	ErrorDocument      string
}

// AgentDocumentReferencePicker renders a progressively enhanced combobox.
// The WASM adapter debounces searches, updates the listbox and serializes the
// selected rows. Existing unreadable references remain removable without
// exposing their document title or identifier as visible text.
func AgentDocumentReferencePicker(locale LocaleContext, model AgentDocumentReferencePickerModel) ui.Node {
	id := safeAgentDOMToken(model.ID)
	if id == "unknown" {
		id = "agent-document-picker"
	}
	if !model.Available {
		return html.Div(html.Props{Class: "agentdoc-picker-unavailable", Raw: map[string]any{"data-agentdoc-picker": id, "data-agentdoc-available": "false"}},
			html.H5(html.Props{}, ui.Text(agentDocumentPickerText(locale, "field_label"))),
			html.P(html.Props{Class: "muted", Role: "status"}, ui.Text(agentDocumentPickerText(locale, "service_unavailable"))),
		)
	}
	state := model.State
	if state == "" {
		state = AgentDocumentPickerIdle
	}
	limit := model.ReferenceLimit
	if limit <= 0 {
		limit = agentdocref.MaxPersonaReferences
	}
	if len(model.References) >= limit {
		state = AgentDocumentPickerLimit
	}
	mode := model.DefaultVersionMode
	if mode == "" {
		mode = string(agentdocref.ModePinned)
	}
	listID := id + "-results"
	statusID := id + "-status"
	rows := make([]ui.Node, 0, len(model.References))
	for index, ref := range model.References {
		rows = append(rows, agentDocumentReferenceRow(locale, id, index, ref, model.LockedVersionMode))
	}
	results := make([]ui.Node, 0, len(model.Suggestions))
	for index, suggestion := range model.Suggestions {
		results = append(results, html.Li(html.Props{ID: id + "-option-" + strconv.Itoa(index), Class: "agentdoc-picker-option", Role: "option", Raw: map[string]any{
			"data-agentdoc-option": suggestion.DocumentID, "data-agentdoc-title": suggestion.Title, "data-agentdoc-location": suggestion.Location,
			"data-agentdoc-updated": suggestion.Updated, "data-agentdoc-version": suggestion.PublishedVersion, "aria-selected": "false",
		}}, html.Strong(html.Props{Dir: "auto"}, ui.Text(suggestion.Title)), html.Small(html.Props{Class: "muted"}, ui.Text(agentDocumentSuggestionDetail(suggestion)))))
	}
	status := agentDocumentPickerStateText(locale, state, model.ErrorDocument)
	disabled := state == AgentDocumentPickerLimit
	combobox := ui.Node(html.Div(html.Props{Class: "agentdoc-picker-combobox"},
		html.Label(html.Props{For: id + "-search"}, ui.Text(agentDocumentPickerText(locale, "add_label"))),
		html.Input(html.Props{ID: id + "-search", Type: "search", Disabled: disabled, Placeholder: agentDocumentPickerText(locale, "search_placeholder"), Raw: map[string]any{
			"role": "combobox", "autocomplete": "off", "aria-autocomplete": "list", "aria-controls": listID, "aria-expanded": strconv.FormatBool(len(results) > 0),
			"aria-describedby": statusID, "data-agentdoc-search": "true",
		}}),
		html.Ul(html.Props{ID: listID, Class: "agentdoc-picker-results", Role: "listbox", Hidden: len(results) == 0, Raw: map[string]any{"data-agentdoc-results": "true"}}, results...),
	))
	stateClass := "agentdoc-picker-state"
	stateRole := "status"
	if state == AgentDocumentPickerFailed {
		combobox = nil
		stateClass += " agentdoc-picker-error"
		stateRole = "alert"
	}
	return html.Div(html.Props{Class: "agentdoc-picker", Raw: map[string]any{
		"data-agentdoc-picker": id, "data-agentdoc-available": "true", "data-agentdoc-state": string(state), "data-agentdoc-default-mode": mode,
		"data-agentdoc-limit-value": limit, "data-agentdoc-locked-mode": strconv.FormatBool(model.LockedVersionMode),
		"data-agentdoc-loading": agentDocumentPickerText(locale, "loading"), "data-agentdoc-empty": agentDocumentPickerText(locale, "empty"),
		"data-agentdoc-failed": agentDocumentPickerText(locale, "failed"), "data-agentdoc-idle": agentDocumentPickerText(locale, "idle"),
		"data-agentdoc-limit": agentDocumentPickerLimitText(locale, limit), "data-agentdoc-retry": agentDocumentPickerText(locale, "retry"),
		"data-agentdoc-remove": agentDocumentPickerText(locale, "remove"), "data-agentdoc-pinned": agentDocumentPickerText(locale, "pinned"),
		"data-agentdoc-latest": agentDocumentPickerText(locale, "latest"), "data-agentdoc-unreadable": agentDocumentPickerText(locale, "unreadable"),
		"data-agentdoc-document-unreadable": agentDocumentPickerText(locale, "document_unreadable"),
		"data-agentdoc-count-template":      agentDocumentPickerText(locale, "count"),
	}},
		html.H5(html.Props{}, ui.Text(agentDocumentPickerText(locale, "field_label"))),
		combobox,
		html.Div(html.Props{Class: stateClass},
			html.P(html.Props{ID: statusID, Role: stateRole, Raw: map[string]any{"aria-live": "polite", "data-agentdoc-status": "true"}}, ui.Text(status)),
			agentDocumentPickerRetry(locale, state),
		),
		html.Ul(html.Props{Class: "agentdoc-picker-selected", Raw: map[string]any{"data-agentdoc-selected": "true"}}, rows...),
		html.Div(html.Props{Class: "agentdoc-picker-footer"},
			html.Span(html.Props{Class: "muted", Hidden: len(model.References) == 0, Raw: map[string]any{"data-agentdoc-count": "true"}}, ui.Text(agentDocumentPickerCount(locale, len(model.References), limit))),
			html.P(html.Props{Class: "muted"}, ui.Text(agentDocumentPickerText(locale, "access_help"))),
		),
	)
}

func agentDocumentReferenceRow(locale LocaleContext, pickerID string, index int, ref PersonaAdminDocumentReference, lockedMode bool) ui.Node {
	title := strings.TrimSpace(ref.Title)
	if title == "" {
		title = strings.TrimSpace(ref.Label)
	}
	rowID := pickerID + "-selected-" + strconv.Itoa(index)
	copyChildren := []ui.Node{html.Strong(html.Props{Dir: "auto"}, ui.Text(title))}
	if location := strings.TrimSpace(ref.Location); location != "" {
		copyChildren = append(copyChildren, html.Small(html.Props{Class: "muted"}, ui.Text(location)))
	}
	if !ref.Readable {
		copyChildren = append(copyChildren, html.Small(html.Props{Class: "muted"}, ui.Text(agentDocumentPickerText(locale, "unreadable"))))
		return html.Li(html.Props{ID: rowID, Class: "agentdoc-picker-row is-unreadable", Raw: agentDocumentReferenceData(ref)},
			html.Div(html.Props{Class: "agentdoc-picker-row-copy"}, copyChildren...),
			html.Button(html.Props{Class: "button secondary compact", Type: "button", Raw: map[string]any{"data-agentdoc-remove": "true", "aria-label": agentDocumentPickerText(locale, "remove") + " " + title}}, ui.Text(agentDocumentPickerText(locale, "remove"))),
		)
	}
	selectedPinned := ref.VersionMode != string(agentdocref.ModeLatestPublished)
	pinnedVersion := locale.FormatNumber(strconv.FormatUint(ref.PinnedVersion, 10), 0)
	versionControl := ui.Node(html.Select(html.Props{ID: rowID + "-mode", Raw: map[string]any{"data-agentdoc-mode": "true"}},
		html.Option(html.Props{Value: string(agentdocref.ModePinned), Selected: selectedPinned}, ui.Text(strings.ReplaceAll(agentDocumentPickerText(locale, "pinned"), "{version}", pinnedVersion))),
		html.Option(html.Props{Value: string(agentdocref.ModeLatestPublished), Selected: !selectedPinned}, ui.Text(agentDocumentPickerText(locale, "latest"))),
	))
	if lockedMode {
		label := agentDocumentPickerText(locale, "latest")
		if selectedPinned {
			label = strings.ReplaceAll(agentDocumentPickerText(locale, "pinned"), "{version}", pinnedVersion)
		}
		versionControl = html.Span(html.Props{Class: "muted", Raw: map[string]any{"data-agentdoc-mode-locked": "true"}}, ui.Text(label))
	}
	versionLabel := ui.Node(html.Label(html.Props{Class: "sr-only", For: rowID + "-mode"}, ui.Text(agentDocumentPickerText(locale, "version_choice")+" "+title)))
	if lockedMode {
		versionLabel = nil
	}
	return html.Li(html.Props{ID: rowID, Class: "agentdoc-picker-row", Raw: agentDocumentReferenceData(ref)},
		html.Div(html.Props{Class: "agentdoc-picker-row-copy"}, append([]ui.Node{html.A(html.Props{Href: agentDocumentHubHref(ref.DocumentID), Target: "_blank", Raw: map[string]any{"rel": "noopener noreferrer"}}, ui.Text(title))}, copyChildren[1:]...)...),
		versionLabel,
		versionControl,
		html.Button(html.Props{Class: "button secondary compact", Type: "button", Raw: map[string]any{"data-agentdoc-remove": "true", "aria-label": agentDocumentPickerText(locale, "remove") + " " + title}}, ui.Text(agentDocumentPickerText(locale, "remove"))),
	)
}

func agentDocumentReferenceData(ref PersonaAdminDocumentReference) map[string]any {
	return map[string]any{"data-agentdoc-reference": "true", "data-document-id": ref.DocumentID, "data-document-label": ref.Label, "data-document-title": ref.Title, "data-document-location": ref.Location, "data-version-mode": ref.VersionMode, "data-pinned-version": ref.PinnedVersion, "data-section-anchor": ref.SectionAnchor, "data-readable": strconv.FormatBool(ref.Readable)}
}

func agentDocumentPickerRetry(locale LocaleContext, state AgentDocumentPickerState) ui.Node {
	return html.Button(html.Props{Class: "button secondary compact", Type: "button", Hidden: state != AgentDocumentPickerFailed, Raw: map[string]any{"data-agentdoc-retry": "true"}}, ui.Text(agentDocumentPickerText(locale, "retry")))
}

func agentDocumentSuggestionDetail(suggestion AgentDocumentSuggestion) string {
	parts := make([]string, 0, 2)
	if value := strings.TrimSpace(suggestion.Location); value != "" {
		parts = append(parts, value)
	}
	if value := strings.TrimSpace(suggestion.Updated); value != "" {
		parts = append(parts, value)
	}
	return strings.Join(parts, " · ")
}

func agentDocumentHubHref(documentID string) string {
	return "/workspace/app/docs?document=" + url.QueryEscape(documentID)
}

func agentDocumentPickerCount(locale LocaleContext, count, limit int) string {
	return strings.NewReplacer("{count}", locale.FormatNumber(strconv.Itoa(count), 0), "{limit}", locale.FormatNumber(strconv.Itoa(limit), 0)).Replace(agentDocumentPickerText(locale, "count"))
}

func agentDocumentPickerLimitText(locale LocaleContext, limit int) string {
	return strings.ReplaceAll(agentDocumentPickerText(locale, "limit"), "{limit}", locale.FormatNumber(strconv.Itoa(limit), 0))
}

func agentDocumentPickerStateText(locale LocaleContext, state AgentDocumentPickerState, document string) string {
	if state == AgentDocumentPickerFailed && strings.TrimSpace(document) != "" {
		return strings.ReplaceAll(agentDocumentPickerText(locale, "document_unreadable"), "{document}", document)
	}
	return agentDocumentPickerText(locale, string(state))
}

func agentDocumentPickerText(locale LocaleContext, key string) string {
	copy := map[string][3]string{
		"field_label":         {"Documents this agent reads", "Dokumente, die dieser Agent liest", "المستندات التي يقرأها هذا الوكيل"},
		"add_label":           {"Add a document", "Dokument hinzufügen", "إضافة مستند"},
		"search_placeholder":  {"Search by title", "Nach Titel suchen", "البحث حسب العنوان"},
		"idle":                {"Search by title to add a document.", "Suchen Sie nach dem Titel, um ein Dokument hinzuzufügen.", "ابحث بالعنوان لإضافة مستند."},
		"loading":             {"Searching documents…", "Dokumente werden gesucht…", "جارٍ البحث في المستندات…"},
		"empty":               {"No documents match", "Keine passenden Dokumente", "لا توجد مستندات مطابقة"},
		"failed":              {"Couldn't load documents.", "Dokumente konnten nicht geladen werden.", "تعذر تحميل المستندات."},
		"limit":               {"You can add up to {limit} documents.", "Sie können bis zu {limit} Dokumente hinzufügen.", "يمكنك إضافة ما يصل إلى {limit} مستندات."},
		"retry":               {"Try again", "Erneut versuchen", "حاول مرة أخرى"},
		"remove":              {"Remove", "Entfernen", "إزالة"},
		"pinned":              {"Pinned to version {version}", "An Version {version} angeheftet", "مثبّت بالإصدار {version}"},
		"latest":              {"Always latest published", "Immer die neueste veröffentlichte Version", "أحدث إصدار منشور دائماً"},
		"version_choice":      {"Version choice for", "Versionsauswahl für", "اختيار الإصدار لـ"},
		"unreadable":          {"Cannot be read by you", "Für Sie nicht lesbar", "لا يمكنك قراءته"},
		"count":               {"{count} of {limit} documents", "{count} von {limit} Dokumenten", "{count} من {limit} مستندات"},
		"access_help":         {"Each person only gets content from documents they can already read.", "Jede Person erhält nur Inhalte aus Dokumenten, die sie bereits lesen kann.", "يحصل كل شخص فقط على محتوى من المستندات التي يمكنه قراءتها بالفعل."},
		"service_unavailable": {"Reference documents are unavailable because the documentation hub is not set up. You can still create this version.", "Referenzdokumente sind nicht verfügbar, weil die Dokumentation noch nicht eingerichtet ist. Sie können diese Version trotzdem erstellen.", "المستندات المرجعية غير متاحة لأن مركز التوثيق غير مُعدّ. لا يزال بإمكانك إنشاء هذا الإصدار."},
		"document_unreadable": {"You cannot read this document: {document}", "Sie können dieses Dokument nicht lesen: {document}", "لا يمكنك قراءة هذا المستند: {document}"},
	}
	values, ok := copy[key]
	if !ok {
		return key
	}
	index := 0
	switch locale.Resolved {
	case "de-DE":
		index = 1
	case "ar":
		index = 2
	}
	return values[index]
}
