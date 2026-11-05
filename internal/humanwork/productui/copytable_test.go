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
		"en-US": {workflowEditorMessages(), "24fefc61562bf7a77ddc6265b66d5d9a0448fbd0662bb40b6b23cb78930368ee"},
		"de-DE": {workflowEditorTranslations("de-DE"), "f64f725883445eb4a3a3dc6ecc1ebb34f57fb3356e62457113172ae7fe2b59cf"},
		"ar":    {workflowEditorTranslations("ar"), "77aac46d1073919a2070eebb8ef39d37fdeb3ad65d799020614fac728afb5b13"},
	} {
		if len(tc.messages) != 177 {
			t.Fatalf("%s carries %d keys, want 177", name, len(tc.messages))
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
