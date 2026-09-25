//go:build js && wasm

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

var chatPersonActions struct {
	sync.Mutex
	epoch      uint64
	selection  uint64
	pending    map[string]uint64
	authorized bool
}

func resetChatPersonActions() {
	chatPersonActions.Lock()
	chatPersonActions.epoch++
	chatPersonActions.selection++
	chatPersonActions.pending = make(map[string]uint64)
	chatPersonActions.authorized = false
	chatPersonActions.Unlock()
}

func withChatPersonCallbacks(callbacks chatui.Callbacks, cfg journeyclient.Config) chatui.Callbacks {
	callbacks.OpenPerson = func(subjectID string) { openChatPerson(cfg, subjectID) }
	callbacks.ClosePerson = func() {
		chatPersonActions.Lock()
		chatPersonActions.selection++
		chatPersonActions.authorized = false
		chatPersonActions.Unlock()
		chatBrowser.mutate(func(model *chatui.Model) { model.ShowPerson = false; model.PersonDetails = nil })
		chatHistory.push(chatBrowser.snapshot())
		refreshChatRoute()
	}
	callbacks.StartDirectMessage = func(subjectID string) { go startChatPersonDM(cfg, subjectID) }
	return callbacks
}

func openChatPerson(cfg journeyclient.Config, subjectID string) {
	openChatPersonRecorded(cfg, subjectID, chatHistory.push)
}

// openChatPersonRecorded opens the person pane and records it with record:
// a click in chat pushes a Back stop; a "#person=" link that already made
// its own browser entry replaces it, so Back returns to where the link was.
func openChatPersonRecorded(cfg journeyclient.Config, subjectID string, record func(chatui.Model)) {
	subjectID = strings.TrimSpace(subjectID)
	workersClient := chatWorkers
	if subjectID == "" || workersClient == nil {
		return
	}
	active := chatBrowser.config(cfg)
	chatPersonActions.Lock()
	chatPersonActions.selection++
	chatPersonActions.authorized = false
	selection, epoch := chatPersonActions.selection, chatPersonActions.epoch
	chatPersonActions.Unlock()
	chatBrowser.mutate(func(model *chatui.Model) {
		model.ShowPerson = true
		model.PersonDetails = &chatui.PersonDetails{ID: subjectID}
	})
	record(chatBrowser.snapshot())
	refreshChatRoute()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		result, err := workersClient.ListChatDirectory(chatRPCContext(ctx, active), &journeyv1.ListChatDirectoryRequest{})
		// The read completes outside the UI event loop. A direct state update
		// there can leave the mounted component's render queued until the next
		// user click, with the panel stuck on its initial placeholders.
		ui.PostAsync(func() {
			if !chatPersonSelectionCurrent(epoch, selection, active, subjectID) {
				return
			}
			if err != nil {
				chatActionFailed("read this person's business details", err)
				chatBrowser.mutate(func(model *chatui.Model) {
					if model.ShowPerson && model.PersonDetails != nil && model.PersonDetails.ID == subjectID {
						model.PersonDetails = &chatui.PersonDetails{ID: subjectID, Unavailable: true}
					}
				})
				refreshChatRoute()
				return
			}
			person, ok := chatPersonDetailsFromWorkers(result.GetWorkers(), subjectID)
			if !ok {
				// The pane itself says the details are unavailable; a second
				// banner above the timeline only pushed the messages down.
				chatBrowser.mutate(func(model *chatui.Model) {
					if model.ShowPerson && model.PersonDetails != nil && model.PersonDetails.ID == subjectID {
						model.PersonDetails = &chatui.PersonDetails{ID: subjectID, Unavailable: true}
					}
				})
				refreshChatRoute()
				return
			}
			chatPersonActions.Lock()
			if chatPersonActions.epoch != epoch || chatPersonActions.selection != selection {
				chatPersonActions.Unlock()
				return
			}
			chatPersonActions.authorized = true
			chatPersonActions.Unlock()
			chatBrowser.mergeDirectory(map[string]string{subjectID: person.Name, person.ID: person.Name})
			chatBrowser.mutate(func(model *chatui.Model) {
				if model.ShowPerson && model.PersonDetails != nil && model.PersonDetails.ID == subjectID {
					model.PersonDetails = &person
				}
			})
			refreshChatRoute()
		})
	}()
}

func chatPersonSelectionCurrent(epoch, selection uint64, cfg journeyclient.Config, subjectID string) bool {
	chatPersonActions.Lock()
	current := chatPersonActions.epoch == epoch && chatPersonActions.selection == selection
	chatPersonActions.Unlock()
	active := chatBrowser.config(cfg)
	model := chatBrowser.snapshot()
	return current && active.Tenant == cfg.Tenant && active.Subject == cfg.Subject && active.Bearer == cfg.Bearer && model.ShowPerson && model.PersonDetails != nil && model.PersonDetails.ID == subjectID
}

