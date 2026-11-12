package productui

import (
	"fmt"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderbilling/billview"
)

// smbBill002Labels carries the fixed drawer labels. Quantities and money
// stay canonical server strings in every locale.
var smbBill002Labels = map[string]map[string]string{
	"en-US": {
		"title": "Add approved work", "eligible": "Eligible", "attention": "Needs attention",
		"source": "Source", "date": "Date", "approved": "Approved quantity", "billed": "Already billed",
		"available": "Available", "rate": "Rate", "amount": "Amount", "select": "Bill",
		"quantity": "Quantity to bill", "preview": "Preview", "previewEmpty": "Select sources to preview server totals.",
		"conflict": "Availability changed while you worked. Your selections were kept; review before submitting.",
		"review":   "Review draft", "noSources": "No approved sources in this period.",
		"unitNote": "Quantities are exact server values.",
	},
	"de-DE": {
		"title": "Genehmigte Arbeit hinzufügen", "eligible": "Zulässig", "attention": "Erfordert Aufmerksamkeit",
		"source": "Quelle", "date": "Datum", "approved": "Genehmigte Menge", "billed": "Bereits abgerechnet",
		"available": "Verfügbar", "rate": "Satz", "amount": "Betrag", "select": "Abrechnen",
		"quantity": "Abzurechnende Menge", "preview": "Vorschau", "previewEmpty": "Wählen Sie Quellen für die Server-Summen.",
		"conflict": "Die Verfügbarkeit hat sich geändert. Ihre Auswahl wurde beibehalten; bitte prüfen Sie vor dem Absenden.",
		"review":   "Entwurf prüfen", "noSources": "Keine genehmigten Quellen in diesem Zeitraum.",
		"unitNote": "Mengen sind exakte Serverwerte.",
	},
	"ar": {
		"title": "إضافة عمل معتمد", "eligible": "مؤهل", "attention": "يحتاج إلى انتباه",
		"source": "المصدر", "date": "التاريخ", "approved": "الكمية المعتمدة", "billed": "تمت فوترته",
		"available": "المتاح", "rate": "السعر", "amount": "المبلغ", "select": "فوترة",
		"quantity": "الكمية المراد فوترتها", "preview": "معاينة", "previewEmpty": "حدد المصادر لمعاينة إجماليات الخادم.",
		"conflict": "تغيرت الإتاحة أثناء عملك. تم الاحتفاظ بتحديداتك؛ راجع قبل الإرسال.",
		"review":   "مراجعة المسودة", "noSources": "لا توجد مصادر معتمدة في هذه الفترة.",
		"unitNote": "الكميات قيم دقيقة من الخادم.",
	},
}

func smbBill002Text(locale, key string) string {
	if labels, ok := smbBill002Labels[locale]; ok {
		if text, ok := labels[key]; ok {
			return text
		}
	}
	return smbBill002Labels["en-US"][key]
}

// SMBBillClaimsCard renders the Job Billing tab "Add approved work" drawer
// content: eligible sources with selection plus quantity entry, sources
// needing attention with exact reasons, the server totals preview, an
// availability-change notice that retains selections, and the review-draft
// link. The heading is the post-action focus target.
func SMBBillClaimsCard(views []billview.BillSourceView, preview *billview.BillPreview, conflict bool, draftLink, locale string) ui.Node {
	text := func(key string) string { return smbBill002Text(locale, key) }
	props := html.Props{Class: "smb-bill-claims-card", ID: "smb-bill-claims"}
	if strings.HasPrefix(locale, "ar") {
		props.Lang = "ar"
		props.Dir = "rtl"
	} else {
		props.Lang = locale
	}
	var eligible, attention []billview.BillSourceView
	for _, view := range views {
		if view.Eligible {
			eligible = append(eligible, view)
		} else {
			attention = append(attention, view)
		}
	}
	nodes := []ui.Node{
		html.H2(html.Props{Class: "smb-bill-claims-title", TabIndex: -1}, ui.Text(text("title"))),
	}
	if conflict {
		nodes = append(nodes, html.P(html.Props{Class: "smb-bill-claims-conflict", Role: "alert"}, ui.Text(text("conflict"))))
	}
	if len(views) == 0 {
		nodes = append(nodes, html.P(html.Props{Class: "smb-bill-claims-empty", Role: "status"}, ui.Text(text("noSources"))))
	}
	if len(eligible) > 0 {
		nodes = append(nodes,
			html.H3(html.Props{}, ui.Text(fmt.Sprintf("%s (%d)", text("eligible"), len(eligible)))),
			smbBill002Table(eligible, text, true))
	}
	if len(attention) > 0 {
		nodes = append(nodes,
			html.H3(html.Props{}, ui.Text(fmt.Sprintf("%s (%d)", text("attention"), len(attention)))),
			smbBill002Table(attention, text, false))
	}
	if preview == nil {
		nodes = append(nodes, html.P(html.Props{Class: "smb-bill-claims-preview-empty", Role: "status"}, ui.Text(text("previewEmpty"))))
	} else {
		nodes = append(nodes, html.P(html.Props{Class: "smb-bill-claims-preview", Role: "status"},
			ui.Text(text("preview")+": "+preview.BillableQty+" "+preview.BillableUnit+" = "+preview.Amount)))
	}
	if draftLink != "" {
		nodes = append(nodes, html.A(html.Props{Class: "smb-bill-claims-review", Href: draftLink}, ui.Text(text("review"))))
	}
	nodes = append(nodes, html.P(html.Props{Class: "smb-bill-claims-unit-note"}, ui.Text(text("unitNote"))))
	return html.Section(props, nodes...)
}

