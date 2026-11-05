package productui

import (
	"fmt"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/inventory"
)

var smbStock001Labels = map[string]map[string]string{
	"en-US": {
		"receipt": "Movement receipt", "kind": "Movement", "quantity": "Quantity",
		"item": "Item", "site": "Site", "operation": "Operation", "sequence": "Sequence",
		"recorded": "Recorded", "balance": "Balance after posting",
		"usable": "Usable", "quarantine": "Quarantine", "total": "On hand",
		"available": "Available", "refused": "Movement refused",
		"serialHeld":  "Serialized stock is not enabled yet; serial actions stay hidden until the asset owner links namespaces.",
		"historyNote": "Movements are immutable; balances always re-derive from the journal.",
	},
	"de-DE": {
		"receipt": "Buchungsbeleg", "kind": "Buchung", "quantity": "Menge",
		"item": "Artikel", "site": "Standort", "operation": "Vorgang", "sequence": "Folge",
		"recorded": "Gebucht", "balance": "Bestand nach Buchung",
		"usable": "Verfügbar", "quarantine": "Quarantäne", "total": "Bestand",
		"available": "Verfügbar für Zusagen", "refused": "Buchung abgelehnt",
		"serialHeld":  "Seriennummernbestand ist noch nicht aktiviert; Serienaktionen bleiben ausgeblendet, bis der Anlageninhaber Namensräume verknüpft.",
		"historyNote": "Buchungen sind unveränderlich; Bestände leiten sich immer aus dem Journal ab.",
	},
	"ar": {
		"receipt": "إيصال الحركة", "kind": "الحركة", "quantity": "الكمية",
		"item": "الصنف", "site": "الموقع", "operation": "العملية", "sequence": "التسلسل",
		"recorded": "سُجّلت", "balance": "الرصيد بعد الترحيل",
		"usable": "المتاح", "quarantine": "الحجر", "total": "المخزون",
		"available": "المتاح للوعود", "refused": "رُفضت الحركة",
		"serialHeld":  "مخزون الأرقام التسلسلية غير مفعّل بعد؛ تبقى إجراءات التسلسل مخفية حتى يربط مالك الأصول مساحات الأسماء.",
		"historyNote": "الحركات غير قابلة للتعديل؛ تُشتق الأرصدة دائمًا من اليومية.",
	},
}

func smbStock001Text(locale, key string) string {
	if labels, ok := smbStock001Labels[locale]; ok {
		if text, ok := labels[key]; ok {
			return text
		}
	}
	return smbStock001Labels["en-US"][key]
}

// SMBStockMovementCard renders one posted movement receipt or its typed
// refusal for the Inventory surfaces: movement facts, the operation link,
// the derived balance, and the serialized-availability note. The heading
// is the post-save focus target; quantities render as canonical decimal
// strings and refusals carry guidance, never raw internals.
func SMBStockMovementCard(movement inventory.Movement, postErr error, balance inventory.BalanceReport, sku, locale string) ui.Node {
	text := func(key string) string { return smbStock001Text(locale, key) }
	props := html.Props{
		Class: "smb-stock-movement-card",
		ID:    "smb-stock-movement-" + movement.OperationID,
		Data:  map[string]string{"operation-id": movement.OperationID, "sequence": fmt.Sprintf("%d", movement.Sequence)},
	}
	if strings.HasPrefix(locale, "ar") {
		props.Lang = "ar"
		props.Dir = "rtl"
	} else {
		props.Lang = locale
	}
	if postErr != nil {
		return html.Section(props,
			html.H2(html.Props{Class: "smb-stock-movement-title", TabIndex: -1}, ui.Text(text("refused"))),
			html.P(html.Props{Class: "smb-stock-movement-error", Role: "alert"}, ui.Text(inventory.MovementRefusalTitle(postErr))),
			html.P(html.Props{Class: "smb-stock-movement-history-note"}, ui.Text(text("historyNote"))),
		)
	}
	site := movement.SiteID
	if site == "" {
		site = movement.FromSite + " → " + movement.ToSite
	}
	facts := [][2]string{
		{text("kind"), string(movement.Kind)},
		{text("quantity"), movement.QuantityText},
		{text("item"), sku},
		{text("site"), site},
		{text("operation"), movement.OperationID},
		{text("sequence"), fmt.Sprintf("%d", movement.Sequence)},
		{text("recorded"), movement.RecordedAt.UTC().Format("2006-01-02 15:04:05 UTC")},
	}
	body := make([]ui.Node, 0, len(facts))
	for _, row := range facts {
		body = append(body, html.Tr(html.Props{},
			html.Th(html.Props{Class: "smb-stock-movement-fact-name"}, ui.Text(row[0])),
			html.Td(html.Props{Class: "smb-stock-movement-fact-value"}, ui.Text(row[1]))))
	}
	balances := [][2]string{
		{text("usable"), balance.Usable.String()},
		{text("quarantine"), balance.Quarantine.String()},
		{text("total"), balance.OnHandTotal.String()},
		{text("available"), balance.Available.String()},
	}
	balanceBody := make([]ui.Node, 0, len(balances))
	for _, row := range balances {
		balanceBody = append(balanceBody, html.Tr(html.Props{},
			html.Th(html.Props{Class: "smb-stock-movement-fact-name"}, ui.Text(row[0])),
			html.Td(html.Props{Class: "smb-stock-movement-fact-value"}, ui.Text(row[1]))))
	}
	nodes := []ui.Node{
		html.H2(html.Props{Class: "smb-stock-movement-title", TabIndex: -1}, ui.Text(text("receipt"))),
		html.Table(html.Props{Class: "smb-stock-movement-facts"}, html.Tbody(html.Props{}, body...)),
		html.H3(html.Props{Class: "smb-stock-movement-balance-title"}, ui.Text(text("balance"))),
		html.Table(html.Props{Class: "smb-stock-movement-balance"}, html.Tbody(html.Props{}, balanceBody...)),
	}
	if len(movement.SerialKeys) == 0 {
		nodes = append(nodes, html.P(html.Props{Class: "smb-stock-movement-serial-note", Role: "status"}, ui.Text(text("serialHeld"))))
	}
	nodes = append(nodes, html.P(html.Props{Class: "smb-stock-movement-history-note"}, ui.Text(text("historyNote"))))
	return html.Section(props, nodes...)
}
