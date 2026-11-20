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
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3790: the browser side of "turn a message into a task". The calls are
// plain JSON over the three /api/chat/messages/ routes; nothing here decides
// who may read a message or create a task, the server does.

type chatworkLinkJSON struct {
	PostID    string `json:"post_id"`
	ProjectID string `json:"project_id"`
	TaskID    string `json:"task_id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	OwnerID   string `json:"owner_id"`
	DueDate   string `json:"due_date"`
}

type chatworkReply struct {
	Projects []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"projects"`
	Links []chatworkLinkJSON `json:"links"`
	Link  *chatworkLinkJSON  `json:"link"`
}

type chatworkConvert struct {
	ConversationID string `json:"conversation_id"`
	Target         string `json:"target"`
	ProjectID      string `json:"project_id"`
	Title          string `json:"title"`
	OwnerID        string `json:"owner_id,omitempty"`
	DueDate        string `json:"due_date,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
}

type chatworkError string

func (e chatworkError) Error() string { return string(e) }

func chatworkErrorCode(err error) string {
	var code chatworkError
	if errors.As(err, &code) {
		return string(code)
	}
	return "unavailable"
}

// chatworkCall makes one request. A non-200 answer is reduced to the server's
// short code; the body of a refusal is never shown.
func chatworkCall(ctx context.Context, client *http.Client, cfg journeyclient.Config, method, path string, query url.Values, command *chatworkConvert) (chatworkReply, error) {
	var reply chatworkReply
	endpoint, problem := tunnelHTTPBase(cfg.TunnelURL)
	if problem != tunnelOK || cfg.Bearer == "" || client == nil {
		return reply, chatworkError("unavailable")
	}
	endpoint.Path, endpoint.RawQuery, endpoint.Fragment = path, query.Encode(), ""
	var body io.Reader
	if command != nil {
		encoded, err := json.Marshal(command)
		if err != nil {
			return reply, err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return reply, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(req)
	if err != nil {
		return reply, chatworkError("unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var refusal struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&refusal)
		if refusal.Code == "" {
			refusal.Code = "unavailable"
		}
		return chatworkReply{}, chatworkError(refusal.Code)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&reply); err != nil {
		return chatworkReply{}, chatworkError("unavailable")
	}
	return reply, nil
}

func chatworkLinked(ctx context.Context, client *http.Client, cfg journeyclient.Config, conversation string) ([]chatworkLinkJSON, error) {
	reply, err := chatworkCall(ctx, client, cfg, http.MethodGet, "/api/chat/messages/linked-work", url.Values{"conversation_id": {conversation}}, nil)
	return reply.Links, err
}

func chatworkProjects(ctx context.Context, client *http.Client, cfg journeyclient.Config) ([]chatui.MessageWorkProject, error) {
	reply, err := chatworkCall(ctx, client, cfg, http.MethodGet, "/api/chat/messages/convert-options", nil, nil)
	if err != nil {
		return nil, err
	}
	projects := make([]chatui.MessageWorkProject, 0, len(reply.Projects))
	for _, project := range reply.Projects {
		if project.ID != "" && strings.TrimSpace(project.Name) != "" {
			projects = append(projects, chatui.MessageWorkProject{ID: project.ID, Name: project.Name})
		}
	}
	return projects, nil
}

func chatworkConvertMessage(ctx context.Context, client *http.Client, cfg journeyclient.Config, message string, command chatworkConvert) (chatworkLinkJSON, error) {
	if strings.TrimSpace(message) == "" || strings.Contains(message, "/") {
		return chatworkLinkJSON{}, chatworkError("invalid_argument")
	}
	reply, err := chatworkCall(ctx, client, cfg, http.MethodPost, "/api/chat/messages/"+url.PathEscape(message)+"/convert", nil, &command)
	if err != nil {
		return chatworkLinkJSON{}, err
	}
	if reply.Link == nil {
		return chatworkLinkJSON{}, chatworkError("unavailable")
	}
	return *reply.Link, nil
}

// chatworkNewKey names one reviewed attempt. The form keeps it until the task
// is made or the person changes what they asked for, so a retry after an
// uncertain answer recovers the same task instead of making a second.
func chatworkNewKey() string { return "chatwork-" + uuid.NewString() }

// chatworkByPost groups the viewer's linked work by the message it came from.
func chatworkByPost(links []chatworkLinkJSON) map[string][]chatui.MessageWorkLink {
	grouped := make(map[string][]chatui.MessageWorkLink)
	for _, link := range links {
		if link.PostID == "" || link.TaskID == "" {
			continue
		}
		grouped[link.PostID] = append(grouped[link.PostID], chatui.MessageWorkLink{PostID: link.PostID, ProjectID: link.ProjectID, TaskID: link.TaskID, Title: link.Title, Status: link.Status, OwnerID: link.OwnerID, DueDate: link.DueDate})
	}
	return grouped
}

// chatworkTitle prefills the form with the message's first line, cut to what a
// task title may hold. The person reviews and edits it before anything is made.
func chatworkTitle(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if utf8.RuneCountInString(line) > 200 {
			line = string([]rune(line)[:200])
		}
		return line
	}
	return ""
}

// chatworkOwners lists who the task may be handed to: the person themselves,
// or the person who wrote the message.
func chatworkOwners(m chatui.Model, msg chatui.Message, me, author string) []chatui.MessageWorkOwner {
	owners := []chatui.MessageWorkOwner{{ID: m.CurrentUser, Label: me}}
	if msg.AuthorID != "" && msg.AuthorID != m.CurrentUser && strings.TrimSpace(msg.Author) != "" {
		owners = append(owners, chatui.MessageWorkOwner{ID: msg.AuthorID, Label: msg.Author + " (" + author + ")"})
	}
	return owners
}
