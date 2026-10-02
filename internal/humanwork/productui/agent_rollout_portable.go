package productui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AgentRolloutSnapshot is the server-owned projection used by Agent Studio.
// IDs are opaque and are always posted back verbatim; labels never carry
// authority.
type AgentRolloutSnapshot struct {
	Loading        bool
	Available      bool
	CanPreview     bool
	CanApprove     bool
	CanPromote     bool
	CanAdvance     bool
	PersonaID      string
	TargetVersion  int64
	Personas       []AgentRolloutPersona
	Versions       []AgentRolloutVersion
	Installations  []AgentRolloutInstallation
	NotListedCount int
	WaitingVersion int64
	WaitingState   string
	WaitingName    string
	Active         *AgentRolloutPlan
	Progress       *AgentRolloutProgress
	Portable       AgentPortableSnapshot
}

type AgentRolloutPersona struct{ ID, Name string }
type AgentRolloutVersion struct {
	PersonaID             string
	Version               int64
	PublishedAt           string
	Current               bool
	Digest, ProfileDigest string
}
type AgentRolloutInstallation struct {
	ID, PersonaID, ConversationID, Name, Status, ConversationKind string
	Version, Revision                                             int64
	MemberCount                                                   uint32
	Visible                                                       bool
	UpdateBlocked                                                 bool
	CanaryEligible                                                bool
}
type AgentRolloutCandidate struct {
	InstallationID, ConversationID, PolicyDigest          string
	Version, Revision, RevocationEpoch, AuthorityRevision int64
}
type AgentRolloutPlan struct {
	ID, Digest, ProfileDigest string
	Version                   int64
	Candidates                []AgentRolloutCandidate
	CanaryCount               int
}
type AgentRolloutProgress struct {
	Revision          int64
	Cursor            int64
	Stage, ApproverID string
}

// AgentPortableSnapshot contains only manifests the current principal may
// export/import. The payload itself is supplied by the API after verification.
type AgentPortableSnapshot struct {
	Available    bool                       `json:"available"`
	CanExport    bool                       `json:"can_export"`
	CanImport    bool                       `json:"can_import"`
	Manifests    []AgentPortableManifest    `json:"manifests"`
	Destinations []AgentPortableDestination `json:"destinations"`
	Definition   string                     `json:"definition,omitempty"`
	Draft        *AgentPortableReviewDraft  `json:"draft,omitempty"`
	Drafts       []AgentPortableReviewDraft `json:"drafts"`
}
type AgentPortableReviewDraft struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Purpose      string `json:"purpose"`
	Instructions string `json:"instructions,omitempty"`
	ImportedAt   string `json:"imported_at"`
	ImportedBy   string `json:"imported_by,omitempty"`
	State        string `json:"state"`
	Version      uint64 `json:"version"`
}
type AgentPortableManifest struct {
	ID             string `json:"id"`
	Version        string `json:"version"`
	Name           string `json:"name"`
	Digest         string `json:"digest"`
	Live           bool   `json:"live"`
	PersonaVersion string `json:"persona_version,omitempty"`
}
type AgentPortableDestination struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Kind     string `json:"kind"`
	SourceID string `json:"source_id"`
}

type AgentPortableMapping struct{ Kind, SourceID, DestinationID string }

func CompletePortableMappings(destinations []AgentPortableDestination, mappings []AgentPortableMapping) bool {
	if len(destinations) == 0 || len(destinations) != len(mappings) {
		return false
	}
	seen := make(map[string]bool, len(mappings))
	for _, mapping := range mappings {
		if mapping.Kind == "" || mapping.SourceID == "" || mapping.DestinationID == "" {
			return false
		}
		key := mapping.Kind + "\x00" + mapping.SourceID
		if seen[key] {
			return false
		}
		seen[key] = true
	}
	for _, destination := range destinations {
		if !seen[destination.Kind+"\x00"+destination.SourceID] {
			return false
		}
	}
	return true
}

func AgentRolloutPortableMount(locale LocaleContext, rollout AgentRolloutSnapshot, portable AgentPortableSnapshot) ui.Node {
	return AgentRolloutPortableMountForTab(locale, rollout, portable, "running")
}

func AgentRolloutPortableMountForTab(locale LocaleContext, rollout AgentRolloutSnapshot, portable AgentPortableSnapshot, selectedTab string) ui.Node {
	if rollout.Loading {
		return html.Div(html.Props{ID: "agent-rollout-portable", Dir: string(locale.Direction), Raw: map[string]any{"data-locale": locale.Resolved}},
			html.Section(html.Props{ID: "agent-rollout-panel", Class: "persona-admin-editor agent-operations-region", Hidden: selectedTab != "rollout", Raw: map[string]any{"role": "tabpanel", "data-agent-operations-panel": "rollout"}, Aria: map[string]string{"labelledby": "agent-operations-tab-rollout", "busy": "true"}},
				html.H2(html.Props{ID: "agent-rollout-title"}, ui.Text(agentRPText(locale, "rollout"))),
				html.P(html.Props{Class: "muted"}, ui.Text(agentRPText(locale, "rollout_help"))),
				html.P(html.Props{ID: "agent-rollout-status", Role: "status", Aria: map[string]string{"live": "polite", "atomic": "true"}}, ui.Text(AgentRolloutPortableStatus(locale, "loading"))),
				html.Button(html.Props{Type: "button", Class: "button secondary", Aria: map[string]string{"description": agentRPText(locale, "refresh_help")}, Raw: map[string]any{"data-rollout-action": "REFRESH", "data-busy-label": agentRPText(locale, "working_short")}}, ui.Text(agentRPText(locale, "refresh_versions")))),
			html.Section(html.Props{ID: "agent-portable-panel", Class: "persona-admin-editor agent-operations-region", Hidden: selectedTab != "move", Raw: map[string]any{"role": "tabpanel", "data-agent-operations-panel": "move"}, Aria: map[string]string{"labelledby": "agent-operations-tab-move", "busy": "true"}},
				html.H2(html.Props{ID: "agent-portable-title"}, ui.Text(agentRPText(locale, "portable"))),
				html.P(html.Props{Class: "muted"}, ui.Text(agentRPText(locale, "portable_help"))),
				html.P(html.Props{ID: "agent-portable-status", Role: "status", Aria: map[string]string{"live": "polite", "atomic": "true"}}, ui.Text(AgentRolloutPortableStatus(locale, "loading")))),
		)
	}
	return html.Div(html.Props{ID: "agent-rollout-portable", Dir: string(locale.Direction), Raw: map[string]any{"data-locale": locale.Resolved, "data-rollout-available": rollout.Available, "data-portable-available": portable.Available}},
		agentRolloutPanelForTab(locale, rollout, selectedTab),
		agentPortablePanelForTab(locale, portable, selectedTab),
	)
}

