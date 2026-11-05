package productui

import (
	"fmt"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/procurement"
)

// smbBuy003Labels carries the fixed workbench-card labels. Quantities stay
// canonical API decimal strings in every locale.
var smbBuy003Labels = map[string]map[string]string{
	"en-US": {
		"title": "Purchase order", "status": "Status", "revision": "Revision",
		"supplier": "Supplier", "total": "Total", "delivery": "Delivery",
		"lines": "Lines", "ordered": "Ordered", "received": "Received", "invoiced": "Invoiced",
		"dispatch": "Dispatch", "unknown": "Delivery is unknown — check the channel before resending.",
		"check": "Check delivery", "amendments": "Pending amendments", "noAmendments": "No pending amendments.",
		"cancellation": "Cancellation", "noCancellation": "No cancellation requested.",
		"ack": "Supplier acknowledgement", "review": "Needs buyer review: outdated acknowledgement is on file.",
		"historyNote": "Revisions are immutable; amendments and cancellations link back instead of rewriting.",
	},
	"de-DE": {
		"title": "Bestellung", "status": "Status", "revision": "Revision",
		"supplier": "Lieferant", "total": "Summe", "delivery": "Lieferung",
		"lines": "Positionen", "ordered": "Bestellt", "received": "Empfangen", "invoiced": "Fakturiert",
		"dispatch": "Versand", "unknown": "Versand ist unbekannt — Kanal prüfen vor erneutem Senden.",
		"check": "Versand prüfen", "amendments": "Offene Änderungen", "noAmendments": "Keine offenen Änderungen.",
		"cancellation": "Stornierung", "noCancellation": "Keine Stornierung angefordert.",
		"ack": "Lieferantenbestätigung", "review": "Prüfung erforderlich: veraltete Bestätigung liegt vor.",
		"historyNote": "Revisionen sind unveränderlich; Änderungen und Stornierungen verweisen zurück statt umzuschreiben.",
	},
	"ar": {
		"title": "أمر الشراء", "status": "الحالة", "revision": "المراجعة",
		"supplier": "المورد", "total": "الإجمالي", "delivery": "التسليم",
		"lines": "البنود", "ordered": "المطلوب", "received": "المستلم", "invoiced": "المفوتر",
		"dispatch": "الإرسال", "unknown": "التسليم غير معروف — تحقق من القناة قبل إعادة الإرسال.",
		"check": "التحقق من التسليم", "amendments": "التعديلات المعلقة", "noAmendments": "لا توجد تعديلات معلقة.",
		"cancellation": "الإلغاء", "noCancellation": "لم يتم طلب إلغاء.",
		"ack": "إقرار المورد", "review": "يلزم مراجعة المشتري: يوجد إقرار قديم في الملف.",
		"historyNote": "المراجعات غير قابلة للتغيير؛ والتعديلات والإلغاءات تشير إلى الأصل بدل إعادة الكتابة.",
	},
}

func smbBuy003Text(locale, key string) string {
	if labels, ok := smbBuy003Labels[locale]; ok {
		if text, ok := labels[key]; ok {
			return text
		}
	}
	return smbBuy003Labels["en-US"][key]
}

