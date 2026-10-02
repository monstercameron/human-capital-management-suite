package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

type chatsaveCommand struct {
	Action         string     `json:"action"`
	ConversationID string     `json:"conversation_id"`
	PostID         string     `json:"post_id"`
	Note           *string    `json:"note,omitempty"`
	SetDue         bool       `json:"set_due,omitempty"`
	DueAt          *time.Time `json:"due_at,omitempty"`
}

type chatsaveError string

func (e chatsaveError) Error() string { return string(e) }

func chatsaveRequest(ctx context.Context, client *http.Client, cfg journeyclient.Config, host, tab, cursor, query string, command *chatsaveCommand) (chat.SavedPage, error) {
	var page chat.SavedPage
	endpoint, err := url.Parse(cfg.TunnelURL)
	if err != nil || endpoint.Host == "" || cfg.Bearer == "" || client == nil {
		return page, chatsaveError("unavailable")
	}
	switch endpoint.Scheme {
	case "ws":
		endpoint.Scheme = "http"
	case "wss":
		endpoint.Scheme = "https"
	case "http", "https":
	default:
		return page, chatsaveError("unavailable")
	}
	endpoint.Path, endpoint.RawQuery, endpoint.Fragment = "/api/chat/saved", "", ""
	params := url.Values{"host": {host}, "tab": {tab}, "cursor": {cursor}, "limit": {"200"}}
	if query != "" {
		params.Set("query", query)
	}
	endpoint.RawQuery = params.Encode()
	method := http.MethodGet
	var body io.Reader
	if command != nil {
		if command.ConversationID == "" || command.PostID == "" {
			return page, chatsaveError("invalid_argument")
		}
		switch command.Action {
		case "save", "remove", "done", "reopen", "note", "due":
		default:
			return page, chatsaveError("invalid_argument")
		}
		b, err := json.Marshal(command)
		if err != nil {
			return page, err
		}
		method = http.MethodPost
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return page, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(req)
	if err != nil {
		return page, chatsaveError("unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var reply struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&reply)
		if reply.Code == "" {
			reply.Code = "unavailable"
		}
		return page, chatsaveError(reply.Code)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 4<<20))
	if command != nil {
		var reply json.RawMessage
		err = decoder.Decode(&reply)
	} else if query != "" {
		err = decoder.Decode(&page.Items)
	} else {
		err = decoder.Decode(&page)
	}
	if err != nil {
		return chat.SavedPage{}, chatsaveError("unavailable")
	}
	return page, nil
}

func chatsaveRows(page chat.SavedPage, cfg journeyclient.Config, model chatui.Model) []chatui.SavedMessageRow {
	rows := []chatui.SavedMessageRow{}
	copy := chatui.SavedMessagesCopy(cfg.Locale)
	for _, item := range page.Items {
		if item.HomeTenantID != cfg.Tenant || item.PersonID != cfg.Subject {
			continue
		}
		row := chatui.SavedMessageRow{TenantID: item.TenantID, ConversationID: item.ConversationID, PostID: item.PostID, Note: item.Note, Done: item.State == chat.SavedDone, Availability: item.Availability, Author: copy.AuthorUnavailable}
		if item.Availability == "readable" && item.Post != nil {
			row.Body, row.Channel, row.Sequence = item.Post.Body, item.Channel, item.Post.Sequence
			row.AuthorID, row.SentAt, row.Revision = item.Post.AuthorID, item.Post.CreatedAt, item.Post.Revision
			row.References, row.Attachments = chatsaveReferences(item.Post.References)
			if item.Post.AuthorHomeTenantID == cfg.Tenant {
				for _, person := range model.SearchDirectory {
					if person.ID == item.Post.AuthorID && person.Name != "" {
						row.Author = person.Name
						break
					}
				}
			}
			for _, member := range model.Members {
				if member.ID == item.Post.AuthorID && member.HomeTenantID == item.Post.AuthorHomeTenantID {
					row.Author = member.Name
					break
				}
			}
			for _, message := range append(append([]chatui.Message{}, model.Messages...), model.ThreadMessages...) {
				if message.ID == item.PostID && model.SelectedID == item.ConversationID && message.Author != "" {
					row.Author = message.Author
					break
				}
			}
			if item.Post.AuthorID == cfg.Subject && item.Post.AuthorHomeTenantID == cfg.Tenant && model.CurrentUserName != "" {
				row.Author = model.CurrentUserName
			}
		}
		if item.Post != nil && (row.Author == item.Post.AuthorID || strings.HasPrefix(row.Author, "hcmnext.") || chatsaveLooksLikeIdentifier(row.Author)) {
			row.Author = copy.AuthorUnavailable
		}
		if row.Availability == "readable" {
			row.Channel = chatsaveChannelLabel(item, model)
			row.InChannel = chatsaveIsChannel(item.ConversationID, model)
			if row.Author == copy.AuthorUnavailable && item.Post != nil {
				// An agent's answer is written by the agent, which the reader
				// knows by the same name the conversation shows.
				for _, persona := range model.ResolvedPersonaMentions {
					if persona.Reference.ID == item.Post.AuthorID && strings.TrimSpace(persona.Reference.Display) != "" {
						row.Author = persona.Reference.Display
						break
					}
				}
			}
		}
		if item.DueAt != nil {
			row.DueAt = *item.DueAt
		}
		if item.State == chat.SavedDone {
			row.DoneAt = item.UpdatedAt
		}
		rows = append(rows, row)
	}
	return rows
}

// chatsaveLooksLikeIdentifier is true for a value a person would read as a
// machine identifier: one unspaced token of hex digits and separators, or a
// namespaced internal key.
func chatsaveLooksLikeIdentifier(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if strings.HasPrefix(value, "hcmnext.") {
		return true
	}
	if len(value) < 16 {
		return false
	}
	digit := false
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
			digit = true
		case r >= 'a' && r <= 'f', r >= 'A' && r <= 'F', r == '-', r == '_', r == ':':
		default:
			return false
		}
	}
	return digit
}

