package productui

import (
	"fmt"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AgentRolloutSnapshot is the server-owned projection used by Agent Studio.
// IDs are opaque and are always posted back verbatim; labels never carry
// authority.
type AgentRolloutSnapshot struct {
	Loading       bool
	Available     bool
	CanPreview    bool
	CanApprove    bool
	CanPromote    bool
	CanAdvance    bool
	PersonaID     string
	Personas      []AgentRolloutPersona
	Versions      []AgentRolloutVersion
	Installations []AgentRolloutInstallation
	Active        *AgentRolloutPlan
	Progress      *AgentRolloutProgress
	Portable      AgentPortableSnapshot
}

type AgentRolloutPersona struct{ ID, Name string }
type AgentRolloutVersion struct {
	PersonaID             string
	Version               int64
	Digest, ProfileDigest string
}
type AgentRolloutInstallation struct {
	ID, PersonaID, ConversationID, Status string
	Version, Revision                     int64
	CanaryEligible                        bool
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
	Available    bool
	CanExport    bool
	CanImport    bool
	Manifests    []AgentPortableManifest
	Destinations []AgentPortableDestination
	Definition   string
	Draft        *AgentPortableReviewDraft
}
type AgentPortableReviewDraft struct {
	ID, Purpose, Instructions string
	Version                   uint64
}
type AgentPortableManifest struct{ ID, Version, Name, Digest string }
type AgentPortableDestination struct{ ID, Label, Kind, SourceID string }

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
	if rollout.Loading {
		return html.Section(html.Props{ID: "agent-rollout-portable", Dir: string(locale.Direction), Raw: map[string]any{"data-locale": locale.Resolved}, Aria: map[string]string{"labelledby": "agent-rollout-portable-title"}},
			html.H2(html.Props{ID: "agent-rollout-portable-title"}, ui.Text(agentRPText(locale, "title"))),
			html.P(html.Props{ID: "agent-rollout-status", Role: "status", Aria: map[string]string{"live": "polite"}}, ui.Text(AgentRolloutPortableStatus(locale, "loading"))),
			html.Button(html.Props{Type: "button", Raw: map[string]any{"data-rollout-action": "REFRESH"}}, ui.Text(agentControlsText(locale, "refresh"))))
	}
	return html.Section(html.Props{ID: "agent-rollout-portable", Dir: string(locale.Direction), Aria: map[string]string{"labelledby": "agent-rollout-portable-title"}, Raw: map[string]any{"data-locale": locale.Resolved, "data-rollout-available": rollout.Available, "data-portable-available": portable.Available}},
		html.H2(html.Props{ID: "agent-rollout-portable-title"}, ui.Text(agentRPText(locale, "title"))),
		agentRolloutPanel(locale, rollout),
		agentPortablePanel(locale, portable),
	)
}