func AgentRolloutPortableStatus(locale LocaleContext, key string) string {
	if key == "canary_empty" {
		return [3]string{"At least one conversation is updated first.", "Mindestens eine Unterhaltung wird zuerst aktualisiert.", "يتم تحديث محادثة واحدة على الأقل أولاً."}[agentRPLocaleIndex(locale)]
	}
	values := map[string][3]string{
		"loading":           {"Loading authorized versions and controls…", "Berechtigte Versionen und Aktionen werden geladen…", "جارٍ تحميل الإصدارات والإجراءات المصرح بها…"},
		"working":           {"Applying the selected action…", "Die ausgewählte Aktion wird ausgeführt…", "جارٍ تطبيق الإجراء المحدد…"},
		"refused":           {"The action was refused. Review your current access and try again.", "Die Aktion wurde abgelehnt. Prüfen Sie Ihren aktuellen Zugriff und versuchen Sie es erneut.", "تم رفض الإجراء. راجع صلاحياتك الحالية وحاول مجددًا."},
		"stale_preview":     {"Stale preview. Refresh versions and preview the rollout again.", "Veraltete Vorschau. Aktualisieren Sie die Versionen und zeigen Sie den Rollout erneut in der Vorschau an.", "المعاينة قديمة. حدّث الإصدارات وعاين الطرح مرة أخرى."},
		"not_allowed":       {"Not allowed. Ask an owner or administrator to check your rollout access.", "Nicht erlaubt. Lassen Sie Ihre Rollout-Berechtigung von einer verantwortlichen Person oder Administration prüfen.", "غير مسموح. اطلب من مالك أو مسؤول التحقق من صلاحية الطرح."},
		"evidence_missing":  {"Evidence missing. Publish the reviewed version in Setup, then try again.", "Nachweis fehlt. Veröffentlichen Sie die geprüfte Version in der Einrichtung und versuchen Sie es erneut.", "الدليل مفقود. انشر الإصدار المراجع في الإعداد ثم حاول مرة أخرى."},
		"unavailable":       {"Unavailable. Refresh and try again; if it keeps failing, ask the technical contact to check the service.", "Nicht verfügbar. Aktualisieren Sie und versuchen Sie es erneut; wenn es weiter fehlschlägt, lassen Sie den Dienst vom technischen Kontakt prüfen.", "غير متاح. حدّث وحاول مرة أخرى؛ إذا استمر الفشل فاطلب من جهة الاتصال التقنية فحص الخدمة."},
		"invalid":           {"Choose a valid portable agent file.", "Wählen Sie eine gültige portable Agentendatei.", "اختر ملف وكيل محمولاً صالحًا."},
		"mapping_required":  {"Choose a destination for every portable reference before importing.", "Wählen Sie vor dem Import ein Ziel für jede Referenz.", "اختر وجهة لكل مرجع محمول قبل الاستيراد."},
		"exported":          {"Agent file downloaded. You can also copy it for another workspace.", "Agentendatei heruntergeladen. Sie können sie auch für einen anderen Arbeitsbereich kopieren.", "تم تنزيل ملف الوكيل. ويمكنك أيضًا نسخه لمساحة عمل أخرى."},
		"copied":            {"Agent file copied.", "Agentendatei kopiert.", "تم نسخ ملف الوكيل."},
		"copy_failed":       {"The agent file could not be copied. Download it instead.", "Die Agentendatei konnte nicht kopiert werden. Laden Sie sie stattdessen herunter.", "تعذر نسخ ملف الوكيل. نزّله بدلاً من ذلك."},
		"imported":          {"Agent imported as a reviewable draft.", "Agent als prüfbarer Entwurf importiert.", "تم استيراد الوكيل كمسودة قابلة للمراجعة."},
		"draft_loaded":      {"Imported draft loaded for review.", "Importierter Entwurf zur Prüfung geladen.", "تم تحميل المسودة المستوردة للمراجعة."},
		"rollout_previewed": {"Rollout preview created. Review it before approving any change.", "Rollout-Vorschau erstellt. Prüfen Sie sie, bevor Sie eine Änderung genehmigen.", "تم إنشاء معاينة الطرح. راجعها قبل الموافقة على أي تغيير."},
		"rollout_updated":   {"The rollout action was completed and the current state is shown below.", "Die Rollout-Aktion wurde abgeschlossen; der aktuelle Status wird unten angezeigt.", "اكتمل إجراء الطرح وتظهر الحالة الحالية أدناه."},
	}
	index := 0
	if locale.Resolved == "de-DE" {
		index = 1
	}
	if locale.Resolved == "ar" {
		index = 2
	}
	return values[key][index]
}

func agentRolloutPanel(locale LocaleContext, snapshot AgentRolloutSnapshot) ui.Node {
	return agentRolloutPanelForTab(locale, snapshot, "running")
}

func agentRolloutPanelForTab(locale LocaleContext, snapshot AgentRolloutSnapshot, selectedTab string) ui.Node {
	ready := len(snapshot.Personas) > 0 && len(snapshot.Versions) > 0 && len(snapshot.Installations) > 0
	status := "ready"
	if !snapshot.Available {
		status = "unavailable"
	} else if snapshot.CanPreview && !ready {
		status = "setup_required"
	}
	header := html.Div(html.Props{Class: "agent-operations-region-header"},
		html.Div(html.Props{}, html.H2(html.Props{ID: "agent-rollout-title"}, ui.Text(agentRPText(locale, "rollout"))), html.P(html.Props{Class: "muted"}, ui.Text(agentRPText(locale, "rollout_help")))),
	)
	children := []ui.Node{header}
	if status != "ready" {
		children = append(children, html.P(html.Props{ID: "agent-rollout-status", Role: "status", Aria: map[string]string{"live": "polite", "atomic": "true"}}, ui.Text(agentRPText(locale, status))))
	}
	if !snapshot.Available {
		return html.Section(html.Props{ID: "agent-rollout-panel", Class: "persona-admin-editor agent-operations-region", Hidden: selectedTab != "rollout", Raw: map[string]any{"role": "tabpanel", "data-agent-operations-panel": "rollout"}, Aria: map[string]string{"labelledby": "agent-operations-tab-rollout"}}, children...)
	}
	header = html.Div(html.Props{Class: "agent-operations-region-header"},
		html.Div(html.Props{}, html.H2(html.Props{ID: "agent-rollout-title"}, ui.Text(agentRPText(locale, "rollout"))), html.P(html.Props{Class: "muted"}, ui.Text(agentRPText(locale, "rollout_help")))),
		html.Button(html.Props{Type: "button", Class: "button secondary", Aria: map[string]string{"description": agentRPText(locale, "refresh_help")}, Raw: map[string]any{"data-rollout-action": "REFRESH", "data-busy-label": agentRPText(locale, "working_short")}}, ui.Text(agentRPText(locale, "refresh_versions"))),
	)
	children[0] = header
	if status == "setup_required" {
		children = append(children, html.P(html.Props{}, html.A(html.Props{Href: agentSetupHref(locale)}, ui.Text(agentRPText(locale, "setup_link"))), ui.Text(agentRPText(locale, "setup_suffix"))))
	}
	if snapshot.WaitingVersion > 0 {
		name := strings.TrimSpace(snapshot.WaitingName)
		if name == "" {
			name = agentRPText(locale, "agent")
		}
		state := agentRPText(locale, "waiting_for_review")
		if snapshot.WaitingState == "EVALUATED" {
			state = agentRPText(locale, "waiting_to_publish")
		}
		message := strings.ReplaceAll(agentRPText(locale, "waiting_version"), "{name}", name)
		message = strings.ReplaceAll(message, "{version}", locale.FormatNumber(fmt.Sprint(snapshot.WaitingVersion), 0))
		message = strings.ReplaceAll(message, "{state}", state)
		children = append(children, html.P(html.Props{Class: "agent-rollout-waiting", Role: "status"}, ui.Text(message+" "), html.A(html.Props{Href: agentSetupHref(locale)}, ui.Text(agentRPText(locale, "setup_link"))), ui.Text(agentRPText(locale, "waiting_suffix"))))
	}
	if snapshot.CanPreview && ready {
		children = append(children, agentRolloutPreviewForm(locale, snapshot))
	}
	if snapshot.Active != nil {
		children = append(children, agentRolloutPlan(locale, *snapshot.Active, snapshot))
	}
	return html.Section(html.Props{ID: "agent-rollout-panel", Class: "persona-admin-editor agent-operations-region", Hidden: selectedTab != "rollout", Raw: map[string]any{"role": "tabpanel", "data-agent-operations-panel": "rollout"}, Aria: map[string]string{"labelledby": "agent-operations-tab-rollout"}}, children...)
}

