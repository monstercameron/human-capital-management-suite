package main

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"sync"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

const chatattach001MaxBytes int64 = 20 << 20

type chatattach001Room struct {
	files   []chatui.Chatattach001Draft
	error   string
	sending bool
}

type chatattach001State struct {
	mu      sync.Mutex
	rooms   map[string]*chatattach001Room
	next    uint64
	choose  func()
	send    func(string, string, []chatui.ChatReference)
	remove  func(string, string)
	dispose func()
	// chooseThread opens the reply box's picker.
	chooseThread func()
	// available is whether the server takes uploads at all. Until it has said
	// so nothing is offered: "Attach a file" on a server that refuses every
	// upload is a control that cannot work.
	available bool
}

// A draft of files belongs to a scope: the conversation, for its composer, or
// one thread of it, for that thread's reply box. The scope is the key of
// everything this state holds; the conversation is cut out of it where the
// server is asked (the upload, the references, the post).
const chatattach001ThreadMark = "\x1fthread:"

// chatattach001Scope is the scope of a conversation's composer (no parent) or
// of the reply box under parent.
func chatattach001Scope(room, parent string) string {
	if room == "" || parent == "" {
		return room
	}
	return room + chatattach001ThreadMark + parent
}

// chatattach001ScopeParts is the conversation of a scope and the message a
// reply from it goes under ("" for the conversation's own composer).
func chatattach001ScopeParts(scope string) (room, parent string) {
	room, parent, _ = strings.Cut(scope, chatattach001ThreadMark)
	return room, parent
}

// chatattach001AvailabilityRoute is where the server says whether it takes
// uploads (application.ChatAttachmentsAvailabilityPath).
const chatattach001AvailabilityRoute = "/v1/chat/media/attachments/availability"

// chatattach001Available reads the server's answer. Anything but a plain yes
// is a no: a server that does not know the question, an error, a refusal.
func chatattach001Available(status int, body string) bool {
	if status != 200 {
		return false
	}
	var answer struct {
		Uploads bool `json:"uploads"`
	}
	return json.Unmarshal([]byte(body), &answer) == nil && answer.Uploads
}

// What the files say about a composer's Send button.
const (
	chatattach001SendLeave = iota // no files: the text decides, as it always did
	chatattach001SendOff          // a file is uploading or failed, or a send is going
	chatattach001SendOn           // uploaded files and no text: they are a message
)

// chatattach001SendState decides a Send button from the files under a composer.
// capable is whether the composer can send at all; text is what its box holds.
func chatattach001SendState(capable bool, text string, files int, ready bool) int {
	switch {
	case !ready:
		return chatattach001SendOff
	case files == 0 || !capable || strings.TrimSpace(text) != "":
		return chatattach001SendLeave
	}
	return chatattach001SendOn
}

// setAvailable records the server's answer about uploads.
func (s *chatattach001State) setAvailable(available bool) {
	s.mu.Lock()
	s.available = available
	s.mu.Unlock()
}

// takes reports whether files may be added here.
func (s *chatattach001State) takes() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.available
}

func chatattach001Allowed(name, contentType string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".exe", ".dll", ".com", ".bat", ".cmd", ".ps1", ".js", ".sh", ".zip", ".rar", ".7z", ".gz", ".tar", ".docm", ".xlsm", ".pptm":
		return false
	}
	switch contentType {
	case "image/png", "image/jpeg", "image/gif", "image/bmp", "application/pdf", "text/plain", "text/csv", "text/markdown", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "application/vnd.openxmlformats-officedocument.presentationml.presentation":
		return true
	case "", "application/octet-stream":
		switch strings.ToLower(path.Ext(name)) {
		case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".pdf", ".txt", ".csv", ".md", ".docx", ".xlsx", ".pptx":
			return true
		}
	}
	return false
}

func (s *chatattach001State) room(id string) *chatattach001Room {
	if s.rooms == nil {
		s.rooms = map[string]*chatattach001Room{}
	}
	if s.rooms[id] == nil {
		s.rooms[id] = &chatattach001Room{}
	}
	return s.rooms[id]
}

func (s *chatattach001State) add(room, name, contentType, url string, size int64) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.room(room)
	if room == "" || r.sending {
		return "", false
	}
	// The reason a file was refused stays until the next choice (clearError):
	// several files arrive as one drop, and an accepted one after a refused one
	// used to wipe the line that said why the other is missing.
	refused := ""
	switch {
	case len(r.files) >= 10:
		refused = "limit"
	case size <= 0:
		refused = "empty"
	case size > chatattach001MaxBytes:
		refused = "size"
	case !chatattach001Allowed(name, contentType):
		refused = "type"
	}
	if refused != "" {
		r.error = refused
		return "", false
	}
	s.next++
	key := fmt.Sprintf("attachment-%d", s.next)
	r.files = append(r.files, chatui.Chatattach001Draft{Key: key, Attachment: chatui.Attachment{Name: name, ContentType: contentType, URL: url, Bytes: size}, Uploading: true})
	return key, true
}

// clearError forgets the last refusal when the person chooses, pastes or drops
// files again.
func (s *chatattach001State) clearError(room string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.room(room).error = ""
}