// SMBBuyWorkbenchCard renders one purchase order for the buyer workbench:
// identity, supplier, totals, per-line ordered/received/invoiced progress,
// dispatch state with the unknown banner, pending amendments, cancellation
// and acknowledgement. The heading is the post-action focus target.
func SMBBuyWorkbenchCard(po procurement.BuyPO, dispatch *procurement.BuyDispatch, pendingAmendments int, cancellation *procurement.BuyCancellation, ack *procurement.BuyAck, locale string) ui.Node {
	text := func(key string) string { return smbBuy003Text(locale, key) }
	props := html.Props{
		Class: "smb-buy-workbench-card",
		ID:    "smb-buy-order-" + po.POID,
		Data:  map[string]string{"po-id": po.POID, "po-revision": fmt.Sprintf("%d", po.Revision), "po-status": string(po.Status)},
	}
	if strings.HasPrefix(locale, "ar") {
		props.Lang = "ar"
		props.Dir = "rtl"
	} else {
		props.Lang = locale
	}
	nodes := []ui.Node{
		html.H2(html.Props{Class: "smb-buy-workbench-title", TabIndex: -1}, ui.Text(text("title")+" "+po.PONumber)),
		html.P(html.Props{Class: "smb-buy-workbench-status", Role: "status"},
			ui.Text(text("status")+": "+string(po.Status)+" — "+text("revision")+": "+fmt.Sprintf("%d", po.Revision))),
		smbBuy003Facts(po, text),
		smbBuy003Lines(po, text),
	}
	if dispatch == nil {
		nodes = append(nodes, html.P(html.Props{Class: "smb-buy-workbench-dispatch-empty", Role: "status"}, ui.Text(text("dispatch")+": —")))
	} else {
		nodes = append(nodes, html.P(html.Props{Class: "smb-buy-workbench-dispatch", Role: "status"},
			ui.Text(text("dispatch")+": "+string(dispatch.State)+" — "+dispatch.Recipient)))
		if dispatch.State == procurement.BuyDispatchUnknown {
			nodes = append(nodes,
				html.P(html.Props{Class: "smb-buy-workbench-unknown", Role: "alert"}, ui.Text(text("unknown"))),
				html.A(html.Props{Class: "smb-buy-workbench-check", Href: "/workspace/app/procurement/orders?po=" + po.POID + "&view=dispatch"},
					ui.Text(text("check"))))
		}
	}
	if pendingAmendments > 0 {
		nodes = append(nodes, html.P(html.Props{Class: "smb-buy-workbench-amendments", Role: "status"},
			ui.Text(fmt.Sprintf("%s: %d.", text("amendments"), pendingAmendments))))
	} else {
		nodes = append(nodes, html.P(html.Props{Class: "smb-buy-workbench-amendments-empty", Role: "status"}, ui.Text(text("noAmendments"))))
	}
	if cancellation == nil {
		nodes = append(nodes, html.P(html.Props{Class: "smb-buy-workbench-cancel-empty", Role: "status"}, ui.Text(text("noCancellation"))))
	} else {
		nodes = append(nodes, html.P(html.Props{Class: "smb-buy-workbench-cancel", Role: "status"},
			ui.Text(text("cancellation")+": "+string(cancellation.Status))))
	}
	if ack == nil {
		nodes = append(nodes, html.P(html.Props{Class: "smb-buy-workbench-ack-empty", Role: "status"}, ui.Text(text("ack")+": —")))
	} else {
		nodes = append(nodes, html.P(html.Props{Class: "smb-buy-workbench-ack", Role: "status"},
			ui.Text(text("ack")+": "+string(ack.Status))))
	}
	if po.NeedsReview {
		nodes = append(nodes, html.P(html.Props{Class: "smb-buy-workbench-review", Role: "alert"}, ui.Text(text("review"))))
	}
	nodes = append(nodes, html.P(html.Props{Class: "smb-buy-workbench-history-note"}, ui.Text(text("historyNote"))))
	return html.Section(props, nodes...)
}

func smbBuy003Facts(po procurement.BuyPO, text func(string) string) ui.Node {
	rows := [][2]string{
		{text("supplier"), po.Supplier.Name},
		{text("total"), po.Total + " " + po.Currency},
		{text("delivery"), po.DeliverySite},
	}
	body := make([]ui.Node, 0, len(rows))
	for _, row := range rows {
		body = append(body, html.Tr(html.Props{},
			html.Th(html.Props{Class: "smb-buy-workbench-fact-name"}, ui.Text(row[0])),
			html.Td(html.Props{Class: "smb-buy-workbench-fact-value"}, ui.Text(row[1]))))
	}
	return html.Table(html.Props{Class: "smb-buy-workbench-facts"}, html.Tbody(html.Props{}, body...))
}

func smbBuy003Lines(po procurement.BuyPO, text func(string) string) ui.Node {
	rows := make([]ui.Node, 0, len(po.Lines))
	for _, line := range po.Lines {
		rows = append(rows, html.Tr(html.Props{},
			html.Td(html.Props{}, ui.Text(line.Description)),
			html.Td(html.Props{}, ui.Text(line.Qty)),
			html.Td(html.Props{}, ui.Text(line.ReceivedQty)),
			html.Td(html.Props{}, ui.Text(line.InvoicedQty))))
	}
	return html.Div(html.Props{},
		html.H3(html.Props{}, ui.Text(text("lines"))),
		html.Table(html.Props{Class: "smb-buy-workbench-lines"},
			html.Thead(html.Props{},
				html.Tr(html.Props{},
					html.Th(html.Props{}, ui.Text(text("lines"))),
					html.Th(html.Props{}, ui.Text(text("ordered"))),
					html.Th(html.Props{}, ui.Text(text("received"))),
					html.Th(html.Props{}, ui.Text(text("invoiced"))))),
			html.Tbody(html.Props{}, rows...)))
}