func AgentRolloutPortableStatus(locale LocaleContext, key string) string {
	values := map[string][3]string{
		"loading":          {"Loading authorized versions and controls…", "Berechtigte Versionen und Aktionen werden geladen…", "جارٍ تحميل الإصدارات والإجراءات المصرح بها…"},
		"working":          {"Applying the selected action…", "Die ausgewählte Aktion wird ausgeführt…", "جارٍ تطبيق الإجراء المحدد…"},
		"refused":          {"The action was refused. Review your current access and try again.", "Die Aktion wurde abgelehnt. Prüfen Sie Ihren aktuellen Zugriff und versuchen Sie es erneut.", "تم رفض الإجراء. راجع صلاحياتك الحالية وحاول مجددًا."},
		"invalid":          {"Supply a valid canonical portable definition.", "Geben Sie eine gültige kanonische portable Definition an.", "أدخل تعريفًا محمولًا صالحًا بصيغة قياسية."},
		"mapping_required": {"Choose a destination for every portable reference before importing.", "Wählen Sie vor dem Import ein Ziel für jede Referenz.", "اختر وجهة لكل مرجع محمول قبل الاستيراد."},
		"exported":         {"Definition exported. Review every destination mapping before importing.", "Definition exportiert. Prüfen Sie vor dem Import jede Zielzuordnung.", "تم تصدير التعريف. راجع كل تعيين وجهة قبل الاستيراد."},
		"imported":         {"Definition imported as a reviewable draft.", "Definition als prüfbarer Entwurf importiert.", "تم استيراد التعريف كمسودة قابلة للمراجعة."},
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
	ready := len(snapshot.Personas) > 0 && len(snapshot.Versions) > 0 && len(snapshot.Installations) > 0
	status := "ready"
	if !snapshot.Available {
		status = "unavailable"
	} else if snapshot.CanPreview && !ready {
		status = "setup_required"
	}
	children := []ui.Node{html.H3(html.Props{}, ui.Text(agentRPText(locale, "rollout"))), html.P(html.Props{ID: "agent-rollout-status", Role: "status", Aria: map[string]string{"live": "polite", "atomic": "true"}}, ui.Text(agentRPText(locale, status)))}
	if !snapshot.Available {
		return html.Section(html.Props{ID: "agent-rollout-panel", Class: "agent-rollout-panel"}, children...)
	}
	children = append(children, html.Button(html.Props{Type: "button", Raw: map[string]any{"data-rollout-action": "REFRESH"}}, ui.Text(agentControlsText(locale, "refresh"))))
	if snapshot.CanPreview && ready {
		children = append(children, agentRolloutPreviewForm(locale, snapshot))
	}
	if snapshot.Active != nil {
		children = append(children, agentRolloutPlan(locale, *snapshot.Active, snapshot))
	}
	return html.Section(html.Props{ID: "agent-rollout-panel", Class: "agent-rollout-panel"}, children...)
}

func agentRolloutPreviewForm(locale LocaleContext, snapshot AgentRolloutSnapshot) ui.Node {
	personaOptions := make([]ui.Node, 0, len(snapshot.Personas))
	for _, p := range snapshot.Personas {
		personaOptions = append(personaOptions, html.Option(html.Props{Value: p.ID}, ui.Text(p.Name)))
	}
	versionOptions := make([]ui.Node, 0, len(snapshot.Versions))
	for _, v := range snapshot.Versions {
		versionOptions = append(versionOptions, html.Option(html.Props{Value: fmt.Sprint(v.Version), Raw: map[string]any{"data-persona-id": v.PersonaID, "data-digest": v.Digest, "data-profile-digest": v.ProfileDigest}}, ui.Text(fmt.Sprint(v.Version))))
	}
	installations := make([]ui.Node, 0, len(snapshot.Installations))
	for _, installation := range snapshot.Installations {
		label := installation.ID
		if installation.ConversationID != "" {
			label += " · " + installation.ConversationID
		}
		installations = append(installations, html.Label(html.Props{Class: "agent-rollout-installation"}, html.Input(html.Props{Type: "checkbox", Name: "installation_ids", Value: installation.ID, Raw: map[string]any{"data-rollout-installation": installation.ID, "data-persona-id": installation.PersonaID}}), ui.Text(" "+label)))
	}
	canaries := make([]ui.Node, 0, len(snapshot.Installations))
	for _, installation := range snapshot.Installations {
		if !installation.CanaryEligible {
			continue
		}
		canaries = append(canaries, html.Label(html.Props{Class: "agent-rollout-canary"}, html.Input(html.Props{Type: "checkbox", Name: "canary_ids", Value: installation.ID, Raw: map[string]any{"data-rollout-canary": installation.ID, "data-persona-id": installation.PersonaID}}), ui.Text(" "+installation.ID)))
	}
	return html.Form(html.Props{ID: "agent-rollout-preview", Class: "agent-rollout-form", Raw: map[string]any{"data-rollout-action": "PREVIEW", "aria-describedby": "agent-rollout-preview-help"}},
		html.Label(html.Props{For: "agent-rollout-persona"}, ui.Text(agentRPText(locale, "persona"))), html.Select(html.Props{ID: "agent-rollout-persona", Name: "persona_id"}, personaOptions...),
		html.Label(html.Props{For: "agent-rollout-version"}, ui.Text(agentRPText(locale, "target_version"))), html.Select(html.Props{ID: "agent-rollout-version", Name: "target_version"}, versionOptions...),
		html.Label(html.Props{For: "agent-rollout-batch"}, ui.Text(agentRPText(locale, "batch_limit"))), html.Input(html.Props{ID: "agent-rollout-batch", Name: "batch_limit", Type: "number", Value: "1", Min: "1"}),
		html.Fieldset(html.Props{ID: "agent-rollout-installations"}, html.Legend(html.Props{}, ui.Text(agentRPText(locale, "installations"))), html.Div(html.Props{}, installations...)),
		html.Fieldset(html.Props{ID: "agent-rollout-canaries", Aria: map[string]string{"describedby": "agent-rollout-canary-help"}}, html.Legend(html.Props{}, ui.Text(agentRPText(locale, "canaries"))), html.P(html.Props{ID: "agent-rollout-canary-help", Class: "muted"}, ui.Text(agentRPText(locale, "canary_help"))), html.Div(html.Props{}, canaries...)),
		html.P(html.Props{ID: "agent-rollout-preview-help", Class: "muted"}, ui.Text(agentRPText(locale, "preview_help"))),
		html.Button(html.Props{Type: "submit", Class: "button primary", Raw: map[string]any{"data-rollout-submit": "PREVIEW"}}, ui.Text(agentRPText(locale, "preview"))),
	)
}

func agentRolloutPlan(locale LocaleContext, plan AgentRolloutPlan, snapshot AgentRolloutSnapshot) ui.Node {
	children := []ui.Node{html.H4(html.Props{}, ui.Text(agentRPText(locale, "plan"))), html.P(html.Props{}, html.Strong(html.Props{}, ui.Text(agentRPText(locale, "rollout_id")+": ")), html.Code(html.Props{}, ui.Text(plan.ID))), html.P(html.Props{}, html.Strong(html.Props{}, ui.Text(agentRPText(locale, "digest")+": ")), html.Code(html.Props{}, ui.Text(plan.Digest))), html.P(html.Props{}, ui.Text(fmt.Sprintf("%s: %d", agentRPText(locale, "canary_count"), plan.CanaryCount)))}
	rows := make([]ui.Node, 0, len(plan.Candidates))
	for _, c := range plan.Candidates {
		rows = append(rows, html.Li(html.Props{Class: "agent-rollout-candidate", Raw: map[string]any{"data-installation-id": c.InstallationID}}, ui.Text(strings.Join([]string{c.InstallationID, c.ConversationID, fmt.Sprint(c.Version), fmt.Sprint(c.Revision), fmt.Sprint(c.RevocationEpoch), fmt.Sprint(c.AuthorityRevision), c.PolicyDigest}, " · "))))
	}
	children = append(children, html.H5(html.Props{}, ui.Text(agentRPText(locale, "candidates"))), html.Ul(html.Props{}, rows...))
	actions := []ui.Node{}
	for _, action := range []string{"APPROVE", "PROMOTE", "ADVANCE"} {
		allowed := (action == "APPROVE" && snapshot.CanApprove) || (action == "PROMOTE" && snapshot.CanPromote) || (action == "ADVANCE" && snapshot.CanAdvance)
		if allowed {
			actions = append(actions, html.Button(html.Props{Type: "button", Class: "button secondary", Raw: map[string]any{"data-rollout-action": action, "data-rollout-id": plan.ID, "data-rollout-digest": plan.Digest, "data-rollout-revision": snapshot.Progress.RevisionIfPresent()}}, ui.Text(agentRPText(locale, strings.ToLower(action)))))
		}
	}
	children = append(children, html.Div(html.Props{Class: "agent-rollout-actions"}, actions...))
	if snapshot.Progress != nil {
		children = append(children, html.P(html.Props{ID: "agent-rollout-progress", Role: "status", Aria: map[string]string{"live": "polite"}}, ui.Text(strings.Join([]string{snapshot.Progress.Stage, fmt.Sprint(snapshot.Progress.Revision), fmt.Sprint(snapshot.Progress.Cursor), snapshot.Progress.ApproverID}, " · "))))
	}
	return html.Section(html.Props{ID: "agent-rollout-plan", Class: "agent-rollout-plan"}, children...)
}

func (p *AgentRolloutProgress) RevisionIfPresent() string {
	if p == nil {
		return ""
	}
	return fmt.Sprint(p.Revision)
}

func agentPortablePanel(locale LocaleContext, snapshot AgentPortableSnapshot) ui.Node {
	children := []ui.Node{html.H3(html.Props{}, ui.Text(agentRPText(locale, "portable")))}
	manifestOptions := make([]ui.Node, 0, len(snapshot.Manifests))
	for _, m := range snapshot.Manifests {
		manifestOptions = append(manifestOptions, html.Option(html.Props{Value: m.ID, Raw: map[string]any{"data-manifest-version": m.Version}}, ui.Text(m.Name+" · "+m.Version)))
	}
	if !snapshot.Available {
		return html.Section(html.Props{ID: "agent-portable-panel"}, append(children, html.P(html.Props{}, ui.Text(agentRPText(locale, "unavailable"))))...)
	}
	if snapshot.CanExport {
		options := make([]ui.Node, 0, len(snapshot.Manifests))
		for _, m := range snapshot.Manifests {
			options = append(options, html.Option(html.Props{Value: m.ID, Raw: map[string]any{"data-manifest-version": m.Version, "data-manifest-digest": m.Digest}}, ui.Text(m.Name+" · "+m.Version)))
		}
		children = append(children, html.Form(html.Props{ID: "agent-portable-export", Raw: map[string]any{"data-portable-action": "export"}}, html.Label(html.Props{For: "agent-portable-manifest"}, ui.Text(agentRPText(locale, "manifest"))), html.Select(html.Props{ID: "agent-portable-manifest", Name: "manifest_id"}, options...), html.Label(html.Props{For: "agent-portable-manifest-version"}, ui.Text(agentRPText(locale, "manifest_version"))), html.Input(html.Props{ID: "agent-portable-manifest-version", Name: "manifest_version", Type: "text"}), html.Button(html.Props{Type: "submit", Class: "button secondary"}, ui.Text(agentRPText(locale, "export")))))
	}
	if snapshot.CanImport {
		mapping := make([]ui.Node, 0, len(snapshot.Destinations))
		for _, d := range snapshot.Destinations {
			mapping = append(mapping, html.Div(html.Props{Class: "agent-portable-mapping"}, html.Label(html.Props{For: "agent-portable-destination-" + d.ID}, ui.Text(d.Label)), html.Input(html.Props{ID: "agent-portable-destination-" + d.ID, Name: "destination_mapping[" + d.ID + "]", Type: "text", Raw: map[string]any{"data-destination-id": d.ID, "data-mapping-kind": d.Kind, "data-source-id": d.SourceID}})))
		}
		children = append(children, html.Form(html.Props{ID: "agent-portable-import", Raw: map[string]any{"data-portable-action": "import", "aria-describedby": "agent-portable-import-help"}}, html.Label(html.Props{For: "agent-portable-target"}, ui.Text(agentRPText(locale, "destination_mappings"))), html.Select(html.Props{ID: "agent-portable-target", Name: "destination_manifest_id"}, manifestOptions...), html.Label(html.Props{For: "agent-portable-body"}, ui.Text(agentRPText(locale, "import_body"))), html.Textarea(html.Props{ID: "agent-portable-body", Name: "manifest", Rows: 8, Raw: map[string]any{"aria-describedby": "agent-portable-import-help"}}, ui.Text(snapshot.Definition)), html.P(html.Props{ID: "agent-portable-import-help", Class: "muted"}, ui.Text(agentRPText(locale, "import_help"))), html.Fieldset(html.Props{}, html.Legend(html.Props{}, ui.Text(agentRPText(locale, "destination_mappings"))), html.Div(html.Props{}, mapping...)), html.Button(html.Props{Type: "submit", Class: "button primary"}, ui.Text(agentRPText(locale, "import")))))
	}
	if snapshot.CanImport || snapshot.CanExport {
		children = append(children, html.Form(html.Props{ID: "agent-portable-review", Raw: map[string]any{"data-portable-action": "read"}},
			html.Label(html.Props{For: "agent-portable-draft-id"}, ui.Text(agentRPText(locale, "draft_id"))), html.Input(html.Props{ID: "agent-portable-draft-id", Name: "definition_id", Type: "text"}),
			html.Button(html.Props{Type: "submit", Class: "button secondary"}, ui.Text(agentRPText(locale, "review_draft")))))
	}
	if snapshot.Draft != nil {
		children = append(children, html.Section(html.Props{ID: "agent-portable-draft", Aria: map[string]string{"labelledby": "agent-portable-draft-title"}},
			html.H4(html.Props{ID: "agent-portable-draft-title"}, ui.Text(agentRPText(locale, "review_draft"))), html.P(html.Props{}, html.Code(html.Props{}, ui.Text(snapshot.Draft.ID)), ui.Text(fmt.Sprintf(" · %d", snapshot.Draft.Version))), html.P(html.Props{}, ui.Text(snapshot.Draft.Purpose)), html.Pre(html.Props{Class: "agent-portable-draft-instructions"}, ui.Text(snapshot.Draft.Instructions))))
	}
	return html.Section(html.Props{ID: "agent-portable-panel"}, children...)
}

func agentRPText(locale LocaleContext, key string) string {
	if key == "setup_required" {
		return [3]string{"Review and publish a persona, then add a conversation installation before previewing a rollout.", "Prüfen und veröffentlichen Sie eine Persona und installieren Sie sie in einer Unterhaltung, bevor Sie einen Rollout vorschauen.", "راجع الشخصية وانشرها، ثم أضف تثبيتًا في محادثة قبل معاينة التوزيع."}[agentRPLocaleIndex(locale)]
	}
	if key == "draft_id" {
		return [3]string{"Imported draft identifier", "Kennung des importierten Entwurfs", "معرف المسودة المستوردة"}[agentRPLocaleIndex(locale)]
	}
	if key == "review_draft" {
		return [3]string{"Review imported draft", "Importierten Entwurf prüfen", "مراجعة المسودة المستوردة"}[agentRPLocaleIndex(locale)]
	}
	values := map[string][3]string{"title": {"Agent rollout and portable definitions", "Agenten-Rollout und portable Definitionen", "توزيع الوكيل والتعريفات المحمولة"}, "rollout": {"Version rollout", "Versions-Rollout", "توزيع الإصدار"}, "portable": {"Portable definitions", "Portable Definitionen", "التعريفات المحمولة"}, "ready": {"Ready for an authorized preview.", "Bereit für eine autorisierte Vorschau.", "جاهز للمعاينة المصرح بها."}, "unavailable": {"This control is unavailable for your current access.", "Diese Steuerung ist für Ihren aktuellen Zugriff nicht verfügbar.", "هذا التحكم غير متاح لصلاحياتك الحالية."}, "persona": {"Agent persona", "Agentenpersona", "شخصية الوكيل"}, "target_version": {"Target version", "Zielversion", "الإصدار المستهدف"}, "batch_limit": {"Batch limit", "Batch-Limit", "حد الدفعة"}, "installations": {"Installations", "Installationen", "التثبيتات"}, "canaries": {"Canary installations", "Canary-Installationen", "تثبيتات الكناري"}, "canary_help": {"Canaries stop separately before broad promotion.", "Canaries werden vor der breiten Freigabe separat angehalten.", "تتوقف نسخ الكناري منفصلة قبل الترويج العام."}, "preview_help": {"Select the exact current installations; preview does not activate anything.", "Wählen Sie die aktuellen Installationen exakt aus; die Vorschau aktiviert nichts.", "حدد التثبيتات الحالية بدقة؛ المعاينة لا تفعل شيئًا."}, "preview": {"Preview rollout", "Rollout vorschauen", "معاينة التوزيع"}, "plan": {"Preview plan", "Vorschauplan", "خطة المعاينة"}, "rollout_id": {"Rollout ID", "Rollout-ID", "معرف التوزيع"}, "digest": {"Digest", "Digest", "البصمة"}, "canary_count": {"Canaries", "Canaries", "نسخ الكناري"}, "candidates": {"Candidates", "Kandidaten", "المرشحون"}, "approve": {"Approve", "Genehmigen", "موافقة"}, "promote": {"Promote", "Freigeben", "ترويج"}, "advance": {"Advance", "Fortsetzen", "تقدم"}, "manifest": {"Manifest", "Manifest", "البيان"}, "manifest_version": {"Manifest version", "Manifestversion", "إصدار البيان"}, "export": {"Export portable definition", "Portable Definition exportieren", "تصدير تعريف محمول"}, "import_body": {"Portable manifest", "Portables Manifest", "البيان المحمول"}, "import_help": {"Import creates a reviewable draft and never grants authority.", "Der Import erstellt einen prüfbaren Entwurf und erteilt niemals Berechtigungen.", "ينشئ الاستيراد مسودة قابلة للمراجعة ولا يمنح صلاحيات أبدًا."}, "destination_mappings": {"Destination mappings", "Zielzuordnungen", "تعيينات الوجهة"}, "import": {"Import as draft", "Als Entwurf importieren", "استيراد كمسودة"}}
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
