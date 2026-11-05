package productui

import (
	"fmt"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset"
)

// smbAsset004Labels carries the fixed maintenance-card labels. Quantities
// stay canonical API decimal strings in every locale.
var smbAsset004Labels = map[string]map[string]string{
	"en-US": {
		"title": "Equipment maintenance", "status": "Status",
		"available": "Available", "warning": "Due warning", "outOfService": "Out of service", "inService": "In service",
		"blocked": "Out of service — bookings are held until verified release.",
		"nextDue": "Next due", "cycle": "Service cycle", "meter": "Meter", "template": "Service template", "project": "Project",
		"standalone": "Standalone — no project", "work": "Outstanding work", "noWork": "No open service work.",
		"release": "Release review", "noCompletion": "No service completion recorded.",
		"timeline": "Timeline", "historyNote": "Service evidence controls eligibility; task completion alone never releases equipment.",
	},
	"de-DE": {
		"title": "Anlagenwartung", "status": "Status",
		"available": "Verfügbar", "warning": "Fälligkeitswarnung", "outOfService": "Außer Betrieb", "inService": "In Wartung",
		"blocked": "Außer Betrieb — Buchungen bleiben bis zur geprüften Freigabe gesperrt.",
		"nextDue": "Nächste Fälligkeit", "cycle": "Wartungszyklus", "meter": "Zähler", "template": "Wartungsvorlage", "project": "Projekt",
		"standalone": "Eigenständig — kein Projekt", "work": "Offene Arbeiten", "noWork": "Keine offenen Wartungsarbeiten.",
		"release": "Freigabeprüfung", "noCompletion": "Kein Wartungsabschluss erfasst.",
		"timeline": "Verlauf", "historyNote": "Servicenachweise steuern die Einsatzfähigkeit; die Aufgabenerledigung allein gibt nie frei.",
	},
	"ar": {
		"title": "صيانة المعدات", "status": "الحالة",
		"available": "متاح", "warning": "تحذير استحقاق", "outOfService": "خارج الخدمة", "inService": "قيد الصيانة",
		"blocked": "خارج الخدمة — الحجوزات معلقة حتى الإصدار المتحقق منه.",
		"nextDue": "الاستحقاق التالي", "cycle": "دورة الصيانة", "meter": "العداد", "template": "قالب الصيانة", "project": "المشروع",
		"standalone": "مستقل — لا يوجد مشروع", "work": "الأعمال المعلقة", "noWork": "لا توجد أعمال صيانة مفتوحة.",
		"release": "مراجعة الإصدار", "noCompletion": "لم يتم تسجيل إنجاز صيانة.",
		"timeline": "الجدول الزمني", "historyNote": "أدلة الصيانة تتحكم في الأهلية؛ وإنجاز المهمة وحده لا يفرج عن المعدة أبدا.",
	},
}

func smbAsset004Text(locale, key string) string {
	if labels, ok := smbAsset004Labels[locale]; ok {
		if text, ok := labels[key]; ok {
			return text
		}
	}
	return smbAsset004Labels["en-US"][key]
}

