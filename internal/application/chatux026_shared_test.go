package application

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// A page loaded after the asker shared a private answer is told which message
// the answer was shared as, so the card reads "Shared with #channel" and can
// lead to the copy. When the asker removes the copy the answer is private
// again. Real chat, real private card, real share.
func TestTodo_CHATUX_026_Integration(t *testing.T) {
	room := chatbug067Room(t)
	surface := room.shareSurface(agentUX070ShareOptions{})

	only := func(answers []personachat.PrivateAnswer, err error) personachat.PrivateAnswer {
		t.Helper()
		if err != nil || len(answers) != 1 {
			t.Fatalf("stored answers = %+v %v, want the asker's one answer", answers, err)
		}
		return answers[0]
	}
	before := only(surface.openingAnswers(room.ctx, room.channel))
	if before.InvocationID != room.question.ID || before.ThreadID != room.question.ID || before.SharedPostID != "" || before.Body == "" {
		t.Fatalf("a private answer that was never shared = %+v", before)
	}

	shared, err := surface.ShareAnswer(room.ctx, room.question.ID, "share-key-0001")
	if err != nil || shared.PostID == "" {
		t.Fatalf("share = %+v %v", shared, err)
	}
	after := only(surface.openingAnswers(room.ctx, room.channel))
	if after.SharedPostID != shared.PostID || after.ID != before.ID {
		t.Fatalf("after sharing the answer names %q as its copy, want %q", after.SharedPostID, shared.PostID)
	}

	// Somebody else's reply under the question with the same words is not the
	// asker's shared copy, and neither is the asker saying something else there.
	copyPosts := room.channelPosts("owner")
	employee := chat.Principal{TenantID: "tenant-a", SubjectID: "employee"}
	if _, err = room.service.SendPost(room.ctx, chat.SendPostRequest{Principal: employee, TenantID: "tenant-a", ConversationID: room.channel, ParentID: room.question.ID, Body: copyPosts[len(copyPosts)-1].Body, IdempotencyKey: "employee-repeats"}); err != nil {
		t.Fatal(err)
	}
	if _, err = room.service.SendPost(room.ctx, chat.SendPostRequest{Principal: room.asker, TenantID: "tenant-a", ConversationID: room.channel, ParentID: room.question.ID, Body: "Thanks, that helps.", IdempotencyKey: "owner-thanks"}); err != nil {
		t.Fatal(err)
	}
	if again := only(surface.openingAnswers(room.ctx, room.channel)); again.SharedPostID != shared.PostID {
		t.Fatalf("another reply was taken for the shared copy: %q", again.SharedPostID)
	}

	// The asker removes the shared copy: the answer is private again.
	if _, err = room.service.DeletePost(room.ctx, chat.DeletePostRequest{Principal: room.asker, TenantID: "tenant-a", ConversationID: room.channel, PostID: shared.PostID, ExpectedRevision: 1}); err != nil {
		t.Fatalf("the asker cannot remove the shared copy: %v", err)
	}
	if removed := only(surface.openingAnswers(room.ctx, room.channel)); removed.SharedPostID != "" {
		t.Fatalf("a removed copy is still named as shared: %q", removed.SharedPostID)
	}

	// The comparison ignores what differs between the card and its copy.
	card := "40 hours carry over.\n\nSources\n- [Paid time off policy](/workspace/app/docs?document=d1&version=v1) <!--chat.agent.source.readable:true-->\n<!--chat.agent.private:asked-->"
	posted := "40 hours carry over.\n\nSources\n- [Paid time off policy](/workspace/app/docs?document=d1&version=v1)"
	if personaShareComparable(card) != personaShareComparable(posted) || personaShareComparable(card) == personaShareComparable("41 hours carry over.") {
		t.Fatalf("the card and its copy do not compare: %q vs %q", personaShareComparable(card), personaShareComparable(posted))
	}
}
