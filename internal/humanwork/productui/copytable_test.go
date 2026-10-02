package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/localize"
)

// copyTableDigest is a canonical sha256 over a message map: every key in
// order with its text and its plural and gender forms in order.
func copyTableDigest(messages map[string]localize.Message) string {
	keys := make([]string, 0, len(messages))
	for key := range messages {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	hash := sha256.New()
	forms := func(label string, values map[string]string) {
		names := make([]string, 0, len(values))
		for name := range values {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Fprintf(hash, " %s%q=%q", label, name, values[name])
		}
	}
	for _, key := range keys {
		message := messages[key]
		fmt.Fprintf(hash, "K%q T%q", key, message.Text)
		forms("P", message.Plural)
		forms("G", message.Gender)
		hash.Write([]byte{'\n'})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func TestParseCopyTable(t *testing.T) {
	got := parseCopyTable("\nplain.key = Hello = world\nspaced.key = \" and \"\nquoted.broken = \"oops\ncount#one = {count} thing\ncount#other = {count} things\nnot an entry\n")
	want := map[string]localize.Message{
		"plain.key":     {Text: "Hello = world"},
		"spaced.key":    {Text: " and "},
		"quoted.broken": {Text: `"oops`},
		"count":         {Plural: map[string]string{"one": "{count} thing", "other": "{count} things"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseCopyTable = %#v, want %#v", got, want)
	}
	if empty := parseCopyTable(""); len(empty) != 0 {
		t.Fatalf("empty table parsed to %d entries", len(empty))
	}
}

// TestWorkflowEditorCopyDigests pins the draft editor's copy to the exact
// content it had when it was a map literal. The digests were taken from the
// literal's output, so a parse or edit mistake in the embedded tables changes
// what an author sees and fails here. A deliberate copy change updates both
// the table and the digest in one commit.
func TestWorkflowEditorCopyDigests(t *testing.T) {
	for name, tc := range map[string]struct {
		messages map[string]localize.Message
		digest   string
	}{
		"en-US": {workflowEditorMessages(), "d2caec77a0b6602decd4a795edd1988c0022fc5f9f1ea3640925ad4c7c22276d"},
		"de-DE": {workflowEditorTranslations("de-DE"), "553efa654a3f71ae2ef7eec98784155afe04a06bd53b94719e70a761ffc547fa"},
		"ar":    {workflowEditorTranslations("ar"), "ae1fcd8fbcba72ecf7465542cc6a28abdc6cb9be0c619152a5fac61ae8170bba"},
	} {
		if len(tc.messages) != 176 {
			t.Fatalf("%s carries %d keys, want 176", name, len(tc.messages))
		}
		if got := copyTableDigest(tc.messages); got != tc.digest {
			t.Fatalf("%s copy digest = %s, want %s", name, got, tc.digest)
		}
	}
	if workflowEditorTranslations("fr-FR") != nil {
		t.Fatal("an unknown locale must have no translations")
	}
	for _, table := range []string{workflowEditorEnglishCopy, workflowEditorGermanCopy, workflowEditorArabicCopy} {
		for _, line := range strings.Split(table, "\n") {
			if line != "" && !strings.Contains(line, " = ") {
				t.Fatalf("copy table line has no ' = ' separator: %q", line)
			}
		}
	}
}
