package productui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
)

// ProjectWorkflowConfigState distinguishes an authorized empty configuration
// from loading and unavailable project service answers.
type ProjectWorkflowConfigState string

const (
	ProjectWorkflowConfigLoading     ProjectWorkflowConfigState = "loading"
	ProjectWorkflowConfigReady       ProjectWorkflowConfigState = "ready"
	ProjectWorkflowConfigUnavailable ProjectWorkflowConfigState = "unavailable"
	ProjectWorkflowConfigFailed      ProjectWorkflowConfigState = "failed"
)

// ProjectWorkflowDiagnostic is safe server-projected validation detail. It
// deliberately has no task field values or protected source text.
type ProjectWorkflowDiagnostic struct{ Code, Path, Message string }

type ProjectWorkflowDiff struct{ Path, Before, After string }

// ProjectWorkflowPreview is the server's review answer. Digests are opaque
// and must be copied back unchanged when the host invokes publish.
type ProjectWorkflowPreview struct {
	DraftRevision                       uint64
	ReviewedDigest, MigrationPlanDigest string
	Valid                               bool
	AffectedTaskCount                   int
	AffectedTaskIDs                     []string
	Diff                                []ProjectWorkflowDiff
	Diagnostics                         []ProjectWorkflowDiagnostic
}

type ProjectWorkflowSaveRequest struct {
	ProjectID, IdempotencyKey string
	ExpectedDraftRevision     uint64
	Configuration             projectworkflow.Config
}

type ProjectWorkflowPreviewRequest struct {
	ProjectID                     string
	ExpectedDraftRevision         uint64
	StatusMappings, FieldMappings map[string]string
}

type ProjectWorkflowPublishRequest struct {
	ProjectID, ReviewedDigest, MigrationPlanDigest, IdempotencyKey string
	ExpectedDraftRevision, ExpectedProjectRevision                 uint64
	StatusMappings, FieldMappings                                  map[string]string
}

// ProjectWorkflowConfigProps is a transport-neutral projection. The host
// owns persistence and authorization; nil callbacks produce disabled controls.
type ProjectWorkflowConfigProps struct {
	Locale                                LocaleContext
	ProjectID                             string
	State                                 ProjectWorkflowConfigState
	DraftRevision, PublishedRevision      uint64
	Draft                                 projectworkflow.Config
	Preview                               ProjectWorkflowPreview
	CanConfigure, CanPublish              bool
	OnSave                                func(ProjectWorkflowSaveRequest)
	OnPreview                             func(ProjectWorkflowPreviewRequest)
	OnPublish                             func(ProjectWorkflowPublishRequest)
	IdempotencyKey, PublishIdempotencyKey string
}

type projectWorkflowConfigCopy struct {
	title, description, loading, unavailable, failed, draft, published  string
	statuses, fields, columns, transitions, taskTypes, name, validation string
	migration, changes, affected, noAffected, noChanges, preview, save  string
	publish, publishUnavailable, reviewFirst, reviewed, valid, invalid  string
	path, before, after, noDiagnostics, noDiff                          string
}

