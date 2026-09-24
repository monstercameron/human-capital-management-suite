package main

import (
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestChatPersonDetailsFromAuthorizedWorkers(t *testing.T) {
	workers := []*journeyv1.Worker{
		{WorkerRef: "manager", SubjectId: "manager", PreferredName: "Morgan", LegalName: "Morgan Manager"},
		{WorkerRef: "peer", WorkerId: "canonical-peer", SubjectId: "peer", PreferredName: "Pat", LegalName: "Pat Person", JobTitle: "Analyst", OrgUnit: "Finance", Location: "Boston", Company: "Example", BusinessUnit: "Operations", ProfilePhotoUrl: "/photos/peer", ManagerRef: "private-raw-edge", ManagerRelationship: &journeyv1.ManagerRelationshipProjection{Disposition: journeyv1.ManagerRelationshipProjection_DISPOSITION_VISIBLE, ManagerWorkerRef: "manager"}, BasePay: "123456", Currency: "USD"},
	}
	// Chat names people by principal subject only; the entity id and the
	// display slug are not identities it joins on.
	if _, ok := chatPersonDetailsFromWorkers(workers, "canonical-peer"); ok {
		t.Fatal("the entity id resolved a chat person")
	}
	for _, id := range []string{"peer"} {
		got, ok := chatPersonDetailsFromWorkers(workers, id)
		if !ok || got.ID != "peer" || got.Name != "Pat Person" || got.Manager != "Morgan Manager" || got.Department != "Finance" || got.JobTitle != "Analyst" || got.Company != "Example" || got.BusinessUnit != "Operations" || got.Location != "Boston" || got.PhotoURL != "/photos/peer" {
			t.Fatalf("projection for %q = %+v, %v", id, got, ok)
		}
		if got.Phone != "" || got.Email != "" {
			t.Fatalf("undisclosed contact fields must remain empty: %+v", got)
		}
	}
}

func TestChatSearchUsesGovernedCanonicalVisibleWorkers(t *testing.T) {
	workers := []*journeyv1.Worker{
		{WorkerRef: "peer-ref", WorkerId: "peer-entity", SubjectId: "peer-ref", PreferredName: "Alex", LegalName: "Alex Rivera"},
		{WorkerRef: "other", SubjectId: "other", PreferredName: "Taylor", LegalName: "Taylor Jones"},
	}
	directory := chatSearchDirectoryFromWorkers(workers)
	if len(directory) != 2 || directory[0].ID != "peer-ref" || directory[0].Name != "Alex Rivera" {
		t.Fatalf("visible worker directory = %+v", directory)
	}
	got := chatSearchVisibleWorkers(directory, "peer-entity", 20)
	if len(got) != 1 || got[0].ID != "peer-ref" || got[0].Name != "Alex Rivera" {
		t.Fatalf("canonical entity id search = %+v", got)
	}
	got = chatSearchVisibleWorkers(directory, "@peer-entity", 20)
	if len(got) != 1 || got[0].ID != "peer-ref" {
		t.Fatalf("@ prefixed entity id search = %+v", got)
	}
	got = chatSearchVisibleWorkers(directory, "peer-ref", 20)
	if len(got) != 1 || got[0].ID != "peer-ref" {
		t.Fatalf("canonical worker reference search = %+v", got)
	}
	got = chatSearchVisibleWorkers(directory, "alex", 20)
	if len(got) != 1 || got[0].ID != "peer-ref" || got[0].Name != "Alex Rivera" {
		t.Fatalf("governed name search = %+v", got)
	}
	if got = chatSearchVisibleWorkers(directory, "alex", 0); len(got) != 0 {
		t.Fatalf("unbounded/zero result cap returned people: %+v", got)
	}
	if got = chatSearchVisibleWorkers(directory[:1], "taylor", 20); len(got) != 0 {
		t.Fatalf("worker absent from governed projection appeared in search: %+v", got)
	}
}

func TestChatPersonDetailsManagerFailsClosed(t *testing.T) {
	worker := &journeyv1.Worker{WorkerRef: "peer", SubjectId: "peer", LegalName: "Pat", ManagerRef: "secret", ManagerRelationship: &journeyv1.ManagerRelationshipProjection{Disposition: journeyv1.ManagerRelationshipProjection_DISPOSITION_WITHHELD, ManagerWorkerRef: "manager"}}
	got, ok := chatPersonDetailsFromWorkers([]*journeyv1.Worker{worker, {WorkerRef: "manager", SubjectId: "manager", LegalName: "Morgan"}}, "peer")
	if !ok || got.Manager != "" {
		t.Fatalf("withheld manager leaked: %+v, %v", got, ok)
	}
	worker.ManagerRelationship.Disposition = journeyv1.ManagerRelationshipProjection_DISPOSITION_VISIBLE
	got, ok = chatPersonDetailsFromWorkers([]*journeyv1.Worker{worker}, "peer")
	if !ok || got.Manager != "" {
		t.Fatalf("manager outside admitted listing leaked: %+v, %v", got, ok)
	}
	if _, ok := chatPersonDetailsFromWorkers([]*journeyv1.Worker{worker}, "unknown"); ok {
		t.Fatal("unknown subject was projected")
	}
}

func TestChatPersonDMAdoptionUsesDirectoryLabelAndVisibleSection(t *testing.T) {
	model := chatui.Model{Sections: []chatui.SidebarSection{{ID: "channels", Name: "Channels"}, {ID: "direct", Name: "Direct messages"}}}
	room := chatui.Conversation{ID: "opaque-room-uuid", Name: "opaque-room-uuid", Kind: chatui.DirectMessage, Joined: true}
	adoptChatPersonDM(&model, room, "canonical-peer", "Anika Desai")
	if len(model.Conversations) != 1 || model.Conversations[0].Name != "Anika Desai" || model.PeerIDs[room.ID] != "canonical-peer" || len(model.Sections[1].Chats) != 1 || model.Sections[1].Chats[0].Name != "Anika Desai" {
		t.Fatalf("new DM missing directory label or section: %+v", model)
	}
	adoptChatPersonDM(&model, room, "canonical-peer", "Anika Desai")
	if len(model.Conversations) != 1 || len(model.Sections[1].Chats) != 1 {
		t.Fatalf("duplicate DM adoption: %+v", model)
	}
	refreshed := chatDirectoryNamedDirect(room, model.PeerIDs, map[string]string{"canonical-peer": "Anika Desai"})
	if refreshed.Name != "Anika Desai" {
		t.Fatalf("fresh server listing restored opaque room id: %+v", refreshed)
	}
}

func TestChatPersonDMNameResolvesInEitherColdLoadOrder(t *testing.T) {
	for _, order := range []string{"directory-first", "peer-first"} {
		t.Run(order, func(t *testing.T) {
			state := newChatStateForTest(t)
			room := chatui.Conversation{ID: "opaque-room-uuid", Name: "opaque-room-uuid", Kind: chatui.DirectMessage, Joined: true}
			state.mutate(func(model *chatui.Model) {
				model.Conversations = []chatui.Conversation{room}
				model.Sections = []chatui.SidebarSection{{ID: "direct", Name: "Direct messages", Chats: []chatui.Conversation{room}}}
			})
			if order == "directory-first" {
				state.mergeDirectory(map[string]string{"peer": "Anika Desai"})
				if state.applyChatDirectory(state.directorySnapshot()) {
					t.Fatal("name changed before a peer was identified")
				}
			}
			state.mutate(func(model *chatui.Model) { model.PeerIDs = map[string]string{room.ID: "peer"} })
			if order == "peer-first" {
				if state.applyChatDirectory(state.directorySnapshot()) {
					t.Fatal("name changed before the directory arrived")
				}
				state.mergeDirectory(map[string]string{"peer": "Anika Desai"})
			}
			if !state.applyChatDirectory(state.directorySnapshot()) {
				t.Fatal("room was not renamed when both facts arrived")
			}
			got := state.snapshot()
			if got.Conversations[0].Name != "Anika Desai" || got.Sections[0].Chats[0].Name != "Anika Desai" || got.PeerIDs[room.ID] != "peer" {
				t.Fatalf("cold DM kept opaque name or lost identity: %+v", got)
			}
			if state.applyChatDirectory(state.directorySnapshot()) {
				t.Fatal("repeat resolution reported a change")
			}
		})
	}
}

func TestChatPersonDMSelectionReadKeepsPeerName(t *testing.T) {
	for _, source := range []string{"directory", "current-section"} {
		t.Run(source, func(t *testing.T) {
			state := newChatStateForTest(t)
			room := chatui.Conversation{ID: "opaque-room-uuid", Name: "opaque-room-uuid", Kind: chatui.DirectMessage, Joined: true}
			state.mutate(func(model *chatui.Model) {
				model.SelectedID = room.ID
				model.Conversations = []chatui.Conversation{room}
				model.Sections = []chatui.SidebarSection{{ID: "direct", Name: "Direct messages", Chats: []chatui.Conversation{room}}}
			})
			stale := state.snapshot() // The listing starts before peer resolution.
			state.mutate(func(model *chatui.Model) {
				model.PeerIDs = map[string]string{room.ID: "peer"}
				if source == "current-section" {
					model.Sections[0].Chats[0].Name = "Anika Desai"
				}
			})
			if source == "directory" {
				state.mergeDirectory(map[string]string{"peer": "Anika Desai"})
			}
			if !state.adoptLoadedChatProjection(state.currentGeneration(), stale, chatCursor{}, false) {
				t.Fatal("selected-room listing did not commit")
			}
			got := state.snapshot()
			if got.Conversations[0].Name != "Anika Desai" || got.Sections[0].Chats[0].Name != "Anika Desai" || got.PeerIDs[room.ID] != "peer" {
				t.Fatalf("selection restored opaque room label: %+v", got)
			}
		})
	}
}

func TestChatPersonDMConversationEventKeepsPeerName(t *testing.T) {
	room := chatui.Conversation{ID: "opaque-room-uuid", Name: "Anika Desai", Kind: chatui.DirectMessage, Joined: true}
	model := chatui.Model{
		Conversations: []chatui.Conversation{room},
		Sections:      []chatui.SidebarSection{{ID: "direct", Chats: []chatui.Conversation{room}}},
		PeerIDs:       map[string]string{room.ID: "peer"},
	}
	applyChatConversationUpdated(&model, &chatv1.Conversation{Id: room.ID, Kind: chatv1.ConversationKind_CONVERSATION_KIND_DIRECT})
	if model.Conversations[0].Name != "Anika Desai" || model.Sections[0].Chats[0].Name != "Anika Desai" {
		t.Fatalf("stream event replaced peer label with opaque ID: %+v", model)
	}
}

func TestChatPersonPanelSurvivesDelayedProjection(t *testing.T) {
	state := newChatStateForTest(t)
	state.mutate(func(model *chatui.Model) { model.SelectedID = "room" })
	stale := state.snapshot()
	state.mutate(func(model *chatui.Model) {
		model.ShowPerson = true
		model.PersonDetails = &chatui.PersonDetails{ID: "peer", Name: "Pat", Ready: true}
	})
	if !state.adoptLoadedChatProjection(state.currentGeneration(), stale, chatCursor{}, false) {
		t.Fatal("delayed projection was not adopted")
	}
	got := state.snapshot()
	if !got.ShowPerson || got.PersonDetails == nil || got.PersonDetails.ID != "peer" || !got.PersonDetails.Ready {
		t.Fatalf("profile opening was reverted: %+v", got.PersonDetails)
	}
	stale = state.snapshot()
	state.mutate(func(model *chatui.Model) { model.ShowPerson = false; model.PersonDetails = nil })
	if !state.adoptLoadedChatProjection(state.currentGeneration(), stale, chatCursor{}, false) {
		t.Fatal("second delayed projection was not adopted")
	}
	got = state.snapshot()
	if got.ShowPerson || got.PersonDetails != nil {
		t.Fatalf("profile close was reverted: %+v", got.PersonDetails)
	}
}
