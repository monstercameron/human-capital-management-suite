package productui

import (
	"fmt"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/stock"
)

// smbStock008Labels carries the fixed lot-card labels. Quantities stay
// canonical API decimal strings; expiry shows the pinned source zone.
var smbStock008Labels = map[string]map[string]string{
	"en-US": {
		"status": "Status", "available": "Available", "quarantined": "Quarantined", "recalled": "Recalled", "disposed": "Disposed",
		"expiry": "Expiry", "onHand": "On hand", "held": "Held", "policy": "Policy revision", "received": "Received",
		"cutoff": "Cutoff", "affectedWork": "Affected work", "noWork": "No affected work.",
		"trace": "Trace", "recall": "Recall", "blocked": "Blocked demand", "shortage": "Shortage",
		"historyNote": "Lot events are immutable; release revalidates expiry and fences.",
	},
	"de-DE": {
		"status": "Status", "available": "Verfügbar", "quarantined": "Gesperrt", "recalled": "Zurückgerufen", "disposed": "Entsorgt",
		"expiry": "Ablauf", "onHand": "Bestand", "held": "Reserviert", "policy": "Richtlinienrevision", "received": "Empfangen",
		"cutoff": "Stichtag", "affectedWork": "Betroffene Arbeit", "noWork": "Keine betroffene Arbeit.",
		"trace": "Verlauf", "recall": "Rückruf", "blocked": "Blockierter Bedarf", "shortage": "Fehlmenge",
		"historyNote": "Chargenereignisse sind unveränderlich; die Freigabe prüft Ablauf und Sperren erneut.",
	},
	"ar": {
		"status": "الحالة", "available": "متاح", "quarantined": "محجوز", "recalled": "مستدعى", "disposed": "متلف",
		"expiry": "انتهاء الصلاحية", "onHand": "المخزون", "held": "محجوز", "policy": "مراجعة السياسة", "received": "الاستلام",
		"cutoff": "الحد الفاصل", "affectedWork": "الأعمال المتأثرة", "noWork": "لا توجد أعمال متأثرة.",
		"trace": "التتبع", "recall": "استدعاء", "blocked": "الطلب المحظور", "shortage": "العجز",
		"historyNote": "أحداث الدفعة غير قابلة للتغيير؛ وإصدارها يعيد التحقق من انتهاء الصلاحية والقيود.",
	},
}

func smbStock008Text(locale, key string) string {
	if labels, ok := smbStock008Labels[locale]; ok {
		if text, ok := labels[key]; ok {
			return text
		}
	}
	return smbStock008Labels["en-US"][key]
}

// SMBStockLotCard renders one lot trace for the Inventory lots surface:
// status, pinned expiry with its source zone, on-hand and held quantities,
// affected-work links and the immutable event timeline. The heading is the
// post-action focus target.
func SMBStockLotCard(trace stock.LotTrace, heldQty string, affectedWork int, locale string) ui.Node {
	text := func(key string) string { return smbStock008Text(locale, key) }
	statusKey := "available"
	switch trace.Lot.State {
	case stock.LotQuarantined:
		statusKey = "quarantined"
	case stock.LotRecalled:
		statusKey = "recalled"
	case stock.LotDisposed:
		statusKey = "disposed"
	}
	props := html.Props{
		Class: "smb-stock-lot-card",
		ID:    "smb-stock-lot-" + trace.Lot.LotID,
		Data:  map[string]string{"lot-id": trace.Lot.LotID, "lot-state": string(trace.Lot.State), "item-sku": trace.Lot.ItemSKU},
	}
	if strings.HasPrefix(locale, "ar") {
		props.Lang = "ar"
		props.Dir = "rtl"
	} else {
		props.Lang = locale
	}
	expiry := trace.Lot.Expiry.LocalDate + " (" + trace.Lot.Expiry.Timezone + ")"
	nodes := []ui.Node{
		html.H2(html.Props{Class: "smb-stock-lot-name", TabIndex: -1}, ui.Text(trace.Lot.SupplierLot+" — "+trace.Lot.ItemSKU)),
		html.P(html.Props{Class: "smb-stock-lot-status", Role: "status"}, ui.Text(text("status")+": "+text(statusKey))),
		smbStock008LotFacts(trace, heldQty, expiry, text),
	}
	for _, report := range trace.Recalls {
		nodes = append(nodes, html.Div(html.Props{Class: "smb-stock-lot-recall", Role: "alert"},
			ui.Text(text("recall")+": "+report.Reason+" — "+text("blocked")+": "+report.BlockedQty+", "+text("shortage")+": "+report.Shortage)))
	}
	if affectedWork > 0 {
		nodes = append(nodes,
			html.P(html.Props{Class: "smb-stock-lot-work", Role: "status"},
				ui.Text(fmt.Sprintf("%s: %d.", text("affectedWork"), affectedWork))),
			html.A(html.Props{Class: "smb-stock-lot-work-link", Href: "/workspace/app/inventory/lots?lot=" + trace.Lot.LotID + "&view=work"},
				ui.Text(text("affectedWork"))))
	} else {
		nodes = append(nodes, html.P(html.Props{Class: "smb-stock-lot-work-empty", Role: "status"}, ui.Text(text("noWork"))))
	}
	nodes = append(nodes, smbStock008LotTimeline(trace, text))
	nodes = append(nodes, html.P(html.Props{Class: "smb-stock-lot-history-note"}, ui.Text(text("historyNote"))))
	return html.Section(props, nodes...)
}

func smbStock008LotFacts(trace stock.LotTrace, heldQty, expiry string, text func(string) string) ui.Node {
	rows := [][2]string{
		{text("expiry"), expiry},
		{text("cutoff"), trace.Cutoff.UTC().Format("2006-01-02 15:04 UTC")},
		{text("onHand"), trace.Lot.OnHand},
		{text("held"), heldQty},
		{text("policy"), trace.Lot.PolicyRevision},
		{text("received"), trace.Lot.ReceivedAt.UTC().Format("2006-01-02")},
	}
	body := make([]ui.Node, 0, len(rows))
	for _, row := range rows {
		body = append(body, html.Tr(html.Props{},
			html.Th(html.Props{Class: "smb-stock-lot-fact-name"}, ui.Text(row[0])),
			html.Td(html.Props{Class: "smb-stock-lot-fact-value"}, ui.Text(row[1]))))
	}
	return html.Table(html.Props{Class: "smb-stock-lot-facts"}, html.Tbody(html.Props{}, body...))
}

func smbStock008LotTimeline(trace stock.LotTrace, text func(string) string) ui.Node {
	items := make([]ui.Node, 0, len(trace.Events))
	for _, event := range trace.Events {
		line := event.Kind + " — " + event.At.UTC().Format("2006-01-02 15:04")
		if event.Reason != "" {
			line += " — " + event.Reason
		}
		if event.Qty != "" {
			line += " — " + event.Qty
		}
		items = append(items, html.Li(html.Props{Class: "smb-stock-lot-event"}, ui.Text(line)))
	}
	return html.Div(html.Props{},
		html.H3(html.Props{Class: "smb-stock-lot-trace-title"}, ui.Text(text("trace"))),
		html.Ol(html.Props{Class: "smb-stock-lot-timeline"}, items...))
}