// chatsaveChannelLabel is the channel name the person already sees in the
// sidebar; the service's own name is the fallback and an identifier is never shown.
func chatsaveChannelLabel(item chat.SavedItem, model chatui.Model) string {
	for _, conversation := range model.Conversations {
		if conversation.ID == item.ConversationID && strings.TrimSpace(conversation.Name) != "" && !chatsaveLooksLikeIdentifier(conversation.Name) {
			return conversation.Name
		}
	}
	if item.Channel == item.ConversationID || chatsaveLooksLikeIdentifier(item.Channel) {
		return ""
	}
	return item.Channel
}

// chatsaveApplyLocal applies one command to a copy of the list the way the
// service will, so the count and the hover-bar bookmark change before the
// round trip. A save is shown only when add carries what the row needs.
func chatsaveApplyLocal(page chat.SavedPage, host string, command chatsaveCommand, add *chat.SavedItem) chat.SavedPage {
	items := make([]chat.SavedItem, 0, len(page.Items)+1)
	found := false
	for _, item := range page.Items {
		if item.TenantID != host || item.ConversationID != command.ConversationID || item.PostID != command.PostID {
			items = append(items, item)
			continue
		}
		found = true
		switch command.Action {
		case "remove":
			continue
		case "done":
			item.State = chat.SavedDone
		case "reopen":
			item.State = chat.SavedTodo
		case "due":
			if command.SetDue {
				item.DueAt = command.DueAt
			}
		case "note":
			if command.Note != nil {
				item.Note = *command.Note
			}
		}
		items = append(items, item)
	}
	if !found && command.Action == "save" && add != nil {
		items = append([]chat.SavedItem{*add}, items...)
	}
	return chat.SavedPage{Items: items, NextCursor: page.NextCursor}
}

// chatsaveOptimisticItem is the row a save will produce, built from the
// message the person is looking at. It is nil when that message is not on screen.
func chatsaveOptimisticItem(cfg journeyclient.Config, host string, command chatsaveCommand, model chatui.Model, now time.Time) *chat.SavedItem {
	if model.SelectedID != command.ConversationID {
		return nil
	}
	for _, message := range append(append([]chatui.Message{}, model.Messages...), model.ThreadMessages...) {
		if message.ID != command.PostID {
			continue
		}
		sent := message.SentAt
		if sent.IsZero() {
			sent = now
		}
		post := chat.Post{ID: message.ID, TenantID: host, ConversationID: command.ConversationID, AuthorID: message.AuthorID, Body: message.Body, Sequence: message.Sequence, Revision: message.Revision, CreatedAt: sent}
		for _, reference := range message.PersonaReferences {
			post.References = append(post.References, chat.Reference{Kind: chat.ReferenceKind(reference.Kind), TenantID: reference.TenantID, ID: reference.ID, Display: reference.Display, ConversationID: reference.ConversationID})
		}
		for range message.Attachments {
			post.References = append(post.References, chat.Reference{Kind: chat.MediaAttachment})
		}
		item := chat.SavedItem{TenantID: host, HomeTenantID: cfg.Tenant, PersonID: cfg.Subject, ConversationID: command.ConversationID, PostID: command.PostID, State: chat.SavedTodo, CreatedAt: now, UpdatedAt: now, Availability: "readable", Post: &post}
		for _, conversation := range model.Conversations {
			if conversation.ID == command.ConversationID {
				item.Channel = conversation.Name
				break
			}
		}
		return &item
	}
	return nil
}

func chatsaveShortcut(key string, alt, shift, ctrl, meta, editable, repeat bool) bool {
	return strings.EqualFold(key, "s") && alt && shift && !ctrl && !meta && !editable && !repeat
}

func chatsaveErrorCode(err error) string {
	var code chatsaveError
	if errors.As(err, &code) {
		return string(code)
	}
	return "unavailable"
}
