package main

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// chatProjectAnswerStates are the settled states a project read can end in.
// "restricted" is the only neutral answer and is reserved for a refusal or a
// malformed answer; "missing", "archived" and "failed" say what happened.
var chatProjectAnswerStates = map[string]bool{"ready": true, "restricted": true, "unavailable": true, "missing": true, "archived": true, "failed": true}

// chatProjectReadFailureState names a read that returned no project or task.
// A refusal stays the neutral restricted answer; a record that is gone is
// missing; any other error (a transient failure, a cancelled batch) is a failed
// read the reader can retry, never a claim that they lack access.
func chatProjectReadFailureState(err error) string {
	reported, ok := status.FromError(err)
	if !ok {
		return "failed"
	}
	switch reported.Code() {
	case codes.PermissionDenied:
		return "restricted"
	case codes.NotFound:
		return "missing"
	}
	return "failed"
}

// chatProjectTaskAnswer is a non-ready answer for one reference.
func chatProjectTaskAnswer(ref chatui.ProjectTaskReference, state string) chatui.ProjectTaskPreview {
	return chatui.ProjectTaskPreview{ProjectID: ref.ProjectID, TaskID: ref.TaskID, State: state}
}

const chatProjectTaskPreviewLimit = 50
const chatProjectTaskPreviewFreshFor = 15 * time.Second
const chatProjectTaskPreviewPendingTimeout = 20 * time.Second

type chatProjectTaskPreviewEntry struct {
	value   chatui.ProjectTaskPreview
	at      time.Time
	pending bool
}

// chatProjectTaskPreviewCache holds only viewer-authorized task projections.
// Identity changes discard every title; short revalidation bounds how long a
// removed project grant can retain a previously authorized title in memory.
type chatProjectTaskPreviewCache struct {
	mu       sync.Mutex
	identity string
	epoch    uint64
	entries  map[string]chatProjectTaskPreviewEntry
	order    []string
}

var chatProjectTaskPreviews = &chatProjectTaskPreviewCache{}

func chatProjectTaskPreviewRefs(model chatui.Model) []chatui.ProjectTaskReference {
	seen := map[string]bool{}
	refs := make([]chatui.ProjectTaskReference, 0, 8)
	add := func(body string) {
		for _, ref := range chatui.ProjectTaskReferences(body, model.EmbedOrigin) {
			key := chatui.ProjectTaskPreviewKey(ref.ProjectID, ref.TaskID)
			if key == "" || seen[key] || len(refs) >= chatProjectTaskPreviewLimit {
				continue
			}
			seen[key] = true
			refs = append(refs, ref)
		}
	}
	add(model.Draft)
	for i := len(model.ThreadMessages) - 1; i >= 0 && len(refs) < chatProjectTaskPreviewLimit; i-- {
		add(model.ThreadMessages[i].Body)
	}
	for i := len(model.Messages) - 1; i >= 0 && len(refs) < chatProjectTaskPreviewLimit; i-- {
		add(model.Messages[i].Body)
	}
	for i := range model.SearchMessages {
		if len(refs) >= chatProjectTaskPreviewLimit {
			break
		}
		add(model.SearchMessages[i].Message.Body)
	}
	return refs
}

func chatProjectTaskPreviewFingerprint(model chatui.Model) string {
	ready := 0
	for _, preview := range model.ProjectTaskPreviews {
		if chatProjectAnswerStates[preview.State] {
			ready++
		}
	}
	refs := chatProjectTaskPreviewRefs(model)
	keys := make([]string, 0, len(refs))
	for _, ref := range refs {
		keys = append(keys, chatui.ProjectTaskPreviewKey(ref.ProjectID, ref.TaskID))
	}
	return model.SelectedID + "|" + strconv.Itoa(ready) + "|" + strings.Join(keys, "|")
}