func projectWorkflowConfigText(locale LocaleContext) projectWorkflowConfigCopy {
	if strings.EqualFold(locale.Resolved, "de-DE") {
		return projectWorkflowConfigCopy{
			title: "Projekt-Workflow konfigurieren", description: "Bearbeiten Sie den Entwurf, prüfen Sie die genaue Änderung und veröffentlichen Sie nur die geprüfte Version.", loading: "Workflow-Konfiguration wird geladen …", unavailable: "Workflow-Konfiguration ist in dieser Ansicht nicht verfügbar.", failed: "Workflow-Konfiguration konnte nicht geladen werden. Versuchen Sie es später erneut.", draft: "Entwurf", published: "Veröffentlicht", statuses: "Status", fields: "Felder", columns: "Spalten", transitions: "Übergänge", taskTypes: "Aufgabentypen", name: "Name", validation: "Validierung", migration: "Migrationsvorschau", changes: "Genaue Änderungen", affected: "Betroffene Aufgaben", noAffected: "Keine betroffenen Aufgaben", noChanges: "Keine Änderungen gemeldet", preview: "Vorschau erstellen", save: "Entwurf speichern", publish: "Geprüfte Version veröffentlichen", publishUnavailable: "Veröffentlichung nicht verfügbar", reviewFirst: "Prüfen Sie zuerst diese Vorschau; die Veröffentlichung bleibt bis dahin deaktiviert.", reviewed: "Vorschau geprüft", valid: "Gültig", invalid: "Korrekturen erforderlich", path: "Pfad", before: "Vorher", after: "Nachher", noDiagnostics: "Keine Validierungsfehler", noDiff: "Keine Änderungen",
		}
	}
	if strings.EqualFold(locale.Resolved, "ar") {
		return projectWorkflowConfigCopy{
			title: "تهيئة سير عمل المشروع", description: "حرّر المسودة، وراجع التغيير الدقيق، وانشر النسخة التي تمت مراجعتها فقط.", loading: "جارٍ تحميل تهيئة سير العمل …", unavailable: "تهيئة سير العمل غير متاحة في هذا العرض.", failed: "تعذر تحميل تهيئة سير العمل. حاول مرة أخرى لاحقًا.", draft: "مسودة", published: "منشور", statuses: "الحالات", fields: "الحقول", columns: "الأعمدة", transitions: "الانتقالات", taskTypes: "أنواع المهام", name: "الاسم", validation: "التحقق", migration: "معاينة الترحيل", changes: "التغييرات الدقيقة", affected: "المهام المتأثرة", noAffected: "لا توجد مهام متأثرة", noChanges: "لم يتم الإبلاغ عن تغييرات", preview: "إنشاء المعاينة", save: "حفظ المسودة", publish: "نشر النسخة المراجعة", publishUnavailable: "النشر غير متاح", reviewFirst: "راجع هذه المعاينة أولًا؛ سيظل النشر معطلًا حتى ذلك الحين.", reviewed: "تمت مراجعة المعاينة", valid: "صالح", invalid: "يلزم التصحيح", path: "المسار", before: "قبل", after: "بعد", noDiagnostics: "لا توجد أخطاء تحقق", noDiff: "لا توجد تغييرات",
		}
	}
	return projectWorkflowConfigCopy{title: "Configure project workflow", description: "Edit the draft, review the exact change, and publish only the reviewed version.", loading: "Loading workflow configuration …", unavailable: "Workflow configuration is unavailable in this view.", failed: "Workflow configuration could not be loaded. Try again later.", draft: "Draft", published: "Published", statuses: "Statuses", fields: "Fields", columns: "Columns", transitions: "Transitions", taskTypes: "Task types", name: "Name", validation: "Validation", migration: "Migration preview", changes: "Exact changes", affected: "Affected tasks", noAffected: "No affected tasks", noChanges: "No changes reported", preview: "Preview changes", save: "Save draft", publish: "Publish reviewed version", publishUnavailable: "Publishing unavailable", reviewFirst: "Review this preview first; publishing stays disabled until then.", reviewed: "Preview reviewed", valid: "Valid", invalid: "Corrections required", path: "Path", before: "Before", after: "After", noDiagnostics: "No validation errors", noDiff: "No changes"}
}