func agentRolloutPreviewForm(locale LocaleContext, snapshot AgentRolloutSnapshot) ui.Node {
	defaultPersona, defaultVersion := agentRolloutDefaultTarget(snapshot)
	personaOptions := make([]ui.Node, 0, len(snapshot.Personas))
	for _, p := range snapshot.Personas {
		personaOptions = append(personaOptions, html.Option(html.Props{Value: p.ID, Selected: p.ID == defaultPersona}, ui.Text(p.Name)))
	}
	versionOptions := make([]ui.Node, 0, len(snapshot.Versions))
	orderedVersions := append([]AgentRolloutVersion(nil), snapshot.Versions...)
	sort.SliceStable(orderedVersions, func(i, j int) bool { return orderedVersions[i].Version > orderedVersions[j].Version })
	rollbackOptions := []ui.Node{}
	reviewedOptions := []ui.Node{}
	for _, v := range orderedVersions {
		label := strings.ReplaceAll(agentRPText(locale, "version_option"), "{version}", locale.FormatNumber(fmt.Sprint(v.Version), 0))
		if v.Version == agentRolloutCurrentVersion(snapshot, v.PersonaID) {
			label = strings.ReplaceAll(agentRPText(locale, "version_option_current"), "{version}", locale.FormatNumber(fmt.Sprint(v.Version), 0))
		}
		label = strings.ReplaceAll(label, "{date}", agentRolloutFormatDate(locale, v.PublishedAt))
		option := html.Option(html.Props{Hidden: v.PersonaID != defaultPersona, Value: fmt.Sprint(v.Version), Selected: v.PersonaID == defaultPersona && v.Version == defaultVersion, Raw: map[string]any{"data-persona-id": v.PersonaID, "data-digest": v.Digest, "data-profile-digest": v.ProfileDigest, "data-current": v.Current}}, ui.Text(label))
		if v.Version == agentRolloutCurrentVersion(snapshot, v.PersonaID) {
			versionOptions = append(versionOptions, option)
		} else if v.Version < agentRolloutCurrentVersion(snapshot, v.PersonaID) {
			rollbackOptions = append(rollbackOptions, option)
		} else {
			reviewedOptions = append(reviewedOptions, option)
		}
	}
	if len(rollbackOptions) > 0 {
		versionOptions = append(versionOptions, html.Optgroup(html.Props{Raw: map[string]any{"label": agentUXR7Text(locale, "rollback_group")}}, rollbackOptions...))
	}
	if len(reviewedOptions) > 0 {
		versionOptions = append(versionOptions, html.Optgroup(html.Props{Raw: map[string]any{"label": agentUXR7Text(locale, "reviewed_versions")}}, reviewedOptions...))
	}
	installations := make([]ui.Node, 0, len(snapshot.Installations))
	notListed := snapshot.NotListedCount
	for index, installation := range snapshot.Installations {
		if installation.UpdateBlocked {
			notListed++
			continue
		}
		installations = append(installations, html.Label(html.Props{Class: "agent-rollout-choice agent-rollout-installation", Hidden: installation.PersonaID != defaultPersona}, html.Input(html.Props{Type: "checkbox", Name: "installation_ids", Value: installation.ID, Raw: map[string]any{"data-rollout-installation": installation.ID, "data-persona-id": installation.PersonaID}}), html.Span(html.Props{}, agentRolloutConversationNodes(locale, snapshot, installation, index)...)))
	}
	canaries := make([]ui.Node, 0, len(snapshot.Installations))
	for index, installation := range snapshot.Installations {
		if !installation.CanaryEligible || installation.UpdateBlocked {
			continue
		}
		canaries = append(canaries, html.Label(html.Props{Class: "agent-rollout-choice agent-rollout-canary", Hidden: true, Raw: map[string]any{"data-rollout-canary-row": installation.ID}}, html.Input(html.Props{Type: "checkbox", Name: "canary_ids", Value: installation.ID, Disabled: true, Raw: map[string]any{"data-rollout-canary": installation.ID, "data-persona-id": installation.PersonaID}}), html.Span(html.Props{}, agentRolloutConversationNodes(locale, snapshot, installation, index)...)))
	}
	allCurrent := agentRolloutAllCurrent(snapshot)
	submitProps := html.Props{Type: "submit", Class: "button primary", Disabled: allCurrent, Raw: map[string]any{"data-rollout-submit": "PREVIEW", "data-busy-label": agentRPText(locale, "working_short")}}
	fields := []ui.Node{
		html.Div(html.Props{Class: "persona-admin-editor-field"}, html.Label(html.Props{For: "agent-rollout-persona"}, ui.Text(agentRPText(locale, "persona"))), html.Select(html.Props{ID: "agent-rollout-persona", Name: "persona_id"}, personaOptions...)),
		html.Div(html.Props{Class: "persona-admin-editor-field"}, html.Label(html.Props{For: "agent-rollout-version"}, ui.Text(agentRPText(locale, "target_version"))), html.Select(html.Props{ID: "agent-rollout-version", Name: "target_version"}, versionOptions...)),
	}
	if allCurrent {
		fields = append(fields, html.P(html.Props{Class: "agent-rollout-current-note", Role: "status"}, ui.Text(agentUXR7Text(locale, "all_current", "{version}", personaAdminLocalizedNumber(locale, fmt.Sprint(defaultVersion)), "{count}", locale.FormatNumber(fmt.Sprint(agentRolloutPersonaCount(snapshot, defaultPersona)), 0)))))
	} else if current := agentRolloutCurrentVersion(snapshot, defaultPersona); current > defaultVersion {
		fields = append(fields, html.P(html.Props{Class: "agent-rollout-rollback-note", Role: "status"}, ui.Text(agentUXR7Text(locale, "rollback_note", "{current}", personaAdminLocalizedNumber(locale, fmt.Sprint(current)), "{target}", personaAdminLocalizedNumber(locale, fmt.Sprint(defaultVersion))))))
	}
	work := []ui.Node{
		html.P(html.Props{ID: "agent-rollout-preview-help", Class: "muted"}, ui.Text(agentRPText(locale, "preview_help"))),
		html.Div(html.Props{Class: "persona-admin-editor-field agent-rollout-number"}, html.Label(html.Props{For: "agent-rollout-batch"}, ui.Text(agentRPText(locale, "batch_limit"))), html.Input(html.Props{ID: "agent-rollout-batch", Name: "batch_limit", Type: "number", Value: "1", Min: "1"})),
		html.H3(html.Props{ID: "agent-rollout-installations-title", Class: "agent-rollout-group-title"}, ui.Text(agentRPText(locale, "installations"))),
	}
	if notListed > 0 {
		work = append(work, html.P(html.Props{Class: "muted agent-rollout-not-listed"}, ui.Text(strings.ReplaceAll(agentRPText(locale, "private_not_listed"), "{count}", locale.FormatNumber(fmt.Sprint(notListed), 0)))))
	}
	work = append(work,
		html.Div(html.Props{ID: "agent-rollout-installations", Class: "agent-rollout-choice-group", Role: "group", Aria: map[string]string{"labelledby": "agent-rollout-installations-title"}}, installations...),
		html.H3(html.Props{ID: "agent-rollout-canaries-title", Class: "agent-rollout-group-title", Hidden: true, Raw: map[string]any{"data-rollout-canary-container": "true"}}, ui.Text(agentRPText(locale, "canaries"))),
		html.P(html.Props{ID: "agent-rollout-canary-help", Class: "muted", Hidden: true, Raw: map[string]any{"data-rollout-canary-container": "true"}}, ui.Text(agentRPText(locale, "canary_help"))),
		html.P(html.Props{Class: "muted", Hidden: true, Raw: map[string]any{"data-rollout-canary-empty": "true", "data-rollout-canary-container": "true"}}, ui.Text(agentRPText(locale, "canary_ready"))),
		html.Div(html.Props{ID: "agent-rollout-canaries", Class: "agent-rollout-choice-group", Hidden: true, Role: "group", Raw: map[string]any{"data-rollout-canary-container": "true"}, Aria: map[string]string{"labelledby": "agent-rollout-canaries-title", "describedby": "agent-rollout-canary-help"}}, canaries...),
		html.Button(submitProps, ui.Text(agentRPText(locale, "preview"))),
		html.P(html.Props{Class: "muted agent-rollout-submit-help"}, ui.Text(agentRPText(locale, "preview_submit_help"))),
	)
	fields = append(fields, html.Div(html.Props{Class: "agent-rollout-target-fields", Hidden: allCurrent}, work...))
	return html.Form(html.Props{ID: "agent-rollout-preview", Class: "persona-admin-editor-form agent-rollout-form", Raw: map[string]any{"data-rollout-action": "PREVIEW", "aria-describedby": "agent-rollout-preview-help"}},
		fields...,
	)
}

func agentRolloutConversationLabel(locale LocaleContext, snapshot AgentRolloutSnapshot, installation AgentRolloutInstallation, ordinal int) string {
	agentName := agentRolloutPersonaName(snapshot, installation.PersonaID)
	if !installation.Visible {
		return strings.ReplaceAll(agentRPText(locale, "private_agent_conversation"), "{name}", agentName)
	}
	name := strings.TrimSpace(installation.Name)
	if name == "" {
		return strings.ReplaceAll(agentRPText(locale, "private_agent_conversation"), "{name}", agentName)
	}
	switch strings.ToUpper(installation.ConversationKind) {
	case "DIRECT":
		if agentName != "" {
			name = agentName
		}
		return strings.ReplaceAll(agentRPText(locale, "your_direct_conversation"), "{name}", name)
	case "PUBLIC_CHANNEL":
		label := strings.ReplaceAll(agentRPText(locale, "public_conversation"), "{name}", strings.TrimPrefix(name, "#"))
		return strings.ReplaceAll(label, "{count}", fmt.Sprint(installation.MemberCount))
	case "PRIVATE_CHANNEL":
		label := strings.ReplaceAll(agentRPText(locale, "private_conversation"), "{name}", strings.TrimPrefix(name, "#"))
		return strings.ReplaceAll(label, "{count}", fmt.Sprint(installation.MemberCount))
	}
	if ordinal < 0 {
		ordinal = 0
	}
	return name
}

