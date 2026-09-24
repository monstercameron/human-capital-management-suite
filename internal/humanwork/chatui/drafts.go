package chatui

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// draftPersistence is tab-scoped so a reload can restore a composer without
// leaving its text in durable browser storage. The platform implementation
// stores one identity-scoped snapshot; native tests inject an in-memory store.
type draftPersistence interface {
	load(string) map[string]string
	save(string, map[string]string)
	clear()
}

type browserDrafts struct {
	identity   string
	selected   string
	values     map[string]string
	storage    draftPersistence
	ready      bool
	discarded  bool
	clearModel *Model
}

func (d *browserDrafts) owner(model Model) string {
	return strings.TrimSpace(model.CurrentTenantID) + "\x00" + strings.TrimSpace(model.CurrentUser)
}

func (d *browserDrafts) hasOwner(model Model) bool {
	return strings.TrimSpace(model.CurrentTenantID) != "" && strings.TrimSpace(model.CurrentUser) != ""
}

func draftOwnerKey(owner string) string {
	sum := sha256.Sum256([]byte(owner))
	return hex.EncodeToString(sum[:])
}

// prepare projects the selected conversation's draft onto the model. Identity
// boundaries and access failures erase the tab snapshot before it can render.
func (d *browserDrafts) prepare(model *Model) {
	if model == nil {
		return
	}
	owner := d.owner(*model)
	if !d.hasOwner(*model) {
		if d.ready {
			d.clear()
		}
		model.Draft = ""
		return
	}
	if d.ready && d.identity != owner {
		d.clear()
		d.identity, d.selected, d.ready, d.discarded = owner, model.SelectedID, true, true
		model.Draft = ""
		return
	}
	if model.State == StateError {
		if model.RevokedConversationID != "" {
			d.discardConversation(model, model.RevokedConversationID, owner)
			return
		}
		if !d.discarded || len(d.values) > 0 || len(model.Preferences.Drafts) > 0 || model.Draft != "" {
			pending := *model
			pending.Preferences.Drafts = copyMap(model.Preferences.Drafts)
			d.clearModel = &pending
		}
		d.identity, d.selected, d.ready = owner, model.SelectedID, true
		d.clear()
		model.Draft = ""
		model.Preferences.Drafts = map[string]string{}
		return
	}
	if !d.ready {
		d.identity, d.selected, d.ready = owner, model.SelectedID, true
		// The chat model is owned by the live chat session and already carries
		// its server-backed sidebar draft projection. Seed a newly mounted
		// composer from it so route navigation does not require private draft
		// text in browser storage.
		d.values = copyMap(model.Preferences.Drafts)
		if d.values == nil {
			d.values = make(map[string]string)
		}
		if model.SelectedID != "" && model.Draft != "" {
			if _, ok := d.values[model.SelectedID]; !ok {
				d.values[model.SelectedID] = model.Draft
			}
		}
	}
	selectedChanged := d.selected != model.SelectedID
	if selectedChanged {
		d.selected = model.SelectedID
	}
	if model.SelectedID == "" {
		model.Draft = ""
		return
	}
	if value, ok := d.values[model.SelectedID]; ok {
		model.Draft = value
		return
	}
	if selectedChanged || d.discarded {
		model.Draft = ""
		return
	}
	if model.Draft != "" {
		d.set(model.SelectedID, model.Draft)
	}
}

func (d *browserDrafts) discardConversation(model *Model, conversationID, owner string) {
	if d.identity != "" && d.identity != owner {
		d.clear()
		d.identity, d.selected, d.ready = owner, model.SelectedID, true
		d.discarded = true
	} else {
		if !d.ready {
			d.identity, d.selected, d.ready = owner, model.SelectedID, true
			d.values = copyMap(model.Preferences.Drafts)
		}
		if d.values == nil {
			d.values = make(map[string]string)
		}
	}
	for id, body := range model.Preferences.Drafts {
		if _, ok := d.values[id]; !ok && body != "" {
			d.values[id] = body
		}
	}
	if model.SelectedID != "" && model.Draft != "" {
		d.values[model.SelectedID] = model.Draft
	}
	pending := *model
	pending.Preferences.Drafts = map[string]string{}
	pending.Draft = ""
	body, exists := d.values[conversationID]
	if !exists {
		body = model.Preferences.Drafts[conversationID]
		exists = body != ""
	}
	if conversationID == model.SelectedID && model.Draft != "" {
		body, exists = model.Draft, true
	}
	if exists {
		pending.Preferences.Drafts[conversationID] = body
		d.clearModel = &pending
	}
	d.set(conversationID, "")
	delete(model.Preferences.Drafts, conversationID)
	if model.SelectedID == conversationID {
		model.Draft = ""
	}
	model.RevokedConversationID = ""
}

func (d *browserDrafts) takeClearModel() *Model {
	if d == nil {
		return nil
	}
	model := d.clearModel
	d.clearModel = nil
	return model
}

func (d *browserDrafts) store() draftPersistence {
	if d.storage == nil {
		d.storage = newDraftPersistence()
	}
	return d.storage
}

func (d *browserDrafts) set(conversationID, value string) {
	if d == nil || conversationID == "" {
		return
	}
	if d.values == nil {
		d.values = make(map[string]string)
	}
	if value == "" {
		delete(d.values, conversationID)
	} else {
		d.discarded = false
		d.values[conversationID] = value
	}
	if d.identity != "" {
		d.store().save(draftOwnerKey(d.identity), d.values)
	}
}

func (d *browserDrafts) canSend(model Model) bool {
	return d != nil && d.ready && !d.discarded && d.hasOwner(model) && d.identity == d.owner(model) && (model.State == StateReady || model.State == StateEmpty)
}

func (d *browserDrafts) selectConversation(currentID, currentValue, nextID string) string {
	if d == nil {
		return ""
	}
	d.set(currentID, currentValue)
	d.selected = nextID
	return d.values[nextID]
}

// clear discards both the in-memory copy and the browser snapshot. It is used
// for revocation, logout and identity changes, never for normal page unmount.
func (d *browserDrafts) clear() {
	if d == nil {
		return
	}
	clear(d.values)
	d.values = make(map[string]string)
	d.selected = ""
	d.discarded = true
	if d.storage != nil {
		d.storage.clear()
	}
}

// releaseMemory drops the component copy on navigation while preserving the
// tab snapshot so a new chat page instance can restore it after navigation.
func (d *browserDrafts) releaseMemory() {
	if d != nil {
		clear(d.values)
		d.values = nil
		d.identity = ""
		d.selected = ""
		d.ready = false
	}
}
