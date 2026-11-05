package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/payable"
)

var smbBill012Labels = map[string]map[string]string{
	"en-US": {
		"title": "Payable obligation", "supplier": "Supplier", "invoice": "Invoice",
		"basis": "Basis", "matching": "Matching", "due": "Due", "terms": "Terms",
		"billed": "Total billed", "approved": "Approved to pay", "disputed": "Disputed",
		"unmatched": "Unmatched", "credits": "Credits applied", "paid": "Paid",
		"remaining": "Remaining due", "sources": "Source links",
		"noSources": "No source links.",
		"blocked":   "Payment blocked",
		"payTitle":  "Record payment",
		"evidence":  "Payment evidence (required)",
		"amount":    "Amount",
		"payCta":    "Record this payment",
		"status":    "Status",
		"unitNote":  "Amounts are exact server values.",
	},
	"de-DE": {
		"title": "Zahlungsverpflichtung", "supplier": "Lieferant", "invoice": "Rechnung",
		"basis": "Grundlage", "matching": "Abgleich", "due": "Fällig", "terms": "Konditionen",
		"billed": "Rechnungsbetrag", "approved": "Zur Zahlung freigegeben", "disputed": "Strittig",
		"unmatched": "Nicht zugeordnet", "credits": "Angerechnete Gutschriften", "paid": "Gezahlt",
		"remaining": "Offener Restbetrag", "sources": "Herkunftsnachweise",
		"noSources": "Keine Herkunftsnachweise.",
		"blocked":   "Zahlung blockiert",
		"payTitle":  "Zahlung erfassen",
		"evidence":  "Zahlungsnachweis (erforderlich)",
		"amount":    "Betrag",
		"payCta":    "Diese Zahlung erfassen",
		"status":    "Status",
		"unitNote":  "Beträge sind exakte Serverwerte.",
	},
	"ar": {
		"title": "التزام الدفع", "supplier": "المورد", "invoice": "الفاتورة",
		"basis": "الأساس", "matching": "المطابقة", "due": "الاستحقاق", "terms": "الشروط",
		"billed": "إجمالي الفاتورة", "approved": "المعتمد للدفع", "disputed": "المتنازع عليه",
		"unmatched": "غير المطابق", "credits": "الأرصدة الدائنة المطبقة", "paid": "المدفوع",
		"remaining": "المتبقي المستحق", "sources": "روابط المصدر",
		"noSources": "لا توجد روابط مصدر.",
		"blocked":   "الدفع محظور",
		"payTitle":  "تسجيل دفعة",
		"evidence":  "دليل الدفع (مطلوب)",
		"amount":    "المبلغ",
		"payCta":    "تسجيل هذه الدفعة",
		"status":    "الحالة",
		"unitNote":  "المبالغ قيم دقيقة من الخادم.",
	},
}

func smbBill012Text(locale, key string) string {
	if labels, ok := smbBill012Labels[locale]; ok {
		if text, ok := labels[key]; ok {
			return text
		}
	}
	return smbBill012Labels["en-US"][key]
}