func agentRolloutConversationNodes(locale LocaleContext, snapshot AgentRolloutSnapshot, installation AgentRolloutInstallation, ordinal int) []ui.Node {
	if !installation.Visible || strings.TrimSpace(installation.Name) == "" {
		return []ui.Node{ui.Text(agentRolloutConversationLabel(locale, snapshot, installation, ordinal))}
	}
	name := strings.TrimPrefix(strings.TrimSpace(installation.Name), "#")
	switch strings.ToUpper(installation.ConversationKind) {
	case "PUBLIC_CHANNEL", "PRIVATE_CHANNEL":
		template := agentRPText(locale, "public_conversation")
		if strings.ToUpper(installation.ConversationKind) == "PRIVATE_CHANNEL" {
			template = agentRPText(locale, "private_conversation")
		}
		parts := strings.Split(template, "{name}")
		prefix := parts[0]
		if strings.HasSuffix(prefix, "#") {
			prefix = strings.TrimSuffix(prefix, "#")
		}
		counted := strings.ReplaceAll(strings.Join(parts[1:], "{name}"), "{count}", locale.FormatNumber(fmt.Sprint(installation.MemberCount), 0))
		return []ui.Node{
			ui.Text(prefix),
			html.Tag("bdi", html.Props{Dir: "ltr"}, ui.Text("#"+name)),
			ui.Text(counted),
		}
	default:
		return []ui.Node{ui.Text(agentRolloutConversationLabel(locale, snapshot, installation, ordinal))}
	}
}

func agentRolloutPersonaName(snapshot AgentRolloutSnapshot, personaID string) string {
	for _, persona := range snapshot.Personas {
		if persona.ID == personaID && strings.TrimSpace(persona.Name) != "" {
			return strings.TrimSpace(persona.Name)
		}
	}
	return agentRPText(ResolveProductLocale(DefaultProductLocale), "agent")
}

func agentRolloutFormatDate(locale LocaleContext, raw string) string {
	if date, ok := personaAdminFormattedDate(locale, raw); ok {
		return date
	}
	return agentRPText(locale, "date_unavailable")
}

func agentRolloutAllCurrent(snapshot AgentRolloutSnapshot) bool {
	persona, version := agentRolloutDefaultTarget(snapshot)
	count := 0
	for _, installation := range snapshot.Installations {
		if installation.PersonaID != persona || installation.UpdateBlocked {
			continue
		}
		count++
		if installation.Version != version {
			return false
		}
	}
	return count > 0
}

func agentRolloutDefaultTarget(snapshot AgentRolloutSnapshot) (string, int64) {
	persona := snapshot.PersonaID
	if persona == "" && len(snapshot.Personas) > 0 {
		persona = snapshot.Personas[0].ID
	}
	if persona == "" && len(snapshot.Versions) > 0 {
		persona = snapshot.Versions[0].PersonaID
	}
	if snapshot.TargetVersion > 0 {
		for _, version := range snapshot.Versions {
			if version.PersonaID == persona && version.Version == snapshot.TargetVersion {
				return persona, version.Version
			}
		}
	}
	current := agentRolloutCurrentVersion(snapshot, persona)
	if current > 0 {
		return persona, current
	}
	for _, version := range snapshot.Versions {
		if version.PersonaID == persona && version.Version > current {
			current = version.Version
		}
	}
	return persona, current
}

func agentRolloutCurrentVersion(snapshot AgentRolloutSnapshot, persona string) int64 {
	var current int64
	for _, version := range snapshot.Versions {
		if version.PersonaID == persona && version.Current && version.Version > current {
			current = version.Version
		}
	}
	if current == 0 {
		for _, version := range snapshot.Versions {
			if version.PersonaID == persona && version.Version > current {
				current = version.Version
			}
		}
	}
	return current
}

func agentRolloutPersonaCount(snapshot AgentRolloutSnapshot, persona string) int {
	count := 0
	for _, in := range snapshot.Installations {
		if in.PersonaID == persona && !in.UpdateBlocked {
			count++
		}
	}
	return count
}

func RenderAgentRolloutForm(locale LocaleContext, snapshot AgentRolloutSnapshot) ui.Node {
	return agentRolloutPreviewForm(locale, snapshot)
}

func agentRolloutLatestVersion(snapshot AgentRolloutSnapshot) int64 {
	var latest int64
	for _, version := range snapshot.Versions {
		if version.Version > latest {
			latest = version.Version
		}
	}
	return latest
}

func agentRolloutPlan(locale LocaleContext, plan AgentRolloutPlan, snapshot AgentRolloutSnapshot) ui.Node {
	children := []ui.Node{html.H3(html.Props{}, ui.Text(agentRPText(locale, "plan"))), html.P(html.Props{}, ui.Text(agentRPText(locale, "canary_count")+": "+locale.FormatNumber(fmt.Sprint(plan.CanaryCount), 0)))}
	rows := []ui.Node{}
	for _, c := range plan.Candidates {
		label := agentRPText(locale, "hidden_conversation")
		for i, in := range snapshot.Installations {
			if in.ID == c.InstallationID {
				label = agentRolloutConversationLabel(locale, snapshot, in, i)
			}
		}
		rows = append(rows, html.Li(html.Props{Class: "agent-rollout-candidate", Raw: map[string]any{"data-installation-id": c.InstallationID, "data-policy-digest": c.PolicyDigest}}, html.Tag("bdi", html.Props{Dir: "auto"}, ui.Text(label)), ui.Text(personaAdminInlineSeparator+agentUXR7Text(locale, "version")+" "+personaAdminLocalizedNumber(locale, fmt.Sprint(plan.Version)))))
	}
	children = append(children, html.Ul(html.Props{}, rows...))
	actions := []ui.Node{}
	object := agentRPText(locale, "rollout_object")
	for _, persona := range snapshot.Personas {
		if persona.ID == snapshot.PersonaID && strings.TrimSpace(persona.Name) != "" {
			object = agentRPText(locale, "rollout_for") + " " + persona.Name
			break
		}
	}
	for _, action := range []string{"APPROVE", "PROMOTE", "ADVANCE"} {
		allowed := (action == "APPROVE" && snapshot.CanApprove) || (action == "PROMOTE" && snapshot.CanPromote) || (action == "ADVANCE" && snapshot.CanAdvance)
		stage := "PREVIEWED"
		if snapshot.Progress != nil {
			stage = snapshot.Progress.Stage
		}
		applicable := (action == "APPROVE" && stage == "PREVIEWED") || (action == "PROMOTE" && stage == "CANARY_COMPLETE") || (action == "ADVANCE" && stage == "APPROVED")
		if allowed && applicable {
			label := agentRolloutActionLabel(locale, action, object, snapshot.Progress, plan)
			actions = append(actions, html.Div(html.Props{Class: "persona-admin-editor-field agent-control-action"}, html.Button(html.Props{Type: "button", Class: "button primary", Raw: map[string]any{"data-rollout-action": action, "data-rollout-id": plan.ID, "data-rollout-digest": plan.Digest, "data-rollout-revision": snapshot.Progress.RevisionIfPresent(), "data-confirm": label + "?", "data-busy-label": agentRPText(locale, "working_short")}}, ui.Text(label)), html.Small(html.Props{Class: "muted"}, ui.Text(agentRPText(locale, "action_help")))))
		}
	}
	children = append(children, html.Div(html.Props{Class: "agent-rollout-actions"}, actions...))
	if snapshot.Progress != nil {
		children = append(children, html.P(html.Props{ID: "agent-rollout-progress", Role: "status", Aria: map[string]string{"live": "polite"}}, ui.Text(agentRolloutStage(locale, snapshot.Progress.Stage))))
	}
	return html.Section(html.Props{ID: "agent-rollout-plan", Class: "agent-rollout-plan"}, children...)
}

