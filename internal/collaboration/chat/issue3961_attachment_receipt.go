package chat

import (
	"fmt"
	"strconv"
	"strings"
)

// Issue 3961: an answer says what it did with the files attached to the
// conversation it answers. The run reads a thread's posts, and a file on a post
// is only a reference there: its content is never loaded. The writer of an
// answer leaves one typed trailing comment, the way an ungrounded answer does,
// and the card turns it into a line in the reader's language. The server writes
// no English sentence of its own into a message a person reads, and the comment
// carries counts only: it never names a file the reader may not open.
const attachmentReceiptPrefix = "<!--chat.agent.attachments:"

// AttachmentReceipt counts the attachments a run saw, by what became of each.
type AttachmentReceipt struct {
	// Read is the number whose content the run received.
	Read int
	// Reference is the number the run knew of by reference only: the content
	// was not read.
	Reference int
	// Unsupported is the number of a kind the run cannot read.
	Unsupported int
	// Unavailable is the number the run could not reach.
	Unavailable int
}

// Total is every attachment the receipt accounts for.
func (r AttachmentReceipt) Total() int {
	return r.Read + r.Reference + r.Unsupported + r.Unavailable
}

// AttachmentReceiptMark is the trailing comment for r, or "" when no attachment
// was in the context the run read.
func AttachmentReceiptMark(r AttachmentReceipt) string {
	if r.Read < 0 || r.Reference < 0 || r.Unsupported < 0 || r.Unavailable < 0 || r.Total() == 0 {
		return ""
	}
	return fmt.Sprintf("%sread=%d,reference=%d,unsupported=%d,unavailable=%d-->", attachmentReceiptPrefix, r.Read, r.Reference, r.Unsupported, r.Unavailable)
}

// SplitAttachmentReceipt removes the comment from body and returns what it
// said. found is false when the body carries no well-formed comment.
func SplitAttachmentReceipt(body string) (clean string, receipt AttachmentReceipt, found bool) {
	start := strings.LastIndex(body, attachmentReceiptPrefix)
	if start < 0 {
		return body, AttachmentReceipt{}, false
	}
	end := strings.Index(body[start:], "-->")
	if end < 0 {
		return body, AttachmentReceipt{}, false
	}
	list := body[start+len(attachmentReceiptPrefix) : start+end]
	for _, part := range strings.Split(list, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		n, err := strconv.Atoi(value)
		if !ok || err != nil || n < 0 || n > 10000 {
			continue
		}
		switch key {
		case "read":
			receipt.Read = n
		case "reference":
			receipt.Reference = n
		case "unsupported":
			receipt.Unsupported = n
		case "unavailable":
			receipt.Unavailable = n
		}
	}
	head := strings.TrimRight(body[:start], " \t\r\n")
	tail := strings.TrimLeft(body[start+end+len("-->"):], " \t\r\n")
	clean = head
	if tail != "" {
		clean = head + "\n\n" + tail
	}
	return clean, receipt, receipt.Total() > 0
}
