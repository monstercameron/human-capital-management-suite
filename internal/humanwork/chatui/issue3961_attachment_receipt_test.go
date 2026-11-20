package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func TestIssue3961_ReferenceOnlyAttachmentIsSaidNotRead(t *testing.T) {
	body := "The checklist changed in two places.\n\nSources\n- [Leave policy · v1.0.0](/workspace/app/docs?document=leave) <!--chat.agent.source.readable:true-->\n" +
		chat.AttachmentReceiptMark(chat.AttachmentReceipt{Reference: 2, Unavailable: 1})
	want := map[string][]string{
		"en-US": {"Attached files", "Reference only, content not read: 2", "Not accessible, not read: 1", "Choose a document"},
		"de-DE": {"Angehängte Dateien", "Nur als Verweis, Inhalt nicht gelesen: 2", "Nicht zugänglich, nicht gelesen: 1", "Dokument auswählen"},
		"ar":    {"الملفات المرفقة", "إشارة فقط، لم يُقرأ المحتوى: 2", "لا يمكن الوصول إليه، لم يُقرأ: 1", "اختيار مستند"},
	}
	for locale, expected := range want {
		envelope := parseAgentReplyEnvelope(body)
		if strings.Contains(envelope.Body, "chat.agent") || strings.Contains(envelope.Body, "attachments") {
			t.Fatalf("%s: the receipt comment leaked into the answer: %q", locale, envelope.Body)
		}
		if len(envelope.Sources) != 1 {
			t.Fatalf("%s: the receipt broke the sources: %+v", locale, envelope.Sources)
		}
		markup := renderNode(t, html.Div(html.Props{}, renderAgentReplySources(Model{Locale: locale}, envelope)...))
		for _, text := range expected {
			if !strings.Contains(markup, text) {
				t.Fatalf("%s: missing %q: %s", locale, text, markup)
			}
		}
		for _, text := range []string{`data-attachment-status="reference"`, `href="/workspace/app/docs"`, "aria-label="} {
			if !strings.Contains(markup, text) {
				t.Fatalf("%s: missing %q: %s", locale, text, markup)
			}
		}
		if strings.Contains(markup, "Content read") {
			t.Fatalf("%s: claims content was read: %s", locale, markup)
		}
	}
}

func TestIssue3961_NoAttachmentsMeansNoReceipt(t *testing.T) {
	envelope := parseAgentReplyEnvelope("Plain answer.")
	markup := renderNode(t, html.Div(html.Props{}, renderAgentReplySources(Model{Locale: "en-US"}, envelope)...))
	if strings.Contains(markup, "attachment-receipt") {
		t.Fatalf("a receipt was drawn with no attachment: %s", markup)
	}
	if chat.AttachmentReceiptMark(chat.AttachmentReceipt{}) != "" {
		t.Fatal("an empty receipt wrote a comment")
	}
}

func TestIssue3961_ReadAttachmentOffersNoWorkaround(t *testing.T) {
	envelope := parseAgentReplyEnvelope("Done.\n" + chat.AttachmentReceiptMark(chat.AttachmentReceipt{Read: 1}))
	markup := renderNode(t, html.Div(html.Props{}, renderAgentReplySources(Model{Locale: "en-US"}, envelope)...))
	if !strings.Contains(markup, "Content read: 1") || strings.Contains(markup, "Choose a document") {
		t.Fatalf("a read file is described wrongly: %s", markup)
	}
}

func TestIssue3961_MarkerIsNeverShown(t *testing.T) {
	text := chatStripInternalMarkers("Hi " + chat.AttachmentReceiptMark(chat.AttachmentReceipt{Reference: 1}))
	if strings.Contains(text, "chat.agent") {
		t.Fatalf("the comment is not stripped from plain surfaces: %q", text)
	}
}
