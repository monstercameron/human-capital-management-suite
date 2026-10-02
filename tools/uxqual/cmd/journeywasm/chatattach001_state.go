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
	r.error = ""
	switch {
	case len(r.files) >= 10:
		r.error = "limit"
	case size <= 0:
		r.error = "empty"
	case size > chatattach001MaxBytes:
		r.error = "size"
	case !chatattach001Allowed(name, contentType):
		r.error = "type"
	}
	if r.error != "" {
		return "", false
	}
	s.next++
	key := fmt.Sprintf("attachment-%d", s.next)
	r.files = append(r.files, chatui.Chatattach001Draft{Key: key, Attachment: chatui.Attachment{Name: name, ContentType: contentType, URL: url, Bytes: size}, Uploading: true})
	return key, true
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

func (s *chatattach001State) complete(room, key, artifact, contentType string, size int64, status int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.room(room)
	for i, f := range r.files {
		if f.Key != key {
			continue
		}
		if status < 200 || status >= 300 || artifact == "" || size != f.Bytes {
			r.error = chatattach001Refusal(status)
			r.files = append(r.files[:i], r.files[i+1:]...)
			return false
		}
		r.files[i].ID, r.files[i].ContentType = artifact, contentType
		r.files[i].Uploading, r.files[i].Progress = false, 100
		return true
	}
	return false
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
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.room(room)
	return &chatui.Chatattach001Composer{Files: append([]chatui.Chatattach001Draft(nil), r.files...), Error: r.error, Sending: r.sending, Choose: s.choose,
		Remove: func(key string) {
			if s.remove != nil {
				s.remove(room, key)
			}
		},
		Send: func(body string, refs []chatui.ChatReference) {
			if s.send != nil {
				s.send(room, body, refs)
			}
		}}
}

func (s *chatattach001State) begin(room, tenant, body string, mentions []chatui.ChatReference) ([]*chatv1.Reference, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.room(room)
	if r.sending || len(r.files) == 0 || len(r.files) > 10 || tenant == "" || strings.TrimSpace(body) == "" {
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