func smbBill002Table(views []billview.BillSourceView, text func(string) string, selectable bool) ui.Node {
	header := html.Tr(html.Props{},
		html.Th(html.Props{}, ui.Text(text("select"))),
		html.Th(html.Props{}, ui.Text(text("source"))),
		html.Th(html.Props{}, ui.Text(text("date"))),
		html.Th(html.Props{}, ui.Text(text("approved"))),
		html.Th(html.Props{}, ui.Text(text("billed"))),
		html.Th(html.Props{}, ui.Text(text("available"))),
		html.Th(html.Props{}, ui.Text(text("rate"))),
		html.Th(html.Props{}, ui.Text(text("amount"))),
	)
	rows := make([]ui.Node, 0, len(views))
	for i, view := range views {
		approved, billed, available := view.ApprovedQty, view.ClaimedQty, view.RemainingQty
		amount := view.RemainingAmount
		if view.UnitDimension == "money" {
			approved, billed, available = view.ApprovedAmount, view.ClaimedAmount, view.RemainingAmount
		}
		unit := view.Unit
		if view.UnitDimension == "money" {
			unit = ""
		} else if unit != "" {
			available += " " + unit
			approved += " " + unit
			billed += " " + unit
		}
		checkID := fmt.Sprintf("smb-bill-claims-check-%d", i)
		qtyID := fmt.Sprintf("smb-bill-claims-qty-%d", i)
		var check ui.Node
		if selectable {
			check = html.Div(html.Props{},
				html.Input(html.Props{Type: "checkbox", ID: checkID, Name: "source", Value: view.FamilyID}),
				html.Label(html.Props{For: checkID}, ui.Text(text("select")+" "+view.Description)),
				html.Label(html.Props{For: qtyID, Class: "smb-bill-claims-qty-label"}, ui.Text(text("quantity"))),
				html.Input(html.Props{Type: "text", ID: qtyID, Name: "quantity", Value: available, Raw: map[string]any{"inputmode": "decimal"}}))
		} else {
			check = html.P(html.Props{Class: "smb-bill-claims-reasons"}, ui.Text(strings.Join(view.Reasons, "; ")))
		}
		rows = append(rows, html.Tr(html.Props{},
			html.Td(html.Props{}, check),
			html.Td(html.Props{}, ui.Text(view.Description)),
			html.Td(html.Props{}, ui.Text(view.ServiceDate)),
			html.Td(html.Props{}, ui.Text(approved)),
			html.Td(html.Props{}, ui.Text(billed)),
			html.Td(html.Props{}, ui.Text(available)),
			html.Td(html.Props{}, ui.Text(smbBill002Rate(view))),
			html.Td(html.Props{}, ui.Text(amount))))
	}
	return html.Table(html.Props{Class: "smb-bill-claims-table"},
		html.Thead(html.Props{}, header),
		html.Tbody(html.Props{}, rows...))
}

func smbBill002Rate(view billview.BillSourceView) string {
	ref := strings.TrimSpace(view.RateRef)
	ref = strings.TrimPrefix(ref, "rate:")
	if ref == "" {
		return "—"
	}
	return ref
}
