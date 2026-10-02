//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// chatlang004 keeps two reads for the languages of a conversation: who reads a
// translation of what the writer is about to send (the composer's line), and
// the selections that were still pending, asked again until they settle.
var chatlang004 struct {
	sync.Mutex
	audienceKey   string
	audienceEpoch uint64
}

// chatlang004ResetAudience makes the next sync read the audience again (a
// person changed their reading settings).
func chatlang004ResetAudience() {
	chatlang004.Lock()
	chatlang004.audienceEpoch++
	chatlang004.Unlock()
}

func chatlang004Host(m chatui.Model, cfg journeyclient.Config) string {
	host := cfg.Tenant
	for _, room := range m.Conversations {
		if room.ID == m.SelectedID && room.HostTenantID != "" {
			host = room.HostTenantID
		}
	}
	return host
}

func chatlang004SyncAudience(ctx context.Context, cfg journeyclient.Config) {
	m := chatBrowser.snapshot()
	if m.ChatFeatures == nil || !m.ChatFeatures.Renderings || !m.ChatFeatures.Translating || m.SelectedID == "" {
		return
	}
	chatlang004.Lock()
	key := chatlang004AudienceKey(m, chatlang004.audienceEpoch)
	if chatlang004.audienceKey == key {
		chatlang004.Unlock()
		return
	}
	chatlang004.audienceKey = key
	chatlang004.Unlock()
	cfg = chatBrowser.config(cfg)
	query := url.Values{"conversation": {m.SelectedID}, "tenant": {chatlang004Host(m, cfg)}}
	generation := chatBrowser.currentGeneration()
	retryKey := chatux012Integrate2Keys + "chatlang-audience|" + m.SelectedID
	chatReadRetries.Cancel(retryKey)
	current := func() bool {
		chatlang004.Lock()
		defer chatlang004.Unlock()
		return ctx.Err() == nil && chatlang004.audienceKey == key
	}
	read := func() bool {
		var view chatui.ChatlangAudience
		call, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := personaChatRequest(call, http.DefaultClient, personaChatHTTPConfig(cfg), http.MethodGet, "/api/chat/renderings/v1/audience?"+query.Encode(), "", &view)
		cancel()
		if ctx.Err() != nil {
			return true
		}
		if err != nil {
			// A refusal is an answer: no line. A failure to get one is asked again.
			return chatux012HTTPFinal(err)
		}
		ui.PostAsync(func() {
			if chatBrowser.commit(generation, func(next *chatui.Model) {
				if next.SelectedID == m.SelectedID {
					next.Chatlang = chatui.ChatlangModel{Audience: view, AudienceRoom: m.SelectedID}
				}
			}) {
				chatStreamRender.Schedule()
			}
		})
		return true
	}
	go func() {
		if !read() {
			chatux012Retry(retryKey, current, read)
		}
	}()
}

// chatlang004Watch asks again about the messages the server could not yet
// translate: at the retry rhythm of every read (1 s, 2 s, 5 s, then 15 s), at
// most twelve times, and only while the same conversation is open.
func chatlang004Watch(ctx context.Context, cfg journeyclient.Config, room string) {
	cfg = chatBrowser.config(cfg)
	key := chatux012Integrate2Keys + "chatlang-pending|" + room
	chatReadRetries.Cancel(key)
	tries := 0
	chatux012Retry(key, func() bool { return ctx.Err() == nil && chatBrowser.selectedID() == room }, func() bool {
		tries++
		m := chatBrowser.snapshot()
		shown := map[string]bool{}
		for _, msg := range integrate2ReaderMessages(m) {
			shown[msg.ID] = true
		}
		var ids []string
		for _, id := range chatlang004Unsettled(m.ReaderSelections) {
			if shown[id] {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 || m.SelectedID != room {
			return true
		}
		if len(ids) > 100 {
			ids = ids[:100]
		}
		query := url.Values{"conversation": {room}, "tenant": {chatlang004Host(m, cfg)}}
		for _, id := range ids {
			query.Add("message", id)
		}
		part := map[string]chatui.ReaderSelection{}
		call, cancel := context.WithTimeout(ctx, 20*time.Second)
		err := personaChatRequest(call, http.DefaultClient, personaChatHTTPConfig(cfg), http.MethodGet, "/api/chat/renderings/v1/reader?"+query.Encode(), "", &part)
		cancel()
		if ctx.Err() != nil {
			return true
		}
		if err != nil {
			return chatux012HTTPFinal(err) || tries >= 12
		}
		answered := map[string]chatui.ReaderSelection{}
		integrate2ApplyReaderAnswer(answered, integrate2ReaderMessages(m), part, nil)
		generation := chatBrowser.currentGeneration()
		ui.PostAsync(func() {
			if chatBrowser.commit(generation, func(next *chatui.Model) {
				if next.SelectedID != room {
					return
				}
				merged := map[string]chatui.ReaderSelection{}
				for id, v := range next.ReaderSelections {
					merged[id] = v
				}
				for id, v := range answered {
					merged[id] = v
				}
				next.ReaderSelections = merged
			}) {
				chatStreamRender.Schedule()
			}
		})
		return tries >= 12 || len(chatlang004Unsettled(answered)) == 0
	})
}
