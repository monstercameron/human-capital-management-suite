package chatui

import (
	"strings"
	"testing"
)

// TestTodo_CHAT_057 is the CHAT-057 PRIMARY matrix test. It proves the
// "/giphy <query>" composer command is parsed (and only consumed, never
// posted), that the picker stays gated behind a configured API key, and that
// resolving a post's embeds only turns a canonical GIPHY link into an embed
// when a validated API result actually backs it; an unmatched link is simply
// left as whatever ordinary link it already was.
func TestTodo_CHAT_057(t *testing.T) {
	for _, tc := range []struct {
		name, body, wantQuery string
		wantOK                bool
	}{
		{name: "bare command opens trending", body: "/giphy", wantQuery: "", wantOK: true},
		{name: "query text", body: "/giphy  confetti party  ", wantQuery: "confetti party", wantOK: true},
		{name: "case insensitive", body: "/GIPHY cats", wantQuery: "cats", wantOK: true},
		{name: "not a command", body: "look at this /giphycat", wantOK: false},
		{name: "unrelated slash command", body: "/giphyx cats", wantOK: false},
		{name: "plain text", body: "just chatting", wantOK: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query, ok := giphyCommand(tc.body)
			if ok != tc.wantOK {
				t.Fatalf("giphyCommand(%q) ok = %v, want %v", tc.body, ok, tc.wantOK)
			}
			if ok && query != tc.wantQuery {
				t.Fatalf("giphyCommand(%q) query = %q, want %q", tc.body, query, tc.wantQuery)
			}
		})
	}

	if GiphyConfigured(GiphyPickerConfig{}) {
		t.Fatal("the picker must stay unavailable without a configured GIPHY API key")
	}
	if !GiphyConfigured(GiphyPickerConfig{APIKey: "server-issued-key"}) {
		t.Fatal("a configured GIPHY API key did not enable the picker")
	}

	body := "check this out https://giphy.com/gifs/party-abc123 and also https://giphy.com/gifs/unmatched-zzz999"
	results := []GiphyResult{
		{ID: "abc123", URL: "https://giphy.com/gifs/party-abc123", EmbedURL: "https://media.giphy.com/media/abc123/giphy.gif", Alt: "party", Rating: GiphyRating},
	}
	embeds := ResolveGiphyPostEmbeds(body, results)
	if len(embeds) != 1 || embeds[0].ID != "abc123" || embeds[0].MediaURL != "https://media.giphy.com/media/abc123/giphy.gif" {
		t.Fatalf("ResolveGiphyPostEmbeds() = %#v, want exactly the validated abc123 embed", embeds)
	}
	// The second link has no backing API result: it stays a plain link, not an
	// embed conjured from the URL alone.
	for _, e := range embeds {
		if e.ID == "zzz999" {
			t.Fatal("an unmatched GIPHY link was rendered as an embed without a validated API result")
		}
	}
}

