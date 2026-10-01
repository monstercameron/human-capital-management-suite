package timeclockstore

import (
	"context"
	"testing"
)

func TestAdapterListEventsRequiresStore(t *testing.T) {
	_, err := (Adapter{}).ListEvents(context.Background(), "tenant-a", 0, 10)
	if err == nil {
		t.Fatal("nil store unexpectedly succeeded")
	}
}
