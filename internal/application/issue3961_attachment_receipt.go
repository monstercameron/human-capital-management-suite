package application

import (
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Issue 3961: what a run did with the files attached to the thread it read. The
// thread read carries a file as a reference only, so every attachment is a
// reference whose content the run did not receive. The count goes onto the
// answer as a typed comment (chat.AttachmentReceiptMark) that the card turns
// into words, so the answer never lets a person believe a file was read.

// personaPostAttachmentCount is how many of a post's references are files.
func personaPostAttachmentCount(references []chat.Reference) int {
	n := 0
	for _, reference := range references {
		if reference.Kind == chat.MediaAttachment {
			n++
		}
	}
	return n
}

// personaThreadAttachmentReceipt is the receipt for the posts a run read.
func personaThreadAttachmentReceipt(posts []agentinvoke.ThreadPost) chat.AttachmentReceipt {
	var receipt chat.AttachmentReceipt
	for _, post := range posts {
		receipt.Reference += post.AttachmentCount
	}
	return receipt
}
