package productui

import (
	"fmt"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderbilling/billview"
)

var smbBill008Labels = map[string]map[string]string{
	"en-US": {
		"title": "Create credit or adjustment", "lines": "Lines", "select": "Adjust",
		"original": "Original", "credited": "Previously credited", "available": "Available",
		"proposed": "Proposed", "quantity": "Quantity", "amount": "Amount", "tax": "Tax",
		"reason": "Reason (required)", "taxNote": "Tax uses the original treatment; no re-determination.",
		"previewEmpty": "Select lines to preview server totals.",
		"review":       "Review", "viewOriginal": "View original", "chain": "Adjustment chain",
		"noChain": "No adjustments yet.", "voidTitle": "Void invoice", "voidCta": "Void this invoice",
		"voidReason": "Void reason (required)", "voidBlocked": "Void unavailable",
		"unitNote": "Amounts are exact server values.",
	},
	"de-DE": {
		"title": "Gutschrift oder Korrektur erstellen", "lines": "Positionen", "select": "Anpassen",
		"original": "Original", "credited": "Bereits gutgeschrieben", "available": "Verfügbar",
		"proposed": "Vorgeschlagen", "quantity": "Menge", "amount": "Betrag", "tax": "Steuer",
		"reason": "Grund (erforderlich)", "taxNote": "Die Steuer folgt der ursprünglichen Behandlung; keine Neubestimmung.",
		"previewEmpty": "Wählen Sie Positionen für die Server-Summen.",
		"review":       "Prüfung", "viewOriginal": "Original anzeigen", "chain": "Korrekturverlauf",
		"noChain": "Noch keine Korrekturen.", "voidTitle": "Rechnung stornieren", "voidCta": "Diese Rechnung stornieren",
		"voidReason": "Stornogrund (erforderlich)", "voidBlocked": "Storno nicht verfügbar",
		"unitNote": "Beträge sind exakte Serverwerte.",
	},
	"ar": {
		"title": "إنشاء إشعار دائن أو تسوية", "lines": "البنود", "select": "تسوية",
		"original": "الأصلي", "credited": "سبق قيده", "available": "المتاح",
		"proposed": "المقترح", "quantity": "الكمية", "amount": "المبلغ", "tax": "الضريبة",
		"reason": "السبب (مطلوب)", "taxNote": "تتبع الضريبة المعاملة الأصلية؛ ولا توجد إعادة تحديد.",
		"previewEmpty": "حدد البنود لمعاينة إجماليات الخادم.",
		"review":       "مراجعة", "viewOriginal": "عرض الأصلي", "chain": "سلسلة التسويات",
		"noChain": "لا توجد تسويات بعد.", "voidTitle": "إلغاء الفاتورة", "voidCta": "إلغاء هذه الفاتورة",
		"voidReason": "سبب الإلغاء (مطلوب)", "voidBlocked": "الإلغاء غير متاح",
		"unitNote": "المبالغ قيم دقيقة من الخادم.",
	},
}

func smbBill008Text(locale, key string) string {
	if labels, ok := smbBill008Labels[locale]; ok {
		if text, ok := labels[key]; ok {
			return text
		}
	}
	return smbBill008Labels["en-US"][key]
}

