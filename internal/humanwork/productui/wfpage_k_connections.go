package productui

import (
	"net/url"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/documents"
)

// WorkflowPageCapabilityAvailability is the one page-level capability
// snapshot. It aliases the shared journey capability verdict so page actions
// and UXBLIND unavailable states cannot drift into separate sources.
type WorkflowPageCapabilityAvailability = JourneyCapabilityAvailability

type WorkflowPageSupportChannel struct {
	ID, Name, Href string
	Eligible       bool
}

type WorkflowPageSupportProjection struct {
	Channel         WorkflowPageSupportChannel
	FallbackContact string
	Reference       chat.WorkflowPageReference
}

// ResolveWorkflowPageSupport prefers a workflow binding and falls back to the
// tenant default without copying any draft or run values into the projection.
func ResolveWorkflowPageSupport(workflow, tenantDefault WorkflowPageSupportChannel, reference chat.WorkflowPageReference, fallbackContact string) WorkflowPageSupportProjection {
	channel := workflow
	if strings.TrimSpace(channel.ID) == "" {
		channel = tenantDefault
	}
	return WorkflowPageSupportProjection{Channel: channel, FallbackContact: fallbackContact, Reference: reference}
}

func workflowPageConnectionText(locale, key string) string {
	values := map[string]map[string]string{
		"en-US": {
			"ask": "Ask in channel", "fallback": "Contact your workspace support team", "reference": "Workflow page reference",
			"restricted": "Restricted workflow reference", "stale": "Document may be out of date", "access": "Document unavailable to you",
			"follow_up": "Create follow-up task", "subject": "Subject", "position": "Position", "organization": "Organization",
			"page": "Workflow page", "approver": "Approval context",
		},
		"de-DE": {
			"ask": "Im Kanal fragen", "fallback": "Wenden Sie sich an das Supportteam Ihres Arbeitsbereichs", "reference": "Workflow-Seitenreferenz",
			"restricted": "Eingeschränkte Workflow-Referenz", "stale": "Dokument möglicherweise veraltet", "access": "Dokument für Sie nicht verfügbar",
			"follow_up": "Folgeaufgabe erstellen", "subject": "Betreff", "position": "Position", "organization": "Organisation",
			"page": "Workflow-Seite", "approver": "Genehmigungskontext",
		},
		"ar": {
			"ask": "اسأل في القناة", "fallback": "تواصل مع فريق دعم مساحة العمل", "reference": "مرجع صفحة سير العمل",
			"restricted": "مرجع سير عمل مقيّد", "stale": "قد يكون المستند قديمًا", "access": "المستند غير متاح لك",
			"follow_up": "إنشاء مهمة متابعة", "subject": "الموضوع", "position": "المنصب", "organization": "المنظمة",
			"page": "صفحة سير العمل", "approver": "سياق الموافقة",
		},
	}
	locale = ResolveProductLocale(locale).Resolved
	if copy := values[locale]; copy != nil && copy[key] != "" {
		return copy[key]
	}
	return values["en-US"][key]
}

func RenderWorkflowPageDocumentLinks(view View, links []documents.WorkflowDocumentLink) ui.Node {
	children := make([]ui.Node, 0, len(links))
	for _, link := range links {
		if link.Readable && link.Href != "" {
			label := link.Title
			if link.Stale {
				label += " (" + workflowPageConnectionText(view.Locale.Resolved, "stale") + ")"
			}
			children = append(children, softwareLink(view.Navigate, html.Props{Class: "workflow-page-document-link", Data: map[string]string{"workflow-page-link": link.SlotID, "section": link.SectionID}}, link.Href, ui.Text(label)))
			continue
		}
		if link.RequestAccess {
			children = append(children, html.Span(html.Props{Class: "workflow-page-document-restricted", Role: "status", Data: map[string]string{"workflow-page-link": link.SlotID}}, ui.Text(workflowPageConnectionText(view.Locale.Resolved, "access"))))
		}
	}
	if len(children) == 0 {
		return nil
	}
	return html.Section(html.Props{Class: "workflow-page-document-links", Role: "region", Aria: map[string]string{"label": docsText(view.Locale.Resolved, "heading")}}, children...)
}

func RenderWorkflowPageSupport(view View, projection WorkflowPageSupportProjection) ui.Node {
	if !projection.Channel.Eligible || strings.TrimSpace(projection.Channel.ID) == "" || strings.TrimSpace(projection.Channel.Href) == "" {
		if strings.TrimSpace(projection.FallbackContact) == "" {
			return nil
		}
		return html.P(html.Props{Class: "workflow-page-support-fallback", Role: "status"}, ui.Text(workflowPageConnectionText(view.Locale.Resolved, "fallback")+": "+projection.FallbackContact))
	}
	children := []ui.Node{
		softwareLink(view.Navigate, html.Props{Class: "workflow-page-support-link", Data: map[string]string{"workflow-page-support": projection.Channel.ID}}, projection.Channel.Href, ui.Text(workflowPageConnectionText(view.Locale.Resolved, "ask"))),
	}
	if reference, ok := chat.NewWorkflowPageReference(projection.Reference); ok {
		children = append(children, html.Span(html.Props{Class: "workflow-page-reference-chip", Data: map[string]string{"chat-reference-kind": string(reference.Kind), "chat-reference-id": reference.ID}}, ui.Text(workflowPageConnectionText(view.Locale.Resolved, "reference"))))
	}
	return html.Section(html.Props{Class: "workflow-page-support", Role: "region", Aria: map[string]string{"label": workflowPageConnectionText(view.Locale.Resolved, "ask")}}, children...)
}

type WorkflowPageContextLink struct {
	Label, Href string
	Readable    bool
}

