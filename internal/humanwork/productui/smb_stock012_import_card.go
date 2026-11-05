package productui

import (
	"fmt"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/stock"
)

// smbStock012Labels carries the fixed import-card labels. Quantities stay
// canonical API decimal strings in every locale.
var smbStock012Labels = map[string]map[string]string{
	"en-US": {
		"title": "Inventory import results", "applied": "rows applied", "of": "of",
		"receipts": "Receipts", "errors": "Row errors", "downloadErrors": "Download row errors (CSV)",
		"resume": "Resume import", "noReceipts": "No rows applied yet — review errors and resume.",
		"site": "Site", "sku": "SKU", "usable": "Usable", "quarantine": "Quarantine", "transit": "In transit",
		"transitNote": "In-transit stock is not available until received.",
		"buckets":     "Opening buckets",
		"historyNote": "Receipts are immutable; corrections link back instead of duplicating.",
	},
	"de-DE": {
		"title": "Ergebnisse des Bestandsimports", "applied": "Zeilen übernommen", "of": "von",
		"receipts": "Belege", "errors": "Zeilenfehler", "downloadErrors": "Zeilenfehler herunterladen (CSV)",
		"resume": "Import fortsetzen", "noReceipts": "Noch keine Zeilen übernommen — Fehler prüfen und fortsetzen.",
		"site": "Standort", "sku": "SKU", "usable": "Verfügbar", "quarantine": "Gesperrt", "transit": "Unterwegs",
		"transitNote": "Unterwegs befindlicher Bestand ist erst nach dem Empfang verfügbar.",
		"buckets":     "Anfangsbestände",
		"historyNote": "Belege sind unveränderlich; Korrekturen verweisen zurück statt zu doppeln.",
	},
	"ar": {
		"title": "نتائج استيراد المخزون", "applied": "صفوف مطبقة", "of": "من",
		"receipts": "الإيصالات", "errors": "أخطاء الصفوف", "downloadErrors": "تنزيل أخطاء الصفوف (CSV)",
		"resume": "استئناف الاستيراد", "noReceipts": "لم يتم تطبيق أي صفوف بعد — راجع الأخطاء واستأنف.",
		"site": "الموقع", "sku": "رمز الصنف", "usable": "متاح", "quarantine": "محجوز", "transit": "قيد النقل",
		"transitNote": "المخزون قيد النقل غير متاح حتى الاستلام.",
		"buckets":     "أرصدة الافتتاح",
		"historyNote": "الإيصالات غير قابلة للتغيير؛ والتصحيحات تشير إلى الأصل بدل التكرار.",
	},
}

func smbStock012Text(locale, key string) string {
	if labels, ok := smbStock012Labels[locale]; ok {
		if text, ok := labels[key]; ok {
			return text
		}
	}
	return smbStock012Labels["en-US"][key]
}

// SMBStockImportBucket is one displayed opening bucket row.
type SMBStockImportBucket struct {
	Site       string
	SKU        string
	Usable     string
	Quarantine string
	Transit    string
}

// SMBStockImportCard renders staged import results for the shared setup
// Import inventory step: applied count, receipt links, row errors with a
// safe download note, resume and the opening buckets. Partial results say
// exactly how many rows applied; nothing claims all-or-nothing success.
func SMBStockImportCard(result stock.ImportCommitResult, buckets []SMBStockImportBucket, locale string) ui.Node {
	text := func(key string) string { return smbStock012Text(locale, key) }
	props := html.Props{
		Class: "smb-stock-import-card",
		ID:    "smb-stock-import-" + result.ImportID,
		Data:  map[string]string{"import-id": result.ImportID, "batch-digest": result.BatchDigest},
	}
	if strings.HasPrefix(locale, "ar") {
		props.Lang = "ar"
		props.Dir = "rtl"
	} else {
		props.Lang = locale
	}
	total := result.Applied + result.Failed + result.Pending + result.Unattempted
	nodes := []ui.Node{
		html.H2(html.Props{Class: "smb-stock-import-title", TabIndex: -1}, ui.Text(text("title"))),
		html.P(html.Props{Class: "smb-stock-import-summary", Role: "status"},
			ui.Text(fmt.Sprintf("%d %s %d %s.", result.Applied, text("of"), total, text("applied")))),
	}
	if len(result.Receipts) == 0 {
		nodes = append(nodes, html.P(html.Props{Class: "smb-stock-import-empty", Role: "status"}, ui.Text(text("noReceipts"))))
	} else {
		links := make([]ui.Node, 0, len(result.Receipts))
		for _, receipt := range result.Receipts {
			links = append(links, html.Li(html.Props{},
				html.A(html.Props{Href: "/workspace/app/inventory/imports?receipt=" + receipt.ReceiptID},
					ui.Text(receipt.ExternalKey+" — "+receipt.ReceiptID))))
		}
		nodes = append(nodes, html.Div(html.Props{},
			html.H3(html.Props{}, ui.Text(text("receipts"))),
			html.Ul(html.Props{Class: "smb-stock-import-receipts"}, links...)))
	}
	if len(result.Errors) > 0 {
		items := make([]ui.Node, 0, len(result.Errors))
		for _, rowErr := range result.Errors {
			items = append(items, html.Li(html.Props{Class: "smb-stock-import-error"},
				ui.Text(fmt.Sprintf("Line %d %s: %s", rowErr.CSVLine, rowErr.Field, rowErr.Message))))
		}
		nodes = append(nodes, html.Div(html.Props{Class: "smb-stock-import-errors", Role: "alert"},
			html.H3(html.Props{}, ui.Text(text("errors"))),
			html.Ul(html.Props{}, items...),
			html.P(html.Props{Class: "smb-stock-import-download"}, ui.Text(text("downloadErrors")))))
	}
	if !result.Complete {
		nodes = append(nodes, html.A(html.Props{Class: "smb-stock-import-resume", Href: "/workspace/app/inventory/imports?import=" + result.ImportID + "&view=resume"},
			ui.Text(text("resume"))))
	}
	if len(buckets) > 0 {
		rows := make([]ui.Node, 0, len(buckets))
		for _, bucket := range buckets {
			rows = append(rows, html.Tr(html.Props{},
				html.Td(html.Props{}, ui.Text(bucket.Site)),
				html.Td(html.Props{}, ui.Text(bucket.SKU)),
				html.Td(html.Props{}, ui.Text(bucket.Usable)),
				html.Td(html.Props{}, ui.Text(bucket.Quarantine)),
				html.Td(html.Props{}, ui.Text(bucket.Transit))))
		}
		nodes = append(nodes, html.Div(html.Props{},
			html.H3(html.Props{}, ui.Text(text("buckets"))),
			html.Table(html.Props{Class: "smb-stock-import-buckets"},
				html.Thead(html.Props{},
					html.Tr(html.Props{},
						html.Th(html.Props{}, ui.Text(text("site"))),
						html.Th(html.Props{}, ui.Text(text("sku"))),
						html.Th(html.Props{}, ui.Text(text("usable"))),
						html.Th(html.Props{}, ui.Text(text("quarantine"))),
						html.Th(html.Props{}, ui.Text(text("transit"))))),
				html.Tbody(html.Props{}, rows...)),
			html.P(html.Props{Class: "smb-stock-import-transit-note"}, ui.Text(text("transitNote")))))
	}
	nodes = append(nodes, html.P(html.Props{Class: "smb-stock-import-history-note"}, ui.Text(text("historyNote"))))
	return html.Section(props, nodes...)
}
