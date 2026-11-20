package application

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// A thread read reports each attached file as a reference the run did not read,
// and the receipt that rides on the answer says so.
func TestIssue3961_ThreadReadCountsAttachmentsAsReferencesOnly(t *testing.T) {
	posts := []chat.Post{
		{ID: "p1", TenantID: "t", ConversationID: "c", Body: "Here is the checklist", References: []chat.Reference{{Kind: chat.MediaAttachment, ID: "m1"}, {Kind: chat.MediaAttachment, ID: "m2"}}},
		{ID: "p2", TenantID: "t", ConversationID: "c", Body: "What changed in the attached checklist?"},
	}
	result := threadResult(posts, "p1")
	receipt := personaThreadAttachmentReceipt(result.Posts)
	if receipt.Reference != 2 || receipt.Read != 0 || receipt.Total() != 2 {
		t.Fatalf("receipt = %+v, want two reference-only attachments and none read", receipt)
	}
	mark := chat.AttachmentReceiptMark(receipt)
	clean, parsed, found := chat.SplitAttachmentReceipt("Answer.\n" + mark)
	if !found || clean != "Answer." || parsed != receipt {
		t.Fatalf("round trip: clean=%q parsed=%+v found=%t", clean, parsed, found)
	}
}

func TestIssue3961_NoAttachmentsWritesNoMark(t *testing.T) {
	result := threadResult([]chat.Post{{ID: "p", Body: "hello"}}, "p")
	if mark := chat.AttachmentReceiptMark(personaThreadAttachmentReceipt(result.Posts)); mark != "" {
		t.Fatalf("a thread with no file wrote %q", mark)
	}
}

func TestIssue3961_MarkCarriesCountsNeverNames(t *testing.T) {
	mark := chat.AttachmentReceiptMark(chat.AttachmentReceipt{Reference: 1})
	if strings.Contains(mark, "m1") || !strings.HasPrefix(mark, "<!--chat.agent.attachments:") {
		t.Fatalf("mark = %q", mark)
	}
	if _, _, found := chat.SplitAttachmentReceipt("x <!--chat.agent.attachments:reference=-3,read=abc-->"); found {
		t.Fatal("a malformed comment was accepted")
	}
}