func agentRolloutActionLabel(locale LocaleContext, action, object string, progress *AgentRolloutProgress, plan AgentRolloutPlan) string {
	stage := ""
	cursor := int64(0)
	if progress != nil {
		stage = strings.ToUpper(progress.Stage)
		cursor = progress.Cursor
	}
	key := strings.ToLower(action)
	if action == "ADVANCE" && stage == "APPROVED" {
		key = "advance_first"
		if cursor >= int64(plan.CanaryCount) && cursor < int64(len(plan.Candidates)) {
			key = "advance_rest"
		}
		if cursor >= int64(len(plan.Candidates))-1 {
			key = "advance_finish"
		}
	}
	if action == "PROMOTE" {
		key = "promote_after_check"
	}
	label := agentRPText(locale, key)
	if !strings.Contains(label, "{object}") && strings.TrimSpace(object) != "" {
		return label + " " + object
	}
	return strings.ReplaceAll(label, "{object}", object)
}

func (p *AgentRolloutProgress) RevisionIfPresent() string {
	if p == nil {
		return ""
	}
	return fmt.Sprint(p.Revision)
}

func agentPortablePanel(locale LocaleContext, snapshot AgentPortableSnapshot) ui.Node {
	return agentPortablePanelForTab(locale, snapshot, "running")
}

func agentPortablePanelForTab(locale LocaleContext, snapshot AgentPortableSnapshot, selectedTab string) ui.Node {
	status := "portable_ready"
	if snapshot.Available && !snapshot.CanExport && !snapshot.CanImport && snapshot.Draft == nil && len(snapshot.Drafts) == 0 {
		status = "portable_empty"
	}
	children := []ui.Node{html.H2(html.Props{ID: "agent-portable-title"}, ui.Text(agentRPText(locale, "portable"))), html.P(html.Props{Class: "muted"}, ui.Text(agentRPText(locale, "portable_help")))}
	if status != "portable_ready" {
		children = append(children, html.P(html.Props{ID: "agent-portable-status", Role: "status", Aria: map[string]string{"live": "polite", "atomic": "true"}}, ui.Text(agentRPText(locale, status))))
	} else {
		children = append(children, html.P(html.Props{ID: "agent-portable-status", Role: "status", Aria: map[string]string{"live": "polite", "atomic": "true"}}))
	}
	manifestOptions := make([]ui.Node, 0, len(snapshot.Manifests))
	for _, m := range snapshot.Manifests {
		manifestOptions = append(manifestOptions, html.Option(html.Props{Value: m.ID, Raw: map[string]any{"data-manifest-version": m.Version}}, ui.Text(agentPortableManifestLabel(locale, m))))
	}
	if !snapshot.Available {
		return html.Section(html.Props{ID: "agent-portable-panel", Class: "persona-admin-editor agent-operations-region", Hidden: selectedTab != "move", Raw: map[string]any{"role": "tabpanel", "data-agent-operations-panel": "move"}, Aria: map[string]string{"labelledby": "agent-operations-tab-move"}}, append(children, html.P(html.Props{}, ui.Text(agentRPText(locale, "unavailable"))))...)
	}
	if snapshot.CanExport {
		manifests := append([]AgentPortableManifest(nil), snapshot.Manifests...)
		sort.SliceStable(manifests, func(i, j int) bool {
			left, _ := strconv.ParseUint(manifests[i].Version, 10, 64)
			right, _ := strconv.ParseUint(manifests[j].Version, 10, 64)
			return left > right
		})
		options := make([]ui.Node, 0, len(manifests))
		for _, m := range manifests {
			name := agentPortableManifestName(locale, m)
			options = append(options, html.Option(html.Props{Value: m.ID, Raw: map[string]any{"data-manifest-version": m.Version, "data-manifest-digest": m.Digest, "data-manifest-name": name}}, ui.Text(agentPortableManifestLabel(locale, m))))
		}
		exportProps := html.Props{ID: "agent-portable-export-submit", Type: "submit", Class: "button secondary", Disabled: len(snapshot.Manifests) == 0, Raw: map[string]any{"data-busy-label": agentRPText(locale, "working_short")}}
		for _, manifest := range snapshot.Manifests {
			if manifest.PersonaVersion == "" || manifest.PersonaVersion != manifest.Version {
				children = append(children, html.P(html.Props{Class: "agent-portable-export-note", Role: "status"}, ui.Text(agentUXR7Text(locale, "export_incomplete"))))
				break
			}
		}
		copyReady := strings.TrimSpace(snapshot.Definition) != ""
		copyProps := html.Props{Type: "button", Class: "button secondary", Disabled: !copyReady, Raw: map[string]any{"data-portable-copy": "true", "data-busy-label": agentRPText(locale, "working_short")}}
		children = append(children, html.Section(html.Props{Class: "agent-portable-step", Aria: map[string]string{"labelledby": "agent-portable-export-title"}},
			html.H3(html.Props{ID: "agent-portable-export-title"}, ui.Text(agentRPText(locale, "export_step"))),
			html.Form(html.Props{ID: "agent-portable-export", Class: "persona-admin-editor-form", Raw: map[string]any{"data-portable-action": "export"}},
				html.Div(html.Props{Class: "persona-admin-editor-field"}, html.Label(html.Props{For: "agent-portable-manifest"}, ui.Text(agentRPText(locale, "manifest"))), html.Select(html.Props{ID: "agent-portable-manifest", Name: "manifest_id"}, options...)),
				html.P(html.Props{Class: "muted"}, ui.Text(agentRPText(locale, "export_help"))),
				html.Div(html.Props{Class: "agent-portable-export-actions"}, html.Button(exportProps, ui.Text(agentRPText(locale, "download"))), html.Button(copyProps, ui.Text(agentRPText(locale, "copy")))),
				html.P(html.Props{Class: "muted"}, ui.Text(agentRPText(locale, map[bool]string{true: "copy_ready_help", false: "copy_unavailable_help"}[copyReady]))),
			),
		))
	}
	if snapshot.CanImport {
		mapping := make([]ui.Node, 0, len(snapshot.Destinations))
		for index, d := range snapshot.Destinations {
			mapping = append(mapping, html.Div(html.Props{Class: "persona-admin-editor-field agent-portable-mapping"}, html.Label(html.Props{For: "agent-portable-destination-" + d.ID}, ui.Text(agentPortableDestinationName(locale, d.Kind, index+1))), html.Input(html.Props{ID: "agent-portable-destination-" + d.ID, Name: "destination_mapping[" + d.ID + "]", Type: "text", Raw: map[string]any{"data-destination-id": d.ID, "data-mapping-kind": d.Kind, "data-source-id": d.SourceID}}), html.Details(html.Props{}, html.Summary(html.Props{}, ui.Text(agentRPText(locale, "technical"))), html.Code(html.Props{}, ui.Text(d.SourceID)))))
		}
		fields := []ui.Node{
			html.Div(html.Props{Class: "persona-admin-editor-field"}, html.Label(html.Props{}, ui.Text(agentRPText(locale, "import_file"))), html.Div(html.Props{Class: "agent-portable-file-control"}, html.Label(html.Props{Class: "button secondary", For: "agent-portable-file"}, ui.Text(agentRPText(locale, "choose_file"))), html.Span(html.Props{ID: "agent-portable-file-name", Class: "muted", Raw: map[string]any{"data-agent-portable-file-name": "true"}}, ui.Text(agentRPText(locale, "file_empty"))), html.Input(html.Props{ID: "agent-portable-file", Name: "manifest_file", Type: "file", Accept: "application/json,.json", Class: "agent-portable-file-input"}))),
			html.P(html.Props{Class: "muted"}, ui.Text(agentRPText(locale, "or_paste"))),
			html.Div(html.Props{Class: "persona-admin-editor-field"}, html.Label(html.Props{For: "agent-portable-body"}, ui.Text(agentRPText(locale, "import_body"))), html.Textarea(html.Props{ID: "agent-portable-body", Name: "manifest", Rows: 8, Placeholder: agentRPText(locale, "paste_placeholder"), Raw: map[string]any{"aria-describedby": "agent-portable-import-help"}}, ui.Text(snapshot.Definition))),
			html.Div(html.Props{Class: "persona-admin-editor-field"}, html.Label(html.Props{For: "agent-portable-target"}, ui.Text(agentRPText(locale, "import_target"))), html.Select(html.Props{ID: "agent-portable-target", Name: "destination_manifest_id"}, manifestOptions...)),
			html.P(html.Props{ID: "agent-portable-import-help", Class: "muted"}, ui.Text(agentRPText(locale, "import_help"))),
		}
		if strings.TrimSpace(snapshot.Definition) != "" {
			fields = append(fields, html.Section(html.Props{Class: "agent-portable-preview", Aria: map[string]string{"labelledby": "agent-portable-preview-title"}}, html.H4(html.Props{ID: "agent-portable-preview-title"}, ui.Text(agentRPText(locale, "preview_title"))), html.P(html.Props{}, ui.Text(agentRPText(locale, "import_preview_help")))))
		}
		if len(mapping) > 0 {
			fields = append(fields, html.Fieldset(html.Props{}, html.Legend(html.Props{}, ui.Text(agentRPText(locale, "destination_mappings"))), html.Div(html.Props{}, mapping...)))
		}
		importReady := len(snapshot.Manifests) > 0
		importClass := "button secondary"
		if importReady {
			importClass = "button primary"
		}
		fields = append(fields, html.Button(html.Props{Type: "submit", Class: importClass, Disabled: !importReady, Raw: map[string]any{"data-busy-label": agentRPText(locale, "working_short")}}, ui.Text(agentRPText(locale, "import"))))
		children = append(children, html.Section(html.Props{Class: "agent-portable-step", Aria: map[string]string{"labelledby": "agent-portable-import-title"}}, html.H3(html.Props{ID: "agent-portable-import-title"}, ui.Text(agentRPText(locale, "import_step"))), html.Form(html.Props{ID: "agent-portable-import", Class: "persona-admin-editor-form", Raw: map[string]any{"data-portable-action": "import", "data-confirm": agentRPText(locale, "import_confirm"), "aria-describedby": "agent-portable-import-help"}}, fields...)))
	}
	if snapshot.CanImport || snapshot.CanExport || len(snapshot.Drafts) > 0 || snapshot.Draft != nil {
		drafts := append([]AgentPortableReviewDraft(nil), snapshot.Drafts...)
		if snapshot.Draft != nil {
			found := false
			for _, draft := range drafts {
				found = found || draft.ID == snapshot.Draft.ID
			}
			if !found {
				drafts = append(drafts, *snapshot.Draft)
			}
		}
		rows := make([]ui.Node, 0, len(drafts))
		for _, draft := range drafts {
			name := strings.TrimSpace(draft.Name)

			if name == "" {
				name = agentRPText(locale, "imported_agent")
			}
			meta := strings.TrimSpace(draft.ImportedAt)
			if meta == "" {
				meta = agentRPText(locale, "date_unavailable")
			} else {
				meta = agentOperationsFormatInstant(locale, meta)
			}
			if draft.ImportedBy != "" {
				meta += personaAdminInlineSeparator + draft.ImportedBy
			}
			rows = append(rows, html.Li(html.Props{Class: "agent-portable-draft-row"}, html.Div(html.Props{}, html.Strong(html.Props{}, ui.Text(name+" · "+agentUXR7Text(locale, "version")+" "+locale.FormatNumber(fmt.Sprint(draft.Version), 0))), html.P(html.Props{Class: "muted"}, ui.Text(meta)), html.P(html.Props{Class: "muted", Dir: "auto"}, ui.Text(draft.Purpose))), html.Form(html.Props{Class: "agent-portable-open-form", Raw: map[string]any{"data-portable-action": "read"}}, html.Button(html.Props{Type: "submit", Class: "button secondary", Raw: map[string]any{"data-definition-id": draft.ID, "data-busy-label": agentRPText(locale, "working_short")}}, ui.Text(agentRPText(locale, "open"))))))
		}
		list := ui.Node(html.P(html.Props{Class: "muted"}, ui.Text(agentRPText(locale, "drafts_empty"))))
		if len(rows) > 0 {
			list = html.Ul(html.Props{Class: "agent-portable-draft-list"}, rows...)
		}
		children = append(children, html.Section(html.Props{ID: "agent-portable-review", Class: "agent-portable-step", Aria: map[string]string{"labelledby": "agent-portable-review-title"}}, html.H3(html.Props{ID: "agent-portable-review-title"}, ui.Text(agentRPText(locale, "review_step"))), html.P(html.Props{Class: "muted"}, ui.Text(agentRPText(locale, "review_help"))), list))
	}
	if snapshot.Draft != nil {
		children = append(children, html.Section(html.Props{ID: "agent-portable-draft", Aria: map[string]string{"labelledby": "agent-portable-draft-title"}},
			html.H3(html.Props{ID: "agent-portable-draft-title"}, ui.Text(agentRPText(locale, "review_draft"))), html.P(html.Props{}, ui.Text(snapshot.Draft.Purpose)), html.P(html.Props{Class: "agent-portable-draft-instructions"}, ui.Text(snapshot.Draft.Instructions)), html.P(html.Props{}, ui.Text(agentUXR7Text(locale, "version")+" "+personaAdminLocalizedNumber(locale, fmt.Sprint(snapshot.Draft.Version))))))
	}
	return html.Section(html.Props{ID: "agent-portable-panel", Class: "persona-admin-editor agent-operations-region", Hidden: selectedTab != "move", Raw: map[string]any{"role": "tabpanel", "data-agent-operations-panel": "move"}, Aria: map[string]string{"labelledby": "agent-operations-tab-move"}}, children...)
}

