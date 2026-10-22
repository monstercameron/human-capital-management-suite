package chatfilter

import (
	"strings"
	"testing"
)

// One message is one judgement per rule. A flag rule that matched a word five
// times recorded five hits, so one message made five moderation queue items and
// a message that repeated the word a few hundred times filled the queue
// (lane R6).
func TestR6_OneHitRecordPerRulePerMessage(t *testing.T) {
	store := newMemStore()
	delivery := &recordingDelivery{}
	store.defs["t"] = []Definition{{ID: "r6", Name: "R6 rule", Version: "1.0.0", Kind: "words", Match: []string{"quartz"}, Action: "flag"}}
	store.rawEnable("t", Enablement{RuleID: "r6", Enabled: true})
	svc := &Service{Store: store, Registry: NewRegistry(), Delivery: delivery}
	body := strings.Repeat("quartz ", 5)
	res, err := svc.Evaluate(t.Context(), Input{Tenant: "t", Channel: "c", Subject: "writer", Body: body}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 5 {
		t.Fatalf("the result still reports every span: %d", len(res.Hits))
	}
	if rows := store.records(); len(rows) != 1 || rows[0].Hit.Span.Start != 0 {
		t.Fatalf("stored %d hit records for one message, want 1 (the first span)", len(rows))
	}
	if len(delivery.records) != 1 {
		t.Fatalf("delivered %d hit records for one message, want 1", len(delivery.records))
	}
}