// SMBBillAdjustmentCard renders the Invoice Actions adjustment drawer: line
// selection with original, previously credited, available and proposed
// treatment, the required reason, the pinned-tax disclosure, the issue
// review with the original link, the adjustment chain and the policy-gated
// void section. The heading is the post-action focus target.
func SMBBillAdjustmentCard(invoice billview.AdjustInvoice, preview *billview.AdjustPreview, chain []billview.Adjustment, originalLink string, voidEligible bool, voidReason, locale string) ui.Node {
	text := func(key string) string { return smbBill008Text(locale, key) }
	props := html.Props{Class: "smb-bill-adjust-card", ID: "smb-bill-adjust"}
	if strings.HasPrefix(locale, "ar") {
		props.Lang = "ar"
		props.Dir = "rtl"
	} else {
		props.Lang = locale
	}
	nodes := []ui.Node{
		html.H2(html.Props{Class: "smb-bill-adjust-title", TabIndex: -1}, ui.Text(text("title")+" "+invoice.Number)),
		html.H3(html.Props{}, ui.Text(text("lines"))),
		smbBill008LineTable(invoice, preview, text),
		html.Label(html.Props{For: "smb-bill-adjust-reason"}, ui.Text(text("reason"))),
		html.Input(html.Props{Type: "text", ID: "smb-bill-adjust-reason", Name: "reason", Required: true}),
		html.P(html.Props{Class: "smb-bill-adjust-tax-note", Role: "status"}, ui.Text(text("taxNote"))),
	}
	if preview == nil {
		nodes = append(nodes, html.P(html.Props{Class: "smb-bill-adjust-preview-empty", Role: "status"}, ui.Text(text("previewEmpty"))))
	} else {
		nodes = append(nodes, html.P(html.Props{Class: "smb-bill-adjust-review", Role: "status"},
			ui.Text(text("review")+": "+preview.TotalAmount+" — "+invoice.Number)))
	}
	if originalLink != "" {
		nodes = append(nodes, html.A(html.Props{Class: "smb-bill-adjust-original", Href: originalLink}, ui.Text(text("viewOriginal"))))
	}
	nodes = append(nodes, html.H3(html.Props{}, ui.Text(text("chain"))))
	if len(chain) == 0 {
		nodes = append(nodes, html.P(html.Props{Class: "smb-bill-adjust-chain-empty", Role: "status"}, ui.Text(text("noChain"))))
	} else {
		items := make([]ui.Node, 0, len(chain))
		for _, adjustment := range chain {
			items = append(items, html.Li(html.Props{},
				ui.Text(adjustment.Number+" "+adjustment.Kind+" "+adjustment.TotalAmount+" "+adjustment.Status)))
		}
		nodes = append(nodes, html.Ul(html.Props{Class: "smb-bill-adjust-chain"}, items...))
	}
	nodes = append(nodes, html.H3(html.Props{}, ui.Text(text("voidTitle"))))
	if voidEligible {
		nodes = append(nodes,
			html.Label(html.Props{For: "smb-bill-adjust-void-reason"}, ui.Text(text("voidReason"))),
			html.Input(html.Props{Type: "text", ID: "smb-bill-adjust-void-reason", Name: "void_reason", Required: true}),
			html.Button(html.Props{Class: "smb-bill-adjust-void-cta", Type: "submit"}, ui.Text(text("voidCta"))))
	} else {
		nodes = append(nodes, html.P(html.Props{Class: "smb-bill-adjust-void-blocked", Role: "status"},
			ui.Text(text("voidBlocked")+": "+voidReason)))
	}
	nodes = append(nodes, html.P(html.Props{Class: "smb-bill-adjust-unit-note"}, ui.Text(text("unitNote"))))
	return html.Section(props, nodes...)
}

func smbBill008LineTable(invoice billview.AdjustInvoice, preview *billview.AdjustPreview, text func(string) string) ui.Node {
	byID := map[string]billview.AdjustPreviewLine{}
	if preview != nil {
		for _, line := range preview.Lines {
			byID[line.LineID] = line
		}
	}
	header := html.Tr(html.Props{},
		html.Th(html.Props{}, ui.Text(text("select"))),
		html.Th(html.Props{}, ui.Text(text("lines"))),
		html.Th(html.Props{}, ui.Text(text("original"))),
		html.Th(html.Props{}, ui.Text(text("credited"))),
		html.Th(html.Props{}, ui.Text(text("available"))),
		html.Th(html.Props{}, ui.Text(text("proposed"))),
	)
	rows := make([]ui.Node, 0, len(invoice.Lines))
	for i, line := range invoice.Lines {
		original := line.Amount + " + " + line.TaxAmount
		credited, available, proposed := "—", line.Amount+" + "+line.TaxAmount, ""
		if row, ok := byID[line.LineID]; ok {
			credited = row.CreditedAmount + " + " + row.CreditedTax
			available = row.AvailableAmount + " + " + row.AvailableTax
			proposed = row.ProposedAmount + " + " + row.ProposedTax
		}
		checkID := fmt.Sprintf("smb-bill-adjust-check-%d", i)
		amountID := fmt.Sprintf("smb-bill-adjust-amount-%d", i)
		rows = append(rows, html.Tr(html.Props{},
			html.Td(html.Props{},
				html.Input(html.Props{Type: "checkbox", ID: checkID, Name: "line", Value: line.LineID}),
				html.Label(html.Props{For: checkID}, ui.Text(text("select")+" "+line.Description))),
			html.Td(html.Props{}, ui.Text(line.Description)),
			html.Td(html.Props{}, ui.Text(original)),
			html.Td(html.Props{}, ui.Text(credited)),
			html.Td(html.Props{}, ui.Text(available)),
			html.Td(html.Props{},
				html.Label(html.Props{For: amountID}, ui.Text(text("amount"))),
				html.Input(html.Props{Type: "text", ID: amountID, Name: "amount", Value: proposed, Raw: map[string]any{"inputmode": "decimal"}}))))
	}
	return html.Table(html.Props{Class: "smb-bill-adjust-lines"},
		html.Thead(html.Props{}, header),
		html.Tbody(html.Props{}, rows...))
}