func (s *chatattach001State) progress(room, key string, percent int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.room(room).files {
		if s.room(room).files[i].Key == key {
			s.room(room).files[i].Progress = min(99, max(0, percent))
		}
	}
}

// The three ways an upload ends. A refused file (too large, a type that is
// not allowed, the storage limit) is taken off the draft with the reason; an
// upload that only failed to get through stays, marked, so it can be retried.
const (
	chatattach001Uploaded = "uploaded"
	chatattach001Kept     = "kept"
	chatattach001Dropped  = "dropped"
)

func (s *chatattach001State) complete(room, key, artifact, contentType string, size int64, status int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.room(room)
	for i, f := range r.files {
		if f.Key != key {
			continue
		}
		if status < 200 || status >= 300 || artifact == "" || size != f.Bytes {
			if reason := chatattach001Refusal(status); reason != "failed" {
				r.error = reason
				r.files = append(r.files[:i], r.files[i+1:]...)
				return chatattach001Dropped
			}
			r.files[i].Uploading, r.files[i].Progress, r.files[i].Failed = false, 0, true
			return chatattach001Kept
		}
		r.files[i].ID, r.files[i].ContentType = artifact, contentType
		r.files[i].Uploading, r.files[i].Progress, r.files[i].Failed = false, 100, false
		return chatattach001Uploaded
	}
	return chatattach001Dropped
}

// retry puts a failed upload back to uploading and reports whether there was
// one to retry.
func (s *chatattach001State) retry(room, key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.room(room)
	if r.sending {
		return false
	}
	for i := range r.files {
		if r.files[i].Key == key && r.files[i].Failed {
			r.files[i].Uploading, r.files[i].Progress, r.files[i].Failed = true, 0, false
			r.error = ""
			return true
		}
	}
	return false
}

// preview is the local picture address of a draft attachment, "" when it has none.
func (s *chatattach001State) preview(room, key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.room(room).files {
		if f.Key == key {
			return f.URL
		}
	}
	return ""
}

func chatattach001Refusal(status int) string {
	switch status {
	case 413:
		return "size"
	case 415, 422:
		return "type"
	case 429:
		return "quota"
	}
	return "failed"
}

func (s *chatattach001State) drop(room, key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.room(room)
	if r.sending {
		return ""
	}
	for i, f := range r.files {
		if f.Key == key {
			r.files = append(r.files[:i], r.files[i+1:]...)
			return f.URL
		}
	}
	return ""
}

func (s *chatattach001State) projection(room string) *chatui.Chatattach001Composer {
	return s.projectionFor(room, "")
}

// projectionFor is the conversation composer's projection and, when a thread
// is open, its reply box's under Thread.
func (s *chatattach001State) projectionFor(room, parent string) *chatui.Chatattach001Composer {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	view := s.view(room, s.choose)
	if scope := chatattach001Scope(room, parent); scope != room {
		view.Thread = s.view(scope, s.chooseThread)
	}
	return view
}

// view is one scope's projection. Choose is nil where the server takes no
// uploads, which is what leaves "Attach a file" out of the page.
func (s *chatattach001State) view(scope string, choose func()) *chatui.Chatattach001Composer {
	r := s.room(scope)
	if !s.available {
		choose = nil
	}
	return &chatui.Chatattach001Composer{Files: append([]chatui.Chatattach001Draft(nil), r.files...), Error: r.error, Sending: r.sending, Choose: choose,
		Remove: func(key string) {
			if s.remove != nil {
				s.remove(scope, key)
			}
		},
		Send: func(body string, refs []chatui.ChatReference) {
			if s.send != nil {
				s.send(scope, body, refs)
			}
		}}
}

// begin starts sending a scope's files with the text that goes with them, which
// may be none: files alone are a message.
func (s *chatattach001State) begin(scope, tenant, body string, mentions []chatui.ChatReference) ([]*chatv1.Reference, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.room(scope)
	room, _ := chatattach001ScopeParts(scope)
	if r.sending || len(r.files) == 0 || len(r.files) > 10 || tenant == "" || room == "" {
		return nil, "", false
	}
	refs, err := personaChatReferences(mentions, tenant, room)
	if err != nil {
		return nil, "", false
	}
	for _, f := range r.files {
		if f.Uploading || f.ID == "" {
			return nil, "", false
		}
		refs = append(refs, &chatv1.Reference{Kind: chatv1.ReferenceKind_REFERENCE_KIND_MEDIA, Id: f.ID, TenantId: tenant, ConversationId: room, Display: f.Name, ContentType: f.ContentType, ByteSize: uint64(f.Bytes)})
	}
	r.sending = true
	identity, _ := json.Marshal(refs)
	return refs, body + "\x00" + string(identity), true
}

func (s *chatattach001State) finish(room string, success bool) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.room(room)
	r.sending = false
	if !success {
		return nil
	}
	var urls []string
	for _, f := range r.files {
		if f.URL != "" {
			urls = append(urls, f.URL)
		}
	}
	r.files = nil
	r.error = ""
	return urls
}