type WorkflowPageApprovalNotification struct {
	WorkflowName, SubjectName, PageVersionHref string
	Readable                                   bool
}

type WorkflowPageContextProjection struct {
	WorkflowName, PageID            string
	PageVersion                     int64
	Subject, Position, Organization WorkflowPageContextLink
	FollowUpTask                    WorkflowPageContextLink
	Approval                        WorkflowPageApprovalNotification
}

// ProjectWorkflowPageContext applies authorization before exposing a profile,
// position, organization or project selector to the page.
func ProjectWorkflowPageContext(input WorkflowPageContextProjection) WorkflowPageContextProjection {
	result := input
	redact := func(link *WorkflowPageContextLink) {
		if !link.Readable {
			*link = WorkflowPageContextLink{}
		}
	}
	redact(&result.Subject)
	redact(&result.Position)
	redact(&result.Organization)
	redact(&result.FollowUpTask)
	if !result.Approval.Readable {
		result.Approval = WorkflowPageApprovalNotification{}
	}
	return result
}

func RenderWorkflowPageContext(view View, context WorkflowPageContextProjection) ui.Node {
	context = ProjectWorkflowPageContext(context)
	children := make([]ui.Node, 0, 6)
	add := func(link WorkflowPageContextLink, key string) {
		if link.Readable && link.Href != "" && link.Label != "" {
			children = append(children, html.Div(html.Props{Class: "workflow-page-context-link"}, html.Span(html.Props{Class: "workflow-page-context-label"}, ui.Text(workflowPageConnectionText(view.Locale.Resolved, key))), softwareLink(view.Navigate, html.Props{}, link.Href, ui.Text(link.Label))))
		}
	}
	add(context.Subject, "subject")
	add(context.Position, "position")
	add(context.Organization, "organization")
	if context.FollowUpTask.Readable {
		children = append(children, softwareLink(view.Navigate, html.Props{Class: "workflow-page-follow-up"}, context.FollowUpTask.Href, ui.Text(workflowPageConnectionText(view.Locale.Resolved, "follow_up"))))
	}
	if context.Approval.Readable {
		approval := context.Approval.WorkflowName + ": " + context.Approval.SubjectName
		approvalChildren := []ui.Node{html.Span(html.Props{}, ui.Text(workflowPageConnectionText(view.Locale.Resolved, "approver"))), ui.Text(approval)}
		if strings.TrimSpace(context.Approval.PageVersionHref) != "" {
			approvalChildren = append(approvalChildren, softwareLink(view.Navigate, html.Props{Class: "workflow-page-version-link"}, context.Approval.PageVersionHref, ui.Text(workflowPageConnectionText(view.Locale.Resolved, "page"))))
		}
		children = append(children, html.Div(html.Props{Class: "workflow-page-approval-context", Data: map[string]string{"page-version": context.Approval.PageVersionHref}}, approvalChildren...))
	}
	if len(children) == 0 {
		return nil
	}
	return html.Section(html.Props{Class: "workflow-page-context", Role: "region", Aria: map[string]string{"label": workflowPageConnectionText(view.Locale.Resolved, "page")}}, children...)
}

func WorkflowPagePersonLink(id, label string) WorkflowPageContextLink {
	if !safeWorkflowPageSelector(id) {
		return WorkflowPageContextLink{}
	}
	return WorkflowPageContextLink{Label: label, Href: "/workspace/app/person?person=" + url.QueryEscape(id), Readable: true}
}

func WorkflowPagePositionLink(id, label string) WorkflowPageContextLink {
	if !safeWorkflowPageSelector(id) {
		return WorkflowPageContextLink{}
	}
	return WorkflowPageContextLink{Label: label, Href: "/workspace/app/position?position_ref=" + url.QueryEscape(id), Readable: true}
}

func WorkflowPageOrganizationLink(id, label string) WorkflowPageContextLink {
	if !safeWorkflowPageSelector(id) {
		return WorkflowPageContextLink{}
	}
	return WorkflowPageContextLink{Label: label, Href: "/workspace/app/organization?selected=" + url.QueryEscape(id), Readable: true}
}

func WorkflowPageFollowUpLink(projectID, taskID, label string) WorkflowPageContextLink {
	href := ProjectTaskHref(projectID, taskID)
	if href == "" {
		return WorkflowPageContextLink{}
	}
	return WorkflowPageContextLink{Label: label, Href: href, Readable: true}
}

func safeWorkflowPageSelector(value string) bool {
	if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "/?#&<>\"\r\n") {
		return false
	}
	return len(value) <= 128
}

// RenderWorkflowPageConnections composes optional products around the page's
// primary form. Unavailable capabilities contribute no dead controls; when no
// connection is published, one shared unavailable state is shown.
func RenderWorkflowPageConnections(view View, availability WorkflowPageCapabilityAvailability, documentLinks []documents.WorkflowDocumentLink, support WorkflowPageSupportProjection, context WorkflowPageContextProjection) ui.Node {
	children := make([]ui.Node, 0, 4)
	if availability.Documents {
		if node := RenderWorkflowPageDocumentLinks(view, documentLinks); node != nil {
			children = append(children, node)
		}
	}
	if availability.Chat {
		if node := RenderWorkflowPageSupport(view, support); node != nil {
			children = append(children, node)
		}
	}
	if availability.Projects {
		if context.FollowUpTask.Readable {
			children = append(children, RenderWorkflowPageContext(view, WorkflowPageContextProjection{FollowUpTask: context.FollowUpTask}))
		}
	}
	if len(children) == 0 && !availability.Documents && !availability.Chat && !availability.Projects {
		return capabilityUnavailablePanel(view.Locale)
	}
	return html.Div(html.Props{Class: "workflow-page-connections"}, children...)
}
