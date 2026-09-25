package projectactivity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	domain "github.com/monstercameron/human-capital-management-suite/internal/domains/projectactivity"
)

func TestPlainTextSanitizer_PreservesBoundedFormattingAndMentions(t *testing.T) {
	got, err := NewPlainTextSanitizer().Sanitize(context.Background(), "<p><strong>Hello</strong> @Member-1</p><p>next <code>x &amp; y</code></p>")
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "<p><strong>Hello</strong> @Member-1</p><p>next <code>x &amp; y</code></p>" {
		t.Fatalf("source = %q", got.Source)
	}
	if got.HTML != "<p><strong>Hello</strong> @Member-1</p><p>next <code>x &amp; y</code></p>" {
		t.Fatalf("safe preview = %q", got.HTML)
	}
	if len(got.MentionHandles) != 1 || got.MentionHandles[0] != "member-1" {
		t.Fatalf("mention handles = %#v", got.MentionHandles)
	}
}

func TestPlainTextSanitizer_XSSAndUnsupportedMarkup(t *testing.T) {
	sanitizer := NewPlainTextSanitizer()
	input := `<script>alert(1)</script><img src=x onerror="alert(2)"><strong onclick="alert(3)">safe</strong><a href="javascript:alert(4)">bad link</a><a href="https://example.org/path" target="_blank" rel="opener">good link</a><!--hidden-->`
	got, err := sanitizer.Sanitize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"<script", "alert(", "<img", "onclick", "javascript:", "target=", "rel=", "hidden"} {
		if strings.Contains(strings.ToLower(got.HTML), forbidden) {
			t.Fatalf("unsafe/unsupported %q in %q", forbidden, got.HTML)
		}
	}
	if !strings.Contains(got.HTML, "<strong>safe</strong>") || !strings.Contains(got.HTML, `<a href="https://example.org/path">good link</a>`) || !strings.Contains(got.HTML, "bad link") {
		t.Fatalf("approved formatting/link lost: %q", got.HTML)
	}
}

func TestPlainTextSanitizer_RejectsControlsAndBoundsInputAndOutput(t *testing.T) {
	sanitizer := NewPlainTextSanitizer()
	for i, input := range []string{"control\x00byte", "delete\x7f", "c1\u0085", "invalid-utf8-\xff", "  \n  "} {
		t.Run(fmt.Sprintf("case-%d", i), func(t *testing.T) {
			if _, err := sanitizer.Sanitize(context.Background(), input); !errors.Is(err, ErrInvalidPlainText) {
				t.Fatalf("Sanitize(%q) error = %v", input, err)
			}
		})
	}
	if _, err := sanitizer.Sanitize(context.Background(), strings.Repeat("x", domain.MaxTextBytes+1)); !errors.Is(err, ErrInvalidPlainText) {
		t.Fatalf("oversize source error = %v", err)
	}
	if _, err := sanitizer.Sanitize(context.Background(), strings.Repeat("&", domain.MaxTextBytes/2)); !errors.Is(err, ErrInvalidPlainText) {
		t.Fatalf("oversize rendered preview error = %v", err)
	}
}

func FuzzTodo_PM_024(f *testing.F) {
	for _, input := range []string{"plain text", "<b>safe</b>", "<script>bad</script>", `<a href="javascript:x">x</a>`, "@member-1"} {
		f.Add(input)
	}
	s := NewPlainTextSanitizer()
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > domain.MaxTextBytes {
			t.Skip()
		}
		got, err := s.Sanitize(context.Background(), input)
		if err == nil {
			if len(got.HTML) > domain.MaxTextBytes || strings.Contains(strings.ToLower(got.HTML), "<script") || strings.Contains(strings.ToLower(got.HTML), "javascript:") {
				t.Fatalf("unsafe sanitizer output %q", got.HTML)
			}
			if len(got.MentionHandles) > 32 {
				t.Fatalf("too many mentions: %d", len(got.MentionHandles))
			}
		}
	})
}