// SMBAssetMaintenanceCard renders the Equipment Maintenance tab: next due,
// service status with the out-of-service banner, outstanding work, release
// review and the service timeline. The heading is the post-action focus
// target; quantities render as canonical decimal strings.
func SMBAssetMaintenanceCard(plan asset.MaintenancePlan, serviceability asset.Serviceability, work *asset.ServiceWorkItem, completion *asset.ServiceCompletion, events []asset.ServiceEvent, meter asset.MeterState, locale string) ui.Node {
	text := func(key string) string { return smbAsset004Text(locale, key) }
	statusKey := "available"
	switch serviceability.State {
	case asset.ServiceDueWarning:
		statusKey = "warning"
	case asset.ServiceOutOfService:
		statusKey = "outOfService"
	case asset.ServiceInService:
		statusKey = "inService"
	}
	props := html.Props{
		Class: "smb-asset-maintenance-card",
		ID:    "smb-asset-maintenance-" + plan.Asset.Id,
		Data:  map[string]string{"asset-id": plan.Asset.String(), "service-state": string(serviceability.State), "service-cycle": fmt.Sprintf("%d", serviceability.Cycle)},
	}
	if strings.HasPrefix(locale, "ar") {
		props.Lang = "ar"
		props.Dir = "rtl"
	} else {
		props.Lang = locale
	}
	meterText := fmt.Sprintf("epoch %d reading %d / threshold %d", meter.Epoch, meter.LastReading, plan.MeterThreshold)
	project := text("standalone")
	if strings.TrimSpace(plan.ProjectRef) != "" {
		project = plan.ProjectRef
	}
	nextDue := plan.NextDue.UTC().Format("2006-01-02")
	nodes := []ui.Node{
		html.H2(html.Props{Class: "smb-asset-maintenance-title", TabIndex: -1}, ui.Text(text("title"))),
		html.P(html.Props{Class: "smb-asset-maintenance-status", Role: "status"}, ui.Text(text("status")+": "+text(statusKey))),
	}
	if serviceability.State == asset.ServiceOutOfService || serviceability.State == asset.ServiceInService {
		nodes = append(nodes, html.P(html.Props{Class: "smb-asset-maintenance-blocked", Role: "alert"}, ui.Text(text("blocked"))))
	}
	facts := [][2]string{
		{text("nextDue"), nextDue},
		{text("cycle"), fmt.Sprintf("%d", serviceability.Cycle)},
		{text("meter"), meterText},
		{text("template"), plan.TemplateRevision},
		{text("project"), project},
	}
	factRows := make([]ui.Node, 0, len(facts))
	for _, row := range facts {
		factRows = append(factRows, html.Tr(html.Props{},
			html.Th(html.Props{Class: "smb-asset-maintenance-fact-name"}, ui.Text(row[0])),
			html.Td(html.Props{Class: "smb-asset-maintenance-fact-value"}, ui.Text(row[1]))))
	}
	nodes = append(nodes, html.Table(html.Props{Class: "smb-asset-maintenance-facts"}, html.Tbody(html.Props{}, factRows...)))
	if work == nil {
		nodes = append(nodes, html.P(html.Props{Class: "smb-asset-maintenance-work-empty", Role: "status"}, ui.Text(text("noWork"))))
	} else {
		nodes = append(nodes, html.Div(html.Props{},
			html.H3(html.Props{}, ui.Text(text("work"))),
			html.P(html.Props{Class: "smb-asset-maintenance-work", Role: "status"},
				ui.Text(fmt.Sprintf("%s — %s — %s", work.Trigger, work.Status, work.ResultExplanation)))))
	}
	if completion == nil {
		nodes = append(nodes, html.P(html.Props{Class: "smb-asset-maintenance-release-empty", Role: "status"}, ui.Text(text("noCompletion"))))
	} else {
		parts := "no parts"
		if completion.PartsReceipt != nil {
			parts = completion.PartsReceipt.ReceiptID
		}
		nodes = append(nodes, html.Div(html.Props{},
			html.H3(html.Props{}, ui.Text(text("release"))),
			html.P(html.Props{Class: "smb-asset-maintenance-release", Role: "status"},
				ui.Text(fmt.Sprintf("%s — %s — %s", completion.Status, parts, completion.Evidence)))))
	}
	items := make([]ui.Node, 0, len(events))
	for _, event := range events {
		line := event.Kind
		if event.Detail != "" {
			line += " — " + event.Detail
		}
		items = append(items, html.Li(html.Props{Class: "smb-asset-maintenance-event"}, ui.Text(line)))
	}
	nodes = append(nodes, html.Div(html.Props{},
		html.H3(html.Props{}, ui.Text(text("timeline"))),
		html.Ol(html.Props{Class: "smb-asset-maintenance-timeline"}, items...)))
	nodes = append(nodes, html.P(html.Props{Class: "smb-asset-maintenance-history-note"}, ui.Text(text("historyNote"))))
	return html.Section(props, nodes...)
}