func (c *chatProjectTaskPreviewCache) claim(identity string, refs []chatui.ProjectTaskReference, now time.Time) ([]chatui.ProjectTaskReference, uint64, map[string]chatui.ProjectTaskPreview) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if identity != c.identity || c.entries == nil {
		c.identity, c.entries, c.order = identity, map[string]chatProjectTaskPreviewEntry{}, nil
		c.epoch++
	}
	shown := make(map[string]chatui.ProjectTaskPreview, len(refs))
	var claims []chatui.ProjectTaskReference
	for _, ref := range refs {
		key := chatui.ProjectTaskPreviewKey(ref.ProjectID, ref.TaskID)
		entry, ok := c.entries[key]
		if ok && entry.pending && now.Sub(entry.at) > chatProjectTaskPreviewPendingTimeout {
			entry.pending = false
			if entry.value.State != "ready" {
				// The read never answered: a retryable failure, not a refusal.
				entry.value = chatProjectTaskAnswer(ref, "failed")
			}
			c.entries[key] = entry
		}
		if ok && (entry.pending || now.Sub(entry.at) < chatProjectTaskPreviewFreshFor) {
			shown[key] = entry.value
			continue
		}
		if !ok {
			if len(c.order) >= chatProjectTaskPreviewLimit {
				delete(c.entries, c.order[0])
				c.order = c.order[1:]
			}
			c.order = append(c.order, key)
		}
		value := chatui.ProjectTaskPreview{ProjectID: ref.ProjectID, TaskID: ref.TaskID, State: "loading"}
		c.entries[key] = chatProjectTaskPreviewEntry{value: value, at: now, pending: true}
		shown[key] = value
		claims = append(claims, ref)
	}
	return claims, c.epoch, shown
}

func (c *chatProjectTaskPreviewCache) finish(identity string, epoch uint64, answers map[string]chatui.ProjectTaskPreview, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if identity != c.identity || epoch != c.epoch {
		return false
	}
	for key, value := range answers {
		if entry, ok := c.entries[key]; ok && entry.pending {
			if !chatProjectAnswerAccepted(key, value) {
				value = chatui.ProjectTaskPreview{ProjectID: entry.value.ProjectID, TaskID: entry.value.TaskID, State: "restricted"}
			}
			c.entries[key] = chatProjectTaskPreviewEntry{value: value, at: now}
		}
	}
	return true
}

func (c *chatProjectTaskPreviewCache) reset() {
	c.mu.Lock()
	c.identity, c.entries, c.order = "", nil, nil
	c.epoch++
	c.mu.Unlock()
}

// projection exposes only fresh cached values for references currently in
// the model. This is applied before rendering, so a title left in an older
// model snapshot cannot survive cache expiry or appear briefly after reload.
func (c *chatProjectTaskPreviewCache) projection(identity string, refs []chatui.ProjectTaskReference, now time.Time) map[string]chatui.ProjectTaskPreview {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]chatui.ProjectTaskPreview, len(refs))
	if identity != c.identity {
		return out
	}
	for _, ref := range refs {
		key := chatui.ProjectTaskPreviewKey(ref.ProjectID, ref.TaskID)
		if entry, ok := c.entries[key]; ok && (entry.pending || now.Sub(entry.at) < chatProjectTaskPreviewFreshFor) {
			out[key] = entry.value
		}
	}
	return out
}

func mergeChatProjectTaskPreviews(model *chatui.Model, shown map[string]chatui.ProjectTaskPreview) bool {
	changed := false
	for key, value := range shown {
		if current, ok := model.ProjectTaskPreviews[key]; !ok || current != value {
			changed = true
			break
		}
	}
	if !changed {
		return false
	}
	next := make(map[string]chatui.ProjectTaskPreview, len(model.ProjectTaskPreviews)+len(shown))
	for key, value := range model.ProjectTaskPreviews {
		next[key] = value
	}
	for key, value := range shown {
		next[key] = value
	}
	model.ProjectTaskPreviews = next
	return true
}

func restrictedChatProjectTaskAnswer(ref chatui.ProjectTaskReference) chatui.ProjectTaskPreview {
	return chatProjectTaskAnswer(ref, "restricted")
}

// chatProjectAnswerAccepted reports whether a resolved answer may be cached
// under its key: a ready card needs a readable title, and every other settled
// state is accepted as the definite answer it names.
func chatProjectAnswerAccepted(key string, value chatui.ProjectTaskPreview) bool {
	if key != chatui.ProjectTaskPreviewKey(value.ProjectID, value.TaskID) || !chatProjectAnswerStates[value.State] {
		return false
	}
	if value.State == "ready" {
		return value.Readable && strings.TrimSpace(value.Title) != ""
	}
	return true
}