// SMBPayableObligationCard renders the payables drawer for one obligation: the
// supplier and invoice identity, the admission basis with matching status and
// terms, the exact money rows (billed, approved, disputed, unmatched,
// credits, paid, remaining), the source links, any blocked-state notice and
// the policy-gated payment form. The heading is the post-action focus target.
func SMBPayableObligationCard(obligation payable.Payable, projection payable.PayableProjection, payEligible bool, payBlockedReason, supplierLink, locale string) ui.Node {
	text := func(key string) string { return smbBill012Text(locale, key) }
	props := html.Props{Class: "smb-payable-card", ID: "smb-payable"}
	if strings.HasPrefix(locale, "ar") {
		props.Lang = "ar"
		props.Dir = "rtl"
	} else {
		props.Lang = locale
	}

	nodes := []ui.Node{
		html.H2(html.Props{Class: "smb-payable-title", TabIndex: -1}, ui.Text(text("title")+" "+obligation.Number)),
		html.P(html.Props{Class: "smb-payable-supplier"},
			ui.Text(text("supplier")+": "+obligation.SupplierName+" — "+text("invoice")+": "+obligation.InvoiceNumber)),
		html.P(html.Props{Class: "smb-payable-meta"},
			ui.Text(text("basis")+": "+obligation.Basis+" — "+text("matching")+": "+obligation.MatchingStatus)),
		html.P(html.Props{Class: "smb-payable-terms"},
			ui.Text(text("due")+": "+obligation.DueDate+" — "+text("terms")+": "+obligation.Terms)),
		smbBill012MoneyTable(obligation, text),
	}
	if supplierLink != "" {
		nodes = append(nodes, html.A(html.Props{Class: "smb-payable-supplier-link", Href: supplierLink}, ui.Text(obligation.SupplierName)))
	}
	nodes = append(nodes, html.H3(html.Props{}, ui.Text(text("sources"))))
	if len(projection.SourceLinks) == 0 {
		nodes = append(nodes, html.P(html.Props{Class: "smb-payable-sources-empty", Role: "status"}, ui.Text(text("noSources"))))
	} else {
		items := make([]ui.Node, 0, len(projection.SourceLinks))
		for _, link := range projection.SourceLinks {
			items = append(items, html.Li(html.Props{}, ui.Text(link)))
		}
		nodes = append(nodes, html.Ul(html.Props{Class: "smb-payable-sources"}, items...))
	}
	nodes = append(nodes, html.P(html.Props{Class: "smb-payable-status", Role: "status"},
		ui.Text(text("status")+": "+obligation.Status+" — "+projection.AccountingStatus)))
	if obligation.Status == payable.PayableDiscrepancy || obligation.Status == payable.PayableQuarantined {
		nodes = append(nodes, html.P(html.Props{Class: "smb-payable-blocked-notice", Role: "alert"},
			ui.Text(text("blocked")+": "+obligation.MatchingStatus)))
	}
	nodes = append(nodes, html.H3(html.Props{}, ui.Text(text("payTitle"))))
	if payEligible {
		nodes = append(nodes,
			html.Label(html.Props{For: "smb-payable-evidence"}, ui.Text(text("evidence"))),
			html.Input(html.Props{Type: "text", ID: "smb-payable-evidence", Name: "evidence", Required: true}),
			html.Label(html.Props{For: "smb-payable-amount"}, ui.Text(text("amount"))),
			html.Input(html.Props{Type: "text", ID: "smb-payable-amount", Name: "amount", Value: obligation.Remaining, Raw: map[string]any{"inputmode": "decimal"}}),
			html.Button(html.Props{Class: "smb-payable-cta", Type: "submit"}, ui.Text(text("payCta"))))
	} else {
		nodes = append(nodes, html.P(html.Props{Class: "smb-payable-pay-blocked", Role: "status"},
			ui.Text(text("blocked")+": "+payBlockedReason)))
	}
	nodes = append(nodes, html.P(html.Props{Class: "smb-payable-unit-note"}, ui.Text(text("unitNote"))))
	return html.Section(props, nodes...)
}

func smbBill012MoneyTable(obligation payable.Payable, text func(string) string) ui.Node {
	rows := [][2]string{
		{text("billed"), obligation.TotalBilled},
		{text("approved"), obligation.ApprovedToPay},
		{text("disputed"), obligation.Disputed},
		{text("unmatched"), obligation.Unmatched},
		{text("credits"), obligation.CreditsApplied},
		{text("paid"), obligation.Paid},
		{text("remaining"), obligation.Remaining},
	}
	body := make([]ui.Node, 0, len(rows))
	for _, row := range rows {
		body = append(body, html.Tr(html.Props{},
			html.Th(html.Props{}, ui.Text(row[0])),
			html.Td(html.Props{}, ui.Text(row[1]))))
	}
	return html.Table(html.Props{Class: "smb-payable-money"}, html.Tbody(html.Props{}, body...))
}
