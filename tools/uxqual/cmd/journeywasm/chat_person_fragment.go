package main

import (
	"net/url"
	"strings"
	"sync"
)

// chatPersonFragment remembers the "#person=<subject>" address chat last
// opened, so a re-render or a reload of the same address does not reopen
// the details pane the reader has closed.
type chatPersonFragment struct {
	mu   sync.Mutex
	last string
}

var chatPersonFragments = &chatPersonFragment{}

// chatPersonFragmentSubject reads a person link a document chip wrote:
// "#person=" and a query-escaped subject ID of printable characters.
func chatPersonFragmentSubject(hash string) (string, bool) {
	if !strings.HasPrefix(hash, "#person=") {
		return "", false
	}
	id, err := url.QueryUnescape(strings.TrimPrefix(hash, "#person="))
	if err != nil || id == "" || len(id) > 256 || strings.TrimSpace(id) != id {
		return "", false
	}
	for _, r := range id {
		if r < 0x20 || r == 0x7f || strings.ContainsRune("<>\"'", r) {
			return "", false
		}
	}
	return id, true
}

// claim reports whether key (tenant, viewer and hash) has not been opened
// yet; an empty key forgets the last one.
func (f *chatPersonFragment) claim(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if key == "" {
		f.last = ""
		return false
	}
	if f.last == key {
		return false
	}
	f.last = key
	return true
}
