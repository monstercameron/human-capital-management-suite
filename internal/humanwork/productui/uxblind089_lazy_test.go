package productui

import (
	"sync"
	"testing"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// TestTodo_UXBLIND_089_LazyInit pins the init half of the cold-load fix: copy
// tables for pages other than the loaded one, and the Docs Markdown parser,
// are built on first use rather than at package init. A cold Home load never
// touches Docs, Agents admin or the access preview, so building their tables
// (and the goldmark parser) before go-main is pure start-up cost.
func TestTodo_UXBLIND_089_LazyInit(t *testing.T) {
	t.Run("value builds on first use", func(t *testing.T) {
		calls := 0
		value := newLazyValue(func() string {
			calls++
			return "built"
		})
		if calls != 0 {
			t.Fatalf("lazy value built %d times before first use, want 0", calls)
		}
		if got := value.Get(); got != "built" {
			t.Fatalf("Get() = %q, want %q", got, "built")
		}
		if calls != 1 {
			t.Fatalf("lazy value built %d times after one Get, want 1", calls)
		}
		if got := value.Get(); got != "built" || calls != 1 {
			t.Fatalf("second Get() = %q after %d builds, want one build", got, calls)
		}
	})

	t.Run("value builds once under concurrency", func(t *testing.T) {
		var calls int
		var mu sync.Mutex
		value := newLazyValue(func() string {
			mu.Lock()
			calls++
			mu.Unlock()
			return "built"
		})
		var wait sync.WaitGroup
		for range 16 {
			wait.Add(1)
			go func() {
				defer wait.Done()
				if got := value.Get(); got != "built" {
					t.Errorf("Get() = %q, want %q", got, "built")
				}
			}()
		}
		wait.Wait()
		if calls != 1 {
			t.Fatalf("concurrent first use built %d times, want 1", calls)
		}
	})

	t.Run("markdown parser builds on first parse", func(t *testing.T) {
		fresh := &lazyMarkdownParser{inner: newLazyValue(buildDocsMarkdownParser)}
		if fresh.inner.value != nil {
			t.Fatal("a new lazy parser already built its goldmark parser")
		}
		root := fresh.Parse(text.NewReader([]byte("# Title\n")))
		if fresh.inner.value == nil {
			t.Fatal("first Parse did not build the goldmark parser")
		}
		heading, ok := root.FirstChild().(*ast.Heading)
		if !ok || heading.Level != 1 {
			t.Fatalf("lazy Parse did not read a level-1 heading: %#v", root.FirstChild())
		}
	})

	t.Run("tables build complete copies", func(t *testing.T) {
		nested := []struct {
			name        string
			build       func() map[string]map[string]string
			locale, key string
			want        string
		}{
			{"docs editor", buildDocsEditorCopy, "en-US", "editor_label", "Document editor"},
			{"docs editor de", buildDocsEditorCopy, "de-DE", "editor_label", "Dokumenteditor"},
			{"docs chat", buildDocsChatCopy, "en-US", "open_channel", "Open channel #{name}, {members}"},
			{"agent admin", buildAgentAdminCopy, "en", "title", "Agent connections and grants"},
			{"agent access", buildAgentAccessCopy, "en", "title", "My agent access"},
			{"persona mention", buildPersonaMentionCopy, "en", "menu", "Mention someone"},
			{"access preview", buildRoleAccessPreviewCopy, "en", "title", "Access preview"},
		}
		for _, tc := range nested {
			messages := tc.build()
			if len(messages) != 3 {
				t.Errorf("%s holds %d locales, want 3", tc.name, len(messages))
			}
			if got := messages[tc.locale][tc.key]; got != tc.want {
				t.Errorf("%s %s %s = %q, want %q", tc.name, tc.locale, tc.key, got, tc.want)
			}
		}
		triples := []struct {
			name  string
			build func() map[string][3]string
			key   string
			want  [3]string
		}{
			{"loading", buildAgentLoadingCopy, "retry", [3]string{"Try again", "Erneut versuchen", "حاول مجدداً"}},
			{"admin form", buildAgentAdminFormCopy, "new_title", [3]string{"New connection revision", "Neue Verbindungsrevision", "إصدار اتصال جديد"}},
		}
		for _, tc := range triples {
			if got := tc.build()[tc.key]; got != tc.want {
				t.Errorf("%s %s = %q, want %q", tc.name, tc.key, got, tc.want)
			}
		}
	})

	t.Run("package tables answer through their accessors", func(t *testing.T) {
		en := LocaleContext{Resolved: "en-US"}
		if got := docsEditorText("en-US", "editor_label"); got != "Document editor" {
			t.Errorf("docsEditorText = %q", got)
		}
		if got := docsChatText("en-US", "open_channel"); got != "Open channel #{name}, {members}" {
			t.Errorf("docsChatText = %q", got)
		}
		if got := agentAdminText(en, "title"); got != "Agent connections and grants" {
			t.Errorf("agentAdminText = %q", got)
		}
		if got := agentAccessText(en, "title"); got != "My agent access" {
			t.Errorf("agentAccessText = %q", got)
		}
		if got := personaMentionText(en, "menu"); got != "Mention someone" {
			t.Errorf("personaMentionText = %q", got)
		}
		if got := roleAccessPreviewText(en, "title"); got != "Access preview" {
			t.Errorf("roleAccessPreviewText = %q", got)
		}
		if got := AgentLoadingText(en, "retry"); got != "Try again" {
			t.Errorf("AgentLoadingText = %q", got)
		}
		if got := agentAdminFormText(en, "new_title"); got != "New connection revision" {
			t.Errorf("agentAdminFormText = %q", got)
		}
	})
}