// ProjectWorkflowConfiguration renders the bounded manual editor and review
// gate. It has no mutation logic: callbacks are the only command path.
func ProjectWorkflowConfiguration(props ProjectWorkflowConfigProps) ui.Node {
	copy := projectWorkflowConfigText(props.Locale)
	label := copy.title
	if props.ProjectID != "" {
		label += " · " + props.ProjectID
	}
	saveSubmit := ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		if props.OnSave != nil && props.CanConfigure {
			props.OnSave(ProjectWorkflowSaveRequest{ProjectID: props.ProjectID, IdempotencyKey: props.IdempotencyKey, ExpectedDraftRevision: props.DraftRevision, Configuration: props.Draft})
		}
	})
	previewSubmit := ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		if props.OnPreview != nil && props.CanConfigure {
			props.OnPreview(ProjectWorkflowPreviewRequest{ProjectID: props.ProjectID, ExpectedDraftRevision: props.DraftRevision})
		}
	})
	publishSubmit := ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		preview := props.Preview
		if props.OnPublish != nil && props.CanPublish && preview.Valid && len(preview.Diagnostics) == 0 && preview.DraftRevision == props.DraftRevision && preview.ReviewedDigest != "" && preview.MigrationPlanDigest != "" {
			props.OnPublish(ProjectWorkflowPublishRequest{ProjectID: props.ProjectID, ReviewedDigest: preview.ReviewedDigest, MigrationPlanDigest: preview.MigrationPlanDigest, IdempotencyKey: props.PublishIdempotencyKey, ExpectedDraftRevision: preview.DraftRevision, ExpectedProjectRevision: props.PublishedRevision})
		}
	})
	if props.State == "" || props.State == ProjectWorkflowConfigLoading {
		return projectWorkflowState(label, copy.loading, "status")
	}
	if props.State == ProjectWorkflowConfigUnavailable {
		return projectWorkflowState(label, copy.unavailable, "status")
	}
	if props.State == ProjectWorkflowConfigFailed {
		return projectWorkflowState(label, copy.failed, "alert")
	}

	config := props.Draft
	children := []ui.Node{
		html.Header(html.Props{Class: "project-workflow-config-header"}, html.H2(html.Props{Text: copy.title}), html.P(html.Props{Class: "muted", Text: copy.description})),
		html.P(html.Props{Class: "project-workflow-revisions", Text: fmt.Sprintf("%s %d · %s %d", copy.draft, props.DraftRevision, copy.published, props.PublishedRevision)}),
	}
	children = append(children, html.Form(html.Props{Class: "project-workflow-editor", OnSubmit: saveSubmit, Data: map[string]string{"projectui-action": "save-workflow-draft", "project-id": props.ProjectID, "draft-revision": strconv.FormatUint(props.DraftRevision, 10)}},
		projectWorkflowSection(copy.statuses, projectWorkflowStatuses(config.Statuses, copy)), projectWorkflowSection(copy.fields, projectWorkflowFields(config.Fields, copy)), projectWorkflowSection(copy.columns, projectWorkflowColumns(config.Columns, copy)),
		html.Div(html.Props{Class: "project-workflow-editor-meta"}, html.Span(html.Props{Text: fmt.Sprintf("%s: %d", copy.transitions, len(config.Transitions))}), html.Span(html.Props{Text: fmt.Sprintf("%s: %d", copy.taskTypes, len(config.TaskTypes))})),
		html.Button(html.Props{Class: "button secondary", Type: "submit", Disabled: !props.CanConfigure || props.OnSave == nil, Text: copy.save}),
	))
	children = append(children, projectWorkflowReview(props, copy, previewSubmit, publishSubmit))
	return html.Section(html.Props{Class: "project-workflow-config", Role: "region", Aria: map[string]string{"label": label}}, children...)
}

func projectWorkflowState(label, message, role string) ui.Node {
	return html.Section(html.Props{Class: "project-workflow-config project-workflow-state", Role: role, Aria: map[string]string{"label": label}}, html.H2(html.Props{Text: label}), html.P(html.Props{Text: message}))
}
func projectWorkflowSection(title string, content ui.Node) ui.Node {
	return html.Fieldset(html.Props{Class: "project-workflow-section"}, html.Legend(html.Props{Text: title}), content)
}

func projectWorkflowStatuses(rows []projectworkflow.Status, copy projectWorkflowConfigCopy) ui.Node {
	items := make([]ui.Node, 0, len(rows))
	for i, row := range rows {
		items = append(items, html.Div(html.Props{Class: "project-workflow-row"}, html.Label(html.Props{For: fmt.Sprintf("project-workflow-status-%d", i), Text: row.ID}), html.Input(html.Props{ID: fmt.Sprintf("project-workflow-status-%d", i), Name: fmt.Sprintf("status-name-%d", i), Value: row.Name, Required: true, Aria: map[string]string{"label": copy.name}}), html.Span(html.Props{Class: "project-workflow-value", Text: string(row.Category)})))
	}
	if len(items) == 0 {
		items = append(items, html.P(html.Props{Class: "muted", Text: copy.noChanges}))
	}
	return html.Div(html.Props{Class: "project-workflow-rows"}, items...)
}
func projectWorkflowFields(rows []projectworkflow.Field, copy projectWorkflowConfigCopy) ui.Node {
	items := make([]ui.Node, 0, len(rows))
	for _, row := range rows {
		items = append(items, html.Div(html.Props{Class: "project-workflow-row"}, html.Span(html.Props{Text: row.ID}), html.Span(html.Props{Text: row.Name}), html.Span(html.Props{Class: "project-workflow-value", Text: string(row.Type)})))
	}
	if len(items) == 0 {
		items = append(items, html.P(html.Props{Class: "muted", Text: copy.noChanges}))
	}
	return html.Div(html.Props{Class: "project-workflow-rows", Aria: map[string]string{"label": copy.fields}}, items...)
}
func projectWorkflowColumns(rows []projectworkflow.Column, copy projectWorkflowConfigCopy) ui.Node {
	items := make([]ui.Node, 0, len(rows))
	for i, row := range rows {
		items = append(items, html.Div(html.Props{Class: "project-workflow-row"}, html.Label(html.Props{For: fmt.Sprintf("project-workflow-column-%d", i), Text: row.ID}), html.Input(html.Props{ID: fmt.Sprintf("project-workflow-column-%d", i), Name: fmt.Sprintf("column-name-%d", i), Value: row.Name, Required: true, Aria: map[string]string{"label": copy.name}}), html.Span(html.Props{Class: "project-workflow-value", Text: strings.Join(row.StatusIDs, ", ")})))
	}
	if len(items) == 0 {
		items = append(items, html.P(html.Props{Class: "muted", Text: copy.noChanges}))
	}
	return html.Div(html.Props{Class: "project-workflow-rows", Aria: map[string]string{"label": copy.columns}}, items...)
}

