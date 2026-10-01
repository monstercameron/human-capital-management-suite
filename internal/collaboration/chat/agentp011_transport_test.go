package chat

import "testing"

func TestTodo_AGENTP_011_EphemeralDeliveryContract(t *testing.T) {
	delivery := EphemeralDelivery{ID: "e1", ThreadID: "root", Body: "private", OnlyVisibleToYou: true}
	watch := WatchEvent{EphemeralDelivery: &delivery}
	if watch.EphemeralDelivery == nil || !watch.EphemeralDelivery.OnlyVisibleToYou {
		t.Fatal("watch contract lost recipient-only delivery")
	}
	if watch.EphemeralDelivery.ThreadID != "root" || watch.EphemeralDelivery.Body != "private" {
		t.Fatalf("watch contract lost delivery content: %+v", watch.EphemeralDelivery)
	}
}