func agentRolloutStage(locale LocaleContext, stage string) string {
	key := "stage_" + strings.ToLower(stage)
	if text := agentRPText(locale, key); text != "" {
		return text
	}
	return agentRPText(locale, "stage_pending")
}

func agentPortableManifestName(locale LocaleContext, manifest AgentPortableManifest) string {
	if name := strings.TrimSpace(manifest.Name); name != "" && name != manifest.ID && len([]rune(name)) <= 48 && !strings.ContainsAny(name, ".!?") && !strings.Contains(strings.ToLower(name), "tenant") {
		return name
	}
	if name := strings.TrimSpace(manifest.Name); strings.Contains(name, ".") {
		name = name[strings.LastIndex(name, ".")+1:]
		if label := DisplayLabel(name); label != "" && !strings.Contains(strings.ToLower(label), "tenant") {
			return label
		}
	}

	return agentRPText(locale, "agent")
}

func agentPortableManifestLabel(locale LocaleContext, manifest AgentPortableManifest) string {
	return agentPortableManifestName(locale, manifest) + personaAdminInlineSeparator + agentUXR7Text(locale, "file_version") + " " + locale.FormatNumber(manifest.Version, 0)
}

func agentSetupHref(locale LocaleContext) string {
	return "/workspace/app/admin/personas?locale=" + locale.Resolved
}

func agentPortableDestinationName(locale LocaleContext, kind string, ordinal int) string {
	key := "mapping_" + strings.ToLower(kind)
	name := agentRPText(locale, key)
	if name == "" {
		name = agentRPText(locale, "mapping_reference")
	}
	return fmt.Sprintf("%s %d", name, ordinal)
}

