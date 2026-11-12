package productui

import (
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/uicomponents"
)

// Issue 3678: a portrait that fails to load must fall back to the initials,
// never to the browser's broken-image icon. The server-rendered <img> can fail
// before the client has bound any handler, so the client also sweeps the page
// once it mounts (avatar_broken_3678_wasm.go). Both paths record the address
// here, and a render consults the record, so a failed portrait is neither
// drawn nor requested again this session.
var brokenPortraits struct {
	mu     sync.Mutex
	failed map[string]bool
}

func markPortraitBroken(url string) {
	url = strings.TrimSpace(url)
	if url == "" {
		return
	}
	brokenPortraits.mu.Lock()
	defer brokenPortraits.mu.Unlock()
	if brokenPortraits.failed == nil {
		brokenPortraits.failed = map[string]bool{}
	}
	brokenPortraits.failed[url] = true
}

func portraitBroken(url string) bool {
	brokenPortraits.mu.Lock()
	defer brokenPortraits.mu.Unlock()
	return brokenPortraits.failed[strings.TrimSpace(url)]
}

// viewerInitials is the two-letter label every surface uses: the first letters
// of the first and last name. A one-word name gives its first two letters, so
// the header never shows a single character.
func viewerInitials(name string) string {
	if len(strings.Fields(name)) == 1 {
		word := strings.TrimSpace(name)
		if utf8.RuneCountInString(word) >= 2 {
			return strings.ToUpper(string([]rune(word)[:2]))
		}
	}
	return uicomponents.Initials(name)
}
