package chatui

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

// TestTodo_CHATBUG_014_ReferencePatterns: the reference searches are skipped for
// text that cannot match, and for no other text. On every text the finders
// answer exactly what the expression answers, and a plain message runs none of
// the expressions.
func TestTodo_CHATBUG_014_ReferencePatterns(t *testing.T) {
	patterns := map[string]*chatperf2Pattern{
		"docToken": docTokenPattern, "docURL": docURLPattern, "journeyURL": journeyURLPattern,
		"projectTaskToken": projectTaskTokenPattern, "projectTaskURL": projectTaskURLPattern,
		"shareURL": shareURLPattern, "channelURL": channelURLPattern, "address": chatbug053Address, "giphy": giphyLinkPattern,
	}
	same := func(name, body string) {
		t.Helper()
		pattern := patterns[name]
		for _, n := range []int{-1, 1, 16} {
			if got, want := pattern.FindAllStringIndex(body, n), pattern.Regexp.FindAllStringIndex(body, n); !reflect.DeepEqual(got, want) {
				t.Fatalf("%s.FindAllStringIndex(%.60q, %d) = %v, the expression says %v", name, body, n, got, want)
			}
			if got, want := pattern.FindAllString(body, n), pattern.Regexp.FindAllString(body, n); !reflect.DeepEqual(got, want) {
				t.Fatalf("%s.FindAllString(%.60q, %d) = %v, the expression says %v", name, body, n, got, want)
			}
		}
	}

	// Text with references of every kind, and without any.
	bodies := []string{
		"", "Lunch at noon?", "see doc:handbook-2026 and task:PRJ-12, then https://cell.test/workspace/app/docs?document=d1.",
		"/workspace/app/journeys?journey=j1 and /workspace/app/project?task=t1 and /workspace/app/docs?document=d2",
		"shared: /workspace/app/chat#share=abc_DEF-1 or /chat/share/tok or #msg=legacy1 or http://other.test/x",
		"#general is at /workspace/app/chat#channel=general; a gif https://giphy.com/gifs/cat-123 and https://www.giphy.com/gifs/dog",
		strings.Repeat("The quarterly numbers are in, and nothing here is a link. ", 200),
		"docs: doc: task: http:/ https:// #msg= /chat/share/",
	}
	matched := map[string]bool{}
	for name, pattern := range patterns {
		if len(pattern.needles) == 0 {
			t.Fatalf("%s is searched for in every text", name)
		}
		for _, body := range bodies {
			same(name, body)
			matched[name] = matched[name] || pattern.Regexp.MatchString(body)
		}
		if !matched[name] {
			t.Fatalf("no text here matches %s, so nothing shows it is still found", name)
		}
	}

	// Text assembled at random from the pieces the expressions are made of. A
	// search skipped for a text the expression matches fails here.
	pieces := []string{
		"http", "s", "://", "www.", "cell.test", "giphy.com", "/gifs/", "/workspace/app/", "docs?", "journeys?", "project?", "chat#share=", "chat#channel=",
		"/chat/share/", "#msg=", "doc:", "task:", "document=d1", "abc_DEF-1", " ", "\n", ".", ",", "\"", "<", ">", "é", "plain words",
	}
	random := rand.New(rand.NewSource(20261002))
	for i := 0; i < 4000; i++ {
		var body strings.Builder
		for n := 1 + random.Intn(7); n > 0; n-- {
			body.WriteString(pieces[random.Intn(len(pieces))])
		}
		for name := range patterns {
			same(name, body.String())
		}
	}

	// A plain message is not searched at all: none of the expressions runs.
	plain := "Lunch at noon? I will bring the slides for the 3 pm review."
	for name, pattern := range patterns {
		if pattern.possible(plain) {
			t.Fatalf("%s is still run on a plain message", name)
		}
	}
	// The references are still found where there are some.
	if refs := DocReferences("see doc:handbook-2026", "https://cell.test"); len(refs) != 1 || refs[0].ID != "handbook-2026" {
		t.Fatalf("document references = %+v", refs)
	}
	if locators := ShareLocators("look: https://cell.test/chat/share/tok123", "https://cell.test"); len(locators) != 1 || locators[0].Token != "tok123" {
		t.Fatalf("share locators = %+v", locators)
	}
	if refs := ProjectTaskReferences("task:PRJ-12 is late", "https://cell.test"); len(refs) != 1 || refs[0].TaskID != "PRJ-12" {
		t.Fatalf("project task references = %+v", refs)
	}
}