func agentRPText(locale LocaleContext, key string) string {
	if key == "canary_ready" {
		return [3]string{"At least one conversation is updated first.", "Mindestens eine Unterhaltung wird zuerst aktualisiert.", "يتم تحديث محادثة واحدة على الأقل أولاً."}[agentRPLocaleIndex(locale)]
	}
	if key == "private_agent_conversation" {
		return [3]string{"A person's private conversation with {name}", "Private Unterhaltung einer Person mit {name}", "محادثة شخص خاصة مع {name}"}[agentRPLocaleIndex(locale)]
	}
	if key == "your_direct_conversation" {
		return [3]string{"Your conversation with {name}", "Ihre Unterhaltung mit {name}", "محادثتك مع {name}"}[agentRPLocaleIndex(locale)]
	}
	if key == "canary_empty" {
		return [3]string{"Choose which of the selected conversations are updated first.", "Wählen Sie aus, welche der ausgewählten Unterhaltungen zuerst aktualisiert werden.", "اختر أي المحادثات المحددة سيتم تحديثها أولاً."}[agentRPLocaleIndex(locale)]
	}
	if key == "version_option_current" {
		return [3]string{"Version {version} (current) · published {date}", "Version {version} (aktuell) · veröffentlicht {date}", "الإصدار {version} (الحالي) · نُشر في {date}"}[agentRPLocaleIndex(locale)]
	}
	if key == "private_non_member" || key == "conversation_hidden" {
		return [3]string{"Private conversation · {count} members · you are not a member", "Private Unterhaltung · {count} Mitglieder · Sie sind kein Mitglied", "محادثة خاصة · {count} أعضاء · أنت لست عضوًا"}[agentRPLocaleIndex(locale)]
	}
	if key == "private_not_listed" {
		return [3]string{"{count} private conversation is not listed; its owner must update it.", "{count} private Unterhaltung wird nicht aufgeführt; ihr Eigentümer muss sie aktualisieren.", "لم تُدرج {count} محادثة خاصة؛ يجب على مالكها تحديثها."}[agentRPLocaleIndex(locale)]
	}
	if key == "rollout" {
		return [3]string{"Rollout", "Versionswechsel", "الطرح"}[agentRPLocaleIndex(locale)]
	}
	if key == "preview" {
		return [3]string{"Preview rollout", "Versionswechsel", "معاينة تغيير الإصدار"}[agentRPLocaleIndex(locale)]
	}
	if key == "canary_empty" {
		return [3]string{"Tick conversations above to choose which are updated first.", "Markieren Sie oben Unterhaltungen, um auszuwaehlen, welche zuerst aktualisiert werden.", "حدد المحادثات أعلاه لاختيار ما سيتم تحديثه أولا."}[agentRPLocaleIndex(locale)]
	}
	if key == "setup_required" {
		return [3]string{"No published agent is available for rollout yet.", "Für den Rollout ist noch kein veröffentlichter Agent verfügbar.", "لا يتوفر وكيل منشور للطرح حتى الآن."}[agentRPLocaleIndex(locale)]
	}
	if key == "review_draft" {
		return [3]string{"Review imported draft", "Importierten Entwurf prüfen", "مراجعة المسودة المستوردة"}[agentRPLocaleIndex(locale)]
	}
	values := map[string][3]string{
		"title":                    {"Agent rollout and portable files", "Agenten-Rollout und portable Dateien", "طرح الوكلاء والملفات المحمولة"},
		"rollout":                  {"Rollout", "Versionswechsel", "الطرح"},
		"rollout_help":             {"Move conversations from the version they run now to a reviewed version, a few at a time.", "Verschieben Sie Unterhaltungen nach und nach von ihrer aktuellen Version auf eine geprüfte Version.", "انقل المحادثات من الإصدار الذي تستخدمه الآن إلى إصدار أحدث، عددًا قليلاً في كل مرة."},
		"portable":                 {"Move an agent between workspaces", "Einen Agenten zwischen Arbeitsbereichen verschieben", "نقل وكيل بين مساحات العمل"},
		"portable_help":            {"Download an agent file, import it as a draft in another workspace, then review it before publication.", "Laden Sie eine Agentendatei herunter, importieren Sie sie in einem anderen Arbeitsbereich als Entwurf und prüfen Sie sie vor der Veröffentlichung.", "نزّل ملف وكيل واستورده كمسودة في مساحة عمل أخرى ثم راجعه قبل النشر."},
		"portable_ready":           {"", "", ""},
		"portable_empty":           {"No portable agent files are available. Publish a reviewed agent in Agent setup or ask an administrator to check your access.", "Es sind keine portablen Agentendateien verfügbar. Veröffentlichen Sie unter „Agenteneinrichtung“ einen geprüften Agenten oder lassen Sie Ihren Zugriff prüfen.", "لا تتوفر ملفات وكلاء محمولة. انشر وكيلاً تمت مراجعته في إعداد الوكلاء أو اطلب من مسؤول التحقق من صلاحياتك."},
		"ready":                    {"Ready to preview.", "Bereit für die Vorschau.", "جاهز للمعاينة."},
		"unavailable":              {"This region is unavailable. Ask an administrator to check your owner access, then refresh.", "Dieser Bereich ist nicht verfügbar. Lassen Sie Ihre Eigentümerberechtigung prüfen und aktualisieren Sie dann.", "هذه المنطقة غير متاحة. اطلب من مسؤول التحقق من وصول المالك ثم حدّث."},
		"persona":                  {"Agent", "Agent", "الوكيل"},
		"target_version":           {"Version to roll out", "Einzuführende Version", "الإصدار المراد طرحه"},
		"batch_limit":              {"How many conversations to update at a time", "Wie viele Unterhaltungen gleichzeitig aktualisiert werden", "عدد المحادثات التي سيتم تحديثها في كل مرة"},
		"installations":            {"Conversations to update", "Zu aktualisierende Unterhaltungen", "المحادثات المراد تحديثها"},
		"installation_object":      {"Conversation", "Unterhaltung", "محادثة"},
		"canaries":                 {"Update these first and wait for my go-ahead", "Diese zuerst aktualisieren und auf meine Freigabe warten", "حدّث هذه أولاً وانتظر موافقتي"},
		"canary_help":              {"The rollout pauses after these so you can check the answers before the rest are updated.", "Die Einführung pausiert danach, damit Sie die Antworten prüfen können, bevor der Rest aktualisiert wird.", "يتوقف الطرح بعد هذه المحادثات حتى تتمكن من التحقق من الإجابات قبل تحديث الباقي."},
		"preview_help":             {"Select the conversations that currently use this agent. Preview does not change them.", "Wählen Sie die Unterhaltungen aus, die diesen Agenten derzeit verwenden. Die Vorschau ändert sie nicht.", "حدد المحادثات التي تستخدم هذا الوكيل حاليًا. لا تغيّرها المعاينة."},
		"preview":                  {"Preview rollout", "Rollout vorschauen", "معاينة تغيير الإصدار"},
		"preview_submit_help":      {"Shows what would change. Nothing is updated until you confirm.", "Zeigt, was sich ändern würde. Nichts wird aktualisiert, bis Sie bestätigen.", "يعرض ما سيتغير. لن يتم تحديث أي شيء حتى تؤكد."},
		"all_current":              {"All conversations already run version {version}.", "Alle Unterhaltungen verwenden bereits Version {version}.", "تستخدم جميع المحادثات الإصدار {version} بالفعل."},
		"version_option":           {"Version {version} · published {date}", "Version {version} · veröffentlicht {date}", "الإصدار {version} · نُشر في {date}"},
		"version_current":          {" · current", " · aktuell", " · الحالي"},
		"conversation_hidden":      {"A conversation you cannot see", "Eine Unterhaltung, die Sie nicht sehen können", "محادثة لا يمكنك رؤيتها"},
		"direct_conversation":      {"Direct conversation with {name}", "Direkte Unterhaltung mit {name}", "محادثة مباشرة مع {name}"},
		"public_conversation":      {"#{name} · Public channel · {count} members", "#{name} · Öffentlicher Kanal · {count} Mitglieder", "#{name} · قناة عامة · {count} أعضاء"},
		"private_conversation":     {"#{name} · Private channel · {count} members", "#{name} · Privater Kanal · {count} Mitglieder", "#{name} · قناة خاصة · {count} أعضاء"},
		"plan":                     {"Preview plan", "Vorschauplan", "خطة المعاينة"},
		"rollout_id":               {"Rollout ID", "Rollout-ID", "معرف التوزيع"},
		"digest":                   {"Digest", "Digest", "البصمة"},
		"canary_count":             {"First group", "Erste Gruppe", "المجموعة الأولى"},
		"candidates":               {"Candidates", "Kandidaten", "المرشحون"},
		"technical":                {"Technical details", "Technische Details", "التفاصيل التقنية"},
		"rollout_object":           {"rollout", "Rollout", "الطرح"},
		"rollout_for":              {"rollout for", "Rollout für", "طرح"},
		"approve":                  {"Approve", "Genehmigen", "الموافقة على"},
		"promote":                  {"Promote", "Freigeben", "ترقية"},
		"advance":                  {"Advance", "Fortsetzen", "متابعة"},
		"advance_first":            {"Update the first conversations in {object}", "Erste Unterhaltungen in {object} aktualisieren", "تحديث المحادثات الأولى في {object}"},
		"promote_after_check":      {"I checked the first conversations in {object}", "Ich habe die ersten Unterhaltungen in {object} geprüft", "تحققت من المحادثات الأولى في {object}"},
		"advance_rest":             {"Continue updating {object}", "Aktualisierung von {object} fortsetzen", "متابعة تحديث {object}"},
		"advance_finish":           {"Finish updating {object}", "Aktualisierung von {object} abschließen", "إنهاء تحديث {object}"},
		"action_help":              {"The server checks your current owner permission before changing the rollout.", "Der Server prüft vor der Änderung Ihre aktuelle Eigentümerberechtigung.", "يتحقق الخادم من صلاحية المالك الحالية قبل تغيير الطرح."},
		"refresh_help":             {"Reload available versions and the current rollout state.", "Verfügbare Versionen und aktuellen Rollout-Status neu laden.", "أعد تحميل الإصدارات المتاحة وحالة الطرح الحالية."},
		"refresh_versions":         {"Check for new versions", "Nach neuen Versionen suchen", "تحديث الإصدارات"},
		"setup_link":               {"Review and publish an agent in Agent setup", "Agenten in der Agenteneinrichtung prüfen und veröffentlichen", "مراجعة وكيل ونشره في إعداد الوكلاء"},
		"setup_suffix":             {", then add it to a conversation before previewing a rollout.", ", und fügen Sie ihn anschließend einer Unterhaltung hinzu, bevor Sie den Rollout in der Vorschau anzeigen.", "، ثم أضفه إلى محادثة قبل معاينة الطرح."},
		"working_short":            {"Working…", "Wird ausgeführt…", "جارٍ التنفيذ…"},
		"manifest":                 {"Agent and version", "Agent und Version", "الوكيل والإصدار"},
		"agent":                    {"Agent", "Agent", "الوكيل"},
		"export_step":              {"1. Export", "1. Exportieren", "1. التصدير"},
		"download":                 {"Download agent file", "Agentendatei herunterladen", "تنزيل ملف الوكيل"},
		"copy":                     {"Copy file contents", "Dateiinhalt kopieren", "نسخ محتوى الملف"},
		"copy_unavailable_help":    {"Download an agent file first, then you can copy its contents.", "Laden Sie zuerst eine Agentendatei herunter; danach können Sie ihren Inhalt kopieren.", "نزّل ملف وكيل أولاً، ثم يمكنك نسخ محتواه."},
		"copy_ready_help":          {"The downloaded agent file is ready to copy into another workspace.", "Die heruntergeladene Agentendatei kann jetzt in einen anderen Arbeitsbereich kopiert werden.", "ملف الوكيل الذي تم تنزيله جاهز للنسخ إلى مساحة عمل أخرى."},
		"live_suffix":              {" (live)", " (live)", " (مباشر)"},
		"waiting_for_review":       {"waiting for review", "wartet auf Prüfung", "بانتظار المراجعة"},
		"waiting_to_publish":       {"ready to publish", "bereit zur Veröffentlichung", "جاهز للنشر"},
		"waiting_version":          {"{name} version {version} is {state}.", "{name} Version {version} {state}.", "الإصدار {version} من {name} {state}."},
		"waiting_suffix":           {" it will appear here.", " erscheint sie hier.", " سيظهر هنا."},
		"export_help":              {"Only reviewed versions can be exported.", "Nur geprüfte Versionen können exportiert werden.", "يمكن تصدير الإصدارات التي تمت مراجعتها فقط."},
		"import_step":              {"2. Import", "2. Importieren", "2. الاستيراد"},
		"import_file":              {"Choose an agent file", "Agentendatei auswählen", "اختيار ملف وكيل"},
		"choose_file":              {"Choose file", "Datei auswählen", "اختيار ملف"},
		"file_empty":               {"No file chosen", "Keine Datei ausgewählt", "لم يتم اختيار ملف"},
		"or_paste":                 {"Or paste the agent file below.", "Oder fügen Sie die Agentendatei unten ein.", "أو الصق ملف الوكيل أدناه."},
		"import_body":              {"Paste agent file", "Agentendatei einfügen", "لصق ملف الوكيل"},
		"paste_placeholder":        {"Paste the exported agent file here", "Exportierte Agentendatei hier einfügen", "الصق ملف الوكيل المُصدّر هنا"},
		"import_target":            {"Create the draft for", "Entwurf erstellen für", "إنشاء المسودة لـ"},
		"import_help":              {"Importing creates a draft. It gives the agent no access until the draft is reviewed and published.", "Der Import erstellt einen Entwurf. Zugriff erhält der Agent erst nach Prüfung und Veröffentlichung.", "ينشئ الاستيراد مسودة. لا يحصل الوكيل على وصول حتى تتم مراجعة المسودة ونشرها."},
		"import_confirm":           {"Import this agent as a draft?", "Diesen Agenten als Entwurf importieren?", "هل تريد استيراد هذا الوكيل كمسودة؟"},
		"preview_title":            {"Import preview", "Importvorschau", "معاينة الاستيراد"},
		"import_preview_help":      {"A new agent draft will be created. Review each reference mapping before importing.", "Ein neuer Agentenentwurf wird erstellt. Prüfen Sie vor dem Import jede Referenzzuordnung.", "سيتم إنشاء مسودة وكيل جديدة. راجع تعيين كل مرجع قبل الاستيراد."},
		"destination_mappings":     {"Destination mappings", "Zielzuordnungen", "تعيينات الوجهة"},
		"mapping_source":           {"Knowledge source", "Wissensquelle", "مصدر المعرفة"},
		"mapping_capability":       {"Capability", "Fähigkeit", "قدرة"},
		"mapping_model_policy":     {"Model policy", "Modellrichtlinie", "سياسة النموذج"},
		"mapping_output_schema":    {"Output format", "Ausgabeformat", "تنسيق الإخراج"},
		"mapping_evaluation_suite": {"Evaluation suite", "Evaluierungssammlung", "حزمة التقييم"},
		"mapping_reference":        {"Reference", "Referenz", "مرجع"},
		"import":                   {"Import as draft", "Als Entwurf importieren", "استيراد كمسودة"},
		"review_step":              {"3. Review imported drafts", "3. Importierte Entwürfe prüfen", "3. مراجعة المسودات المستوردة"},
		"open":                     {"Open", "Öffnen", "فتح"},
		"imported_agent":           {"Imported agent", "Importierter Agent", "وكيل مستورد"},
		"date_unavailable":         {"Import date unavailable", "Importdatum nicht verfügbar", "تاريخ الاستيراد غير متاح"},
		"drafts_empty":             {"No imported drafts yet. Import an agent file to create one.", "Noch keine importierten Entwürfe. Importieren Sie eine Agentendatei, um einen zu erstellen.", "لا توجد مسودات مستوردة بعد. استورد ملف وكيل لإنشاء مسودة."},
		"review_help":              {"Open an imported draft to verify its purpose and instructions before review.", "Öffnen Sie einen importierten Entwurf, um Zweck und Anweisungen vor der Prüfung zu kontrollieren.", "افتح مسودة مستوردة للتحقق من غرضها وتعليماتها قبل المراجعة."},
		"stage_previewed":          {"Preview ready for approval.", "Vorschau zur Genehmigung bereit.", "المعاينة جاهزة للموافقة."},
		"stage_canary_complete":    {"The first conversations were updated. Check their answers before updating the rest.", "Die ersten Unterhaltungen sind aktualisiert. Prüfen Sie ihre Antworten, bevor Sie die übrigen aktualisieren.", "تم تحديث المحادثات الأولى. تحقق من إجاباتها قبل تحديث البقية."},
		"stage_approved":           {"Rollout approved and ready to advance.", "Rollout genehmigt und zum Fortsetzen bereit.", "تمت الموافقة على الطرح وهو جاهز للمتابعة."},
		"stage_pending":            {"Check progress here before continuing.", "Prüfen Sie hier den Fortschritt, bevor Sie fortfahren.", "تحقق من التقدم هنا قبل المتابعة."},
	}
	index := 0
	if locale.Resolved == "de-DE" {
		index = 1
	}
	if locale.Resolved == "ar" {
		index = 2
	}
	if v, ok := values[key]; ok {
		return v[index]
	}
	return ""
}

func agentRPLocaleIndex(locale LocaleContext) int {
	if locale.Resolved == "de-DE" {
		return 1
	}
	if locale.Resolved == "ar" {
		return 2
	}
	return 0
}
