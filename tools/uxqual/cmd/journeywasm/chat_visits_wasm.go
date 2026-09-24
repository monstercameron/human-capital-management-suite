//go:build js && wasm

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/productclient"
)

const maxChatVisitDestinations = productclient.MaxChatVisitEntries

type chatVisitScopeState struct {
	counts map[string]int
	last   string
}

var chatVisitScope string
var chatVisitCurrent *chatVisitScopeState

func init() {
	productui.ActionLauncherChatVisitsProvider = currentChatLauncherVisits
}

func currentChatLauncherVisits() []productui.ActionLauncherChatVisit {
	model := chatBrowser.snapshot()
	tenant, principal := model.CurrentTenantID, model.CurrentUser
	if tenant == "" || principal == "" {
		return nil
	}
	counts := chatVisitState(tenant, principal).counts
	visits := make([]productui.ActionLauncherChatVisit, 0, len(model.Conversations))
	seen := make(map[string]bool, len(model.Conversations))
	for _, conversation := range model.Conversations {
		id := strings.TrimSpace(conversation.ID)
		if id == "" || seen[id] || !conversation.Joined {
			continue
		}
		seen[id] = true
		host := conversation.HostTenantID
		if host == "" {
			host = tenant
		}
		visits = append(visits, productui.ActionLauncherChatVisit{Conversation: conversation, Count: counts[chatVisitDestinationDigest(host, id)]})
	}
	return visits
}

func recordSelectedChatVisit(tenant, principal, selected string) {
	model := chatBrowser.snapshot()
	if tenant == "" || principal == "" || selected == "" || model.CurrentTenantID != tenant || model.CurrentUser != principal {
		return
	}
	host := tenant
	joined := false
	for _, conversation := range model.Conversations {
		if conversation.ID == selected && conversation.Joined {
			joined = true
			if conversation.HostTenantID != "" {
				host = conversation.HostTenantID
			}
			break
		}
	}
	if !joined {
		return
	}
	state := chatVisitState(tenant, principal)
	visitID := chatVisitDestinationDigest(host, selected)
	if state.last == visitID {
		return
	}
	state.last = visitID
	if state.counts[visitID] < productclient.MaxChatVisitCount {
		state.counts[visitID]++
	}
	pruneChatVisitCounts(state.counts)
	persistChatVisitState(tenant, principal, state.counts)
}

func releaseSelectedChatVisit(tenant, principal string) {
	if state := chatVisitState(tenant, principal); state != nil {
		state.last = ""
	}
}

func chatVisitState(tenant, principal string) *chatVisitScopeState {
	digest := chatVisitScopeDigest(tenant, principal)
	if digest == "" {
		return &chatVisitScopeState{counts: map[string]int{}}
	}
	if chatVisitScope == digest && chatVisitCurrent != nil {
		return chatVisitCurrent
	}
	state := &chatVisitScopeState{counts: map[string]int{}}
	key := productclient.ChatVisitsStorageKey(digest)
	if key != "" {
		if storage, ok := browserSessionStorage(); ok {
			if raw, readOK := browserStorageGet(storage, key); readOK {
				if visits, valid := productclient.DecodeChatVisits(raw); valid {
					for _, visit := range visits {
						state.counts[visit.ConversationID] = visit.Count
					}
				}
			}
		}
	}
	chatVisitScope, chatVisitCurrent = digest, state
	return state
}

func pruneChatVisitCounts(counts map[string]int) {
	if len(counts) <= maxChatVisitDestinations {
		return
	}
	visits := make([]productclient.ChatVisit, 0, len(counts))
	for id, count := range counts {
		visits = append(visits, productclient.ChatVisit{ConversationID: id, Count: count})
	}
	sort.Slice(visits, func(i, j int) bool {
		if visits[i].Count != visits[j].Count {
			return visits[i].Count > visits[j].Count
		}
		return visits[i].ConversationID < visits[j].ConversationID
	})
	for _, evicted := range visits[maxChatVisitDestinations:] {
		delete(counts, evicted.ConversationID)
	}
}

func persistChatVisitState(tenant, principal string, counts map[string]int) {
	visits := make([]productclient.ChatVisit, 0, len(counts))
	for id, count := range counts {
		if strings.TrimSpace(id) == "" || count < 1 {
			continue
		}
		visits = append(visits, productclient.ChatVisit{ConversationID: id, Count: count})
	}
	sort.Slice(visits, func(i, j int) bool {
		if visits[i].Count != visits[j].Count {
			return visits[i].Count > visits[j].Count
		}
		return visits[i].ConversationID < visits[j].ConversationID
	})
	if len(visits) > maxChatVisitDestinations {
		visits = visits[:maxChatVisitDestinations]
	}
	value, ok := productclient.EncodeChatVisits(visits)
	if !ok {
		return
	}
	key := productclient.ChatVisitsStorageKey(chatVisitScopeDigest(tenant, principal))
	if key == "" {
		return
	}
	if storage, available := browserSessionStorage(); available {
		browserStorageSet(storage, key, value)
	}
}

func chatVisitScopeDigest(tenant, principal string) string {
	if tenant == "" || principal == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(tenant + "\x00" + principal))
	return hex.EncodeToString(digest[:])
}

func chatVisitDestinationDigest(hostTenant, conversationID string) string {
	if hostTenant == "" || conversationID == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(hostTenant + "\x00" + conversationID))
	return hex.EncodeToString(digest[:])
}
