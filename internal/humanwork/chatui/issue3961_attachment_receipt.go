package chatui

import (
	"strconv"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Issue 3961: a file shown beside a question is not proof the agent read it. The
// card says, under the answer, what became of each file the run saw: its content
// was read, it was a reference only, its kind is unsupported, or it could not be
// reached. A file that was not read comes with the one authorized way forward:
// choose the document from the person's documents. The counts come from the run
// (chat.AttachmentReceipt); nothing here predicts a capability from the file's
// presence, and no file's name or detail is printed, so nothing the reader may
// not open is disclosed.

// issue3961Docs is the Documents hub, the authorized place to choose a document.
const issue3961Docs = "/workspace/app/docs"

var issue3961Copy = map[string][3]string{
	"heading":     {"Attached files", "Angehängte Dateien", "الملفات المرفقة"},
	"read":        {"Content read", "Inhalt gelesen", "تمت قراءة المحتوى"},
	"reference":   {"Reference only, content not read", "Nur als Verweis, Inhalt nicht gelesen", "إشارة فقط، لم يُقرأ المحتوى"},
	"unsupported": {"Unsupported file type, not read", "Nicht unterstützter Dateityp, nicht gelesen", "نوع ملف غير مدعوم، لم يُقرأ"},
	"unavailable": {"Not accessible, not read", "Nicht zugänglich, nicht gelesen", "لا يمكن الوصول إليه، لم يُقرأ"},
	"hint":        {"This answer is not based on what is inside those files. To have an agent use a file's content, choose it from your documents.", "Diese Antwort beruht nicht auf dem Inhalt dieser Dateien. Damit ein Agent den Inhalt nutzt, wählen Sie die Datei aus Ihren Dokumenten.", "لا تستند هذه الإجابة إلى محتوى تلك الملفات. لكي يستخدم الوكيل محتوى ملف، اخترْه من مستنداتك."},
	"choose":      {"Choose a document", "Dokument auswählen", "اختيار مستند"},
}

func issue3961Text(locale, key string) string {
	copy := issue3961Copy[key]
	return chatbug039Text("issue3961."+key, copy[chatbug039LocaleIndex(locale)], copy[0])
}

// issue3961Receipt draws the receipt under an answer, or nothing when the run
// saw no attachment.
func issue3961Receipt(model Model, receipt chat.AttachmentReceipt) ui.Node {
	if receipt.Total() == 0 {
		return nil
	}
	heading := issue3961Text(model.Locale, "heading")
	lines := make([]ui.Node, 0, 4)
	for _, row := range []struct {
		key string
		n   int
	}{{"read", receipt.Read}, {"reference", receipt.Reference}, {"unsupported", receipt.Unsupported}, {"unavailable", receipt.Unavailable}} {
		if row.n > 0 {
			lines = append(lines, html.Li(html.Props{Class: "agent-attachment-receipt-line", Data: map[string]string{"attachment-status": row.key}, Text: issue3961Text(model.Locale, row.key) + ": " + strconv.Itoa(row.n)}))
		}
	}
	children := []ui.Node{html.H4(html.Props{Text: heading}), html.Ul(html.Props{}, lines...)}
	if receipt.Reference+receipt.Unsupported+receipt.Unavailable > 0 {
		open := ui.UseEvent(func(event ui.Event) {
			if eventPlainClick(event) && model.Callbacks.Navigate != nil {
				event.PreventDefault()
				model.Callbacks.Navigate(issue3961Docs)
			}
		})
		children = append(children,
			html.P(html.Props{Class: "agent-attachment-receipt-hint", Dir: "auto", Text: issue3961Text(model.Locale, "hint")}),
			html.A(html.Props{Class: "agent-attachment-receipt-choose", Href: issue3961Docs, OnClick: open, Text: issue3961Text(model.Locale, "choose")}))
	}
	return html.Section(html.Props{Class: "agent-attachment-receipt", Aria: map[string]string{"label": heading}, Data: map[string]string{"attachment-receipt": "true"}}, children...)
}

// issue3961Split removes the receipt comment from an answer's body.
func issue3961Split(body string) (string, chat.AttachmentReceipt) {
	clean, receipt, found := chat.SplitAttachmentReceipt(body)
	if !found {
		return body, chat.AttachmentReceipt{}
	}
	return clean, receipt
}

const issue3961Styles = `.agent-attachment-receipt{margin-block:.65rem 0;padding-block-start:.55rem;border-block-start:1px solid var(--hcm-color-border);max-width:100%;box-sizing:border-box}.agent-attachment-receipt h4{margin:0 0 .3rem;font-size:.8125rem;color:var(--hcm-color-text-muted)}.agent-attachment-receipt ul{margin:0;padding-inline-start:1.2rem}.agent-attachment-receipt-hint{margin:.3rem 0;font-size:.8125rem;color:var(--hcm-color-text-muted);overflow-wrap:anywhere}.agent-attachment-receipt-choose{display:inline-flex;align-items:center;min-height:44px}`
