package productui

import (
	"fmt"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/stock"
)

// smbStock002Labels carries the fixed card labels in the surfaces this family
// ships. Quantities stay canonical API decimal strings in every locale.
var smbStock002Labels = map[string]map[string]string{
	"en-US": {
		"status": "Status", "active": "Active", "archived": "Archived — hidden from pickers", "retired": "Retired",
		"sku": "SKU", "baseUnit": "Base unit", "tracking": "Tracking", "scale": "Decimal places", "revision": "Revision",
		"preview": "Conversion preview", "cannotPost": "Conversion cannot post",
		"aliases": "Barcodes", "symbology": "Symbology", "code": "Code", "unit": "Unit",
		"noAliases":   "No barcodes registered — add one to receive by scan.",
		"retireNote":  "Retirement releases the SKU for a successor; history stays readable.",
		"blocked":     "Retirement blocked",
		"obligations": "View obligations",
		"historyNote": "Revisions never rewrite history; scans resolve the current revision.",
	},
	"de-DE": {
		"status": "Status", "active": "Aktiv", "archived": "Archiviert — in Auswahl ausgeblendet", "retired": "Stillgelegt",
		"sku": "SKU", "baseUnit": "Basiseinheit", "tracking": "Verfolgung", "scale": "Dezimalstellen", "revision": "Revision",
		"preview": "Umrechnungsvorschau", "cannotPost": "Umrechnung kann nicht gebucht werden",
		"aliases": "Barcodes", "symbology": "Symbologie", "code": "Code", "unit": "Einheit",
		"noAliases":   "Keine Barcodes registriert — einen hinzufügen, um per Scan zu empfangen.",
		"retireNote":  "Die Stilllegung gibt die SKU für einen Nachfolger frei; der Verlauf bleibt lesbar.",
		"blocked":     "Stilllegung blockiert",
		"obligations": "Verpflichtungen ansehen",
		"historyNote": "Revisionen schreiben den Verlauf nie um; Scans lösen die aktuelle Revision auf.",
	},
	"ar": {
		"status": "الحالة", "active": "نشط", "archived": "مؤرشف — مخفي من أدوات الاختيار", "retired": "متقاعد",
		"sku": "رمز الصنف", "baseUnit": "الوحدة الأساسية", "tracking": "التتبع", "scale": "المنازل العشرية", "revision": "المراجعة",
		"preview": "معاينة التحويل", "cannotPost": "تعذّر ترحيل التحويل",
		"aliases": "الرموز الشريطية", "symbology": "نوع الترميز", "code": "الرمز", "unit": "الوحدة",
		"noAliases":   "لا توجد رموز شريطية مسجلة — أضف رمزًا للاستلام بالمسح.",
		"retireNote":  "التقاعد يحرر رمز الصنف لخليفة؛ ويبقى السجل قابلًا للقراءة.",
		"blocked":     "التقاعد محظور",
		"obligations": "عرض الالتزامات",
		"historyNote": "المراجعات لا تعيد كتابة السجل أبدًا؛ وعمليات المسح تحل المراجعة الحالية.",
	},
}

func smbStock002Text(locale, key string) string {
	if labels, ok := smbStock002Labels[locale]; ok {
		if text, ok := labels[key]; ok {
			return text
		}
	}
	return smbStock002Labels["en-US"][key]
}