func projectWorkflowReview(props ProjectWorkflowConfigProps, copy projectWorkflowConfigCopy, previewSubmit, publishSubmit ui.Handler) ui.Node {
	preview := props.Preview
	valid := preview.Valid && len(preview.Diagnostics) == 0 && preview.ReviewedDigest != "" && preview.MigrationPlanDigest != "" && preview.DraftRevision == props.DraftRevision
	status := copy.invalid
	statusClass := "project-workflow-invalid"
	if valid {
		status, statusClass = copy.valid, "project-workflow-valid"
	}
	previewForm := html.Form(html.Props{Class: "project-workflow-preview", OnSubmit: previewSubmit, Data: map[string]string{"projectui-action": "preview-workflow-draft", "project-id": props.ProjectID, "expected-draft-revision": strconv.FormatUint(props.DraftRevision, 10)}}, html.H3(html.Props{Text: copy.migration}), html.P(html.Props{Class: statusClass, Role: "status", Text: status}), projectWorkflowDiagnostics(preview, copy), projectWorkflowDiff(preview, copy), projectWorkflowAffected(preview, copy), html.Button(html.Props{Class: "button secondary", Type: "submit", Disabled: !props.CanConfigure || props.OnPreview == nil, Text: copy.preview}))
	publishDisabled := !props.CanPublish || props.OnPublish == nil || !valid
	publishMessage := copy.reviewFirst
	if valid {
		publishMessage = copy.reviewed
	}
	if publishDisabled {
		return html.Div(html.Props{Class: "project-workflow-review"}, previewForm, html.Div(html.Props{Class: "project-workflow-publish"}, html.P(html.Props{Role: "status", Text: publishMessage}), html.Button(html.Props{Class: "button primary", Type: "button", Disabled: true, Text: copy.publishUnavailable})))
	}
	publish := html.Form(html.Props{Class: "project-workflow-publish", OnSubmit: publishSubmit, Data: map[string]string{"projectui-action": "publish-workflow-draft", "project-id": props.ProjectID, "expected-draft-revision": strconv.FormatUint(preview.DraftRevision, 10), "reviewed-digest": preview.ReviewedDigest, "migration-plan-digest": preview.MigrationPlanDigest}}, html.P(html.Props{Role: "status", Text: publishMessage}), html.Button(html.Props{Class: "button primary", Type: "submit", Text: copy.publish}))
	return html.Div(html.Props{Class: "project-workflow-review"}, previewForm, publish)
}
func projectWorkflowDiagnostics(preview ProjectWorkflowPreview, copy projectWorkflowConfigCopy) ui.Node {
	if len(preview.Diagnostics) == 0 {
		return html.P(html.Props{Class: "muted", Text: copy.noDiagnostics})
	}
	items := make([]ui.Node, 0, len(preview.Diagnostics))
	for _, d := range preview.Diagnostics {
		items = append(items, html.Li(html.Props{}, html.Strong(html.Props{Text: d.Code}), html.Span(html.Props{Text: " · " + d.Path}), html.P(html.Props{Text: d.Message})))
	}
	return html.Div(html.Props{Class: "project-workflow-diagnostics", Role: "alert", Aria: map[string]string{"label": copy.validation}}, html.H4(html.Props{Text: copy.validation}), html.Ul(html.Props{}, items...))
}
func projectWorkflowDiff(preview ProjectWorkflowPreview, copy projectWorkflowConfigCopy) ui.Node {
	if len(preview.Diff) == 0 {
		return html.P(html.Props{Class: "muted", Text: copy.noDiff})
	}
	rows := make([]ui.Node, 0, len(preview.Diff))
	for _, d := range preview.Diff {
		rows = append(rows, html.Li(html.Props{Class: "project-workflow-diff-row"}, html.Strong(html.Props{Text: d.Path}), html.Span(html.Props{Text: copy.before + ": " + d.Before}), html.Span(html.Props{Text: copy.after + ": " + d.After})))
	}
	return html.Div(html.Props{Class: "project-workflow-diff", Aria: map[string]string{"label": copy.changes}}, html.H4(html.Props{Text: copy.changes}), html.Ul(html.Props{}, rows...))
}
func projectWorkflowAffected(preview ProjectWorkflowPreview, copy projectWorkflowConfigCopy) ui.Node {
	if preview.AffectedTaskCount == 0 {
		return html.P(html.Props{Class: "muted", Text: copy.noAffected})
	}
	items := make([]ui.Node, 0, len(preview.AffectedTaskIDs))
	for _, id := range preview.AffectedTaskIDs {
		if strings.TrimSpace(id) != "" {
			items = append(items, html.Li(html.Props{}, html.Code(html.Props{Text: id})))
		}
	}
	return html.Div(html.Props{Class: "project-workflow-affected", Aria: map[string]string{"label": copy.affected}}, html.H4(html.Props{Text: fmt.Sprintf("%s (%d)", copy.affected, preview.AffectedTaskCount)}), html.Ul(html.Props{}, items...))
}

