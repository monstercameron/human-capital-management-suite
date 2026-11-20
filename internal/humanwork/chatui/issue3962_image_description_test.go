package chatui_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func issue3962Draft(alt string, decorative bool) chatui.Chatattach001Draft {
	return chatui.Chatattach001Draft{Key: "draft-1", Attachment: chatui.Attachment{ID: "art-1", Name: "chart.png", ContentType: "image/png", Bytes: 2048, URL: "blob:chart", AltText: alt, Decorative: decorative}}
}

// TestIssue3962_ComposerOffersDescriptionForImagesOnly proves an attached image
// gets a labelled description field and a decorative choice in all three
// languages, a document does not, and the filename stays its own text.
func TestIssue3962_ComposerOffersDescriptionForImagesOnly(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			m := chatattach001Model(locale)
			m.Chatattach001.Files = []chatui.Chatattach001Draft{issue3962Draft("Bar chart of hires", false), {Key: "doc", Attachment: chatui.Attachment{Name: "policy.pdf", ContentType: "application/pdf", Bytes: 10}}}
			composer, _ := chatattach001Markup(t, m)
			if strings.ContainsAny(composer, "⟦⟧") {
				t.Fatalf("prints a copy key: %s", composer)
			}
			for _, want := range []string{
				`data-chatattach001-alt="draft-1"`, `data-chatattach001-decorative="draft-1"`, `maxLength="1000"`,
				`value="Bar chart of hires"`, chatui.Issue3962Text(locale, "label"), chatui.Issue3962Text(locale, "decorative"), chatui.Issue3962Text(locale, "hint"), "chart.png",
			} {
				if !strings.Contains(composer, want) {
					t.Fatalf("composer misses %q: %s", want, composer)
				}
			}
			if strings.Count(composer, "data-chatattach001-alt=") != 1 {
				t.Fatalf("a document got a description field: %s", composer)
			}
			for _, key := range []string{"label", "decorative", "hint", "open", "description"} {
				for _, other := range []string{"en-US", "de-DE", "ar"} {
					if chatui.Issue3962Text(locale, key) == "" || chatui.Issue3962Text(locale, key) == key {
						t.Fatalf("%s %s has no copy", locale, key)
					}
					_ = other
				}
			}
		})
	}
}

// TestIssue3962_DecorativeDisablesTheDescription proves the decorative choice is
// a checked box that takes the description field out of use, and that no
// agent suggestion is offered when no image service exists.
func TestIssue3962_DecorativeDisablesTheDescription(t *testing.T) {
	m := chatattach001Model("en-US")
	m.Chatattach001.Files = []chatui.Chatattach001Draft{issue3962Draft("kept words", true)}
	composer, _ := chatattach001Markup(t, m)
	if !strings.Contains(composer, `data-chatattach001-decorative="draft-1"`) || !strings.Contains(composer, "checked") {
		t.Fatalf("decorative box is not checked: %s", composer)
	}
	field := composer[strings.Index(composer, `data-chatattach001-alt=`):]
	field = field[:strings.Index(field, ">")]
	if !strings.Contains(field, "disabled") {
		t.Fatalf("description stays editable while decorative: %s", field)
	}
	if strings.Contains(composer, "Suggest description") {
		t.Fatalf("offers a suggestion with no image service: %s", composer)
	}
}

func issue3962Page(t *testing.T, locale string, a chatui.Attachment) string {
	t.Helper()
	m := chatattach001Model(locale)
	a.ID, a.ContentType, a.Bytes = "art-1", "image/png", 4096
	m.Messages[0].Attachments = []chatui.Attachment{a}
	_, page := chatattach001Markup(t, m)
	return page
}

// TestIssue3962_MessageImageIsNamedByItsDescription proves the conversation
// names the image by what its author wrote, keeps the filename available
// separately, and falls back to the filename only when nothing was written.
func TestIssue3962_MessageImageIsNamedByItsDescription(t *testing.T) {
	page := issue3962Page(t, "en-US", chatui.Attachment{Name: "IMG_4021.png", AltText: "Org chart with three teams"})
	if !strings.Contains(page, `alt="Org chart with three teams"`) || !strings.Contains(page, `Open image: Org chart with three teams`) {
		t.Fatalf("image is not named by its description: %s", page)
	}
	if !strings.Contains(page, `data-media-name="IMG_4021.png"`) || !strings.Contains(page, `data-media-alt="Org chart with three teams"`) {
		t.Fatalf("viewer data lost the name or the description: %s", page)
	}
	if strings.Contains(page, `alt="IMG_4021.png"`) {
		t.Fatalf("filename still stands in as the description: %s", page)
	}
	plain := issue3962Page(t, "en-US", chatui.Attachment{Name: "IMG_4021.png"})
	if !strings.Contains(plain, `alt="IMG_4021.png"`) || strings.Contains(plain, "data-media-alt=") {
		t.Fatalf("undescribed image changed: %s", plain)
	}
}

// TestIssue3962_DecorativeImageIsHiddenFromReaders proves a decorative image has
// an empty alternative, not its filename.
func TestIssue3962_DecorativeImageIsHiddenFromReaders(t *testing.T) {
	page := issue3962Page(t, "en-US", chatui.Attachment{Name: "divider.png", Decorative: true})
	if strings.Contains(page, `alt="divider.png"`) || !strings.Contains(page, `data-media-decorative="true"`) {
		t.Fatalf("decorative image reads its filename: %s", page)
	}
	if !strings.Contains(page, `alt=""`) {
		t.Fatalf("decorative image has no empty alt: %s", page)
	}
}

// TestIssue3962_DescriptionIsEscaped proves author text cannot add markup.
func TestIssue3962_DescriptionIsEscaped(t *testing.T) {
	m := chatattach001Model("en-US")
	m.Messages[0].Attachments = []chatui.Attachment{{ID: "art-1", Name: "x.png", ContentType: "image/png", AltText: `"><script>alert(1)</script>`}}
	raw, err := ui.RenderToString(chatui.Build(m))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "<script>alert(1)</script>") {
		t.Fatalf("description injected markup")
	}
}
