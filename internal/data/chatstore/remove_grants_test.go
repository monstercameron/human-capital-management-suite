package chatstore

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every table the store deletes from must be granted DELETE to the serving
// roles by a migration. A cell with separated roles otherwise answers 503 for
// the action (seen for the saved list, then found for reactions and pins).
func TestStoreDeletesAreGrantedToServingRoles(t *testing.T) {
	granted := map[string]bool{}
	for _, name := range []string{"00028_chatsave_remove_grant.sql", "00030_chat_runtime_remove_grants.sql", "00040_chatcmd002_card_votes.sql"} {
		raw, err := os.ReadFile(filepath.Join("migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		for _, table := range regexp.MustCompile(`chat_[a-z_]+`).FindAllString(string(raw), -1) {
			granted[table] = true
		}
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`DELETE FROM (chat_[a-z_]+)`)
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range pattern.FindAllStringSubmatch(string(raw), -1) {
			if !granted[match[1]] {
				t.Errorf("%s deletes from %s, which no migration grants DELETE on to the serving roles", file, match[1])
			}
		}
	}
}