const projectWorkflowConfigurationStyles = `.project-workflow-config{display:grid;gap:1rem;max-inline-size:60rem}.project-workflow-config-header{display:grid;gap:.35rem}.project-workflow-config-header h2{margin:0}.project-workflow-revisions{color:var(--muted);font-size:.875rem}.project-workflow-editor,.project-workflow-review{display:grid;gap:.75rem;padding:1rem;border:1px solid var(--line,var(--control-border));border-radius:var(--hcm-radius-surface);background:var(--surface)}.project-workflow-section{display:grid;gap:.5rem;min-inline-size:0}.project-workflow-section legend{font-weight:700}.project-workflow-row{display:grid;grid-template-columns:minmax(8rem,1fr) minmax(10rem,2fr) minmax(7rem,1fr);gap:.5rem;align-items:center}.project-workflow-row input{min-inline-size:0}.project-workflow-value{color:var(--muted);overflow-wrap:anywhere}.project-workflow-editor-meta{display:flex;flex-wrap:wrap;gap:1rem;color:var(--muted);font-size:.875rem}.project-workflow-review{grid-template-columns:minmax(0,1fr) minmax(15rem,20rem)}.project-workflow-review h3,.project-workflow-review h4{margin-block:.25rem}.project-workflow-preview{display:grid;gap:.65rem;min-inline-size:0}.project-workflow-publish{display:grid;align-content:start;gap:.65rem;padding:1rem;border:1px solid var(--control-border);border-radius:var(--hcm-radius-control)}.project-workflow-valid{color:var(--hcm-color-success)}.project-workflow-invalid{color:var(--hcm-color-danger)}.project-workflow-diagnostics{padding:.65rem;border-radius:var(--hcm-radius-control);background:var(--hcm-color-danger-surface);color:var(--hcm-color-danger)}.project-workflow-diagnostics ul,.project-workflow-diff ul,.project-workflow-affected ul{margin:0;padding-inline-start:1.25rem}.project-workflow-diagnostics p{margin:.2rem 0 0}.project-workflow-diff-row{display:grid;gap:.2rem}.project-workflow-affected code{unicode-bidi:plaintext}@media (max-width:40rem){.project-workflow-review{grid-template-columns:1fr}.project-workflow-row{grid-template-columns:1fr}.project-workflow-row label{font-weight:650}.project-workflow-editor,.project-workflow-review{padding:.75rem}}@media (prefers-reduced-motion:reduce){.project-workflow-config *{scroll-behavior:auto;transition:none!important;animation:none!important}}`

func projectWorkflowConfigurationStylesheet() string { return projectWorkflowConfigurationStyles }