// TestTodo_CHAT_057_Security is the CHAT-057 SECURITY matrix test. It proves
// that a GIF only ever becomes a post embed when its ID, canonical page
// link, media host and rating are all independently validated: an ID
// embedded in one link cannot borrow a different result's authorization, a
// disallowed rating or a non-GIPHY (or non-animated-GIF) media host is
// refused, and the picker's own ID/URL validators reject the same class of
// spoofed input a caller could try to smuggle through the composer.
func TestTodo_CHAT_057_Security(t *testing.T) {
	for _, id := range []string{"", "../etc/passwd", "abc 123", "abc/123", "javascript:alert(1)"} {
		if ValidGiphyID(id) {
			t.Errorf("ValidGiphyID(%q) = true, want a rejected opaque ID", id)
		}
	}
	for _, raw := range []string{
		"javascript:alert(1)//giphy.com/gifs/x-abc123",
		"http://giphy.com/gifs/x-abc123",
		"https://giphy.com.evil.example/gifs/x-abc123",
		"https://evil.example/gifs/x-abc123",
		"https://user@giphy.com/gifs/x-abc123",
		"https://giphy.com/gifs/x-abc123?redirect=evil.example",
		"https://giphy.com/gifs/x-abc123#https://evil.example",
	} {
		if _, _, ok := CanonicalGiphyURL(raw); ok {
			t.Errorf("CanonicalGiphyURL(%q) accepted a spoofed GIPHY link", raw)
		}
	}
	for _, raw := range []string{
		"http://media.giphy.com/media/x/giphy.gif",
		"https://media.giphy.com.evil.example/media/x/giphy.gif",
		"https://notmedia.giphy.com/media/x/giphy.gif",
		"https://media.giphy.com:8443/media/x/giphy.gif",
	} {
		if validGiphyMediaURL(raw) {
			t.Errorf("validGiphyMediaURL(%q) accepted a spoofed media host", raw)
		}
	}

	body := "https://giphy.com/gifs/one-abc123 https://giphy.com/gifs/two-def456"
	results := []GiphyResult{
		// abc123's own page link points at a different ID; the mismatch must be
		// rejected rather than trusted because the surrounding text matched.
		{ID: "abc123", URL: "https://giphy.com/gifs/one-def456", EmbedURL: "https://media.giphy.com/media/abc123/giphy.gif", Rating: GiphyRating},
		// def456 carries a disallowed rating.
		{ID: "def456", URL: "https://giphy.com/gifs/two-def456", EmbedURL: "https://media.giphy.com/media/def456/giphy.gif", Rating: "r"},
	}
	if got := ResolveGiphyPostEmbeds(body, results); len(got) != 0 {
		t.Fatalf("ResolveGiphyPostEmbeds() accepted an ID/URL mismatch or a disallowed rating: %#v", got)
	}

	// A media URL that resolves to something other than an animated GIF
	// rendition (still frame, video, or an off-host redirect) is refused even
	// when the ID, canonical URL and rating all validate.
	stillFrame := []GiphyResult{{ID: "abc123", URL: "https://giphy.com/gifs/one-abc123", EmbedURL: "https://media.giphy.com/media/abc123/giphy_s.webp", Rating: GiphyRating}}
	if got := ResolveGiphyPostEmbeds("https://giphy.com/gifs/one-abc123", stillFrame); len(got) != 0 {
		t.Fatalf("ResolveGiphyPostEmbeds() accepted a non-GIF media rendition: %#v", got)
	}
}

// TestTodo_CHAT_057_Accessibility is the CHAT-057 ACCESSIBILITY matrix test.
// It proves the picker's no-key, loading, error and empty-result states all
// render with localized accessible names, and that a configured picker's
// trigger is reachable rather than disabled.
func TestTodo_CHAT_057_Accessibility(t *testing.T) {
	localized := Model{State: StateReady, SelectedID: "room", Conversations: []Conversation{{ID: "room", Name: "People"}}, Text: func(key string) string {
		switch key {
		case KeyGiphyUnavailable:
			return "Les GIF nécessitent une clé administrateur"
		case KeyGiphyLoading:
			return "Chargement des GIF"
		case KeyGiphyLoadError:
			return "Erreur de chargement des GIF"
		case KeyGiphyNoResults:
			return "Aucun GIF trouvé"
		case KeyGiphyClose:
			return "Fermer les GIF"
		case KeyGiphySearch:
			return "Rechercher des GIF"
		default:
			return key
		}
	}}
	markup := render(t, localized)
	for _, want := range []string{
		`aria-label="Les GIF nécessitent une clé administrateur"`, "Les GIF nécessitent une clé administrateur",
		`data-loading="Chargement des GIF"`,
		`data-load-error="Erreur de chargement des GIF"`,
		`data-no-results="Aucun GIF trouvé"`,
		`data-close="Fermer les GIF"`,
		`role="dialog"`, `role="status"`,
		`Rechercher des GIF`, `Powered by GIPHY`, `maxLength="50"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("GIPHY picker accessibility markup is missing %q", want)
		}
	}
	if !strings.Contains(markup, `data-action="giphy-toggle"`) || !strings.Contains(markup, "disabled") {
		t.Fatal("an unconfigured picker's trigger must be disabled, not silently broken")
	}

	configured := Model{State: StateReady, SelectedID: "room", GiphyAPIKey: "public-key", Conversations: []Conversation{{ID: "room", Name: "People"}}}
	enabledMarkup := render(t, configured)
	start := strings.Index(enabledMarkup, `data-action="giphy-toggle"`)
	if start < 0 {
		t.Fatal("configured GIPHY picker trigger was not rendered")
	}
	end := strings.Index(enabledMarkup[start:], ">")
	if end < 0 || strings.Contains(enabledMarkup[start:start+end], "disabled") {
		t.Fatalf("configured GIPHY trigger is disabled: %s", enabledMarkup[start:start+end])
	}
}