func startChatPersonDM(cfg journeyclient.Config, subjectID string) {
	active := chatBrowser.config(cfg)
	model := chatBrowser.snapshot()
	if !model.ShowPerson || model.PersonDetails == nil || model.PersonDetails.ID != subjectID || subjectID == "" {
		return
	}
	chatPersonActions.Lock()
	key := active.Tenant + "\x00" + active.Subject + "\x00" + subjectID
	selection := chatPersonActions.selection
	if !chatPersonActions.authorized || chatPersonActions.pending[key] == selection {
		chatPersonActions.Unlock()
		return
	}
	chatPersonActions.pending[key] = selection
	epoch := chatPersonActions.epoch
	chatPersonActions.Unlock()
	defer func() {
		chatPersonActions.Lock()
		if chatPersonActions.epoch == epoch && chatPersonActions.pending[key] == selection {
			delete(chatPersonActions.pending, key)
		}
		chatPersonActions.Unlock()
	}()
	client := chatBrowser.conversationClient()
	if client == nil {
		return
	}
	startingRoom := model.SelectedID
	peerName := model.PersonDetails.Name
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	callCtx := chatRPCContext(ctx, active)
	// A fresh admitted listing is required: the rail may still be loading or
	// show only its first page. A peer-owned room can already exist there.
	cursor := ""
	seenCursors := map[string]bool{}
	for page := 0; page < 50; page++ {
		if !chatPersonSelectionCurrent(epoch, selection, active, subjectID) || chatBrowser.selectedID() != startingRoom {
			return
		}
		listed, err := client.ListConversations(callCtx, &chatv1.ListConversationsRequest{TenantId: active.Tenant, Cursor: cursor, PageSize: 100})
		if err != nil {
			if chatPersonSelectionCurrent(epoch, selection, active, subjectID) && chatBrowser.selectedID() == startingRoom {
				chatActionFailed("find an existing direct message", err)
			}
			return
		}
		if !chatPersonSelectionCurrent(epoch, selection, active, subjectID) || chatBrowser.selectedID() != startingRoom {
			return
		}
		for _, room := range listed.GetConversations() {
			if room == nil || room.GetKind() != chatv1.ConversationKind_CONVERSATION_KIND_DIRECT || !room.GetJoined() {
				continue
			}
			result, err := client.ListMemberships(callCtx, &chatv1.ListMembershipsRequest{TenantId: active.Tenant, ConversationId: room.GetId(), PageSize: 3})
			if err != nil {
				if chatPersonSelectionCurrent(epoch, selection, active, subjectID) && chatBrowser.selectedID() == startingRoom {
					chatActionFailed("find an existing direct message", err)
				}
				return
			}
			if !chatPersonSelectionCurrent(epoch, selection, active, subjectID) || chatBrowser.selectedID() != startingRoom {
				return
			}
			if chatDirectMembersMatch(result.GetMemberships(), active.Tenant, active.Subject, subjectID) {
				if chatPersonSelectionCurrent(epoch, selection, active, subjectID) && chatBrowser.selectedID() == startingRoom {
					chatBrowser.mutate(func(model *chatui.Model) {
						adoptChatPersonDM(model, chatConversation(room), subjectID, peerName)
					})
					openChatPersonDM(active, room.GetId())
				}
				return
			}
		}
		next := listed.GetNextCursor()
		if next == "" {
			break
		}
		if next == cursor || seenCursors[next] {
			chatActionSucceeded("Could not finish checking existing direct messages")
			return
		}
		seenCursors[next] = true
		cursor = next
		if page == 49 {
			chatActionSucceeded("Could not finish checking existing direct messages")
			return
		}
	}
	if !chatPersonSelectionCurrent(epoch, selection, active, subjectID) || chatBrowser.selectedID() != startingRoom {
		return
	}
	created, err := client.CreateConversation(callCtx, &chatv1.CreateConversationRequest{
		TenantId: active.Tenant, Kind: chatv1.ConversationKind_CONVERSATION_KIND_DIRECT,
		OwnerId: active.Subject, Members: []*chatv1.MemberRef{{TenantId: active.Tenant, SubjectId: subjectID}},
		IdempotencyKey: chatPersonDMKey(active.Tenant, active.Subject, subjectID),
	})
	if err != nil {
		if chatPersonSelectionCurrent(epoch, selection, active, subjectID) {
			chatActionFailed("start this direct message", err)
		}
		return
	}
	if !chatPersonSelectionCurrent(epoch, selection, active, subjectID) || chatBrowser.selectedID() != startingRoom {
		return
	}
	room := created.GetConversation()
	if room.GetId() == "" {
		return
	}
	chatBrowser.mutate(func(model *chatui.Model) {
		adoptChatPersonDM(model, chatConversation(room), subjectID, peerName)
	})
	invalidateChatRecipientProjection()
	openChatPersonDM(active, room.GetId())
}

func openChatPersonDM(cfg journeyclient.Config, roomID string) {
	already := chatBrowser.alreadyOpen(roomID) || chatBrowser.alreadyOpening(roomID)
	chatPersonActions.Lock()
	chatPersonActions.selection++
	chatPersonActions.authorized = false
	chatPersonActions.Unlock()
	chatBrowser.mutate(func(model *chatui.Model) {
		model.ShowPerson = false
		model.PersonDetails = nil
	})
	// NAV: starting (or finding) a DM from a person's details selected the
	// room without pushing it to the address bar; see
	// pushChatHistoryForSelection. Not for the already-open case: that is a
	// no-op selection, and pushing an identical entry would let Back land
	// right back where the reader already was.
	if already {
		refreshChatRoute()
	} else {
		pushChatHistoryForSelection(roomID)
		openChatConversation(cfg, roomID)
	}
	chatui.FocusComposerFor(roomID)
}

func chatDirectMembersMatch(members []*chatv1.Membership, tenant, caller, peer string) bool {
	want := 2
	if caller == peer {
		want = 1
	}
	if len(members) != want {
		return false
	}
	seen := map[string]bool{}
	for _, m := range members {
		if m == nil || m.GetLeftAt() != nil || m.GetHomeTenantId() != tenant || m.GetSubjectId() == "" || seen[m.GetSubjectId()] {
			return false
		}
		seen[m.GetSubjectId()] = true
	}
	return seen[caller] && seen[peer]
}

func chatPersonDMKey(tenant, caller, peer string) string {
	ids := []string{caller, peer}
	sort.Strings(ids)
	sum := sha256.Sum256([]byte(tenant + "\x00" + ids[0] + "\x00" + ids[1]))
	return "wasm-person-dm-" + hex.EncodeToString(sum[:])
}
