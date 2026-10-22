package chatstore

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// TestTodo_CHATGATE_014_Integration_Leaver: removing a member from a conversation
// erases the labels, segments and answers the gate holds for them in the same
// transaction as the removal (migration 00049), leaves everybody else alone, and
// a removal that does not go through erases nothing.
func TestTodo_CHATGATE_014_Integration_Leaver(t *testing.T) {
	store, service, c := chatgateFixture(t)
	visible := chatgate.Visibility{Administrators: true, Members: true, Consumers: []string{chatgate.ConsumerSegments}}
	c.Key, c.ExpectedRevision = "redefine", 2
	d := chatgate.Definition{Mode: "automatic", Purpose: "Join discussion", Fields: []chatgate.Field{{ID: "office", Kind: "single_choice", KindVersion: "1.0.0", Label: "Office", Purpose: "Place you", DataClass: "INTERNAL", Required: true, Options: []string{"Denver", "Austin"}, Visibility: visible, RetentionDays: 30}}}
	if err := service.Define(t.Context(), c, d); err != nil {
		t.Fatal(err)
	}
	c.Key, c.ExpectedRevision = "republish", 3
	if _, err := service.Publish(t.Context(), c, chatgate.Version{Major: 2}); err != nil {
		t.Fatal(err)
	}
	c.Key, c.ExpectedRevision = "rules", 4
	if err := service.SetSegments(t.Context(), c, []chatgate.SegmentRule{{ID: "r1", Field: "office", Value: "Denver", Label: "Denver crew", Segment: "Denver office"}}, "First rules"); err != nil {
		t.Fatal(err)
	}
	for i, person := range []string{"worker", "friend"} {
		join := c
		join.Actor.Person, join.Key, join.ExpectedRevision = person, "join-"+person, uint64(5+i)
		if _, err := service.Submit(t.Context(), join, "2.0.0", map[string]json.RawMessage{"office": json.RawMessage(`"Denver"`)}); err != nil {
			t.Fatal(err)
		}
	}
	// What the database holds, read from the tables and not through the service.
	held := func() (placed, answers map[string]int, revision uint64) {
		t.Helper()
		placed, answers = map[string]int{}, map[string]int{}
		if err := store.RunTenantTx(t.Context(), "tenant-a", func(tx dbport.Tx) error {
			var payload []byte
			if err := tx.QueryRow(t.Context(), `SELECT state_json FROM chat_gate_state WHERE tenant_id='tenant-a' AND conversation_id='gated'`).Scan(&payload); err != nil {
				return err
			}
			var state chatgate.State
			if err := json.Unmarshal(payload, &state); err != nil {
				return err
			}
			revision = state.Gate.Revision
			for person, ps := range state.Segments.Assignments {
				placed[person] = len(ps)
			}
			rows, err := tx.Query(t.Context(), `SELECT person_id FROM chat_gate_answer WHERE tenant_id='tenant-a' AND conversation_id='gated'`)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var person string
				if err := rows.Scan(&person); err != nil {
					return err
				}
				answers[person]++
			}
			return rows.Err()
		}); err != nil {
			t.Fatal(err)
		}
		return placed, answers, revision
	}
	placed, answers, revision := held()
	if placed["worker"] != 2 || placed["friend"] != 2 || answers["worker"] != 1 || answers["friend"] != 1 {
		t.Fatalf("before the removal: placements %v answers %v", placed, answers)
	}

	adapter := NewAdapter(store)
	owner := chat.Principal{TenantID: "tenant-a", SubjectID: "owner"}
	// A removal that does not go through (stale revision) erases nothing.
	if _, err := adapter.RemoveMembership(t.Context(), owner, "tenant-a", "gated", "tenant-a", "worker", 99); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("a removal at a stale revision: %v", err)
	}
	if placed, answers, _ = held(); placed["worker"] != 2 || answers["worker"] != 1 {
		t.Fatalf("a refused removal erased the person's placements: %v %v", placed, answers)
	}
	// The removal itself.
	if _, err := adapter.RemoveMembership(t.Context(), owner, "tenant-a", "gated", "tenant-a", "worker", 1); err != nil {
		t.Fatal(err)
	}
	placed, answers, after := held()
	if _, ok := placed["worker"]; ok || answers["worker"] != 0 {
		t.Fatalf("after the removal the person still holds placements %v or answers %v", placed, answers)
	}
	if placed["friend"] != 2 || answers["friend"] != 1 {
		t.Fatalf("the removal touched somebody else: %v %v", placed, answers)
	}
	if after != revision+1 {
		t.Fatalf("the gate's revision moved from %d to %d, want one step", revision, after)
	}
	repo := service.Repository.(*GateRepository)
	if got, err := repo.SegmentState(t.Context(), c.Scope); err != nil || got.InAny("worker", []string{"Denver office"}) || !got.InAny("friend", []string{"Denver office"}) {
		t.Fatalf("segments after the removal: %+v %v", got, err)
	}
	// A conversation with no gate is removed from as before.
	seedConversationRow(t, store, Conversation{ID: "plain", TenantID: "tenant-a", Kind: "PRIVATE_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, w8Members("owner", "sam"))
	if _, err := adapter.RemoveMembership(t.Context(), owner, "tenant-a", "plain", "tenant-a", "sam", 1); err != nil {
		t.Fatalf("a removal from a channel without a gate: %v", err)
	}
}
