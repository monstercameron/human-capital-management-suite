package chatui

import (
	"reflect"
	"testing"
)

func TestResolveGiphyPostEmbedsMatchesValidatedIDsAndKeepsLinks(t *testing.T) {
	body := "See https://giphy.com/gifs/wave-abc123, then https://www.giphy.com/gifs/abc123 and https://giphy.com/gifs/no-data-xzy999."
	results := []GiphyResult{
		{ID: "abc123", URL: "https://giphy.com/gifs/wave-abc123", EmbedURL: "https://media.giphy.com/media/abc123/giphy.gif?cid=test", Alt: "wave", Rating: GiphyRating},
		// An API result cannot authorize a different ID embedded in its URL.
		{ID: "xzy999", URL: "https://giphy.com/gifs/someone-else-abc123", EmbedURL: "https://media.giphy.com/media/xzy999/giphy.gif", Rating: GiphyRating},
	}
	got := ResolveGiphyPostEmbeds(body, results)
	want := []GiphyPostEmbed{{
		ID: "abc123", PageURL: "https://giphy.com/gifs/wave-abc123",
		MediaURL: "https://media.giphy.com/media/abc123/giphy.gif?cid=test", Alt: "wave",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveGiphyPostEmbeds() = %#v, want %#v", got, want)
	}
}

func TestResolveGiphyPostEmbedsRejectsInvalidRatingAndMedia(t *testing.T) {
	body := "https://giphy.com/gifs/one-abc123 https://giphy.com/gifs/two-def456 https://giphy.com/gifs/three-ghi789"
	results := []GiphyResult{
		{ID: "abc123", URL: "https://giphy.com/gifs/one-abc123", EmbedURL: "https://media.giphy.com/media/abc123/giphy.gif", Rating: "pg"},
		{ID: "def456", URL: "https://giphy.com/gifs/two-def456", EmbedURL: "https://media.giphy.com/media/def456/giphy.webp", Rating: GiphyRating},
		{ID: "ghi789", URL: "https://giphy.com/gifs/three-ghi789", EmbedURL: "https://evil.example/ghi789.gif", Rating: GiphyRating},
	}
	if got := ResolveGiphyPostEmbeds(body, results); len(got) != 0 {
		t.Fatalf("ResolveGiphyPostEmbeds() accepted untrusted results: %#v", got)
	}
}

func TestResolveGiphyPostEmbedsPreservesAPIResultOrderForDistinctLinks(t *testing.T) {
	body := "https://giphy.com/gifs/second-def456 then https://giphy.com/gifs/first-abc123"
	results := []GiphyResult{
		{ID: "abc123", URL: "https://giphy.com/gifs/first-abc123", EmbedURL: "https://media1.giphy.com/media/abc123/giphy.gif", Rating: GiphyRating},
		{ID: "def456", URL: "https://giphy.com/gifs/second-def456", EmbedURL: "https://media2.giphy.com/media/def456/giphy.gif", Rating: GiphyRating},
	}
	got := ResolveGiphyPostEmbeds(body, results)
	if len(got) != 2 || got[0].ID != "def456" || got[1].ID != "abc123" {
		t.Fatalf("ResolveGiphyPostEmbeds() = %#v, expected body order", got)
	}
}