// SMBStockItemCard renders one catalog revision for the Inventory items
// surface: identity facts, the exact conversion preview or its refusal,
// barcode rows and the retirement obligations note. The heading is the
// post-save focus target; quantities render as canonical decimal strings.
func SMBStockItemCard(item stock.CatalogItem, preview *stock.ConversionPreview, previewErr error, openReservations int, locale string) ui.Node {
	text := func(key string) string { return smbStock002Text(locale, key) }
	statusKey := "active"
	switch item.Status {
	case stock.ItemStatusArchived:
		statusKey = "archived"
	case stock.ItemStatusRetired:
		statusKey = "retired"
	}
	props := html.Props{
		Class: "smb-stock-item-card",
		ID:    "smb-stock-item-" + item.ItemID,
		Data:  map[string]string{"item-id": item.ItemID, "revision": fmt.Sprintf("%d", item.Revision), "item-status": string(item.Status)},
	}
	if strings.HasPrefix(locale, "ar") {
		props.Lang = "ar"
		props.Dir = "rtl"
	} else {
		props.Lang = locale
	}
	nodes := []ui.Node{
		html.H2(html.Props{Class: "smb-stock-item-name", TabIndex: -1}, ui.Text(item.Name)),
		html.P(html.Props{Class: "smb-stock-item-status", Role: "status"}, ui.Text(text("status")+": "+text(statusKey))),
		smbStock002Facts(item, text),
	}
	if previewErr != nil {
		nodes = append(nodes, html.Div(html.Props{Class: "smb-stock-item-error", Role: "alert"}, ui.Text(text("cannotPost")+": "+previewErr.Error())))
	} else if preview != nil {
		nodes = append(nodes, html.P(html.Props{Class: "smb-stock-item-preview", Role: "status"}, ui.Text(text("preview")+": "+preview.Display)))
	}
	nodes = append(nodes, smbStock002Aliases(item, text))
	if openReservations > 0 {
		nodes = append(nodes,
			html.P(html.Props{Class: "smb-stock-item-blocked", Role: "alert"},
				ui.Text(fmt.Sprintf("%s: %d.", text("blocked"), openReservations))),
			html.A(html.Props{Class: "smb-stock-item-obligations", Href: "/workspace/app/inventory/items?item=" + item.ItemID + "&view=obligations"},
				ui.Text(text("obligations"))),
		)
	} else {
		nodes = append(nodes, html.P(html.Props{Class: "smb-stock-item-retire-note"}, ui.Text(text("retireNote"))))
	}
	nodes = append(nodes, html.P(html.Props{Class: "smb-stock-item-history-note"}, ui.Text(text("historyNote"))))
	return html.Section(props, nodes...)
}

func smbStock002Facts(item stock.CatalogItem, text func(string) string) ui.Node {
	rows := [][2]string{
		{text("sku"), item.SKU},
		{text("baseUnit"), item.BaseUnit},
		{text("tracking"), string(item.Tracking)},
		{text("scale"), fmt.Sprintf("%d", item.Scale)},
		{text("revision"), fmt.Sprintf("%d", item.Revision)},
	}
	body := make([]ui.Node, 0, len(rows))
	for _, row := range rows {
		body = append(body, html.Tr(html.Props{},
			html.Th(html.Props{Class: "smb-stock-item-fact-name"}, ui.Text(row[0])),
			html.Td(html.Props{Class: "smb-stock-item-fact-value"}, ui.Text(row[1]))))
	}
	return html.Table(html.Props{Class: "smb-stock-item-facts"}, html.Tbody(html.Props{}, body...))
}

func smbStock002Aliases(item stock.CatalogItem, text func(string) string) ui.Node {
	if len(item.Aliases) == 0 {
		return html.Div(html.Props{},
			html.H3(html.Props{Class: "smb-stock-item-aliases-title"}, ui.Text(text("aliases"))),
			html.P(html.Props{Class: "smb-stock-item-aliases-empty", Role: "status"}, ui.Text(text("noAliases"))))
	}
	rows := make([]ui.Node, 0, len(item.Aliases))
	for _, alias := range item.Aliases {
		unit := alias.PackUnit
		if unit == "" {
			unit = item.BaseUnit
		}
		rows = append(rows, html.Tr(html.Props{},
			html.Td(html.Props{}, ui.Text(alias.Symbology)),
			html.Td(html.Props{Class: "smb-stock-item-alias-code"}, ui.Text(alias.Code)),
			html.Td(html.Props{}, ui.Text(unit))))
	}
	return html.Div(html.Props{},
		html.H3(html.Props{Class: "smb-stock-item-aliases-title"}, ui.Text(text("aliases"))),
		html.Table(html.Props{Class: "smb-stock-item-aliases"},
			html.Thead(html.Props{},
				html.Tr(html.Props{},
					html.Th(html.Props{}, ui.Text(text("symbology"))),
					html.Th(html.Props{}, ui.Text(text("code"))),
					html.Th(html.Props{}, ui.Text(text("unit"))))),
			html.Tbody(html.Props{}, rows...)))
}
