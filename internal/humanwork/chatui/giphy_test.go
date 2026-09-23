package chatui

import (
	"strings"
	"testing"
)

func TestGiphyIDAndCanonicalURLValidation(t *testing.T) {
	if GiphyConfigured(GiphyPickerConfig{}) || GiphyConfigured(GiphyPickerConfig{APIKey: " \t"}) {
		t.Fatal("empty or whitespace GIPHY key enabled the picker")
	}
	if !GiphyConfigured(GiphyPickerConfig{APIKey: "configured-key"}) {
		t.Fatal("configured GIPHY key did not enable the picker")
	}
	valid := []string{"abc123", "YsTs5ltWtEhnq"}
	for _, id := range valid {
		if !ValidGiphyID(id) {
			t.Errorf("ValidGiphyID(%q) = false", id)
		}
	}
	for _, id := range []string{"", "bad/id", "bad id", "x?y", strings.Repeat("a", 129)} {
		if ValidGiphyID(id) {
			t.Errorf("ValidGiphyID(%q) = true", id)
		}
	}

	for _, tc := range []struct {
		url, wantID string
	}{
		{"https://giphy.com/gifs/confused-flying-YsTs5ltWtEhnq", "YsTs5ltWtEhnq"},
		{"https://www.giphy.com/gifs/abc123", "abc123"},
	} {
		gotURL, gotID, ok := CanonicalGiphyURL(tc.url)
		if !ok || gotURL != tc.url || gotID != tc.wantID {
			t.Errorf("CanonicalGiphyURL(%q) = (%q, %q, %v)", tc.url, gotURL, gotID, ok)
		}
	}
	for _, raw := range []string{
		"http://giphy.com/gifs/foo-abc123",
		"https://evil.example/gifs/foo-abc123",
		"https://giphy.com.evil.example/gifs/foo-abc123",
		"https://user@giphy.com/gifs/foo-abc123",
		"https://giphy.com:443/gifs/foo-abc123",
		"https://giphy.com/gifs/foo-abc123?tracking=1",
		"https://giphy.com/gifs/foo-abc123#fragment",
		"https://giphy.com/embed/abc123",
		"https://giphy.com/gifs/foo-not_an_id",
	} {
		if gotURL, gotID, ok := CanonicalGiphyURL(raw); ok {
			t.Errorf("CanonicalGiphyURL(%q) accepted as (%q, %q)", raw, gotURL, gotID)
		}
	}
}

func TestGiphyMediaURLAllowlist(t *testing.T) {
	for _, raw := range []string{
		"https://media.giphy.com/media/abc/giphy.gif",
		"https://media1.giphy.com/media/abc/100w.gif",
	} {
		if !validGiphyMediaURL(raw) {
			t.Errorf("validGiphyMediaURL(%q) = false", raw)
		}
	}
	for _, raw := range []string{
		"http://media.giphy.com/media/abc.gif",
		"https://media.giphy.com.evil.example/media/abc.gif",
		"https://evil.example/media/abc.gif",
		"https://user@media.giphy.com/media/abc.gif",
		"https://media.giphy.com:443/media/abc.gif",
		"https://mediabad.giphy.com/media/abc.gif",
	} {
		if validGiphyMediaURL(raw) {
			t.Errorf("validGiphyMediaURL(%q) = true", raw)
		}
	}
}
